package main

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"
)

// zoneCheckEvery is how often the clock reads the computer's time zone file again.
const zoneCheckEvery = time.Minute

// computerClock returns the time in the computer's time zone. Go reads that zone once, at
// start, into time.Local, so the clock reads /etc/localtime itself, at most once every
// zoneCheckEvery: a change of the zone, or of its rules, then shows without a restart. TZ,
// when set, names the zone for the whole process, as it does for time.Local.
func computerClock(log *slog.Logger) func() time.Time {
	if _, ok := os.LookupEnv("TZ"); ok {
		return time.Now
	}
	c := &zoneClock{file: "/etc/localtime", wall: time.Now, loc: time.Local, log: log}
	return c.now
}

// zoneClock gives the wall time in the zone of file, which it reads again at most once every
// zoneCheckEvery. Its location stays the same value while the file's content stays the same.
type zoneClock struct {
	file string
	wall func() time.Time
	log  *slog.Logger

	mu      sync.Mutex
	data    []byte
	loc     *time.Location
	checked time.Time
}

func (c *zoneClock) now() time.Time {
	t := c.wall()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.checked.IsZero() || t.Sub(c.checked) >= zoneCheckEvery {
		c.checked = t
		c.reload()
	}
	return t.In(c.loc)
}

// reload reads the zone file. Without the file the zone is UTC, as for time.Local. When the
// file can't be read or parsed, the clock keeps its zone and logs why.
func (c *zoneClock) reload() {
	data, err := os.ReadFile(c.file)
	if errors.Is(err, fs.ErrNotExist) {
		c.data, c.loc = nil, time.UTC
		return
	}
	if err != nil {
		c.log.Warn("read the time zone; the clock keeps its zone", "file", c.file, "err", err)
		return
	}
	if bytes.Equal(data, c.data) {
		return
	}
	loc, err := time.LoadLocationFromTZData("Local", data)
	if err != nil {
		c.log.Warn("parse the time zone; the clock keeps its zone", "file", c.file, "err", err)
		return
	}
	c.data, c.loc = data, loc
}
