package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"handful/internal/db"
)

// KnownPrompt returns the system prompt that the bot's Claude session sessionID in chat chatID
// knows (the bot's own id for its chat, a room's id for its seat there). known is false when
// Handful stored none for that session. An empty prompt means Claude Code holds no Handful
// prompt for it, as after /compact in a process of its own.
func (s *Service) KnownPrompt(ctx context.Context, botID, chatID, sessionID string) (prompt string, known bool, err error) {
	row, err := s.q.GetSessionPrompt(ctx, db.GetSessionPromptParams{BotID: botID, ChatID: chatID})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read the known prompt of bot %s in chat %s: %w", botID, chatID, err)
	}
	if row.SessionID != sessionID {
		return "", false, nil
	}
	return row.Prompt, true, nil
}

// SetKnownPrompt records prompt as what the bot's Claude session sessionID in chat chatID knows.
func (s *Service) SetKnownPrompt(ctx context.Context, botID, chatID, sessionID, prompt string) error {
	if err := s.q.SetSessionPrompt(ctx, db.SetSessionPromptParams{BotID: botID, ChatID: chatID, SessionID: sessionID, Prompt: prompt}); err != nil {
		return fmt.Errorf("record the known prompt of bot %s in chat %s: %w", botID, chatID, err)
	}
	return nil
}
