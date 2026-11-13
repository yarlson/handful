package core

import (
	"context"
	"strings"
	"testing"
)

// The sending lead's wake after a delivery tells it that it approves the task, with the task's id,
// and the approval it is told to make goes through.
func TestTheSendingLeadIsToldToApproveItsDeliveredTask(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, g.ID); err != nil {
		t.Fatal(err)
	}
	write := f.create(t, h.docsLead, item("Write", h.eve.ID, ""))
	finishQueued(t, h.svc)
	if err := h.svc.StartItemWork(ctx, write.ID, h.eve.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.SubmitItem(ctx, Actor{BotID: h.eve.ID, TeamID: h.docs.ID, Role: RoleWork}, write.ID, "guide.md"); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	if _, err := h.svc.DeliverGoal(ctx, h.docsLead, "The guide is in guide.md."); err != nil {
		t.Fatal(err)
	}
	turns, err := h.svc.QueuedTurns(ctx, 10)
	if err != nil || len(turns) != 1 {
		t.Fatalf("queued turns = %+v, %v; want the sending lead's wake", turns, err)
	}
	want := "Only you, or the user, approve it. Check the result, then approve it with update_board approve and goal_id " + g.ID +
		", or ask for changes with update_board changes, goal_id " + g.ID + " and text."
	if prompt := turns[0].Prompt; !strings.Contains(prompt, want) || strings.Contains(prompt, "note it and stop") {
		t.Fatalf("the sending lead's wake = %q; want it to say %q", prompt, want)
	}
	if got, err := h.svc.ApproveTask(ctx, f.lead, g.ID); err != nil || got.State != GoalDone {
		t.Fatalf("the sending lead's approve = %+v, %v; want the task done", got, err)
	}
}
