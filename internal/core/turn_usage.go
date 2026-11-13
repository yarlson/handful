package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"handful/internal/db"
)

// TurnUsage is the tokens of a turn's Claude calls: the turn's totals, the number of calls, the
// context of the last call, and Cold when the first call wrote more than half of its context to
// the cache instead of reading it.
type TurnUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	Calls            int64 `json:"calls"`
	ContextTokens    int64 `json:"contextTokens"`
	Cold             bool  `json:"cold"`
}

// RecordTurnUsage stores the usage of the bot's turn turnID; a later record of the same turn,
// as after a retry on a fresh session, replaces it.
func (s *Service) RecordTurnUsage(ctx context.Context, botID, turnID string, u TurnUsage) error {
	cold := int64(0)
	if u.Cold {
		cold = 1
	}
	err := s.q.SetTurnUsage(ctx, db.SetTurnUsageParams{
		TurnID: turnID, BotID: botID, InputTokens: u.InputTokens, CacheReadTokens: u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens, OutputTokens: u.OutputTokens, Calls: u.Calls,
		ContextTokens: u.ContextTokens, Cold: cold, RecordedAt: s.nowMs(),
	})
	if err != nil {
		return fmt.Errorf("record the token usage of turn %s: %w", turnID, err)
	}
	return nil
}

// LastTurnUsage returns the usage of the bot's latest recorded turn, in any of its chats; ok is
// false when none is recorded.
func (s *Service) LastTurnUsage(ctx context.Context, botID string) (u TurnUsage, ok bool, err error) {
	row, err := s.q.LastTurnUsage(ctx, botID)
	if errors.Is(err, sql.ErrNoRows) {
		return TurnUsage{}, false, nil
	}
	if err != nil {
		return TurnUsage{}, false, fmt.Errorf("read the last turn usage of bot %s: %w", botID, err)
	}
	return TurnUsage{
		InputTokens: row.InputTokens, CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens,
		OutputTokens: row.OutputTokens, Calls: row.Calls, ContextTokens: row.ContextTokens, Cold: row.Cold == 1,
	}, true, nil
}
