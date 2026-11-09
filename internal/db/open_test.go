package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// A database created by YarGPT before migrations existed has the 001 schema and
// user_version 0. Opening it must keep its data and add the 002 changes.
func TestOpenMigratesAnUnversionedV1Database(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "yargpt.db")
	old, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		string(v1),
		`INSERT INTO bots (id, name, instructions, claude_session_id, created_at, updated_at) VALUES ('b2', 'Later', '', '', 5, 5)`,
		`INSERT INTO bots (id, name, instructions, claude_session_id, created_at, updated_at) VALUES ('b1', 'Scout', '', 'sess-1', 1, 1)`,
		`INSERT INTO entries (bot_id, seq, rev, id, kind, turn_id, body, created_at) VALUES ('b1', 1, 1, 'e1', 'user', 't1', '{"text":"hi"}', 1)`,
		`INSERT INTO entries (bot_id, seq, rev, id, kind, turn_id, body, created_at) VALUES ('b1', 2, 2, 'e2', 'user', 't1', '{"text":"look","images":["a.png","b.gif"]}', 2)`,
		`INSERT INTO turns (id, bot_id, source, client_nonce, prompt, status, created_at) VALUES ('t1', 'b1', 'user', 'n1', 'hi', 'done', 1)`,
	} {
		if _, err := old.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare v1 database: %v", err)
		}
	}
	old.Close()

	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open v1 database: %v", err)
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != 13 {
		t.Fatalf("user_version = %d, %v; want 13", version, err)
	}
	q := New(conn)
	bot, err := q.GetBot(ctx, "b1")
	if err != nil || bot.Kind != "agent" || bot.ClaudeSessionID != "sess-1" {
		t.Fatalf("bot after migration = %+v, %v; want kind agent and the old session", bot, err)
	}
	bots, err := q.ListBots(ctx)
	if err != nil || len(bots) != 2 || bots[0].ID != "b1" || bots[0].Position != 1 || bots[1].Position != 2 {
		t.Fatalf("bots after migration = %+v, %v; want creation order with positions 1 and 2", bots, err)
	}
	entry, err := q.GetEntry(ctx, "e1")
	if err != nil || entry.Body != `{"text":"hi"}` {
		t.Fatalf("entry after migration = %+v, %v", entry, err)
	}
	withImages, err := q.GetEntry(ctx, "e2")
	if err != nil || withImages.Body != `{"text":"look","media":["a.png","b.gif"]}` {
		t.Fatalf("entry with images after migration = %+v, %v; want the images under media", withImages, err)
	}
	turn, err := q.GetTurnByNonce(ctx, GetTurnByNonceParams{BotID: "b1", ClientNonce: sql.NullString{String: "n1", Valid: true}})
	if err != nil || turn.RoomID != "" || turn.Hops != 0 {
		t.Fatalf("turn after migration = %+v, %v; want empty room and 0 hops", turn, err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO turns (id, bot_id, source, prompt, status, room_id, created_at) VALUES ('t2', 'b1', 'room', 'x', 'queued', 'r1', 2)`); err != nil {
		t.Fatalf("new turn source rejected after migration: %v", err)
	}

	if _, err := q.InsertArtifact(ctx, InsertArtifactParams{ID: "a1", Kind: "plan", Title: "Plan", Content: "x", Links: "[]", AuthorID: "b1", CreatedAt: 1, UpdatedAt: 1}); err != nil {
		t.Fatalf("insert artifact after migration: %v", err)
	}
	if err := q.InsertArtifactTag(ctx, InsertArtifactTagParams{ArtifactID: "a1", Tag: "dev-3"}); err != nil {
		t.Fatalf("insert artifact tag after migration: %v", err)
	}
	if _, err := q.DeleteArtifact(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	if tags, err := q.ListArtifactTags(ctx, "a1"); err != nil || len(tags) != 0 {
		t.Fatalf("tags of a deleted artifact = %v, %v; want none", tags, err)
	}

	again, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	again.Close()
}
