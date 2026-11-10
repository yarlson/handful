package db

import (
	"context"
	"path/filepath"
	"testing"
)

// A board from before handoffs keeps every goal, item and item event as it
// was: every goal comes from the user, open requests stay open, and one team
// still has at most one open goal.
func TestTheHandoffMigrationKeepsTheBoardAsItWas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "yargpt.db")
	old := openAt(t, path, 14)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
		  ('ana', 'Ana', '', '', 'agent', 1, 1), ('bo', 'Bo', '', '', 'agent', 1, 1),
		  ('ship', 'Ship', '', '', 'room', 1, 1), ('docs', 'Docs', '', '', 'room', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id, request_slots) VALUES ('ship', 'ana', 2), ('docs', 'bo', 1)`,
		`INSERT INTO goals (id, team_id, text, state, budget, queue_pos, created_at) VALUES
		  ('g-active', 'ship', 'Ship it.', 'active', 30, 0, 1), ('g-queued', 'ship', 'Next.', 'queued', 0, 1, 2),
		  ('g-done', 'ship', 'Old.', 'done', 30, 0, 0), ('g-ended', 'docs', 'Gone.', 'ended', 20, 0, 1),
		  ('g-docs', 'docs', 'Docs.', 'paused', 20, 0, 3)`,
		`INSERT INTO items (id, team_id, goal_id, title, brief, acceptance, state, owner_id, depends_on, created_by, position, created_at, updated_at, intake, intake_note, slot_at, for_item_id) VALUES
		  ('i-own', 'ship', 'g-active', 'Own', 'b', 'a', 'todo', 'ana', '["i-req"]', 'ana', 1, 1, 1, '', '', 0, ''),
		  ('i-req', 'docs', 'g-active', 'Request', 'b', 'a', 'doing', 'bo', '[]', 'ana', 2, 1, 2, 'accepted', 'Taken.', 2, ''),
		  ('i-chain', 'ship', 'g-docs', 'Chain', 'b', 'a', 'todo', '', '[]', 'bo', 1, 1, 1, 'requested', '', 0, 'i-req')`,
		`INSERT INTO item_events (id, item_id, kind, actor_id, actor_name, round, text, created_at) VALUES
		  ('e1', 'i-own', 'created', 'ana', 'Ana', 0, '', 1), ('e2', 'i-req', 'changes', 'bo', 'Bo', 1, 'Fix it.', 2)`,
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
	for id, want := range map[string]Goal{
		"g-active": {TeamID: "ship", Text: "Ship it.", State: "active", Budget: 30, CreatedAt: 1},
		"g-queued": {TeamID: "ship", Text: "Next.", State: "queued", QueuePos: 1, CreatedAt: 2},
		"g-done":   {TeamID: "ship", Text: "Old.", State: "done", Budget: 30},
		"g-ended":  {TeamID: "docs", Text: "Gone.", State: "ended", Budget: 20, CreatedAt: 1},
		"g-docs":   {TeamID: "docs", Text: "Docs.", State: "paused", Budget: 20, CreatedAt: 3},
	} {
		got, err := q.GetGoal(ctx, id)
		want.ID, want.Origin, want.ArtifactIds = id, "user", "[]"
		if err != nil || got != want {
			t.Errorf("goal %s = %+v, %v; want %+v", id, got, err, want)
		}
	}
	req, err := q.GetItem(ctx, "i-req")
	if err != nil || req.State != "doing" || req.Intake != "accepted" || req.IntakeNote != "Taken." || req.SlotAt != 2 || req.OwnerID != "bo" {
		t.Fatalf("open request = %+v, %v; want it as it was", req, err)
	}
	own, err := q.GetItem(ctx, "i-own")
	if err != nil || own.DependsOn != `["i-req"]` {
		t.Fatalf("own item = %+v, %v; want its dependencies", own, err)
	}
	if chain, err := q.GetItem(ctx, "i-chain"); err != nil || chain.ForItemID != "i-req" || chain.Intake != "requested" {
		t.Fatalf("chained request = %+v, %v", chain, err)
	}
	if events, err := q.ListItemEvents(ctx, "i-req"); err != nil || len(events) != 1 || events[0].Text != "Fix it." || events[0].Round != 1 {
		t.Fatalf("events of the request = %+v, %v", events, err)
	}
	for _, team := range []string{"ship", "docs"} {
		if row, err := q.GetTeam(ctx, team); err != nil || row.StartHandoffs != "auto" {
			t.Fatalf("team %s = %+v, %v; want handoffs that start on their own", team, row, err)
		}
	}
	if latest, err := q.GetLatestGoal(ctx, "ship"); err != nil || latest.ID != "g-active" {
		t.Fatalf("Ship's latest goal = %+v, %v", latest, err)
	}
	// One open goal per team still holds, and a handoff never counts as open.
	if _, err := conn.ExecContext(ctx, `INSERT INTO goals (id, team_id, text, state, budget, created_at) VALUES ('g-two', 'ship', 'Two.', 'active', 30, 4)`); err == nil {
		t.Fatal("a second open goal was stored")
	}
	if _, err := q.InsertTask(ctx, InsertTaskParams{ID: "h1", TeamID: "ship", Text: "Handed.", State: "incoming", CreatedAt: 5, Origin: "handoff", FromTeamID: "docs", FromBotID: "bo", ArtifactIds: "[]", Depth: 1}); err != nil {
		t.Fatalf("hand over to a team with an open goal: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE goals SET state = 'shelved' WHERE id = 'h1'`); err == nil {
		t.Fatal("a goal of an unknown state was stored")
	}
	if _, err := conn.ExecContext(ctx, `UPDATE teams SET start_handoffs = 'never' WHERE team_id = 'ship'`); err == nil {
		t.Fatal("an unknown start_handoffs was stored")
	}
	// The foreign keys still cascade: a deleted team takes its goals, items and events.
	if _, err := conn.ExecContext(ctx, "DELETE FROM bots WHERE id = 'ship'"); err != nil {
		t.Fatal(err)
	}
	var goals, items, events int
	if err := conn.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM goals WHERE team_id = 'ship'), (SELECT COUNT(*) FROM items WHERE goal_id = 'g-active'), (SELECT COUNT(*) FROM item_events)`).Scan(&goals, &items, &events); err != nil || goals != 0 || items != 0 || events != 0 {
		t.Fatalf("after deleting Ship: goals %d, items %d, events %d, %v; want none", goals, items, events, err)
	}
}
