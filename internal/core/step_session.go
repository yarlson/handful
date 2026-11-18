package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"handful/internal/db"
)

// Step names a member's turns in one role on one step of a team's board: work or review. Those
// turns run in a Claude session of their own, with their own cursor in the team's room. The zero
// Step stands for the member's seat in the room, which all its other turns there use.
type Step struct{ ItemID, Role string }

// StepOf returns the step that turn t works on or reviews; the zero Step for any other turn,
// such as a chat, lead, retro or task turn, or a turn that a step turn's post woke.
func StepOf(t Turn) Step {
	if t.BoardRole != RoleWork && t.BoardRole != RoleReview {
		return Step{}
	}
	return Step{ItemID: t.ItemID, Role: t.BoardRole}
}

// stepSession returns what Handful stored for the bot's session on step; the zero row when
// nothing is.
func (s *Service) stepSession(ctx context.Context, botID string, step Step) (db.GetStepSessionRow, error) {
	row, err := s.q.GetStepSession(ctx, db.GetStepSessionParams{ItemID: step.ItemID, BotID: botID, Role: step.Role})
	if errors.Is(err, sql.ErrNoRows) {
		return db.GetStepSessionRow{}, nil
	}
	if err != nil {
		return db.GetStepSessionRow{}, fmt.Errorf("read the %s session of bot %s on step %s: %w", step.Role, botID, step.ItemID, err)
	}
	return row, nil
}

// SetStepSession stores sessionID as the bot's Claude session on step.
func (s *Service) SetStepSession(ctx context.Context, botID string, step Step, sessionID string) error {
	if err := s.q.SetStepSession(ctx, db.SetStepSessionParams{ItemID: step.ItemID, BotID: botID, Role: step.Role, ClaudeSessionID: sessionID}); err != nil {
		return fmt.Errorf("set the %s session of bot %s on step %s: %w", step.Role, botID, step.ItemID, err)
	}
	return nil
}
