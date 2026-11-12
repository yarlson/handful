package agent

import (
	"context"
	"testing"
)

// A dispatch that read the queue before drain mode began starts none of those turns after it:
// an update or a restart that waits for the running work to end never misses a turn that
// started behind its back.
func TestADispatchThatReadTheQueueBeforeADrainStartsNothingAfterIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	id, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "hello", "n1")
	if err != nil {
		t.Fatal(err)
	}
	turns, err := h.svc.QueuedTurnsPage(ctx, false, 0, dispatchPage)
	if err != nil || len(turns) != 1 || turns[0].ID != id {
		t.Fatalf("queue before the drain = %+v, %v; want the message's turn", turns, err)
	}

	h.svc.SetDraining(true)
	// Run starts here, so it stops the turn's process at the end; its own dispatch sees the drain.
	h.start(t)
	started, full := h.runner.startTurns(ctx, turns)
	if started != 0 || full || h.runner.Running(bot.ID) {
		t.Fatalf("startTurns during the drain = %d started, full %v, running %v; want nothing started", started, full, h.runner.Running(bot.ID))
	}
	if status := h.turnStatus(t, id); status != "queued" {
		t.Fatalf("turn during the drain is %s; want queued", status)
	}

	h.svc.SetDraining(false)
	h.waitTurnStatus(t, id, bot.ID, "done")
}
