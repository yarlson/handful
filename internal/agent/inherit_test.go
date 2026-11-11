package agent

import (
	"context"
	"net/http/httptest"
	"testing"

	"handful/internal/core"
)

// AC13, end to end: a step's owner starts a background task in its work
// turn; when the task reports, the turn that Claude Code starts works on the
// same task and step.
func TestATaskReportTurnWorksOnTheStepThatStartedTheTask(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "task-report")
	ctx := context.Background()
	team, _, bo, _ := h.newTeam(t)
	items := h.newGoal(t, team, boardItem("Build", "Build the site.", bo.ID, ""))
	board, err := h.svc.Board(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.start(t)
	waitFor(t, "Bo's work turn starts the task", func() bool { return len(h.runner.Tasks(team.ID)) == 1 })
	if _, _, err := h.svc.SubmitUserMessage(ctx, team.ID, "@Bo is the build done?", "n1"); err != nil {
		t.Fatal(err)
	}
	var goalID, itemID string
	waitFor(t, "the task turn", func() bool {
		err := h.conn.QueryRowContext(ctx, `SELECT goal_id, item_id FROM turns WHERE bot_id = ? AND source = 'task'`, bo.ID).Scan(&goalID, &itemID)
		return err == nil
	})
	if goalID != board.Goal.ID || itemID != items[0].ID {
		t.Fatalf("task turn works on %q, %q; want the task %q and the step %q", goalID, itemID, board.Goal.ID, items[0].ID)
	}
}

