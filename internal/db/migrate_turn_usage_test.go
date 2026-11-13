package db

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
)

// A database from before turn_usage keeps its turns, with no usage; the migration's script runs
// again without a change, and a deleted turn's usage goes with it.
func TestTheTurnUsageMigrationStartsEmptyAndRunsAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "handful.db")
	old := openAt(t, path, 24)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES ('ana', 'Ana', '', '', 'agent', 1, 1)`,
		`INSERT INTO turns (id, bot_id, source, prompt, status, created_at) VALUES ('t1', 'ana', 'user', 'hi', 'done', 1)`,
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
	if row, err := q.LastTurnUsage(ctx, "ana"); err == nil {
		t.Fatalf("usage after the migration = %+v; want none", row)
	}
	if err := q.SetTurnUsage(ctx, SetTurnUsageParams{TurnID: "t1", BotID: "ana", Calls: 3, ContextTokens: 142000, Cold: 1, RecordedAt: 5}); err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(migrations, "migrations/025_turn_usage.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(script)); err != nil {
		t.Fatalf("run the migration again: %v", err)
	}
	if row, err := q.LastTurnUsage(ctx, "ana"); err != nil || row.Calls != 3 || row.ContextTokens != 142000 || row.Cold != 1 {
		t.Fatalf("usage after a second run = %+v, %v; want it kept", row, err)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM turns WHERE id = 't1'`); err != nil {
		t.Fatal(err)
	}
	if row, err := q.LastTurnUsage(ctx, "ana"); err == nil {
		t.Fatalf("usage of a deleted turn = %+v; want it gone", row)
	}
}
