package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"testing"
)

// openAt creates a database with the first n migrations, as a YarGPT of
// that version left it.
func openAt(t *testing.T, path string, n int) *sql.DB {
	t.Helper()
	ctx := context.Background()
	conn, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	for _, name := range names[:n] {
		script, err := fs.ReadFile(migrations, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, string(script)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
		t.Fatal(err)
	}
	return conn
}

// migrateAt applies the migrations above an old database's version up to
// last, so that a test of migration last sees its result before later ones.
func migrateAt(t *testing.T, path string, last int) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateTo(t.Context(), conn, last); err != nil {
		conn.Close()
		t.Fatalf("migrate to %d: %v", last, err)
	}
	return conn
}

// A database from before the goal queue keeps every goal and item; the
// requests get their intake from their state, the open accepted ones keep
// working in a slot, and a team still has at most one open goal.
func TestTheGoalQueueMigrationKeepsTheBoardAndBackfillsTheIntake(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "yargpt.db")
	old := openAt(t, path, 12)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES ('ship', 'Ship', '', '', 'room', 1, 1), ('docs', 'Docs', '', '', 'room', 1, 1), ('ana', 'Ana', '', '', 'agent', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id) VALUES ('ship', 'ana'), ('docs', 'ana')`,
		`INSERT INTO goals (id, team_id, text, state, budget, created_at) VALUES
		  ('g1', 'ship', 'Ship it.', 'active', 30, 1), ('g2', 'ship', 'Old.', 'done', 30, 0), ('g3', 'docs', 'Docs.', 'ended', 20, 0), ('g4', 'docs', 'More docs.', 'paused', 20, 2)`,
		`INSERT INTO items (id, team_id, goal_id, title, brief, acceptance, state, owner_id, depends_on, created_by, position, created_at, updated_at) VALUES
		  ('own', 'ship', 'g1', 'Own', 'b', 'a', 'doing', 'ana', '[]', 'ana', 1, 1, 11),
		  ('new', 'docs', 'g1', 'New', 'b', 'a', 'todo', '', '[]', 'ana', 2, 2, 12),
		  ('doing', 'docs', 'g1', 'Doing', 'b', 'a', 'doing', 'ana', '["own"]', 'ana', 3, 3, 13),
		  ('ready', 'docs', 'g1', 'Ready', 'b', 'a', 'todo', 'ana', '[]', 'ana', 4, 4, 14),
		  ('review', 'docs', 'g1', 'Review', 'b', 'a', 'review', 'ana', '[]', 'ana', 5, 5, 15),
		  ('blocked', 'docs', 'g1', 'Blocked', 'b', 'a', 'blocked', 'ana', '[]', 'ana', 6, 6, 16),
		  ('done', 'docs', 'g1', 'Done', 'b', 'a', 'done', 'ana', '[]', 'ana', 7, 7, 17),
		  ('dropped', 'docs', 'g1', 'Dropped', 'b', 'a', 'dropped', 'ana', '[]', 'ana', 8, 8, 18),
		  ('unowned', 'docs', 'g1', 'Unowned', 'b', 'a', 'blocked', '', '[]', 'ana', 9, 9, 19),
		  ('old', 'ship', 'g2', 'Old', 'b', 'a', 'done', 'ana', '[]', 'ana', 1, 0, 0)`,
	} {
		if _, err := old.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare the database: %v", err)
		}
	}
	old.Close()

	// Migration 019 turns the open requests into tasks.
	conn := migrateAt(t, path, 18)
	defer conn.Close()
	q := New(conn)
	goals := map[string]string{"g1": "active", "g2": "done", "g3": "ended", "g4": "paused"}
	for id, state := range goals {
		g, err := q.GetGoal(ctx, id)
		if err != nil || g.State != state || g.QueuePos != 0 {
			t.Errorf("goal %s = %+v, %v; want it %s, not queued", id, g, err, state)
		}
	}
	want := map[string]struct {
		intake string
		slot   int64
	}{
		"own": {"", 0}, "new": {"requested", 0}, "doing": {"accepted", 13}, "ready": {"accepted", 14}, "review": {"accepted", 15},
		"blocked": {"accepted", 16}, "done": {"accepted", 0}, "dropped": {"declined", 0}, "unowned": {"requested", 0}, "old": {"", 0},
	}
	items, err := q.ListGoalItems(ctx, "g1")
	if err != nil || len(items) != 9 {
		t.Fatalf("items of g1 = %d, %v; want 9", len(items), err)
	}
	for id, w := range want {
		it, err := q.GetItem(ctx, id)
		if err != nil || it.Intake != w.intake || it.SlotAt != w.slot {
			t.Errorf("item %s = %+v, %v; want intake %q and slot %d", id, it, err, w.intake, w.slot)
		}
	}
	if it, _ := q.GetItem(ctx, "doing"); it.DependsOn != `["own"]` || it.Title != "Doing" || it.UpdatedAt != 13 || it.OwnerID != "ana" {
		t.Errorf("item doing = %+v; want its fields kept", it)
	}
	if _, err := q.InsertGoal(ctx, InsertGoalParams{ID: "g5", TeamID: "ship", Text: "Two.", State: "active", Budget: 30, CreatedAt: 3}); err == nil {
		t.Fatal("a second open goal of a team was stored; want goals_one_open to reject it")
	}
	if _, err := q.InsertGoal(ctx, InsertGoalParams{ID: "g6", TeamID: "ship", Text: "Next.", State: "queued", QueuePos: 1, CreatedAt: 3}); err != nil {
		t.Fatalf("queued goal: %v", err)
	}
	var slots int64
	if err := conn.QueryRowContext(ctx, "SELECT request_slots FROM teams WHERE team_id = 'docs'").Scan(&slots); err != nil || slots != 1 {
		t.Fatalf("request slots = %d, %v; want 1", slots, err)
	}
	if _, err := conn.ExecContext(ctx, "DELETE FROM bots WHERE id = 'ship'"); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE goal_id IN ('g1', 'g2')").Scan(&left); err != nil || left != 0 {
		t.Fatalf("items of a deleted team = %d, %v; want them gone with its goals", left, err)
	}
}
