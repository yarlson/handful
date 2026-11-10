package db

import (
	"context"
	"maps"
	"path/filepath"
	"testing"
	"time"
)

// requestTask is the task that a request became, as stored.
type requestTask struct {
	ID, TeamID, Text, State, FromTeamID, FromBotID, FromGoalID, Brief, IntakeNote string
	Budget, QueuePos, Depth, LaterUntil                                           int64
}

func taskWaitingOn(t *testing.T, ctx context.Context, q *Queries, itemID string) requestTask {
	t.Helper()
	var r requestTask
	err := q.db.QueryRowContext(ctx, `SELECT id, team_id, text, state, from_team_id, from_bot_id, from_goal_id, brief, intake_note, budget, queue_pos, depth, later_until
		FROM goals WHERE waits_on_item_id = ?`, itemID).
		Scan(&r.ID, &r.TeamID, &r.Text, &r.State, &r.FromTeamID, &r.FromBotID, &r.FromGoalID, &r.Brief, &r.IntakeNote, &r.Budget, &r.QueuePos, &r.Depth, &r.LaterUntil)
	if err != nil {
		t.Fatalf("the task that waits on %s: %v", itemID, err)
	}
	return r
}

// Every open request of a task that is not over becomes a task of the
// serving team, sent by the asking team and linked to the request item,
// which stays a step of the asking task. The plans keep their order, and
// the requests of tasks that are over stay as they were.
func TestTheOneTaskKindMigrationMakesEveryOpenRequestALinkedTask(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "yargpt.db")
	old := openAt(t, path, 18)
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at) VALUES
		  ('ana', 'Ana', '', '', 'agent', 1, 1), ('bo', 'Bo', '', '', 'agent', 1, 1), ('cy', 'Cy', '', '', 'agent', 1, 1),
		  ('ship', 'Ship', '', '', 'room', 1, 1), ('docs', 'Docs', '', '', 'room', 1, 1), ('ops', 'Ops', '', '', 'room', 1, 1)`,
		`INSERT INTO teams (team_id, lead_bot_id, goal_budget) VALUES ('ship', 'ana', 30), ('docs', 'bo', 40), ('ops', 'cy', 50)`,
		`INSERT INTO room_members (room_id, bot_id) VALUES ('ship', 'ana'), ('docs', 'bo'), ('ops', 'cy')`,
		`INSERT INTO goals (id, team_id, text, state, budget, queue_pos, created_at, depth) VALUES
		  ('g-ship', 'ship', 'Ship it.', 'active', 30, 0, 1, 0),
		  ('g-next', 'ship', 'Next.', 'queued', 0, 1, 2, 0),
		  ('g-last', 'ship', 'Last.', 'queued', 0, 2, 3, 0),
		  ('g-ops', 'ops', 'Run.', 'active', 50, 0, 4, 0),
		  ('g-ops-next', 'ops', 'Then.', 'queued', 0, 1, 5, 0),
		  ('g-old', 'ship', 'Old.', 'ended', 30, 0, 0, 0),
		  ('g-stop', 'ship', 'Alpha.', 'stopped', 30, 0, 6, 0),
		  ('g-back', 'ship', 'Back.', 'incoming', 0, 0, 7, 0)`,
		`INSERT INTO items (id, team_id, goal_id, title, brief, acceptance, state, owner_id, reviewer_id, depends_on, note, created_by, position, created_at, updated_at,
		  intake, intake_note, defer_revisit, defer_until, slot_at, jumped_at, for_item_id) VALUES
		  ('r-ask', 'docs', 'g-ship', 'Write the guide', 'For users.', 'A page.', 'todo', '', '', '[]', '', 'ana', 1, 10, 10, 'requested', '', '', 0, 0, 0, ''),
		  ('r-work', 'docs', 'g-ship', 'Fix the index', 'b', '', 'doing', 'bo', 'bo', '[]', 'Half done.', 'ana', 2, 11, 12, 'accepted', '', '', 0, 20, 0, ''),
		  ('r-wait', 'docs', 'g-ship', 'Add a FAQ', 'b', '', 'todo', 'bo', '', '[]', '', 'ana', 3, 9, 13, 'accepted', '', '', 0, 0, 0, ''),
		  ('r-ops', 'ops', 'g-ship', 'Open a port', 'b', '', 'todo', 'cy', '', '[]', '', 'ana', 4, 12, 14, 'accepted', '', '', 0, 0, 0, ''),
		  ('r-later', 'docs', 'g-ship', 'Translate', 'b', '', 'todo', '', '', '[]', '', 'ana', 5, 13, 15, 'deferred', 'After the release.', 'goal_end', 0, 0, 0, ''),
		  ('r-date', 'docs', 'g-ship', 'Review copy', 'b', '', 'todo', '', '', '[]', '', 'ana', 6, 14, 16, 'deferred', 'Monday.', 'date', 5000, 0, 0, ''),
		  ('r-no', 'docs', 'g-ship', 'Rewrite all', 'b', '', 'dropped', '', '', '[]', '', 'ana', 7, 15, 17, 'declined', 'Too big.', '', 0, 0, 0, ''),
		  ('r-chain', 'ops', 'g-ship', 'Host the index', 'b', '', 'todo', '', '', '[]', '', 'bo', 8, 16, 18, 'requested', '', '', 0, 0, 0, 'r-work'),
		  ('r-done', 'docs', 'g-ship', 'Spell check', 'b', '', 'done', 'bo', '', '[]', '', 'ana', 9, 17, 19, 'accepted', '', '', 0, 0, 0, ''),
		  ('i-own', 'ship', 'g-ship', 'Release', 'b', '', 'todo', 'ana', '', '["r-ask"]', '', 'ana', 10, 18, 18, '', '', '', 0, 0, 0, ''),
		  ('r-old', 'docs', 'g-old', 'Old ask', 'b', '', 'todo', 'bo', '', '[]', '', 'ana', 1, 1, 1, 'accepted', '', '', 0, 0, 0, ''),
		  ('r-stop', 'ops', 'g-stop', 'Open a gate', 'b', '', 'doing', 'cy', '', '[]', '', 'ana', 1, 20, 21, 'accepted', '', '', 0, 0, 0, ''),
		  ('r-back', 'docs', 'g-back', 'Write a note', 'b', '', 'todo', '', '', '[]', '', 'ana', 1, 21, 22, 'requested', '', '', 0, 0, 0, '')`,
		`INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, goal_id, item_id, board_role, created_at) VALUES
		  ('t-work', 'bo', 'room', 'Work on your item again.', 'queued', 'docs', 0, 'g-ship', 'r-work', 'work', 30)`,
	} {
		if _, err := old.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("prepare the database: %v", err)
		}
	}
	old.Close()

	before := time.Now().Add(-time.Second).UnixMilli()
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	q := New(conn)
	week := int64(7 * 24 * time.Hour / time.Millisecond)

	ask := taskWaitingOn(t, ctx, q, "r-ask")
	if want := (requestTask{
		ID: ask.ID, TeamID: "docs", Text: "Write the guide", State: "incoming", FromTeamID: "ship", FromBotID: "ana", FromGoalID: "g-ship",
		Brief: "For users.\n\nDone when: A page.", Depth: 1,
	}); ask != want {
		t.Fatalf("requested = %+v; want %+v", ask, want)
	}
	// Docs has no Now task and no plan: the request that worked in a slot is
	// Now, the one that waited for a slot is next.
	if work := taskWaitingOn(t, ctx, q, "r-work"); work.State != "active" || work.Budget != 40 || work.QueuePos != 0 {
		t.Fatalf("accepted in a slot = %+v; want docs' Now task with its goal budget", work)
	}
	if wait := taskWaitingOn(t, ctx, q, "r-wait"); wait.State != "queued" || wait.QueuePos != 1 || wait.Budget != 0 {
		t.Fatalf("accepted, waiting = %+v; want #1 in docs' plan", wait)
	}
	// Ops works on its own task: the request goes after its plan.
	if ops := taskWaitingOn(t, ctx, q, "r-ops"); ops.TeamID != "ops" || ops.State != "queued" || ops.QueuePos != 2 {
		t.Fatalf("accepted by a busy team = %+v; want #2 in ops' plan", ops)
	}
	// A stopped task, and one back in its team's inbox, can start again: their
	// requests become tasks too.
	if stop := taskWaitingOn(t, ctx, q, "r-stop"); stop.TeamID != "ops" || stop.State != "queued" || stop.QueuePos != 3 || stop.FromGoalID != "g-stop" {
		t.Fatalf("accepted for a stopped task = %+v; want #3 in ops' plan, for g-stop", stop)
	}
	back := taskWaitingOn(t, ctx, q, "r-back")
	if back.TeamID != "docs" || back.State != "incoming" || back.FromGoalID != "g-back" {
		t.Fatalf("requested for a task in the inbox = %+v; want in docs' inbox, for g-back", back)
	}
	later := taskWaitingOn(t, ctx, q, "r-later")
	if later.State != "later" || later.IntakeNote != "After the release." || later.LaterUntil < before+week || later.LaterUntil > time.Now().UnixMilli()+week {
		t.Fatalf("deferred to a goal's end = %+v; want later for 7 days from the migration", later)
	}
	if date := taskWaitingOn(t, ctx, q, "r-date"); date.State != "later" || date.LaterUntil != 5000 {
		t.Fatalf("deferred to a date = %+v; want later until that date", date)
	}
	if no := taskWaitingOn(t, ctx, q, "r-no"); no.State != "declined" || no.IntakeNote != "Too big." {
		t.Fatalf("declined = %+v; want pushed back with its note", no)
	}

	var chain requestTask
	if err := conn.QueryRowContext(ctx, `SELECT team_id, state, from_team_id, from_bot_id, from_goal_id, depth FROM goals WHERE text = 'Host the index'`).
		Scan(&chain.TeamID, &chain.State, &chain.FromTeamID, &chain.FromBotID, &chain.FromGoalID, &chain.Depth); err != nil {
		t.Fatal(err)
	}
	if work := taskWaitingOn(t, ctx, q, "r-work"); chain != (requestTask{TeamID: "ops", State: "incoming", FromTeamID: "docs", FromBotID: "bo", FromGoalID: work.ID, Depth: 2}) {
		t.Fatalf("chained = %+v; want sent by docs for the task %s", chain, work.ID)
	}

	for _, id := range []string{"r-ask", "r-work", "r-wait", "r-ops", "r-later", "r-date", "r-stop", "r-back"} {
		it, err := q.GetItem(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if (it.TeamID != "ship" && id != "r-back") || it.State != "todo" || it.OwnerID != "" || it.ReviewerID != "" || it.Note != "" || it.Intake != "" || it.SlotAt != 0 || it.DeferRevisit != "" {
			t.Fatalf("request %s = %+v; want a step of ship's task without an owner", id, it)
		}
	}
	if it, err := q.GetItem(ctx, "r-chain"); err != nil || it.State != "dropped" || it.Intake != "withdrawn" || it.IntakeNote != "It became a task of team Ops." {
		t.Fatalf("chained item = %+v, %v; want it gone from ship's task", it, err)
	}
	for id, want := range map[string]string{"r-no": "declined", "r-done": "accepted", "r-old": "accepted"} {
		if it, err := q.GetItem(ctx, id); err != nil || it.Intake != want || it.TeamID != "docs" {
			t.Fatalf("item %s = %+v, %v; want it as it was", id, it, err)
		}
	}
	if it, err := q.GetItem(ctx, "i-own"); err != nil || it.DependsOn != `["r-ask"]` {
		t.Fatalf("own step = %+v, %v; want it still to wait on the request", it, err)
	}
	var tasksOfOld int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM goals WHERE from_goal_id = 'g-old'`).Scan(&tasksOfOld); err != nil || tasksOfOld != 0 {
		t.Fatalf("tasks from the ended goal = %d, %v; want none", tasksOfOld, err)
	}
	for id, want := range map[string]int64{"g-next": 1, "g-last": 2, "g-ops-next": 1} {
		if g, err := q.GetGoal(ctx, id); err != nil || g.State != "queued" || g.QueuePos != want {
			t.Fatalf("planned %s = %+v, %v; want #%d", id, g, err, want)
		}
	}

	var status string
	if err := conn.QueryRowContext(ctx, `SELECT status FROM turns WHERE id = 't-work'`).Scan(&status); err != nil || status != "interrupted" {
		t.Fatalf("queued turn of a converted request = %q, %v; want interrupted", status, err)
	}
	// Every task that arrives in an inbox wakes its team's lead to decide on
	// it, and no other task does.
	var chainID string
	if err := conn.QueryRowContext(ctx, `SELECT id FROM goals WHERE text = 'Host the index'`).Scan(&chainID); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.QueryContext(ctx, `SELECT goal_id, bot_id, room_id, board_role, status, prompt FROM turns WHERE decision = 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	decisions := map[string]string{}
	prompts := map[string]string{}
	for rows.Next() {
		var goal, bot, room, role, st, prompt string
		if err := rows.Scan(&goal, &bot, &room, &role, &st, &prompt); err != nil {
			t.Fatal(err)
		}
		decisions[goal] += bot + " " + room + " " + role + " " + st + ";"
		prompts[goal] = prompt
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	wantDecisions := map[string]string{ask.ID: "bo docs lead queued;", back.ID: "bo docs lead queued;", chainID: "cy ops lead queued;"}
	if !maps.Equal(decisions, wantDecisions) {
		t.Fatalf("decision turns = %q; want %q", decisions, wantDecisions)
	}
	if want := `Team Ship sent your team a task: "Write the guide". It was a request before requests became tasks. ` +
		`Take it into your plan, ask its sender, defer it with later, pass it on, or push it back.`; prompts[ask.ID] != want {
		t.Fatalf("decision prompt = %q; want %q", prompts[ask.ID], want)
	}
	if want := `Team Docs sent your team a task: "Host the index". It was a request before requests became tasks. ` +
		`Take it into your plan, ask its sender, defer it with later, pass it on, or push it back.`; prompts[chainID] != want {
		t.Fatalf("decision prompt of the chained task = %q; want %q", prompts[chainID], want)
	}

	events, err := q.ListGoalEvents(ctx, taskWaitingOn(t, ctx, q, "r-no").ID)
	if err != nil || len(events) != 2 || events[0].Kind != "sent" || events[0].ActorName != "Ana" || events[1].Kind != "pushed_back" || events[1].Text != "Too big." {
		t.Fatalf("history of the declined task = %+v, %v; want sent by Ana, then pushed back", events, err)
	}
}
