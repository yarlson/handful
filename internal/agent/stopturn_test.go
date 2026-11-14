package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

// stdinLog is what the turn-stop fake read on stdin besides messages: the interrupt lines.
func (h *harness) stdinLog(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.dir, "stdin.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func (h *harness) pid(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

// waitTaskTurn waits until the process of key runs a task turn.
func (h *harness) waitTaskTurn(t *testing.T, key sessionKey) {
	t.Helper()
	waitFor(t, "a task turn runs", func() bool {
		h.runner.mu.Lock()
		defer h.runner.mu.Unlock()
		s := h.runner.sessions[key]
		return s != nil && s.waking
	})
}

// waitMessageTurnGone waits until no message turn of botID is in the running list. A message
// turn before a task turn leaves the list a moment after it ends; until then StopTurn finds it too.
func (h *harness) waitMessageTurnGone(t *testing.T, botID string) {
	t.Helper()
	waitFor(t, "the message turn leaves the running list", func() bool {
		h.runner.mu.Lock()
		defer h.runner.mu.Unlock()
		_, ok := h.runner.running[botID]
		return !ok
	})
}

// #37: Stop turn ends only the running turn; the process and its background task go on, the
// next message goes to the same process, and the task's report still wakes the bot.
func TestStopTurnEndsOnlyTheTurnAndTheBackgroundTaskGoesOn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "turn-stop")
	bot := h.newBot(t)
	h.start(t)
	ctx := context.Background()
	turn, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "build the site and HANG", "n1")
	if err != nil {
		t.Fatal(err)
	}
	h.waitEntry(t, bot.ID, activityOf("Bash", "running"))
	if !h.runner.StopTurn(bot.ID) {
		t.Fatal("StopTurn returned false for a running turn")
	}
	h.waitTurnStatus(t, turn, bot.ID, "interrupted")
	note := h.waitEntry(t, bot.ID, isEvent("interrupted"))
	if _, msg := h.turnRow(t, turn); msg != "stopped by the user" || bodyField(note, "text") != "Stopped this turn." {
		t.Fatalf("turn error %q, note %q; want stopped by the user and Stopped this turn.", msg, bodyField(note, "text"))
	}
	h.waitEntry(t, bot.ID, activityOf("Bash", "error"))
	if stop := h.stdinLog(t); !strings.Contains(stop, `"subtype":"interrupt","cancel_queued":true`) {
		t.Fatalf("stdin after the stop = %q; want an interrupt that also drops the turn's queued message", stop)
	}
	want := []core.BackgroundTask{{ID: "t1", Description: "Build the site"}}
	if got := h.runner.Tasks(bot.ID); !reflect.DeepEqual(got, want) || !processAlive(h.pid(t)) {
		t.Fatalf("after the stop: tasks %+v, process alive %v; want %+v and the process alive", got, processAlive(h.pid(t)), want)
	}
	if h.runner.StopTurn(bot.ID) {
		t.Fatal("a second StopTurn returned true; want false, with no turn running")
	}

	next, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "FINISH", "n2")
	if err != nil {
		t.Fatal(err)
	}
	h.waitTurnStatus(t, next, bot.ID, "done")
	h.waitEntry(t, bot.ID, func(e core.Entry) bool { return e.Kind == core.KindBot && bodyField(e, "text") == "pong" })
	h.waitEntry(t, bot.ID, isEvent("task"))
	waitFor(t, "the process exits", func() bool { return !h.runner.HasSession(bot.ID) })
	if calls := h.argsLog(t); len(calls) != 1 {
		t.Fatalf("claude started %d times; want once, with the next message in the same process", len(calls))
	}
}

