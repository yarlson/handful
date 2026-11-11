package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"handful/internal/db"
)

// approvalTimeout ends an approval before Claude Code gives up on the tool
// call: it aborts an MCP call that sends nothing for 300 s, even with
// MCP_TOOL_TIMEOUT set, and a card left after that would take a decision
// that reaches no one.
const approvalTimeout time.Duration = 270e9 // 270 s, in nanoseconds

// Approval decisions a user can make on an approval card.
const (
	DecisionOnce   = "once"
	DecisionAlways = "always"
	DecisionDeny   = "deny"
)

var ErrApprovalClosed = errors.New("approval is no longer pending")

type approvalBody struct {
	Bot      string          `json:"bot,omitempty"` // the asking bot's name, shown in rooms
	Tool     string          `json:"tool"`
	Input    json.RawMessage `json:"input"`
	Status   string          `json:"status"` // pending, allowed, denied, expired
	Decision string          `json:"decision,omitempty"`
}

// approvalWaiters connects a pending approval card to the worker that waits on it.
// Pending approvals live only in memory: when the server stops, the waiting turn stops too.
type approvalWaiters struct {
	mu sync.Mutex
	m  map[string]waiter // approval entry id -> waiter
}

type waiter struct {
	turnID string
	botID  string      // the asking bot; an "always" decision grants the tool to it
	ch     chan string // receives the decision, or "" when the approval expires
}

func newApprovalWaiters() *approvalWaiters {
	return &approvalWaiters{m: make(map[string]waiter)}
}

func (a *approvalWaiters) add(entryID, turnID, botID string) chan string {
	w := waiter{turnID: turnID, botID: botID, ch: make(chan string, 1)}
	a.mu.Lock()
	a.m[entryID] = w
	a.mu.Unlock()
	return w.ch
}

func (a *approvalWaiters) take(entryID string) (waiter, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w, ok := a.m[entryID]
	delete(a.m, entryID)
	return w, ok
}

// expireTurn ends every pending approval of the turn.
func (a *approvalWaiters) expireTurn(turnID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, w := range a.m {
		if w.turnID == turnID {
			delete(a.m, id)
			w.ch <- ""
		}
	}
}

// ApprovalRequest is a bot's request to use a tool. The card goes to
// TranscriptID: the bot's own chat, or the room the bot is working in.
type ApprovalRequest struct {
	BotID        string
	BotName      string
	TranscriptID string
	TurnID       string
	Tool         string
	Input        json.RawMessage
}

// RequestApproval asks the user whether the bot may use a tool. A standing
// grant answers at once; otherwise an approval card is added to the
// transcript and the call waits for a decision, ctx cancellation or the timeout.
func (s *Service) RequestApproval(ctx context.Context, req ApprovalRequest) (bool, string, error) {
	botID, turnID, tool, input := req.BotID, req.TurnID, req.Tool, req.Input
	grants, err := s.ToolGrants(ctx, botID)
	if err != nil {
		return false, "", err
	}
	if slices.Contains(grants, tool) {
		return true, "", nil
	}
	// A call outside any turn, such as a background subagent's after its
	// turn ended, has no turn to wait in and no one to see the card.
	if turnID == "" {
		return false, "No turn is running, so the user cannot be asked; ask again in a turn.", nil
	}
	body := approvalBody{Tool: tool, Input: input, Status: "pending"}
	transcript := req.TranscriptID
	if transcript != botID {
		body.Bot = req.BotName
	}
	// The waiter exists before the card is visible, so that a decision or an
	// expiry that follows the card at once finds it.
	entryID := uuid.NewString()
	ch := s.approvals.add(entryID, turnID, botID)
	entry, err := s.appendEntryWithID(ctx, entryID, transcript, KindApproval, turnID, body)
	if err != nil {
		s.approvals.take(entryID)
		return false, "", err
	}
	timer := time.NewTimer(approvalTimeout)
	defer timer.Stop()

	var decision string
	select {
	case decision = <-ch:
	case <-ctx.Done():
		decision = s.awaitTaken(entry.ID, ch)
	case <-timer.C:
		decision = s.awaitTaken(entry.ID, ch)
	}
	if decision == "" {
		body.Status = "expired"
		// The request context may be done; record the outcome regardless.
		if _, err := s.UpdateEntry(context.WithoutCancel(ctx), entry.ID, body); err != nil {
			return false, "", err
		}
		return false, "The user did not answer the approval request in time.", nil
	}
	return decision != DecisionDeny, "The user denied this action.", nil
}

// awaitTaken ends the wait for an approval that timed out or whose request
// ended. If a decision or a turn's expiry took the waiter first, it sends on
// ch, and that outcome stands, so the worker gets what the card shows.
func (s *Service) awaitTaken(entryID string, ch chan string) string {
	if _, ok := s.approvals.take(entryID); ok {
		return ""
	}
	return <-ch
}

// ExpireTurnApprovals ends the pending approvals of a turn that has stopped.
func (s *Service) ExpireTurnApprovals(turnID string) { s.approvals.expireTurn(turnID) }

// ResolveApproval records the user's decision on a pending approval card.
func (s *Service) ResolveApproval(ctx context.Context, entryID, decision string) error {
	body, err := s.decidableApproval(ctx, entryID, decision)
	if err != nil {
		return err
	}
	ch, ok := s.approvals.take(entryID)
	if !ok {
		return ErrApprovalClosed
	}
	// The waiting worker gets the decision even if recording it below fails.
	defer func() { ch.ch <- decision }()
	body.Decision = decision
	body.Status = "allowed"
	if decision == DecisionDeny {
		body.Status = "denied"
	}
	if decision == DecisionAlways {
		if err := s.q.AddToolGrant(ctx, db.AddToolGrantParams{BotID: ch.botID, ToolName: body.Tool, CreatedAt: s.nowMs()}); err != nil {
			return fmt.Errorf("grant %s to bot %s: %w", body.Tool, ch.botID, err)
		}
	}
	if _, err := s.UpdateEntry(ctx, entryID, body); err != nil {
		return err
	}
	return nil
}

// decidableApproval returns the approval card that the decision answers, or
// why the decision cannot answer it.
func (s *Service) decidableApproval(ctx context.Context, entryID, decision string) (approvalBody, error) {
	var body approvalBody
	if decision != DecisionOnce && decision != DecisionAlways && decision != DecisionDeny {
		return body, invalid("decision must be once, always or deny")
	}
	entry, err := s.GetEntry(ctx, entryID)
	if err != nil {
		return body, err
	}
	if entry.Kind != KindApproval {
		return body, invalid("entry %s is not an approval", entryID)
	}
	if err := json.Unmarshal(entry.Body, &body); err != nil {
		return body, fmt.Errorf("decode approval %s: %w", entryID, err)
	}
	if decision == DecisionAlways && body.Tool == ToolCreateSetup {
		return body, invalid("the user confirms every setup; choose once or deny")
	}
	return body, nil
}

// ToolGrants lists tools the user always allows for this bot.
func (s *Service) ToolGrants(ctx context.Context, botID string) ([]string, error) {
	grants, err := s.q.ListToolGrants(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("list tool grants of bot %s: %w", botID, err)
	}
	return grants, nil
}

func (s *Service) RevokeToolGrant(ctx context.Context, botID, tool string) error {
	n, err := s.q.DeleteToolGrant(ctx, db.DeleteToolGrantParams{BotID: botID, ToolName: tool})
	if err != nil {
		return fmt.Errorf("revoke %s from bot %s: %w", tool, botID, err)
	}
	if n == 0 {
		return notFound("tool grant", tool)
	}
	return nil
}
