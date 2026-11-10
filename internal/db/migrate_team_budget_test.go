package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A team from before the daily budget starts with no limit, and its budget
// watch starts when the migration runs.
func TestTheTeamBudgetMigrationStartsEveryTeamWatchedWithNoLimit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "yargpt.db")
	old := openAt(t, path, 17)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
		  ('ana', 'Ana', '', '', 'agent', 1, 1), ('ship', 'Ship', '', '', 'room', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id, request_slots, start_handoffs, decision_minutes) VALUES ('ship', 'ana', 1, 'auto', 45)`,
	} {
		if _, err := old.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare the database: %v", err)
		}
	}
	old.Close()

	before := time.Now().Add(-time.Second).UnixMilli()
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	team, err := New(conn).GetTeam(ctx, "ship")
	if err != nil {
		t.Fatal(err)
	}
	if team.DecisionMinutes != 45 || team.DailyBudget != 0 || team.BudgetNotedOn != "" || team.BudgetWaitedOn != "" {
		t.Fatalf("team = %+v; want it as it was, with no limit", team)
	}
	if team.BudgetSince < before || team.BudgetSince > time.Now().UnixMilli() {
		t.Fatalf("budget watch since %d; want the migration's run, after %d", team.BudgetSince, before)
	}
}
