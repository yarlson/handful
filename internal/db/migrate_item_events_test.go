package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

// A database from before open questions keeps every entry; only a question
// that shows as waiting today, the latest user or bot message of its chat,
// stays open. The new item_events table works and cascades with its item.
func TestTheItemEventsMigrationKeepsEntriesAndOpensOnlyTodaysQuestions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "yargpt.db")
	old := openAt(t, path, 13)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
		  ('ana', 'Ana', '', '', 'agent', 1, 1), ('bo', 'Bo', '', '', 'agent', 1, 1), ('cy', 'Cy', '', '', 'agent', 1, 1), ('ship', 'Ship', '', '', 'room', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id) VALUES ('ship', 'ana')`,
		`INSERT INTO entries (bot_id, seq, rev, id, kind, turn_id, body, created_at) VALUES
		  ('ana', 1, 1, 'a-old', 'bot', '', '{"text":"Old?","needsReply":true}', 1),
		  ('ana', 2, 2, 'a-user', 'user', '', '{"text":"Yes."}', 2),
		  ('ana', 3, 3, 'a-new', 'bot', '', '{"text":"New?","needsReply":true}', 3),
		  ('ana', 4, 4, 'a-tool', 'activity', '', '{"text":"ran"}', 4),
		  ('bo', 1, 1, 'b-ask', 'bot', '', '{"text":"Asked?","needsReply":true}', 1),
		  ('bo', 2, 2, 'b-user', 'user', '', '{"text":"Answer."}', 2),
		  ('cy', 1, 1, 'c-plain', 'bot', '', '{"text":"Plain.","needsReply":false}', 1),
		  ('ship', 1, 1, 's-ask', 'bot', '', '{"text":"Ship?","needsReply":true,"speakerId":"ana"}', 1)`,
		`INSERT INTO goals (id, team_id, text, state, budget, created_at) VALUES ('g1', 'ship', 'Ship it.', 'active', 30, 1)`,
		`INSERT INTO items (id, team_id, goal_id, title, brief, acceptance, state, owner_id, depends_on, created_by, position, created_at, updated_at) VALUES
		  ('i1', 'ship', 'g1', 'One', 'b', 'a', 'todo', '', '[]', 'ana', 1, 1, 1)`,
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
	want := map[string]string{"a-old": "answered", "a-new": "open", "b-ask": "answered", "s-ask": "open", "a-user": "", "a-tool": "", "c-plain": ""}
	for id, state := range want {
		e, err := q.GetEntry(ctx, id)
		if err != nil {
			t.Fatalf("entry %s: %v", id, err)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(e.Body), &body); err != nil {
			t.Fatal(err)
		}
		got, _ := body["questionState"].(string)
		if got != state || body["text"] == nil || e.Rev != e.Seq {
			t.Errorf("entry %s = %+v; want questionState %q and its fields kept", id, e, state)
		}
	}
	counts, err := q.CountOpenQuestions(ctx)
	if err != nil || len(counts) != 2 {
		t.Fatalf("open question counts = %+v, %v; want ana and ship", counts, err)
	}
	if err := q.InsertItemEvent(ctx, InsertItemEventParams{ID: "e1", ItemID: "i1", Kind: "created", ActorID: "ana", ActorName: "Ana", CreatedAt: 5}); err != nil {
		t.Fatal(err)
	}
	if err := q.InsertItemEvent(ctx, InsertItemEventParams{ID: "e2", ItemID: "i1", Kind: "renamed", ActorName: "Ana", CreatedAt: 6}); err == nil {
		t.Fatal("an event of an unknown kind was stored")
	}
	if events, err := q.ListItemEvents(ctx, "i1"); err != nil || len(events) != 1 || events[0].Kind != "created" {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if _, err := conn.ExecContext(ctx, "DELETE FROM bots WHERE id = 'ship'"); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM item_events").Scan(&left); err != nil || left != 0 {
		t.Fatalf("events of a deleted team = %d, %v; want them gone with its items", left, err)
	}
}
