package db

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
)

// A database from before the dismiss tables keeps its goals and teams, with
// nothing put away, and the migration's script runs again without a change.
func TestTheDismissMigrationStartsWithNothingPutAwayAndRunsAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "handful.db")
	old := openAt(t, path, 20)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
		  ('ana', 'Ana', '', '', 'agent', 1, 1), ('ship', 'Ship', '', '', 'room', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id, request_slots, start_handoffs, decision_minutes) VALUES ('ship', 'ana', 1, 'auto', 45)`,
		`INSERT INTO goals (id, team_id, text, state, budget, queue_pos, created_at) VALUES ('g', 'ship', 'Guide.', 'declined', 0, 0, 5)`,
	} {
		if _, err := old.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare the database: %v", err)
		}
	}
	old.Close()
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	q := New(conn)
	if at, err := q.GetGoalDismissal(ctx, "g"); err != nil || at != 0 {
		t.Fatalf("goal put away at %d, %v; want not put away", at, err)
	}
	if err := q.DismissOffTask(ctx, DismissOffTaskParams{TeamID: "ship", At: 7}); err != nil {
		t.Fatal(err)
	}
	if n, err := q.DismissGoal(ctx, DismissGoalParams{ID: "g", At: 8}); err != nil || n != 1 {
		t.Fatalf("put away = %d, %v", n, err)
	}
	script, err := fs.ReadFile(migrations, "migrations/021_dismiss.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(script)); err != nil {
		t.Fatalf("run the migration again: %v", err)
	}
	if at, err := q.GetGoalDismissal(ctx, "g"); err != nil || at != 8 {
		t.Fatalf("goal put away at %d, %v after a second run; want 8", at, err)
	}
	var off int64
	if err := conn.QueryRowContext(ctx, `SELECT dismissed_at FROM off_task_dismissals WHERE team_id = 'ship'`).Scan(&off); err != nil || off != 7 {
		t.Fatalf("turns put away at %d, %v after a second run; want 7", off, err)
	}
	// Deleting the team deletes what it put away.
	if _, err := conn.ExecContext(ctx, `DELETE FROM bots WHERE id = 'ship'`); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := conn.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM goal_dismissals) + (SELECT count(*) FROM off_task_dismissals)`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d dismissals left after the team was deleted, %v; want 0", left, err)
	}
}
