package main

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// zoneData returns the system's zone file for name.
func zoneData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("/usr/share/zoneinfo", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// offsetHours is the offset from UTC, in hours, of t's zone at t.
func offsetHours(t time.Time) int {
	_, offset := t.Zone()
	return offset / 3600
}

func TestTheClockFollowsAChangeOfTheComputersZone(t *testing.T) {
	file := filepath.Join(t.TempDir(), "localtime")
	if err := os.WriteFile(file, zoneData(t, "America/New_York"), 0o600); err != nil {
		t.Fatal(err)
	}
	wall := time.Date(2027, 1, 15, 12, 0, 0, 0, time.UTC)
	c := &zoneClock{file: file, wall: func() time.Time { return wall }, loc: time.Local, log: slog.New(slog.DiscardHandler)}
	first := c.now()
	if offsetHours(first) != -5 || !first.Equal(wall) {
		t.Fatalf("now = %v; want %v in New York, UTC-5", first, wall)
	}
	if err := os.WriteFile(file, zoneData(t, "Europe/Riga"), 0o600); err != nil {
		t.Fatal(err)
	}
	wall = wall.Add(zoneCheckEvery - time.Second)
	if got := c.now(); offsetHours(got) != -5 {
		t.Fatalf("now within a minute of the last check = %v; want New York still", got)
	}
	wall = wall.Add(time.Second)
	riga := c.now()
	if offsetHours(riga) != 2 || !riga.Equal(wall) {
		t.Fatalf("now a minute after the last check = %v; want %v in Riga, UTC+2", riga, wall)
	}
	wall = wall.Add(zoneCheckEvery)
	if got := c.now(); got.Location() != riga.Location() {
		t.Fatalf("location after a check of the same file = %p; want the same location %p", got.Location(), riga.Location())
	}
}

func TestTheClockKeepsItsZoneWhenTheFileIsBad(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "localtime")
	if err := os.WriteFile(file, zoneData(t, "Europe/Riga"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	wall := time.Date(2027, 1, 15, 12, 0, 0, 0, time.UTC)
	c := &zoneClock{file: file, wall: func() time.Time { return wall }, loc: time.Local, log: slog.New(slog.NewTextHandler(&logs, nil))}
	c.now()
	for _, bad := range []struct {
		name  string
		place func() error
	}{
		{"not a zone", func() error { return os.WriteFile(file, []byte("hello"), 0o600) }},
		{"a folder", func() error { return errors.Join(os.Remove(file), os.Mkdir(file, 0o700)) }},
	} {
		if err := bad.place(); err != nil {
			t.Fatal(err)
		}
		wall = wall.Add(zoneCheckEvery)
		if got := c.now(); offsetHours(got) != 2 {
			t.Fatalf("%s: now = %v; want Riga still", bad.name, got)
		}
	}
	if strings.Count(logs.String(), "the clock keeps its zone") != 2 {
		t.Fatalf("logs = %q; want one warning for each bad file", logs.String())
	}
}

func TestWithoutAZoneFileTheClockIsInUTC(t *testing.T) {
	wall := time.Date(2027, 1, 15, 12, 0, 0, 0, time.FixedZone("X", 3600))
	c := &zoneClock{file: filepath.Join(t.TempDir(), "missing"), wall: func() time.Time { return wall }, loc: time.Local, log: slog.New(slog.DiscardHandler)}
	if got := c.now(); got.Location() != time.UTC {
		t.Fatalf("now = %v; want UTC", got)
	}
}

func TestTZNamesTheClocksZone(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	if got := computerClock(slog.New(slog.DiscardHandler))(); got.Location() != time.Local {
		t.Fatalf("clock's location with TZ set = %v; want time.Local, which TZ names", got.Location())
	}
	os.Unsetenv("TZ")
	if got := computerClock(slog.New(slog.DiscardHandler))(); got.Location() == time.Local {
		t.Fatal("clock's location without TZ = time.Local; want the zone read from /etc/localtime")
	}
}
