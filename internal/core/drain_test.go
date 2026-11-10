package core

import (
	"testing"
	"time"
)

func TestDrainModeKeepsItsStartAndTellsClientsOfEachChange(t *testing.T) {
	svc := newTestService(t)
	start := time.UnixMilli(1_800_000_000_000)
	now := start
	svc.now = func() time.Time { return now }
	events, stop := svc.Hub().Subscribe()
	defer stop()
	drainEvents := func() []bool {
		var got []bool
		for {
			select {
			case e := <-events:
				if e.Type == "drain" {
					got = append(got, *e.Draining)
				}
			default:
				return got
			}
		}
	}
	// A turn signal that waits is drained first, so the test sees only the drain's.
	select {
	case <-svc.TurnsReady():
	default:
	}

	if on, since := svc.Draining(); on || since != 0 {
		t.Fatalf("draining at start = %v since %d; want off", on, since)
	}
	svc.SetDraining(false)
	if got := drainEvents(); len(got) != 0 {
		t.Fatalf("events after turning off what is off = %v; want none", got)
	}
	svc.SetDraining(true)
	now = start.Add(time.Minute)
	svc.SetDraining(true)
	if on, since := svc.Draining(); !on || since != start.UnixMilli() {
		t.Fatalf("draining = %v since %d; want on since %d, the first time", on, since, start.UnixMilli())
	}
	if got := drainEvents(); len(got) != 1 || !got[0] {
		t.Fatalf("drain events after turning it on twice = %v; want one [true]", got)
	}
	select {
	case <-svc.TurnsReady():
		t.Fatal("turning drain mode on signalled turns; want no signal")
	default:
	}

	svc.SetDraining(false)
	if on, since := svc.Draining(); on || since != 0 {
		t.Fatalf("draining after it ended = %v since %d; want off", on, since)
	}
	if got := drainEvents(); len(got) != 1 || got[0] {
		t.Fatalf("drain events after it ended = %v; want one [false]", got)
	}
	select {
	case <-svc.TurnsReady():
	default:
		t.Fatal("leaving drain mode did not signal the queued turns")
	}
}

func TestARestartEventCarriesItsState(t *testing.T) {
	svc := newTestService(t)
	events, stop := svc.Hub().Subscribe()
	defer stop()
	svc.PublishRestart(RestartExpired)
	e := <-events
	if e.Type != "restart" || e.State != RestartExpired {
		t.Fatalf("event = %+v; want a restart event with state %q", e, RestartExpired)
	}
}

func TestDrainModeStaysOnWhileAnyHolderHoldsIt(t *testing.T) {
	svc := newTestService(t)
	start := time.UnixMilli(1_800_000_000_000)
	now := start
	svc.now = func() time.Time { return now }
	svc.HoldDrain(DrainRestart, true)
	now = start.Add(time.Minute)
	svc.SetDraining(true)
	svc.HoldDrain(DrainRestart, false)
	if on, since := svc.Draining(); !on || since != start.UnixMilli() {
		t.Fatalf("drain after the restart let go = %v since %d; want on since %d, held by the updater", on, since, start.UnixMilli())
	}
	svc.SetDraining(false)
	if on, since := svc.Draining(); on || since != 0 {
		t.Fatalf("drain after both let go = %v since %d; want off", on, since)
	}
}
