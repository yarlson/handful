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
	before := time.Now().UnixMilli()
	waitFor(t, "Solo's turn starts", func() bool { return h.runner.Running(solo.ID) })
	got := h.runner.Busy(ctx)
	if len(got) != 1 || got[0].Since < before || got[0].Since > time.Now().UnixMilli() {
		t.Fatalf("busy while Solo works = %+v; want Solo's turn since it started", got)
	}
	if got[0].Since = 0; !reflect.DeepEqual(got, []core.Busy{{BotID: solo.ID, Name: "Solo", Kind: core.BusyTurn}}) {
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
	// The fake claude logs "end" before the runner records the turn as done,
	// so wait for the recorded status, not the log.
	for name, id := range map[string]string{"Ana's message": toAna, "Solo's next message": next} {
		waitFor(t, name+" runs after the drain", func() bool { return h.turnStatus(t, id) == "done" })
	}
	waitFor(t, "the held turns run", func() bool {
		raw, _ := os.ReadFile(filepath.Join(h.dir, "order.log"))
		// Solo's two turns, Ana's message and the team's board work; the
		// team's lead may run one more turn, so count at least 4.
		return strings.Count(string(raw), "end ") >= 4
	})
}

func TestBusyListsABotWhoseBackgroundTaskKeepsItsProcess(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "background")
	bot := h.newBot(t)
	h.start(t)
	before := time.Now().UnixMilli()
	h.startBackgroundTask(t, bot)
	got := h.runner.Busy(context.Background())
	if len(got) != 1 || got[0].Since < before || got[0].Since > time.Now().UnixMilli() {
		t.Fatalf("busy while a background task runs = %+v; want it since the task started", got)
	}
	want := []core.Busy{{BotID: bot.ID, Name: "Tester", Kind: core.BusyTask}}
	if got[0].Since = 0; !reflect.DeepEqual(got, want) {
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
	t0 := time.UnixMilli(1_000_000)
	r.running[zed.ID] = runningTurn{since: t0.Add(5 * time.Second)}
	r.sessions[sessionKey{zed.ID, "room"}] = &session{key: sessionKey{zed.ID, "room"}, bot: zed, tasks: []core.BackgroundTask{{ID: "t1"}}, busySince: t0}
	r.running[amy2.ID] = runningTurn{}
	// Amy (1) is busy in two chats; she is listed since the earlier one.
	r.sessions[sessionKey{amy1.ID, ""}] = &session{key: sessionKey{amy1.ID, ""}, waking: true, busySince: t0.Add(3 * time.Second)}
	r.sessions[sessionKey{amy1.ID, "room"}] = &session{key: sessionKey{amy1.ID, "room"}, tasks: []core.BackgroundTask{{ID: "t3"}}, busySince: t0.Add(2 * time.Second)}
	r.sessions[sessionKey{amy1.ID, "later"}] = &session{key: sessionKey{amy1.ID, "later"}, tasks: []core.BackgroundTask{{ID: "t4"}}, busySince: t0.Add(4 * time.Second)}
	r.sessions[sessionKey{"gone", ""}] = &session{key: sessionKey{"gone", ""}, tasks: []core.BackgroundTask{{ID: "t2"}}}
	r.sessions[sessionKey{zed.ID, ""}] = &session{key: sessionKey{zed.ID, ""}, bot: zed}
	// Aaron's id sorts after every other id, but his name sorts first.
	r.running["~aaron"] = runningTurn{}
	r.sessions[sessionKey{"~aaron", ""}] = &session{key: sessionKey{"~aaron", ""}, bot: core.Bot{ID: "~aaron", Name: "Aaron"}}
	// An idle process, with no turn and no task, keeps nothing waiting.
	r.sessions[sessionKey{"idle", ""}] = &session{key: sessionKey{"idle", ""}, tasks: []core.BackgroundTask{}}
	r.mu.Unlock()

	want := []core.Busy{
		{BotID: "~aaron", Name: "Aaron", Kind: core.BusyTurn},
		{BotID: amy1.ID, Name: "Amy", Kind: core.BusyTask, Since: t0.Add(2 * time.Second).UnixMilli()},
		{BotID: amy2.ID, Name: "Amy", Kind: core.BusyTurn},
		{BotID: zed.ID, Name: "Zed", Kind: core.BusyTurn, Since: t0.Add(5 * time.Second).UnixMilli()},
		{BotID: "gone", Name: "gone", Kind: core.BusyTask},
	}
	if got := r.Busy(context.Background()); !reflect.DeepEqual(got, want) {
		t.Fatalf("busy = %+v; want %+v", got, want)
	}
}
