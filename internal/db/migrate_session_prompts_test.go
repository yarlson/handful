package db

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
)

// A database from before session_prompts keeps its bots and sessions and has no known prompt,
// the migration's script runs again without a change, and a deleted bot's or room's rows go.
func TestTheSessionPromptsMigrationStartsEmptyAndRunsAgain(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "handful.db")
	old := openAt(t, path, 23)
	if _, err := old.ExecContext(ctx, `INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
	  ('ana', 'Ana', '', 'sess-ana', 'agent', 1, 1), ('plan', 'Plan', '', '', 'room', 1, 1)`); err != nil {
		t.Fatalf("prepare the database: %v", err)
	}
	old.Close()
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	q := New(conn)
	if bot, err := q.GetBot(ctx, "ana"); err != nil || bot.ClaudeSessionID != "sess-ana" {
		t.Fatalf("bot after the migration = %+v, %v; want its session kept", bot, err)
	}
	if row, err := q.GetSessionPrompt(ctx, GetSessionPromptParams{BotID: "ana", ChatID: "ana"}); err == nil {
		t.Fatalf("known prompt after the migration = %+v; want none", row)
	}
	for _, p := range []SetSessionPromptParams{{BotID: "ana", ChatID: "ana", SessionID: "sess-ana", Prompt: "# You are Ana\n"}, {BotID: "ana", ChatID: "plan", SessionID: "sess-plan", Prompt: "# You are Ana in Plan\n"}} {
		if err := q.SetSessionPrompt(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	script, err := fs.ReadFile(migrations, "migrations/024_session_prompts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(script)); err != nil {
		t.Fatalf("run the migration again: %v", err)
	}
	if row, err := q.GetSessionPrompt(ctx, GetSessionPromptParams{BotID: "ana", ChatID: "ana"}); err != nil || row.Prompt != "# You are Ana\n" {
		t.Fatalf("known prompt after a second run = %+v, %v; want it kept", row, err)
	}
	if err := q.DeleteBot(ctx, "plan"); err != nil {
		t.Fatal(err)
	}
	if row, err := q.GetSessionPrompt(ctx, GetSessionPromptParams{BotID: "ana", ChatID: "plan"}); err == nil {
		t.Fatalf("known prompt in a deleted room = %+v; want it gone", row)
	}
	if err := q.DeleteBot(ctx, "ana"); err != nil {
		t.Fatal(err)
	}
	if row, err := q.GetSessionPrompt(ctx, GetSessionPromptParams{BotID: "ana", ChatID: "ana"}); err == nil {
		t.Fatalf("known prompt of a deleted bot = %+v; want it gone", row)
	}
}
