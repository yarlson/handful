package core

import "sync"

// Busy is a bot whose work keeps the server from restarting: a turn that
// runs, or a background task that keeps its claude process alive.
type Busy struct {
	BotID string `json:"botId"`
	Name  string `json:"name"`
	Kind  string `json:"kind"` // BusyTurn or BusyTask
}

const (
	BusyTurn = "turn"
	BusyTask = "task"
)

// drain is the drain mode of the server: while it is on, the runner starts
// no turn, so that an update can restart the server once the running work
// ends. It lives in memory, so a restart ends it.
type drain struct {
	mu    sync.Mutex
	since int64 // ms when drain mode began; 0 when it is off
}

// SetDraining turns drain mode on or off and tells the clients. Turning it
// on again keeps the time it began. Turning it off lets queued turns start.
func (s *Service) SetDraining(on bool) {
	s.drain.mu.Lock()
	changed := on != (s.drain.since > 0)
	if !on {
		s.drain.since = 0
	}
	if on && changed {
		s.drain.since = s.nowMs()
	}
	s.drain.mu.Unlock()
	if !changed {
		return
	}
	s.hub.Publish(Event{Type: "drain", Draining: &on})
	if !on {
		s.signalTurns()
	}
}

// Draining reports whether drain mode is on, and since when (ms; 0 when off).
func (s *Service) Draining() (bool, int64) {
	s.drain.mu.Lock()
	defer s.drain.mu.Unlock()
	return s.drain.since > 0, s.drain.since
}
