package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"handful/internal/db"
)

// KnownPrompt is what Handful stored for a bot's chat (the bot's own id for its chat, a room's
// id for its seat there): the Claude session and the system prompt that session knows. Stored is
// false when nothing is. An empty prompt means Claude Code holds no Handful prompt for the
// session, as after /compact in a process of its own.
type KnownPrompt struct {
	SessionID string
	Prompt    string
	Stored    bool
}

// For returns the prompt that the Claude session sessionID knows; known is false when Handful
// stored none for that session.
func (k KnownPrompt) For(sessionID string) (prompt string, known bool) {
	if !k.Stored || k.SessionID != sessionID {
		return "", false
	}
	return k.Prompt, true
}

// KnownPrompt returns what Handful stored for the bot's chat chatID.
func (s *Service) KnownPrompt(ctx context.Context, botID, chatID string) (KnownPrompt, error) {
	row, err := s.q.GetSessionPrompt(ctx, db.GetSessionPromptParams{BotID: botID, ChatID: chatID})
	if errors.Is(err, sql.ErrNoRows) {
		return KnownPrompt{}, nil
	}
	if err != nil {
		return KnownPrompt{}, fmt.Errorf("read the known prompt of bot %s in chat %s: %w", botID, chatID, err)
	}
	return KnownPrompt{SessionID: row.SessionID, Prompt: row.Prompt, Stored: true}, nil
}

// SetKnownPrompt records prompt as what the bot's Claude session sessionID in chat chatID knows.
func (s *Service) SetKnownPrompt(ctx context.Context, botID, chatID, sessionID, prompt string) error {
	if err := s.q.SetSessionPrompt(ctx, db.SetSessionPromptParams{BotID: botID, ChatID: chatID, SessionID: sessionID, Prompt: prompt}); err != nil {
		return fmt.Errorf("record the known prompt of bot %s in chat %s: %w", botID, chatID, err)
	}
	return nil
}

// ReplaceKnownPrompt records prompt as what the bot's Claude session sessionID in chat chatID
// knows, only when the chat still holds was, what the caller read before. It returns false when
// something else, such as a compaction, stored a prompt since; that stays.
func (s *Service) ReplaceKnownPrompt(ctx context.Context, botID, chatID string, was KnownPrompt, sessionID, prompt string) (bool, error) {
	var n int64
	var err error
	if was.Stored {
		n, err = s.q.ReplaceSessionPrompt(ctx, db.ReplaceSessionPromptParams{SessionID: sessionID, Prompt: prompt, BotID: botID, ChatID: chatID, WasSessionID: was.SessionID, WasPrompt: was.Prompt})
	} else {
		n, err = s.q.AddSessionPrompt(ctx, db.AddSessionPromptParams{BotID: botID, ChatID: chatID, SessionID: sessionID, Prompt: prompt})
	}
	if err != nil {
		return false, fmt.Errorf("record the known prompt of bot %s in chat %s: %w", botID, chatID, err)
	}
	return n == 1, nil
}
