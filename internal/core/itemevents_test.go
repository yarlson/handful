package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// history lists the events of an item as "kind round actor: text".
func (f *boardFixture) history(t *testing.T, it Item) []string {
	t.Helper()
	events, err := f.svc.ItemEvents(context.Background(), it.ID)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range events {
		if e.ItemID != it.ID || e.ID == "" || e.CreatedAt == 0 {
			t.Fatalf("event %+v of item %s", e, it.ID)
		}
		out = append(out, strings.TrimSpace(fmt.Sprintf("%s %d %s(%s): %s", e.Kind, e.Round, e.ActorName, e.ActorID, e.Text)))
	}
	return out
}

func (f *boardFixture) wantHistory(t *testing.T, it Item, want ...string) {
	t.Helper()
	if got := f.history(t, it); !slices.Equal(got, want) {
		t.Fatalf("history of %q = %q; want %q", it.Title, got, want)
	}
}

func TestEveryReviewMoveLeavesOneEvent(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	bo, ana, cy := "Bo("+f.bo.ID+")", "Ana("+f.ana.ID+")", "Cy("+f.cy.ID+")"
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.wantHistory(t, parse, "created 0 "+bo+":")
	f.start(t, parse, f.ana)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.cy, RoleWork), parse.ID, "Not mine."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("submit by another member: err = %v; want ErrInvalid", err)
	}
	long := strings.Repeat("r", maxResultInEvent+20)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, long); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RequestChanges(ctx, f.as(f.cy, RoleReview), parse.ID, "  Handle quotes.  "); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RequestChanges(ctx, f.as(f.cy, RoleReview), parse.ID, "Keep the quotes fix; add tabs."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "v3"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.PassItem(ctx, f.as(f.cy, RoleReview), parse.ID, "Good."); err != nil {
		t.Fatal(err)
	}
	f.wantHistory(t, parse,
		"created 0 "+bo+":",
		"submitted 0 "+ana+": "+long[:maxResultInEvent],
		"changes 1 "+cy+": Handle quotes.",
		"submitted 0 "+ana+": v2",
		"changes 2 "+cy+": Keep the quotes fix; add tabs.",
		"submitted 0 "+ana+": v3",
		"passed 0 "+cy+": Good.",
	)
	changes, err := f.svc.ChangeRequests(ctx, parse.ID)
	if err != nil || len(changes) != 2 || changes[0].Round != 1 || changes[1].Text != "Keep the quotes fix; add tabs." {
		t.Fatalf("change requests = %+v, %v; want both rounds", changes, err)
	}

	docs := f.create(t, f.lead, item("Docs", "", ""))
	if _, err := f.svc.AssignItem(ctx, f.lead, docs.ID, f.ana.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	f.start(t, docs, f.ana)
	if _, err := f.svc.BlockItem(ctx, f.as(f.ana, RoleWork), docs.ID, "Which format?"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.UnblockItem(ctx, f.lead, docs.ID, "Markdown."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DropItem(ctx, f.lead, docs.ID, "Not needed."); err != nil {
		t.Fatal(err)
	}
	f.wantHistory(t, docs,
		"created 0 "+bo+":",
		"assigned 0 "+bo+":",
		"blocked 0 "+ana+": Which format?",
		"unblocked 0 "+bo+": Markdown.",
		"dropped 0 "+bo+": Not needed.",
	)
	if _, err := f.svc.ItemEvents(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("events of a missing item: err = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.ChangeRequests(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("change requests of a missing item: err = %v; want ErrNotFound", err)
	}
}

func TestAMemberLeavingMidReviewLeavesABlockedEvent(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	owned := f.create(t, f.lead, item("Owned", f.cy.ID, ""))
	f.finish(t, parse, f.ana)
	if _, err := f.svc.SetRoomMembers(ctx, f.team.ID, []string{f.ana.ID, f.bo.ID}); err != nil {
		t.Fatal(err)
	}
	f.wantHistory(t, parse,
		"created 0 Bo("+f.bo.ID+"):",
		"submitted 0 Ana("+f.ana.ID+"): Done: Parse",
		"blocked 0 Handful(): Cy left the team before reviewing it.",
	)
	f.wantHistory(t, owned, "created 0 Bo("+f.bo.ID+"):")
}

func TestTheBoardShowsTheChangeTheOwnerWorksOn(t *testing.T) {
	l := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := l.create(t, l.lead, item("Parse", l.ana.ID, l.cy.ID))
	itemOn := func(step string) Item {
		t.Helper()
		board, err := l.svc.Board(ctx, l.team.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range board.Items {
			if it.ID == parse.ID {
				return it
			}
		}
		t.Fatalf("after %s, Parse is not on the board", step)
		return Item{}
	}
	created := itemOn("create")
	if created.LatestChange != nil || created.EventCount != 1 || created.HistoryFrom == 0 {
		t.Fatalf("new item = %+v; want one event and no change request", created)
	}
	l.finish(t, parse, l.ana)
	if _, err := l.svc.RequestChanges(ctx, l.as(l.cy, RoleReview), parse.ID, "Handle quotes."); err != nil {
		t.Fatal(err)
	}
	got := itemOn("the first review").LatestChange
	if got == nil || got.Round != 1 || got.By != "Cy" || got.Text != "Handle quotes." || got.At == 0 {
		t.Fatalf("latest change after round 1 = %+v", got)
	}
	if _, err := l.svc.SubmitItem(ctx, l.as(l.ana, RoleWork), parse.ID, "v2"); err != nil {
		t.Fatal(err)
	}
	if it := itemOn("the resubmission"); it.LatestChange != nil || it.EventCount != 4 {
		t.Fatalf("after the resubmission = %+v; want no latest change and 4 events", it)
	}
	if _, err := l.svc.RequestChanges(ctx, l.as(l.cy, RoleReview), parse.ID, "Add tabs."); err != nil {
		t.Fatal(err)
	}
	if got := itemOn("the second review").LatestChange; got == nil || got.Round != 2 || got.Text != "Add tabs." {
		t.Fatalf("latest change after round 2 = %+v", got)
	}

	gb, err := l.svc.GoalBoard(ctx, l.goal.ID)
	if err != nil || len(gb.Items) == 0 || gb.Items[0].EventCount == 0 {
		t.Fatalf("goal board = %+v, %v; want the items' history", gb, err)
	}
}
