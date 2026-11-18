package core

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"handful/internal/db/dbtest"
)

// A recorded window is the latest state of that window, with the time it came, after a restart
// too; a later record of it replaces it, and each record goes to the event stream.
func TestAPlanWindowKeepsItsLatestRecordAcrossARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	open := func() *Service {
		t.Helper()
		conn, err := dbtest.Open(ctx, filepath.Join(dir, "handful.db"))
		if err != nil {
			t.Fatalf("open db: %v", err)
		}
		t.Cleanup(func() { conn.Close() })
		svc, err := New(conn, dir, time.UTC)
		if err != nil {
			t.Fatalf("new service: %v", err)
		}
		return svc
	}
	svc := open()
	svc.now = func() time.Time { return time.UnixMilli(1000) }
	events, stop := svc.Hub().Subscribe()
	defer stop()
	if got, err := svc.PlanWindows(ctx); err != nil || len(got) != 0 {
		t.Fatalf("windows before any record = %+v, %v; want none", got, err)
	}
	if err := svc.RecordPlanWindows(ctx, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		t.Fatalf("event after recording no window = %+v; want none", ev)
	default:
	}
	weekly := PlanWindow{Window: "seven_day", Status: "allowed_warning", Utilization: new(0.78), ResetsAt: 1795136400, SurpassedThreshold: new(0.75)}
	session := PlanWindow{Window: "five_hour", Status: "allowed", Utilization: new(0.0)}
	if err := svc.RecordPlanWindows(ctx, []PlanWindow{weekly, session}); err != nil {
		t.Fatal(err)
	}
	weekly.ReceivedAt, session.ReceivedAt = 1000, 1000
	if ev := <-events; ev.Type != "planWindows" || !reflect.DeepEqual(ev.PlanWindows, []PlanWindow{weekly, session}) {
		t.Fatalf("event = %+v; want planWindows with %+v and %+v", ev, weekly, session)
	}
	later := PlanWindow{Window: "seven_day", Status: "allowed_warning", ResetsAt: 1795136400, SurpassedThreshold: new(0.9)}
	svc.now = func() time.Time { return time.UnixMilli(2000) }
	if err := svc.RecordPlanWindows(ctx, []PlanWindow{later}); err != nil {
		t.Fatal(err)
	}
	later.ReceivedAt = 2000
	want := []PlanWindow{session, later}
	if got, err := open().PlanWindows(ctx); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("windows after a restart = %+v, %v; want %+v", got, err, want)
	}
}
