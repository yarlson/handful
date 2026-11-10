package agent

import (
	"context"
	"testing"

	"yargpt/internal/core"
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
