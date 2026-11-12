package httpapi

import (
	"sync"
	"time"
)

// The rate limits of the phone listener (spec 6a748f8a section 5.4).
const (
	phoneRate      = 20  // requests per second a phone may make, on average
	phoneBurst     = 60  // requests a phone may make at once, as a page loads
	phoneSendRate  = 1.0 // messages per second a phone may send
	unpairedPerMin = 30  // requests without a session per minute, from all callers together
)

// bucket is a token bucket: it holds up to burst tokens and gains rate tokens a second.
type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) take(now time.Time, rate, burst float64) bool {
	if b.last.IsZero() {
		b.tokens = burst
	} else {
		b.tokens = min(burst, b.tokens+now.Sub(b.last).Seconds()*rate)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// phoneLimits counts the phone listener's requests: per paired phone, its messages per phone,
// and the requests without a session from everyone together, because behind a proxy every
// request comes from the same address.
type phoneLimits struct {
	now      func() time.Time
	mu       sync.Mutex
	devices  map[string]*bucket
	sends    map[string]*bucket
	unpaired bucket
}

func newPhoneLimits(now func() time.Time) *phoneLimits {
	return &phoneLimits{now: now, devices: map[string]*bucket{}, sends: map[string]*bucket{}}
}

// allow reports whether phone id may make a request now; send says it sends a message.
func (l *phoneLimits) allow(id string, send bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if !bucketOf(l.devices, id).take(now, phoneRate, phoneBurst) {
		return false
	}
	return !send || bucketOf(l.sends, id).take(now, phoneSendRate, 1)
}

// allowUnpaired reports whether one more request without a session may get its answer now.
func (l *phoneLimits) allowUnpaired() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.unpaired.take(l.now(), unpairedPerMin/60.0, unpairedPerMin)
}

func bucketOf(m map[string]*bucket, id string) *bucket {
	b, ok := m[id]
	if !ok {
		b = &bucket{}
		m[id] = b
	}
	return b
}
