package db

import (
	"context"
	"path/filepath"
	"testing"
)

// A board from before the task inbox keeps every goal, item, item event and
// turn as it was; the new columns get their defaults, and a done task no
// longer holds its team.
func TestTheTaskInboxMigrationKeepsTheBoardAndFreesDoneTasks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "yargpt.db")
	old := openAt(t, path, 15)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
		  ('ana', 'Ana', '', '', 'agent', 1, 1), ('bo', 'Bo', '', '', 'agent', 1, 1),
		  ('ship', 'Ship', '', '', 'room', 1, 1), ('docs', 'Docs', '', '', 'room', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id, request_slots, start_handoffs) VALUES ('ship', 'ana', 1, 'ask'), ('docs', 'bo', 1, 'auto')`,
		`INSERT INTO goals (id, team_id, text, state, budget, queue_pos, created_at, origin, from_team_id, from_bot_id, brief, artifact_ids, ask_rounds, asked_at, depth) VALUES
		  ('g-done', 'ship', 'Shipped.', 'delivered', 30, 0, 1, 'user', '', '', '', '[]', 0, 0, 0),
		  ('g-queued', 'ship', 'Next.', 'queued', 0, 1, 2, 'user', '', '', '', '[]', 0, 0, 0),
		  ('g-in', 'docs', 'Guide.', 'incoming', 0, 0, 3, 'handoff', 'ship', 'ana', 'Why.', '["a1"]', 2, 5, 1)`,
		`INSERT INTO items (id, team_id, goal_id, title, brief, acceptance, state, owner_id, depends_on, created_by, position, created_at, updated_at) VALUES
		  ('i-1', 'ship', 'g-done', 'Parse', 'b', 'a', 'done', 'ana', '[]', 'ana', 1, 1, 1)`,
		`INSERT INTO item_events (id, item_id, kind, actor_id, actor_name, round, text, created_at) VALUES ('e1', 'i-1', 'submitted', 'ana', 'Ana', 0, 'Done.', 2)`,
		`INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, goal_id, item_id, board_role, created_at, started_at) VALUES
		  ('t1', 'ana', 'room', 'Plan it.', 'done', 'ship', 0, 'g-done', '', 'lead', 1, 1)`,
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
	in, err := q.GetGoal(ctx, "g-in")
	want := Goal{ID: "g-in", TeamID: "docs", Text: "Guide.", State: "incoming", CreatedAt: 3, Origin: "handoff", FromTeamID: "ship", FromBotID: "ana", Brief: "Why.", ArtifactIds: `["a1"]`, AskRounds: 2, AskedAt: 5, Depth: 1}
	if err != nil || in != want {
		t.Fatalf("incoming task = %+v, %v; want %+v", in, err, want)
	}
	if done, err := q.GetGoal(ctx, "g-done"); err != nil || done.State != "delivered" || done.Budget != 30 {
		t.Fatalf("done task = %+v, %v", done, err)
	}
	if queued, err := q.GetGoal(ctx, "g-queued"); err != nil || queued.QueuePos != 1 || queued.Pinned != 0 {
		t.Fatalf("planned task = %+v, %v", queued, err)
	}
	if events, err := q.ListItemEvents(ctx, "i-1"); err != nil || len(events) != 1 || events[0].Text != "Done." {
		t.Fatalf("item events = %+v, %v", events, err)
	}
	if n, err := q.CountGoalTurns(ctx, "g-done"); err != nil || n != 1 {
		t.Fatalf("turns of the done task = %d, %v; want the old planning turn", n, err)
	}
	for _, team := range []string{"ship", "docs"} {
		if row, err := q.GetTeam(ctx, team); err != nil || row.InboxMax != 10 || row.InboxPerSender != 3 {
			t.Fatalf("team %s = %+v, %v; want the default inbox limits", team, row, err)
		}
	}
	// A done task no longer holds its team: a new one starts beside it.
	if _, err := conn.ExecContext(ctx, `INSERT INTO goals (id, team_id, text, state, budget, created_at) VALUES ('g-now', 'ship', 'Now.', 'active', 30, 4)`); err != nil {
		t.Fatalf("a Now task beside a done one: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO goals (id, team_id, text, state, budget, created_at) VALUES ('g-two', 'ship', 'Two.', 'paused', 30, 5)`); err == nil {
		t.Fatal("a second Now task was stored")
	}
	for _, stmt := range []string{
		`UPDATE goals SET state = 'later', later_until = 9 WHERE id = 'g-in'`,
		`UPDATE goals SET state = 'stopped' WHERE id = 'g-queued'`,
		`INSERT INTO goals (id, team_id, text, state, budget, created_at, origin, from_bot_id) VALUES ('g-p', 'docs', 'Tidy.', 'incoming', 0, 6, 'proposal', 'bo')`,
		`INSERT INTO goal_events (id, goal_id, kind, actor_id, actor_name, text, created_at) VALUES ('ge1', 'g-in', 'later', 'bo', 'Bo', 'Busy.', 7)`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	for _, stmt := range []string{
		`UPDATE goals SET state = 'shelved' WHERE id = 'g-in'`,
		`UPDATE goals SET origin = 'robot' WHERE id = 'g-in'`,
		`INSERT INTO goal_events (id, goal_id, kind, actor_id, actor_name, created_at) VALUES ('ge2', 'g-in', 'sneezed', '', '', 1)`,
	} {
		if _, err := conn.ExecContext(ctx, stmt); err == nil {
			t.Fatalf("%s was stored", stmt)
		}
	}
	// The foreign keys still cascade: a deleted team takes its tasks, steps and events.
	if _, err := conn.ExecContext(ctx, "DELETE FROM bots WHERE id IN ('ship', 'docs')"); err != nil {
		t.Fatal(err)
	}
	var goals, items, events, goalEvents int
	if err := conn.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM goals), (SELECT COUNT(*) FROM items), (SELECT COUNT(*) FROM item_events), (SELECT COUNT(*) FROM goal_events)`).Scan(&goals, &items, &events, &goalEvents); err != nil || goals+items+events+goalEvents != 0 {
		t.Fatalf("after deleting the teams: goals %d, items %d, events %d, task events %d, %v; want none", goals, items, events, goalEvents, err)
	}
}
