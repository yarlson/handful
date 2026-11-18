package core

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"handful/internal/db"
)

// ModelSpend is the tokens and the cost of a turn's calls to one model, its helpers' calls
// included.
type ModelSpend struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64 // Claude Code's estimate at list price
}

// TurnSpend is what a turn cost, per model ID, and how many helpers it started.
type TurnSpend struct {
	Models  map[string]ModelSpend
	Helpers int64
}

// Plus is the spend of t and o together.
func (t TurnSpend) Plus(o TurnSpend) TurnSpend {
	sum := TurnSpend{Models: maps.Clone(t.Models), Helpers: t.Helpers + o.Helpers}
	if sum.Models == nil {
		sum.Models = map[string]ModelSpend{}
	}
	for model, m := range o.Models {
		had := sum.Models[model]
		sum.Models[model] = ModelSpend{
			InputTokens:      had.InputTokens + m.InputTokens,
			OutputTokens:     had.OutputTokens + m.OutputTokens,
			CacheReadTokens:  had.CacheReadTokens + m.CacheReadTokens,
			CacheWriteTokens: had.CacheWriteTokens + m.CacheWriteTokens,
			CostUSD:          had.CostUSD + m.CostUSD,
		}
	}
	return sum
}

// CostUSD is the cost of the turn's calls to all models.
func (t TurnSpend) CostUSD() float64 {
	cost := 0.0
	for _, model := range slices.Sorted(maps.Keys(t.Models)) {
		cost += t.Models[model].CostUSD
	}
	return cost
}

// RecordTurnSpend stores what the bot's turn turnID cost; a later record of the same turn
// replaces it.
func (s *Service) RecordTurnSpend(ctx context.Context, botID, turnID string, sp TurnSpend) error {
	err := s.inTx(ctx, func(q *db.Queries) error {
		err := q.SetTurnSpend(ctx, db.SetTurnSpendParams{
			TurnID: turnID, BotID: botID, CostUsd: sp.CostUSD(), Helpers: sp.Helpers, RecordedAt: s.nowMs(),
		})
		if err != nil {
			return err
		}
		if err := q.DeleteTurnModelSpend(ctx, turnID); err != nil {
			return err
		}
		for model, m := range sp.Models {
			err := q.AddTurnModelSpend(ctx, db.AddTurnModelSpendParams{
				TurnID: turnID, Model: model, InputTokens: m.InputTokens, OutputTokens: m.OutputTokens,
				CacheReadTokens: m.CacheReadTokens, CacheWriteTokens: m.CacheWriteTokens, CostUsd: m.CostUSD,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record the spend of turn %s: %w", turnID, err)
	}
	return nil
}

// BotSpend is what a bot's turns cost in a period, and the helpers they started.
type BotSpend struct {
	BotID   string  `json:"botId"`
	CostUSD float64 `json:"costUsd"`
	Helpers int64   `json:"helpers"`
}

// SpendSince returns the spend of each bot with a turn recorded in the last period, the
// largest cost first.
func (s *Service) SpendSince(ctx context.Context, period time.Duration) ([]BotSpend, error) {
	rows, err := s.q.SpendByBot(ctx, s.nowMs()-period.Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("read the spend of the last %s: %w", period, err)
	}
	spend := make([]BotSpend, len(rows))
	for i, r := range rows {
		spend[i] = BotSpend{BotID: r.BotID, CostUSD: r.CostUsd, Helpers: r.Helpers}
	}
	return spend, nil
}
