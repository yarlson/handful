package core

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"handful/internal/db"
)

// MaxAgentMessage is the longest message, in characters, that a bot may send
// to another bot or post into a room with send_to_agent.
const MaxAgentMessage = 8000

// InboxMessage is a direct message that waits for the recipient's next turn.
type InboxMessage struct {
	ID        string
	FromID    string
	FromName  string
	Text      string
	CreatedAt int64
}

// AgentMessageResult says how a direct message was delivered.
type AgentMessageResult struct {
	Delivered string   // "woke", "held" or "room"
	Woken     []string // members woken by a room post
}

// SendAgentMessage delivers a message from one bot to another bot or to a room
// the sender belongs to. To a bot, priority wakes the recipient with a hidden
// [agent] turn; without priority, or past the hop bound, the message waits
// until the recipient's next turn. hops is the sender's current turn depth.
// files go only with a room post, since direct messages carry text only.
// turnID is the sender's turn, which a room post passes its task on from.
func (s *Service) SendAgentMessage(ctx context.Context, fromID, turnID, targetID, text string, files []string, priority bool, hops int64) (AgentMessageResult, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > MaxAgentMessage {
		return AgentMessageResult{}, invalid("message must be 1 to %d characters", MaxAgentMessage)
	}
	if targetID == fromID {
		return AgentMessageResult{}, invalid("a bot cannot message itself")
	}
	from, err := s.GetBot(ctx, fromID)
	if err != nil {
		return AgentMessageResult{}, err
	}
	text = withoutSelfIntro(text, from.Name)
	target, err := s.GetBot(ctx, targetID)
	if err != nil {
		return AgentMessageResult{}, err
	}
	if target.IsRoom() {
		woken, err := s.PostToRoom(ctx, target.ID, from.ID, turnID, text, files, hops, false)
		if err != nil {
			return AgentMessageResult{}, err
		}
		return AgentMessageResult{Delivered: "room", Woken: woken}, nil
	}

	if len(files) > 0 {
		return AgentMessageResult{}, invalid("files can go only to a room; a direct message to %s carries text only", target.Name)
	}
	return s.directMessage(ctx, from, target, text, priority, hops)
}

// directMessage delivers text from one bot to another, as SendAgentMessage
// describes: it wakes the recipient, or holds the message for its next turn.
func (s *Service) directMessage(ctx context.Context, from, target Bot, text string, priority bool, hops int64) (AgentMessageResult, error) {
	wake := priority && hops+1 <= maxAgentHops
	if _, err := s.AppendEntry(ctx, from.ID, KindAgent, "", map[string]any{"direction": "out", "peerId": target.ID, "peerName": target.Name, "text": text, "priority": priority}); err != nil {
		return AgentMessageResult{}, err
	}
	if _, err := s.AppendEntry(ctx, target.ID, KindAgent, "", map[string]any{"direction": "in", "peerId": from.ID, "peerName": from.Name, "text": text, "priority": priority, "held": !wake}); err != nil {
		return AgentMessageResult{}, err
	}
	if !wake {
		if err := s.q.InsertInboxMessage(ctx, db.InsertInboxMessageParams{ID: uuid.NewString(), BotID: target.ID, FromBotID: from.ID, FromName: from.Name, Text: text, CreatedAt: s.nowMs()}); err != nil {
			return AgentMessageResult{}, fmt.Errorf("hold message for %s: %w", target.Name, err)
		}
		return AgentMessageResult{Delivered: "held"}, nil
	}
	if _, err := s.enqueueTurn(ctx, Turn{BotID: target.ID, Source: SourceAgent, Hops: hops + 1, Prompt: agentPrompt(from, text)}); err != nil {
		return AgentMessageResult{}, err
	}
	return AgentMessageResult{Delivered: "woke"}, nil
}

// Inbox returns the bot's held messages, oldest first. They stay held until
// AckInbox, so a turn that fails before it runs does not lose them.
func (s *Service) Inbox(ctx context.Context, botID string) ([]InboxMessage, error) {
	rows, err := s.q.ListInbox(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("read inbox of bot %s: %w", botID, err)
	}
	out := make([]InboxMessage, len(rows))
	for i, r := range rows {
		out[i] = InboxMessage{ID: r.ID, FromID: r.FromBotID, FromName: r.FromName, Text: r.Text, CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// AckInbox deletes held messages that a finished turn has read.
func (s *Service) AckInbox(ctx context.Context, msgs []InboxMessage) error {
	for _, m := range msgs {
		if err := s.q.DeleteInboxMessage(ctx, m.ID); err != nil {
			return fmt.Errorf("acknowledge inbox message %s: %w", m.ID, err)
		}
	}
	return nil
}

func agentPrompt(from Bot, text string) string {
	return fmt.Sprintf(`[agent] Message from %s (id %s), sent with priority:
%s

This comes from another of the user's bots, not from the user. Act on it if it asks something of you. A task from a teammate is work for the user: do it as you would the user's own request, and invoke a listed skill when one covers it. If it asks a question or a task, %s is waiting: send the answer or result with send_to_agent (target_id %s) and priority true, so it reaches them now. If there is nothing to answer, end without replying, so bots never trade acknowledgements. Tell the user with send_to_user only what they would want to know. Your name is shown with your messages, so do not start them with it.`, from.Name, from.ID, text, from.Name, from.ID)
}
