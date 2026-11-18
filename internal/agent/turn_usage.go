package agent

import (
	"context"

	"handful/internal/core"
)

// callTokens follows the model calls of a turn's main thread: how many, and the tokens of the
// first and the last.
type callTokens struct {
	count       int64
	lastID      string
	first, last tokenUsage
}

// note counts the assistant event ev when it starts a call of the main thread. The events of one
// call carry its message id; a subagent's events carry the tool call that started it.
func (c *callTokens) note(ev streamEvent) {
	m := ev.Message
	if ev.ParentToolUseID != nil || m == nil || m.Usage == nil || m.ID == "" || m.ID == c.lastID {
		return
	}
	if c.count == 0 {
		c.first = *m.Usage
	}
	c.count++
	c.lastID, c.last = m.ID, *m.Usage
}

// turnUsage is the usage of a turn whose result event is ev and whose calls are c. A turn is
// cold when its first call writes more than half of its context to the cache. ok is false when
// the turn made no model call.
func turnUsage(ev streamEvent, c callTokens) (u core.TurnUsage, ok bool) {
	if ev.Usage == nil || c.count == 0 {
		return core.TurnUsage{}, false
	}
	return core.TurnUsage{
		InputTokens:      ev.Usage.InputTokens,
		CacheReadTokens:  ev.Usage.CacheReadInputTokens,
		CacheWriteTokens: ev.Usage.CacheCreationInputTokens,
		OutputTokens:     ev.Usage.OutputTokens,
		Calls:            c.count,
		ContextTokens:    c.last.context(),
		Cold:             2*c.first.CacheCreationInputTokens > c.first.context(),
	}, true
}

// spendMeter finds the spend of each result event of one claude process.
type spendMeter struct {
	spawned int64 // the helpers the process had started at its previous result
}

// spendOf is what the process spent since its previous result, as the result event ev reports
// it. A result without model_usage_since_last_result spends nothing.
func (m *spendMeter) spendOf(ev streamEvent) core.TurnSpend {
	sp := core.TurnSpend{Models: map[string]core.ModelSpend{}}
	for model, u := range ev.ModelUsageSinceLastResult {
		sp.Models[model] = core.ModelSpend{
			InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadTokens: u.CacheReadInputTokens,
			CacheWriteTokens: u.CacheCreationInputTokens, CostUSD: u.CostUSD,
		}
	}
	if ev.SubagentStats != nil {
		sp.Helpers = ev.SubagentStats.Spawned - m.spawned
		m.spawned = ev.SubagentStats.Spawned
	}
	return sp
}

// recordUsage stores the usage that the result event ev reports for the active turn cur of s,
// and the spend of all of cur's results.
func (r *Runner) recordUsage(s *session, cur *activeTurn, ev streamEvent) {
	ctx := context.Background()
	if len(cur.out.spend.Models) > 0 {
		if err := r.svc.RecordTurnSpend(ctx, s.key.botID, cur.turn.ID, cur.out.spend); err != nil {
			r.log.Error("record turn spend", "turn", cur.turn.ID, "err", err)
		}
	}
	u, ok := turnUsage(ev, cur.out.calls)
	if !ok {
		return
	}
	if err := r.svc.RecordTurnUsage(ctx, s.key.botID, cur.turn.ID, u); err != nil {
		r.log.Error("record turn usage", "turn", cur.turn.ID, "err", err)
	}
}