// #37: Stop turn ends a task turn, the bot's wake for a background task, without the process;
// the process outlives the stop's fallback wait, the next message goes to the same process, and
// the task's report still comes.
func TestStopTurnEndsATaskTurnAndKeepsTheProcess(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "turn-stop")
	h.runner.stopWait = 100 * time.Millisecond
	bot := h.newBot(t)
	h.start(t)
	ctx := context.Background()
	h.startBackgroundTask(t, bot)
	wake, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "WAKE", "n2")
	if err != nil {
		t.Fatal(err)
	}
	h.waitTurnStatus(t, wake, bot.ID, "done")
	h.waitTaskTurn(t, sessionKey{botID: bot.ID})
	if !h.runner.StopTurn(bot.ID) {
		t.Fatal("StopTurn returned false for a running task turn")
	}
	note := h.waitEntry(t, bot.ID, func(e core.Entry) bool { return isEvent("interrupted")(e) && e.TurnID != wake })
	h.waitTurnStatus(t, note.TurnID, bot.ID, "interrupted")
	if status, msg := h.turnRow(t, note.TurnID); status != "interrupted" || msg != "stopped by the user" || bodyField(note, "text") != "Stopped this turn." {
		t.Fatalf("task turn = %q %q, note %q; want interrupted, stopped by the user, Stopped this turn.", status, msg, bodyField(note, "text"))
	}
	if stop := h.stdinLog(t); !strings.Contains(stop, `"subtype":"interrupt","cancel_queued":false`) {
		t.Fatalf("stdin after the stop = %q; want an interrupt that keeps queued messages", stop)
	}
	waitFor(t, "the task turn ends", func() bool { return !h.runner.Running(bot.ID) })
	// The stop's fallback timer runs out during this wait; the turn it watched has ended.
	time.Sleep(3 * h.runner.stopWait)
	if !h.runner.HasSession(bot.ID) || !processAlive(h.pid(t)) {
		t.Fatalf("the process ended %v after the task turn stopped; want it to go on for its background task", 3*h.runner.stopWait)
	}
	next, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "FINISH", "n3")
	if err != nil {
		t.Fatal(err)
	}
	h.waitTurnStatus(t, next, bot.ID, "done")
	h.waitEntry(t, bot.ID, isEvent("task"))
	if calls := h.argsLog(t); len(calls) != 1 {
		t.Fatalf("claude started %d times; want once", len(calls))
	}
}

// #37: a stopped task turn leaves no mark on the process: Stop turn stops the next task turn too.
func TestStopTurnStopsTheNextTaskTurnToo(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "turn-stop")
	bot := h.newBot(t)
	h.start(t)
	ctx := context.Background()
	key := sessionKey{botID: bot.ID}
	h.startBackgroundTask(t, bot)
	wake, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "WAKE", "n2")
	if err != nil {
		t.Fatal(err)
	}
	h.waitTurnStatus(t, wake, bot.ID, "done")
	h.waitTaskTurn(t, key)
	if !h.runner.StopTurn(bot.ID) {
		t.Fatal("StopTurn returned false for the first task turn")
	}
	first := h.waitEntry(t, bot.ID, func(e core.Entry) bool { return isEvent("interrupted")(e) && e.TurnID != wake })
	waitFor(t, "the first task turn ends", func() bool { return !h.runner.Running(bot.ID) })

	again, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "WAKE", "n3")
	if err != nil {
		t.Fatal(err)
	}
	h.waitTurnStatus(t, again, bot.ID, "done")
	h.waitTaskTurn(t, key)
	h.waitMessageTurnGone(t, bot.ID)
	if !h.runner.StopTurn(bot.ID) {
		t.Fatal("StopTurn returned false for the second task turn")
	}
	second := h.waitEntry(t, bot.ID, func(e core.Entry) bool {
		return isEvent("interrupted")(e) && e.TurnID != wake && e.TurnID != again && e.TurnID != first.TurnID
	})
	h.waitTurnStatus(t, second.TurnID, bot.ID, "interrupted")
}

// waitRoomTurnsDone waits until botID has done turns in roomID.
func (h *harness) waitRoomTurnsDone(t *testing.T, botID, roomID string, turns int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("%d done turns in the room", turns), func() bool {
		var done int
		err := h.conn.QueryRowContext(context.Background(), `SELECT count(*) FROM turns WHERE bot_id = ? AND room_id = ? AND status = 'done'`, botID, roomID).Scan(&done)
		return err == nil && done == turns
	})
}

// #37: Stop turn in a bot's own chat leaves the bot's task turn in a room running; Stop turn in
// the room stops it.
func TestStopTurnStopsARoomsTaskTurnOnlyFromTheRoom(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "turn-stop")
	ctx := context.Background()
	ana := h.newNamedBot(t, "Ana")
	room, err := h.svc.CreateRoom(ctx, "Plan", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, room.ID, "build the site", "n1"); err != nil {
		t.Fatal(err)
	}
	h.waitRoomTurnsDone(t, ana.ID, room.ID, 1)
	waitFor(t, "Ana's first turn is over", func() bool { return !h.runner.Running(ana.ID) })
	if _, _, err := h.svc.SubmitUserMessage(ctx, room.ID, "WAKE", "n2"); err != nil {
		t.Fatal(err)
	}
	h.waitRoomTurnsDone(t, ana.ID, room.ID, 2)
	h.waitTaskTurn(t, sessionKey{botID: ana.ID, roomID: room.ID})
	h.waitMessageTurnGone(t, ana.ID)
	if h.runner.StopTurn(ana.ID) {
		t.Fatal("StopTurn in Ana's own chat stopped her task turn in the room")
	}
	if !h.runner.StopTurn(room.ID) {
		t.Fatal("StopTurn in the room returned false for Ana's task turn there")
	}
	note := h.waitEntry(t, room.ID, isEvent("interrupted"))
	h.waitTurnStatus(t, note.TurnID, ana.ID, "interrupted")
}

