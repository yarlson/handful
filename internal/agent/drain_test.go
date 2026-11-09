package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"yargpt/internal/core"
)

// turnStatus returns the status of a turn.
func (h *harness) turnStatus(t *testing.T, id string) string {
	t.Helper()
	var status string
	if err := h.conn.QueryRowContext(t.Context(), `SELECT status FROM turns WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestDrainModeLetsRunningTurnsFinishAndHoldsTheQueueUntilItEnds(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "slow")
	ctx := context.Background()
	team, ana, bo, _ := h.newTeam(t)
	solo := h.newNamedBot(t, "Solo")
	h.start(t)

	if _, _, err := h.svc.SubmitUserMessage(ctx, solo.ID, "LONG build it", "n1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "Solo's turn starts", func() bool { return h.runner.Running(solo.ID) })
	if got := h.runner.Busy(ctx); !reflect.DeepEqual(got, []core.Busy{{BotID: solo.ID, Name: "Solo", Kind: core.BusyTurn}}) {
		t.Fatalf("busy while Solo works = %+v; want Solo's turn", got)
	}
	h.svc.SetDraining(true)

	// Turns of every kind that come during the drain wait: a user's message,
	// the next message to the busy bot, and a team's board work.
	toAna, _, err := h.svc.SubmitUserMessage(ctx, ana.ID, "hello", "n2")
	if err != nil {
		t.Fatal(err)
	}
	next, _, err := h.svc.SubmitUserMessage(ctx, solo.ID, "and then", "n3")
	if err != nil {
		t.Fatal(err)
	}
	h.newGoal(t, team, boardItem("Docs", "Write the docs.", bo.ID, ""))

	waitFor(t, "Solo's running turn finishes", func() bool { return !h.runner.Running(solo.ID) })
	time.Sleep(300 * time.Millisecond) // dispatch runs when a turn ends; give it the chance to start the queue
	if raw, _ := os.ReadFile(filepath.Join(h.dir, "order.log")); string(raw) != "start "+solo.ID+"-long\nend "+solo.ID+"-long\n" {
		t.Fatalf("order.log during the drain = %q; want only the turn that ran before it", raw)
	}
	for name, id := range map[string]string{"Ana's message": toAna, "Solo's next message": next} {
		if status := h.turnStatus(t, id); status != "queued" {
			t.Fatalf("%s during the drain is %s; want queued", name, status)
		}
	}
	board, err := h.svc.Board(ctx, team.ID)
	if err != nil || board.Goal == nil || board.Goal.Used != 0 {
		t.Fatalf("goal during the drain = %+v, %v; want no turn used", board.Goal, err)
	}
	if got := h.runner.Busy(ctx); len(got) != 0 || got == nil {
		t.Fatalf("busy after the running turn = %#v; want an empty list", got)
	}

	h.svc.SetDraining(false)
	waitFor(t, "the held turns run", func() bool {
		raw, _ := os.ReadFile(filepath.Join(h.dir, "order.log"))
		return strings.Count(string(raw), "end ") == 4
	})
	if status := h.turnStatus(t, toAna); status != "done" {
		t.Fatalf("Ana's message after the drain is %s; want done", status)
	}
}

func TestBusyListsABotWhoseBackgroundTaskKeepsItsProcess(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "background")
	bot := h.newBot(t)
	h.start(t)
	h.startBackgroundTask(t, bot)
	want := []core.Busy{{BotID: bot.ID, Name: "Tester", Kind: core.BusyTask}}
	if got := h.runner.Busy(context.Background()); !reflect.DeepEqual(got, want) {
		t.Fatalf("busy while a background task runs = %+v; want %+v", got, want)
	}
}

func TestBusyNamesEachBotOnceAndSortsByNameThenID(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	zed, amy1, amy2 := h.newNamedBot(t, "Zed"), h.newNamedBot(t, "Amy"), h.newNamedBot(t, "Amy")
	if amy2.ID < amy1.ID {
		amy1, amy2 = amy2, amy1
	}
	r := h.runner
	r.mu.Lock()
	// Zed runs a turn and has tasks in a room too; the Amys are known only by
	// id, so their names come from the service; a deleted bot shows its id.
	r.running[zed.ID] = runningTurn{}
	r.sessions[sessionKey{zed.ID, "room"}] = &session{key: sessionKey{zed.ID, "room"}, bot: zed, tasks: []core.BackgroundTask{{ID: "t1"}}}
	r.running[amy2.ID] = runningTurn{}
	r.sessions[sessionKey{amy1.ID, ""}] = &session{key: sessionKey{amy1.ID, ""}, waking: true}
	r.sessions[sessionKey{"gone", ""}] = &session{key: sessionKey{"gone", ""}, tasks: []core.BackgroundTask{{ID: "t2"}}}
	r.sessions[sessionKey{zed.ID, ""}] = &session{key: sessionKey{zed.ID, ""}, bot: zed}
	// An idle process, with no turn and no task, keeps nothing waiting.
	r.sessions[sessionKey{"idle", ""}] = &session{key: sessionKey{"idle", ""}, tasks: []core.BackgroundTask{}}
	r.mu.Unlock()

	want := []core.Busy{
		{BotID: amy1.ID, Name: "Amy", Kind: core.BusyTask},
		{BotID: amy2.ID, Name: "Amy", Kind: core.BusyTurn},
		{BotID: zed.ID, Name: "Zed", Kind: core.BusyTurn},
		{BotID: "gone", Name: "gone", Kind: core.BusyTask},
	}
	if got := r.Busy(context.Background()); !reflect.DeepEqual(got, want) {
		t.Fatalf("busy = %+v; want %+v", got, want)
	}
}
