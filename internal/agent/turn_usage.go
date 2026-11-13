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

// recordUsage stores the usage that the result event ev reports for the active turn cur of s.
func (r *Runner) recordUsage(s *session, cur *activeTurn, ev streamEvent) {
	u, ok := turnUsage(ev, cur.out.calls)
	if !ok {
		return
	}
	if err := r.svc.RecordTurnUsage(context.Background(), s.key.botID, cur.turn.ID, u); err != nil {
		r.log.Error("record turn usage", "turn", cur.turn.ID, "err", err)
	}
}
