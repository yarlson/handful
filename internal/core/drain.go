package core

import "sync"

// Busy is a bot whose work keeps the server from restarting: a turn that
// runs, or a background task that keeps its claude process alive.
type Busy struct {
	BotID string `json:"botId"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`  // BusyTurn or BusyTask
	Since int64  `json:"since"` // ms: when the turn started, or when the process became busy with tasks
}

const (
	BusyTurn = "turn"
	BusyTask = "task"
)

// drain is the drain mode of the server: while it is on, the runner starts
// no turn, so that an update or a restart can end the server once the
// running work ends. Each holder turns its own hold on and off; drain mode
// is on while any holds it, so one ending does not let turns start that
// another still waits out. It lives in memory, so a restart ends it.
type drain struct {
	mu      sync.Mutex
	holders map[string]bool
	since   int64 // ms when drain mode began; 0 when it is off
}

// The holders of drain mode.
const (
	DrainUpdate  = "update"  // POST and DELETE /api/drain: the app's updater
	DrainRestart = "restart" // an idle restart that waits for the work to end
)

// SetDraining turns the updater's hold on drain mode on or off.
func (s *Service) SetDraining(on bool) { s.HoldDrain(DrainUpdate, on) }

// HoldDrain turns holder's hold on drain mode on or off, and tells the
// clients when drain mode itself changes. Drain mode keeps the time it
// began while any holder holds it. When the last hold ends, queued turns
// start.
func (s *Service) HoldDrain(holder string, on bool) {
	s.drain.mu.Lock()
	was := len(s.drain.holders) > 0
	if s.drain.holders == nil {
		s.drain.holders = map[string]bool{}
	}
	if on {
		s.drain.holders[holder] = true
	} else {
		delete(s.drain.holders, holder)
	}
	is := len(s.drain.holders) > 0
	if is && !was {
		s.drain.since = s.nowMs()
	}
	if !is {
		s.drain.since = 0
	}
	s.drain.mu.Unlock()
	if is == was {
		return
	}
	s.hub.Publish(Event{Type: "drain", Draining: &is})
	if !is {
		s.signalTurns()
	}
}

// Draining reports whether drain mode is on, and since when (ms; 0 when off).
func (s *Service) Draining() (bool, int64) {
	s.drain.mu.Lock()
	defer s.drain.mu.Unlock()
	return s.drain.since > 0, s.drain.since
}
