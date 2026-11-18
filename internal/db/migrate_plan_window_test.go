package db

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
)

// A database from before plan_windows starts with no window; the migration's script runs again
// without a change, and a later record of a window replaces it.
func TestThePlanWindowsMigrationStartsEmptyAndRunsAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "handful.db")
	openAt(t, path, 28).Close()
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	q := New(conn)
	if rows, err := q.ListPlanWindows(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("windows after the migration = %+v, %v; want none", rows, err)
	}
	weekly := SetPlanWindowParams{
		Name: "seven_day", Status: "allowed_warning", Utilization: sql.NullFloat64{Float64: 0.78, Valid: true},
		ResetsAt: 1795136400, SurpassedThreshold: sql.NullFloat64{Float64: 0.75, Valid: true}, ReceivedAt: 5,
	}
	if err := q.SetPlanWindow(ctx, weekly); err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(migrations, "migrations/029_plan_windows.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(script)); err != nil {
		t.Fatalf("run the migration again: %v", err)
	}
	if rows, err := q.ListPlanWindows(ctx); err != nil || len(rows) != 1 || rows[0] != PlanWindow(weekly) {
		t.Fatalf("windows after a second run = %+v, %v; want %+v kept", rows, err, weekly)
	}
	later := SetPlanWindowParams{Name: "seven_day", Status: "allowed", ResetsAt: 1795741200, ReceivedAt: 9}
	if err := q.SetPlanWindow(ctx, later); err != nil {
		t.Fatal(err)
	}
	if rows, err := q.ListPlanWindows(ctx); err != nil || len(rows) != 1 || rows[0] != PlanWindow(later) {
		t.Fatalf("windows after a later record = %+v, %v; want only %+v", rows, err, later)
	}
}
