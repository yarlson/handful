package agent

import (
	"context"
	"slices"
	"strings"
	"time"

	"handful/internal/core"
)

// Busy lists the bots whose work keeps the server from restarting for an
// update: a running turn, or a claude process that a task turn or
// background tasks keep alive. A bot with both is listed once, as a turn.
// The list is sorted by name and is never nil.
func (r *Runner) Busy(ctx context.Context) []core.Busy {
	kinds, names := r.busyLocked()
	busy := make([]core.Busy, 0, len(kinds))
	for id, w := range kinds {
		busy = append(busy, core.Busy{BotID: id, Name: r.botName(ctx, id, names[id]), Kind: w.kind, Since: msOf(w.since)})
	}
	slices.SortFunc(busy, func(a, b core.Busy) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.BotID, b.BotID)
	})
	return busy
}

// work is what keeps a bot busy, and since when.
type work struct {
	kind  string
	since time.Time
}

// busyLocked returns the work of each busy bot, by bot id, and the names of
// the bots whose sessions are known. A bot busy in several sessions is
// listed since the earliest of them.
func (r *Runner) busyLocked() (kinds map[string]work, names map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kinds, names = map[string]work{}, map[string]string{}
	for _, s := range r.sessions {
		names[s.key.botID] = s.bot.Name
		w, seen := kinds[s.key.botID]
		if (s.waking || len(s.tasks) > 0) && (!seen || s.busySince.Before(w.since)) {
			kinds[s.key.botID] = work{kind: core.BusyTask, since: s.busySince}
		}
	}
	for id, rt := range r.running {
		kinds[id] = work{kind: core.BusyTurn, since: rt.since}
	}
	return kinds, names
}

// botName returns known, else the bot's name from the service; the id when
// the bot is gone.
func (r *Runner) botName(ctx context.Context, id, known string) string {
	if known != "" {
		return known
	}
	bot, err := r.svc.GetBot(ctx, id)
	if err != nil {
		return id
	}
	return bot.Name
}

// msOf is t in ms, or 0 for the zero time.
func msOf(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