// #37: when Claude Code does not end the run after the interrupt, the process stops after
// stopWait, so the stop never hangs: for a turn and for a task turn.
func TestStopTurnStopsTheProcessWhenClaudeIgnoresTheInterrupt(t *testing.T) {
	t.Parallel()
	t.Run("turn", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, "sleep")
		h.runner.stopWait = 100 * time.Millisecond
		bot := h.newBot(t)
		h.start(t)
		turn, _, err := h.svc.SubmitUserMessage(context.Background(), bot.ID, "work", "n1")
		if err != nil {
			t.Fatal(err)
		}
		h.waitEntry(t, bot.ID, activityOf("Bash", "running"))
		pid := h.pid(t)
		if !h.runner.StopTurn(bot.ID) {
			t.Fatal("StopTurn returned false for a running turn")
		}
		h.waitTurnStatus(t, turn, bot.ID, "interrupted")
		waitFor(t, "the process exits", func() bool { return !h.runner.HasSession(bot.ID) && !processAlive(pid) })
	})
	t.Run("task turn", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t, "task-hang")
		h.runner.stopWait = 100 * time.Millisecond
		bot := h.newBot(t)
		h.start(t)
		task := h.taskTurnRunning(t, bot.ID)
		h.waitTaskTurn(t, sessionKey{botID: bot.ID})
		h.waitMessageTurnGone(t, bot.ID)
		if !h.runner.StopTurn(bot.ID) {
			t.Fatal("StopTurn returned false for a running task turn")
		}
		if h.runner.StopTurn(bot.ID) {
			t.Fatal("a second StopTurn on the stopping task turn returned true; want false, with no second interrupt")
		}
		h.waitTurnStatus(t, task, bot.ID, "interrupted")
		waitFor(t, "the process exits", func() bool { return !h.runner.HasSession(bot.ID) })
	})
}

// stdinBuffer stands in for a claude process's stdin and keeps what was written to it.
type stdinBuffer struct{ strings.Builder }

func (*stdinBuffer) Close() error { return nil }

// #37: a stop that comes after the run ended, while the turn still counts as running, writes no
// interrupt: Claude Code would apply it to the next run in the process.
func TestStopCommandSendsNoInterruptForARunThatEnded(t *testing.T) {
	t.Parallel()
	stdin := &stdinBuffer{}
	c := &command{turn: core.Turn{ID: "t1"}, done: make(chan outcome, 1)}
	c.done <- outcome{resultText: "built"}
	r := &Runner{stopWait: time.Hour}
	out := r.stopCommand(&session{stdin: stdin}, c)
	if out.resultText != "built" || stdin.Len() != 0 {
		t.Fatalf("outcome %q, stdin %q; want the run's outcome \"built\" and nothing written", out.resultText, stdin.String())
	}
}

// #37: Stop turn in a room's chat ends the room's turn, not the bot's turn elsewhere, and in a
// bot's own chat it ends no turn the bot runs in a room.
func TestStopTurnEndsOnlyTheTurnOfTheChatItIsFor(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "sleep")
	h.runner.stopWait = 100 * time.Millisecond
	ctx := context.Background()
	ana := h.newNamedBot(t, "Ana")
	room, err := h.svc.CreateRoom(ctx, "Plan", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, room.ID, "work", "n1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Ana's turn in the room runs", func() bool { return h.runner.Running(ana.ID) })
	var turn string
	if err := h.conn.QueryRowContext(ctx, `SELECT id FROM turns WHERE bot_id = ? AND room_id = ? AND status = 'running'`, ana.ID, room.ID).Scan(&turn); err != nil {
		t.Fatal(err)
	}
	if h.runner.StopTurn(ana.ID) {
		t.Fatal("StopTurn in Ana's own chat stopped her turn in the room")
	}
	if !h.runner.StopTurn(room.ID) {
		t.Fatal("StopTurn in the room returned false for Ana's turn there")
	}
	h.waitTurnStatus(t, turn, ana.ID, "interrupted")
}

// #37: Stop turn leaves the team's board running: a member's work on a step goes on.
func TestStopTurnLeavesTheBoardsTurnsRunning(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "sleep")
	team, _, bo, _ := h.newTeam(t)
	h.newGoal(t, team, boardItem("Parse", "Parse it.", bo.ID, ""))
	h.start(t)
	waitFor(t, "Bo's work to run", func() bool { return h.runner.Running(bo.ID) })
	if h.runner.StopTurn(team.ID) || h.runner.StopTurn(bo.ID) {
		t.Fatal("StopTurn stopped Bo's work on the board")
	}
	if !h.runner.Running(bo.ID) {
		t.Fatal("Bo's work stopped; want it to go on")
	}
}
