package core

import (
	"context"
	"testing"
)

func TestStepOfNamesOnlyWorkAndReviewTurns(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		turn Turn
		want Step
	}{
		{Turn{BoardRole: RoleWork, ItemID: "i1"}, Step{ItemID: "i1", Role: RoleWork}},
		{Turn{BoardRole: RoleReview, ItemID: "i1"}, Step{ItemID: "i1", Role: RoleReview}},
		{Turn{BoardRole: RoleLead, ItemID: "i1"}, Step{}},
		{Turn{Source: SourceTask, ItemID: "i1"}, Step{}},
		{Turn{Source: SourceRoom, ItemID: "i1"}, Step{}},
	} {
		if got := StepOf(c.turn); got != c.want {
			t.Errorf("StepOf(%+v) = %+v; want %+v", c.turn, got, c.want)
		}
	}
}

// A step session keeps its own Claude session, room cursor and known prompt, apart from the
// member's seat in the room; clearing the team's context clears both.
func TestAStepSessionIsApartFromTheSeatUntilTheContextIsCleared(t *testing.T) {
	t.Parallel()
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	work := Step{ItemID: parse.ID, Role: RoleWork}
	if err := f.svc.SetRoomSession(ctx, f.team.ID, f.ana.ID, "room-ana"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.MarkRoomSeen(ctx, f.team.ID, f.ana.ID, Step{}, 7); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetStepSession(ctx, f.ana.ID, work, "step-ana"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.MarkRoomSeen(ctx, f.team.ID, f.ana.ID, work, 3); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetKnownPrompt(ctx, f.ana.ID, f.team.ID, work, "step-ana", "# Step prompt\n"); err != nil {
		t.Fatal(err)
	}
	seat := func(step Step) RoomSeat {
		t.Helper()
		u, err := f.svc.PendingRoomUpdate(ctx, f.team.ID, f.ana.ID, step)
		if err != nil {
			t.Fatal(err)
		}
		return u.Seat
	}
	known := func(step Step) KnownPrompt {
		t.Helper()
		k, err := f.svc.KnownPrompt(ctx, f.ana.ID, f.team.ID, step)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	review := Step{ItemID: parse.ID, Role: RoleReview}
	for _, c := range []struct {
		name    string
		step    Step
		session string
		seen    int64
		known   KnownPrompt
	}{
		{"the seat", Step{}, "room-ana", 7, KnownPrompt{}},
		{"the work session", work, "step-ana", 3, KnownPrompt{SessionID: "step-ana", Prompt: "# Step prompt\n", Stored: true}},
		{"the review session, never started", review, "", 0, KnownPrompt{}},
	} {
		if got := seat(c.step); got.ClaudeSessionID != c.session || got.LastSeenSeq != c.seen {
			t.Errorf("%s: session %q, seen %d; want %q, %d", c.name, got.ClaudeSessionID, got.LastSeenSeq, c.session, c.seen)
		}
		if got := known(c.step); got != c.known {
			t.Errorf("%s: known prompt %+v; want %+v", c.name, got, c.known)
		}
	}
	if ok, err := f.svc.ReplaceKnownPrompt(ctx, f.ana.ID, f.team.ID, work, KnownPrompt{}, "step-ana", "# Other\n"); err != nil || ok {
		t.Fatalf("replace over a stale read = %v, %v; want false, the stored prompt kept", ok, err)
	}
	if ok, err := f.svc.ReplaceKnownPrompt(ctx, f.ana.ID, f.team.ID, work, known(work), "step-ana", "# Newer\n"); err != nil || !ok {
		t.Fatalf("replace over the current read = %v, %v; want true", ok, err)
	}
	if got := known(work).Prompt; got != "# Newer\n" {
		t.Fatalf("known prompt after replace = %q; want %q", got, "# Newer\n")
	}

	if err := f.svc.ClearContext(ctx, f.team.ID); err != nil {
		t.Fatal(err)
	}
	if got := seat(work); got.ClaudeSessionID != "" || got.LastSeenSeq != 0 {
		t.Fatalf("work session after clear: session %q, seen %d; want none, so the next work turn starts a new one", got.ClaudeSessionID, got.LastSeenSeq)
	}
	if got := known(work); got != (KnownPrompt{}) {
		t.Fatalf("work session's known prompt after clear = %+v; want none", got)
	}
}
