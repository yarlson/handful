package agent

import (
	"context"
	"slices"
	"strings"

	"yargpt/internal/core"
)

// Busy lists the bots whose work keeps the server from restarting for an
// update: a running turn, or a claude process that a task turn or
// background tasks keep alive. A bot with both is listed once, as a turn.
// The list is sorted by name and is never nil.
func (r *Runner) Busy(ctx context.Context) []core.Busy {
	kinds, names := r.busyLocked()
	busy := make([]core.Busy, 0, len(kinds))
	for id, kind := range kinds {
		busy = append(busy, core.Busy{BotID: id, Name: r.botName(ctx, id, names[id]), Kind: kind})
	}
	slices.SortFunc(busy, func(a, b core.Busy) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.BotID, b.BotID)
	})
	return busy
}

// busyLocked returns the kind of work of each busy bot, by bot id, and the
// names of the bots whose sessions are known.
func (r *Runner) busyLocked() (kinds, names map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kinds, names = map[string]string{}, map[string]string{}
	for _, s := range r.sessions {
		names[s.key.botID] = s.bot.Name
		if s.waking || len(s.tasks) > 0 {
			kinds[s.key.botID] = core.BusyTask
		}
	}
	for id := range r.running {
		kinds[id] = core.BusyTurn
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
