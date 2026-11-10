package core

import (
	"context"
	"fmt"

	"yargpt/internal/db"
)

// The intake of a request, which items from before tasks replaced requests
// still carry (migration 019): how the serving team took it.
const (
	IntakeRequested = "requested"
	IntakeAccepted  = "accepted"
	IntakeDeclined  = "declined"
	IntakeDeferred  = "deferred"
	IntakeWithdrawn = "withdrawn"
)

// When a deferred request came back, as old items carry it.
const (
	RevisitGoalEnd = "goal_end"
	RevisitDate    = "date"
	RevisitManual  = "manual"
)

// Reason says why a request is where it is.
type Reason struct {
	Code    string `json:"code"`
	Text    string `json:"text"`
	WaitsOn string `json:"waitsOn,omitempty"`
}

// Slots was how a team's request slots were used; a board's Slots is zero.
type Slots struct {
	Limit   int64 `json:"limit"`
	Used    int64 `json:"used"`
	Applies bool  `json:"applies"`
}

// errRequestsAreTasks is what an action on a request gets: a team asks
// another for work with a task now.
var errRequestsAreTasks = invalid("Requests are tasks now: send the team a task with send_task, and name the step that waits on it with waits_for. Put a task off with later, and take back a task you sent with withdraw.")

// RequestWork no longer makes a request; see errRequestsAreTasks.
func (s *Service) RequestWork(context.Context, Actor, string, NewItem) (Item, error) {
	return Item{}, errRequestsAreTasks
}

// DeferItem no longer defers a request; see errRequestsAreTasks.
func (s *Service) DeferItem(context.Context, Actor, string, string, string, int64) (Item, error) {
	return Item{}, errRequestsAreTasks
}

// UndeferItem no longer brings a request back; see errRequestsAreTasks.
func (s *Service) UndeferItem(context.Context, Actor, string) (Item, error) {
	return Item{}, errRequestsAreTasks
}

// WithdrawItem no longer withdraws a request; see errRequestsAreTasks.
func (s *Service) WithdrawItem(context.Context, Actor, string) (Item, error) {
	return Item{}, errRequestsAreTasks
}

// noteTo is a board note for a team's channel.
type noteTo struct{ teamID, text string }

func (s *Service) postNotes(ctx context.Context, notes []noteTo) error {
	for _, n := range notes {
		if n.teamID == "" {
			continue // a handoff that the user made for no team
		}
		if err := s.boardNote(ctx, n.teamID, n.text); err != nil {
			return err
		}
	}
	return nil
}

// actorName names who acts for a channel note.
func actorName(team Bot, a Actor) string {
	if a.BotID != "" {
		return memberName(team, a.BotID)
	}
	if a.Role == RoleUser {
		return "The user"
	}
	return "The app"
}

// updateItem stores an item's changes with q when its state and intake are
// still from and fromIntake, and returns how many rows changed.
func updateItem(ctx context.Context, q *db.Queries, it Item, from, fromIntake string) (int64, error) {
	n, err := q.UpdateItem(ctx, db.UpdateItemParams{
		State: it.State, OwnerID: it.OwnerID, ReviewerID: it.ReviewerID, Result: it.Result, Note: it.Note,
		BlockedFrom: it.BlockedFrom, ReviewRounds: it.ReviewRounds, UpdatedAt: it.UpdatedAt,
		Intake: it.Intake, IntakeNote: it.IntakeNote, DeferRevisit: it.DeferRevisit, DeferUntil: it.DeferUntil, JumpedAt: it.JumpedAt,
		ID: it.ID, FromState: from, FromIntake: fromIntake,
	})
	if err != nil {
		return 0, fmt.Errorf("update item %s: %w", it.ID, err)
	}
	return n, nil
}