// AC13 through the tools: a mention that a step's work turn posts in the
// team's channel with send_to_agent passes the task on, as send_to_user does.
func TestAMentionSentToTheTeamWithSendToAgentCarriesTheTask(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, bo, _ := h.newTeam(t)
	h.newGoal(t, team, boardItem("Build", "Build the site.", bo.ID, ""))
	board, err := h.svc.Board(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	var workID string
	if err := h.conn.QueryRowContext(ctx, `SELECT id FROM turns WHERE bot_id = ? AND board_role = 'work'`, bo.ID).Scan(&workID); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	token, err := h.runner.tokens.issue(runContext{BotID: bo.ID, BotName: "Bo", TurnID: workID, RoomID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcpSession(t, srv.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	if text, isErr := callText(t, cs, "send_to_agent", map[string]any{"target_id": team.ID, "message": "@Ana which format?", "priority": false}); isErr {
		t.Fatalf("send_to_agent to the team = %q; want it posted", text)
	}
	var goalID, role string
	if err := h.conn.QueryRowContext(ctx, `SELECT goal_id, board_role FROM turns WHERE bot_id = ? AND prompt = 'Bo mentioned you.'`, ana.ID).Scan(&goalID, &role); err != nil {
		t.Fatal(err)
	}
	if goalID != board.Goal.ID || role != "" {
		t.Fatalf("Ana's turn works on %q with role %q; want the task %q as a conversation turn", goalID, role, board.Goal.ID)
	}
	// A post in another room isn't the posting turn's reply, so a direct
	// chat turn still delivers its final text.
	own, err := h.runner.tokens.issue(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "own"})
	if err != nil {
		t.Fatal(err)
	}
	direct, err := mcpSession(t, srv.URL, own)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { direct.Close() })
	if text, isErr := callText(t, direct, "send_to_agent", map[string]any{"target_id": team.ID, "message": "Notes are in the doc.", "priority": false}); isErr {
		t.Fatalf("send_to_agent from Ana's own chat = %q; want it posted", text)
	}
	if n, err := h.svc.TurnReplyCount(ctx, "own"); err != nil || n != 0 {
		t.Fatalf("replies of Ana's own-chat turn = %d, %v; want 0", n, err)
	}
}

// The turn that starts a new background task is the one a later task turn
// works for; a change that starts no task keeps it.
func TestTheTurnThatStartsABackgroundTaskIsRecorded(t *testing.T) {
	t.Parallel()
	r := newHarness(t, "reply").runner
	s := &session{key: sessionKey{"b", "room"}, bot: core.Bot{ID: "b", Name: "Bo"}, open: true, pending: map[string]*command{}}
	report := func(turnID string, ids ...string) {
		t.Helper()
		token, err := r.tokens.issue(runContext{BotID: "b", TurnID: turnID})
		if err != nil {
			t.Fatal(err)
		}
		s.token = token
		list := make([]streamTask, len(ids))
		for i, id := range ids {
			list[i] = streamTask{TaskID: id}
		}
		r.setTasks(s, list)
	}
	report("start", "t1")
	report("later", "t1")
	if s.taskTurnID != "start" {
		t.Fatalf("after a change with no new task the turn is %q; want start", s.taskTurnID)
	}
	report("second", "t1", "t2")
	if s.taskTurnID != "second" {
		t.Fatalf("after a new task the turn is %q; want second", s.taskTurnID)
	}
	if startsTask([]core.BackgroundTask{{ID: "a"}}, nil) || startsTask(nil, nil) || !startsTask(nil, []core.BackgroundTask{{ID: "a"}}) {
		t.Fatal("startsTask is wrong")
	}
}

// A conversation turn that inherits a task names it in its message, with
// its step; a board turn's brief names its work instead.
func TestAnInheritedTurnNamesItsTaskAndStep(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, _, bo, _ := h.newTeam(t)
	items := h.newGoal(t, team, boardItem("Parse", "Parse it.", bo.ID, ""))
	board, _ := h.svc.Board(ctx, team.ID)
	for _, c := range []struct {
		turn core.Turn
		want string
	}{
		{core.Turn{GoalID: board.Goal.ID, ItemID: items[0].ID}, "\nThis turn is on the task \"Ship CSV export.\" (task id " + board.Goal.ID + "), step \"Parse\" (id " + items[0].ID + ").\n"},
		{core.Turn{GoalID: board.Goal.ID}, "\nThis turn is on the task \"Ship CSV export.\" (task id " + board.Goal.ID + ").\n"},
		{core.Turn{GoalID: board.Goal.ID, ItemID: items[0].ID, BoardRole: core.RoleWork}, ""},
		{core.Turn{}, ""},
	} {
		got, err := h.runner.inheritedTask(ctx, c.turn)
		if err != nil || got != c.want {
			t.Errorf("inheritedTask(%+v) = %q, %v; want %q", c.turn, got, err, c.want)
		}
	}
}

// An inherited conversation turn still waits for the room's chat and isn't
// board work; a step's work turn runs beside the chat.
func TestInheritedTurnsAreConversationForTheRunner(t *testing.T) {
	t.Parallel()
	r := newHarness(t, "reply").runner
	chat := map[string]runningTurn{"bo": {roomID: "team"}}
	inherited := core.Turn{ID: "i", BotID: "cy", RoomID: "team", GoalID: "g", ItemID: "s"}
	work := core.Turn{ID: "w", BotID: "cy", RoomID: "team", GoalID: "g", ItemID: "s", BoardRole: core.RoleWork}
	r.mu.Lock()
	r.running = chat
	waits, runs := r.mayStartLocked(context.Background(), inherited), r.chatBusyLocked("team")
	r.running = map[string]runningTurn{"bo": {roomID: "team", work: true, board: true, goalID: "g"}}
	stepBusy := r.chatBusyLocked("team")
	noop := func() {}
	r.running = map[string]runningTurn{"bo": {roomID: "team", goalID: "g", cancel: noop}, "ana": {roomID: "x", goalID: "g", board: true, cancel: noop}}
	r.mu.Unlock()
	if waits || !runs || stepBusy {
		t.Fatalf("inherited turn may start = %v (want false), chat busy = %v (want true), step work holds chat = %v (want false)", waits, runs, stepBusy)
	}
	if boardTurn(inherited) || !stepTurn(work) || stepTurn(inherited) {
		t.Fatal("an inherited turn must be conversation, a work turn a step turn")
	}
	if n := r.CancelGoal("g"); n != 1 {
		t.Fatalf("CancelGoal stopped %d turns; want only the board turn", n)
	}
}
