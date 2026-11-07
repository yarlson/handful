package core

import (
	"context"
	"fmt"

	"yargpt/internal/db"
)

// ReorderBots sets the order of one sidebar section: ids must list every
// agent bot, or every room, exactly once. Room members take their turns in
// the agents' order.
func (s *Service) ReorderBots(ctx context.Context, ids []string) error {
	all, err := s.ListBots(ctx)
	if err != nil {
		return err
	}
	kindOf := map[string]string{}
	for _, b := range all {
		kindOf[b.ID] = b.Kind
	}
	if len(ids) == 0 {
		return invalid("the order is empty")
	}
	kind, seen := kindOf[ids[0]], map[string]bool{}
	for _, id := range ids {
		k, ok := kindOf[id]
		if !ok {
			return notFound("bot", id)
		}
		if k != kind {
			return invalid("the order mixes bots and rooms")
		}
		if seen[id] {
			return invalid("bot %s appears twice in the order", id)
		}
		seen[id] = true
	}
	for id, k := range kindOf {
		if k == kind && !seen[id] {
			return invalid("the order must list every item of its section; %s is missing", id)
		}
	}

	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	for i, id := range ids {
		if err := qtx.SetBotPosition(ctx, db.SetBotPositionParams{Position: int64(i + 1), ID: id}); err != nil {
			return fmt.Errorf("set position of bot %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bot order: %w", err)
	}
	s.hub.Publish(Event{Type: "bots"})
	return nil
}
