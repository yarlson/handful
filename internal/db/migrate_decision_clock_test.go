package db

import (
	"context"
	"path/filepath"
	"testing"
)

// A board from before the decision clock keeps its tasks and their history;
// every task starts with no time counted, every team with 30 minutes to
// decide, and a missed decision is an event a task can have.
func TestTheDecisionClockMigrationKeepsTasksAndTheirHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "yargpt.db")
	old := openAt(t, path, 16)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
		  ('ana', 'Ana', '', '', 'agent', 1, 1), ('ship', 'Ship', '', '', 'room', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id, request_slots, start_handoffs) VALUES ('ship', 'ana', 1, 'auto')`,
		`INSERT INTO goals (id, team_id, text, state, budget, created_at, ask_rounds, asked_at) VALUES ('g-in', 'ship', 'Guide.', 'incoming', 0, 3, 1, 5)`,
		`INSERT INTO goal_events (id, goal_id, kind, actor_id, actor_name, text, created_at) VALUES ('ge1', 'g-in', 'asked', 'ana', 'Ana', 'Which parts?', 4)`,
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
	g, err := q.GetGoal(ctx, "g-in")
	if err != nil || g.Text != "Guide." || g.AskRounds != 1 || g.AskedAt != 5 || g.DecisionMs != 0 || g.DecisionTick != 0 || g.MissedAt != 0 {
		t.Fatalf("task = %+v, %v; want it as it was, with no decision time counted", g, err)
	}
	if events, err := q.ListGoalEvents(ctx, "g-in"); err != nil || len(events) != 1 || events[0].Text != "Which parts?" || events[0].Kind != "asked" {
		t.Fatalf("events = %+v, %v; want the old event", events, err)
	}
	if team, err := q.GetTeam(ctx, "ship"); err != nil || team.DecisionMinutes != 30 {
		t.Fatalf("team = %+v, %v; want 30 minutes to decide", team, err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO goal_events (id, goal_id, kind, actor_id, actor_name, text, created_at) VALUES ('ge2', 'g-in', 'missed', '', 'YarGPT', 'Late.', 6)`); err != nil {
		t.Fatalf("a missed event: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO goal_events (id, goal_id, kind, actor_id, actor_name, created_at) VALUES ('ge3', 'g-in', 'sneezed', '', '', 7)`); err == nil {
		t.Fatal("an unknown event kind was stored")
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM goals WHERE id = 'g-in'`); err != nil {
		t.Fatal(err)
	}
	if events, err := q.ListGoalEvents(ctx, "g-in"); err != nil || len(events) != 0 {
		t.Fatalf("events of a deleted task = %+v, %v; want them gone with it", events, err)
	}
}
