package db

import (
	"context"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
)

// A database from before turn_spend keeps its turns, with no spend; the migration's script runs
// again without a change, and a deleted turn's spend goes with it, per model too.
func TestTheTurnSpendMigrationStartsEmptyAndRunsAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "handful.db")
	old := openAt(t, path, 29)
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
	if rows, err := q.SpendByBot(ctx, 0); err != nil || len(rows) != 0 {
		t.Fatalf("spend after the migration = %+v, %v; want none", rows, err)
	}
	if err := q.SetTurnSpend(ctx, SetTurnSpendParams{TurnID: "t1", BotID: "ana", CostUsd: 0.5, Helpers: 1, RecordedAt: 5}); err != nil {
		t.Fatal(err)
	}
	haiku := AddTurnModelSpendParams{TurnID: "t1", Model: "claude-haiku-5-5", InputTokens: 8, OutputTokens: 410, CacheReadTokens: 26715, CacheWriteTokens: 28048, CostUsd: 0.5}
	if err := q.AddTurnModelSpend(ctx, haiku); err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(migrations, "migrations/030_turn_spend.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(script)); err != nil {
		t.Fatalf("run the migration again: %v", err)
	}
	want := []SpendByBotRow{{BotID: "ana", CostUsd: 0.5, Helpers: 1}}
	if rows, err := q.SpendByBot(ctx, 0); err != nil || !reflect.DeepEqual(rows, want) {
		t.Fatalf("spend after a second run = %+v, %v; want %+v", rows, err, want)
	}
	var got TurnModelSpend
	err = conn.QueryRowContext(ctx, `SELECT * FROM turn_model_spend`).Scan(&got.TurnID, &got.Model, &got.InputTokens, &got.OutputTokens, &got.CacheReadTokens, &got.CacheWriteTokens, &got.CostUsd)
	if err != nil || got != TurnModelSpend(haiku) {
		t.Fatalf("spend per model after a second run = %+v, %v; want %+v", got, err, haiku)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM turns WHERE id = 't1'`); err != nil {
		t.Fatal(err)
	}
	if rows, err := q.SpendByBot(ctx, 0); err != nil || len(rows) != 0 {
		t.Fatalf("spend of a deleted turn = %+v, %v; want it gone", rows, err)
	}
	var left int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM turn_model_spend`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("spend per model of a deleted turn = %d rows, %v; want none", left, err)
	}
}
