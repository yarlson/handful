package core

import (
	"sync"
	"time"
)

// Event is one change that clients receive on the event stream.
type Event struct {
	Type        string            `json:"type"` // "entry", "state", "tasks", "bots", "pause" or "drain"
	BotID       string            `json:"botId,omitempty"`
	Entry       *Entry            `json:"entry,omitempty"`
	Running     *bool             `json:"running,omitempty"`
	Tasks       *[]BackgroundTask `json:"tasks,omitempty"`
	PausedUntil *time.Time        `json:"pausedUntil,omitempty"` // pause: absent when the pause ended
	Draining    *bool             `json:"draining,omitempty"`    // drain: whether drain mode is on
}

const subscriberBuffer = 256

// Hub fans events out to subscribers. A subscriber that falls behind is
// dropped; it reconnects and catches up from the transcript by rev.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

// Subscribe returns a channel of events and a function that ends the subscription.
// The channel is closed when the subscription ends or the subscriber falls behind.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() { h.drop(ch) }
}

func (h *Hub) Publish(e Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func (h *Hub) drop(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}
