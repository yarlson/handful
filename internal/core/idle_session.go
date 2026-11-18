package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"handful/internal/db"
)

// ResumedSession returns the Claude session that the bot's next turn resumes: in its chat when
// roomID is "", else at its seat in the room or, for a non-zero step, on that step. It is "" when
// no turn would resume one: the bot or the step is gone, the bot left the room, or the step is
// done or dropped.
func (s *Service) ResumedSession(ctx context.Context, botID, roomID string, step Step) (string, error) {
	if step != (Step{}) {
		return s.resumedStepSession(ctx, botID, step)
	}
	if roomID != "" {
		seats, err := s.RoomSeats(ctx, roomID)
		if err != nil {
			return "", err
		}
		for _, seat := range seats {
			if seat.ID == botID {
				return seat.ClaudeSessionID, nil
			}
		}
		return "", nil
	}
	bot, err := s.GetBot(ctx, botID)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return bot.ClaudeSessionID, err
}

func (s *Service) resumedStepSession(ctx context.Context, botID string, step Step) (string, error) {
	item, err := s.q.GetItem(ctx, step.ItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get step %s: %w", step.ItemID, err)
	}
	if item.State == ItemDone || item.State == ItemDropped {
		return "", nil
	}
	row, err := s.stepSession(ctx, botID, step)
	return row.ClaudeSessionID, err
}

// OpenSessionTurns counts the bot's queued and running turns that run in the same Claude
// session as ResumedSession's.
func (s *Service) OpenSessionTurns(ctx context.Context, botID, roomID string, step Step) (int64, error) {
	n, err := s.q.CountOpenSessionTurns(ctx, db.CountOpenSessionTurnsParams{BotID: botID, RoomID: roomID, Role: step.Role, ItemID: step.ItemID})
	if err != nil {
		return 0, fmt.Errorf("count the open turns of bot %s in %q: %w", botID, roomID, err)
	}
	return n, nil
}

// StartCompactionTurn records a running compaction of the Claude session that turn from ran in,
// on from's task and step, and returns its id.
func (s *Service) StartCompactionTurn(ctx context.Context, from Turn) (string, error) {
	now := s.nowMs()
	row, err := s.q.InsertRunningTurn(ctx, db.InsertRunningTurnParams{ID: uuid.NewString(), BotID: from.BotID, Source: SourceCompact, Prompt: "/compact", RoomID: from.RoomID, Hops: from.Hops, GoalID: from.GoalID, ItemID: from.ItemID, CreatedAt: now, StartedAt: sql.NullInt64{Int64: now, Valid: true}})
	if err != nil {
		return "", fmt.Errorf("record compaction turn for bot %s: %w", from.BotID, err)
	}
	return row.ID, nil
}
