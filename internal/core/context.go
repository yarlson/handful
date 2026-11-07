package core

import (
	"context"
	"fmt"
)

// ClearContext makes the next turn start a fresh Claude session: the bot's
// main session, or every member's session in a room. The transcript stays,
// with an event that marks where the fresh context begins.
func (s *Service) ClearContext(ctx context.Context, id string) error {
	bot, err := s.GetBot(ctx, id)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Context cleared. %s starts fresh from here; earlier messages stay visible.", bot.Name)
	if bot.IsRoom() {
		for _, m := range bot.Members {
			if err := s.SetRoomSession(ctx, id, m.ID, ""); err != nil {
				return err
			}
		}
		text = "Context cleared. Members start fresh in this room from here; earlier messages stay visible."
	} else if err := s.SetSession(ctx, id, ""); err != nil {
		return err
	}
	_, err = s.AppendEntry(ctx, id, KindEvent, "", map[string]string{"type": "context-cleared", "text": text})
	return err
}

// QueueCompaction queues a compaction of the bot's Claude session, or of each
// member's session in a room. It returns the number of compactions queued.
func (s *Service) QueueCompaction(ctx context.Context, id string) (int, error) {
	bot, err := s.GetBot(ctx, id)
	if err != nil {
		return 0, err
	}
	if !bot.IsRoom() {
		_, err := s.enqueueTurn(ctx, Turn{BotID: id, Source: SourceCompact})
		return 1, err
	}
	for _, m := range bot.Members {
		if _, err := s.enqueueTurn(ctx, Turn{BotID: m.ID, Source: SourceCompact, RoomID: id}); err != nil {
			return 0, err
		}
	}
	return len(bot.Members), nil
}
