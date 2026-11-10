package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// boardFixture is team "Ship" of Ana, Bo (the lead) and Cy with an active
// goal, and no queued turns.
type boardFixture struct {
	svc          *Service
	team         Bot
	ana, bo, cy  Bot
	goal         Goal
	lead         Actor
	names, items map[string]string // bot id -> name; item id -> title
}

func newBoardFixture(t *testing.T, budget int64) *boardFixture {
	t.Helper()
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, cy := newTestTeam(t, svc)
	team, err := svc.SetRoomMembers(ctx, team.ID, []string{ana.ID, bo.ID, cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	if team, err = svc.UpdateTeam(ctx, team.ID, TeamPatch{GoalBudget: &budget}); err != nil {
		t.Fatal(err)
	}
	goal, err := svc.StartGoal(ctx, team.ID, "Ship CSV export.")
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, svc)
	f := &boardFixture{
		svc: svc, team: team, ana: ana, bo: bo, cy: cy, goal: goal,
		lead:  Actor{BotID: bo.ID, TeamID: team.ID, Role: RoleLead},
		names: map[string]string{ana.ID: "Ana", bo.ID: "Bo", cy.ID: "Cy"},
		items: map[string]string{},
	}
	return f
}

// needs gives request n the item of the fixture's goal that waits on it,
// unless it names one: a todo item "<title> waits" without an owner.
func (f *boardFixture) needs(t *testing.T, n NewItem) NewItem {
	t.Helper()
	if len(n.NeededBy) == 0 {
		n.NeededBy = []string{f.create(t, f.lead, item(n.Title+" waits", "", "")).ID}
	}
	return n
}

// waitedOn gives request n an item of the lead a's goal that waits on it.
func waitedOn(t *testing.T, svc *Service, a Actor, n NewItem) NewItem {
	t.Helper()
	w, err := svc.CreateItem(context.Background(), a, item(n.Title+" waits", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	n.NeededBy = []string{w.ID}
	return n
}

// as is bot b in a turn in the team with a board role.
func (f *boardFixture) as(b Bot, role string) Actor {
	return Actor{BotID: b.ID, TeamID: f.team.ID, Role: role}
}

func (f *boardFixture) create(t *testing.T, a Actor, n NewItem) Item {
	t.Helper()
	it, err := f.svc.CreateItem(context.Background(), a, n)
	if err != nil {
		t.Fatalf("create %q: %v", n.Title, err)
	}
	f.items[it.ID] = it.Title
	return it
}

func item(title, owner, reviewer string, deps ...string) NewItem {
	return NewItem{Title: title, Brief: "Do " + title + ".", Acceptance: title + " works.", OwnerID: owner, ReviewerID: reviewer, DependsOn: deps}
}

// queued lists the queued board turns as "who role item", oldest first.
func (f *boardFixture) queued(t *testing.T) []string {
	t.Helper()
	turns, err := f.svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tr := range turns {
		if tr.GoalID != f.goal.ID || tr.RoomID != f.team.ID {
			t.Fatalf("queued turn %+v; want only turns of the goal in the team", tr)
		}
		out = append(out, strings.TrimSpace(fmt.Sprintf("%s %s %s", f.names[tr.BotID], tr.BoardRole, f.items[tr.ItemID])))
	}
	return out
}

func (f *boardFixture) wantQueued(t *testing.T, step string, want ...string) {
	t.Helper()
	if got := f.queued(t); !slices.Equal(got, want) {
		t.Fatalf("after %s, queued turns = %q; want %q", step, got, want)
	}
}

func (f *boardFixture) start(t *testing.T, it Item, owner Bot) {
	t.Helper()
	finishQueued(t, f.svc)
	if err := f.svc.StartItemWork(context.Background(), it.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
}

func (f *boardFixture) state(t *testing.T, it Item) Item {
	t.Helper()
	board, err := f.svc.Board(context.Background(), f.team.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range board.Items {
		if got.ID == it.ID {
			return got
		}
	}
	t.Fatalf("item %q is not on the board", it.Title)
	return Item{}
}

func TestStartGoalWakesTheLeadToPlanIt(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, _, bo, _ := newTestTeam(t, svc)

	goal, err := svc.StartGoal(ctx, team.ID, "  Ship CSV export.  ")
	if err != nil || goal.Text != "Ship CSV export." || goal.State != GoalActive || goal.Budget != DefaultGoalBudget || goal.Used != 0 {
		t.Fatalf("StartGoal = %+v, %v; want an active goal with the team's budget", goal, err)
	}
	if text := lastEventText(t, svc, team.ID); text != "New goal: Ship CSV export." {
		t.Fatalf("channel event = %q; want the new goal", text)
	}
	turns, _ := svc.QueuedTurns(ctx, 10)
	if len(turns) != 1 || turns[0].BotID != bo.ID || turns[0].BoardRole != RoleLead || turns[0].GoalID != goal.ID || turns[0].RoomID != team.ID || turns[0].ItemID != "" {
		t.Fatalf("queued turns = %+v; want one lead turn of Bo for the goal", turns)
	}
	board, err := svc.Board(ctx, team.ID)
	if err != nil || board.Goal == nil || board.Goal.ID != goal.ID || len(board.Items) != 0 {
		t.Fatalf("Board = %+v, %v; want the goal without items", board, err)
	}
	if next, err := svc.StartGoal(ctx, team.ID, "Another goal."); err != nil || next.State != GoalQueued || next.QueuePos != 1 {
		t.Fatalf("a second goal = %+v, %v; want it queued", next, err)
	}
}

func TestStartGoalRejectsBadInput(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, _, _ := newTestTeam(t, svc)
	room, err := svc.CreateRoom(ctx, "Chat", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if board, err := svc.Board(ctx, team.ID); err != nil || board.Goal != nil || board.Items == nil {
		t.Fatalf("Board of a team without goals = %+v, %v; want no goal and an empty list", board, err)
	}
	for name, c := range map[string]struct{ id, text string }{
		"a plain room":   {room.ID, "Plan."},
		"an empty goal":  {team.ID, " "},
		"a long goal":    {team.ID, strings.Repeat("a", maxGoalText+1)},
		"a missing team": {"missing", "Plan."},
	} {
		if _, err := svc.StartGoal(ctx, c.id, c.text); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
			t.Errorf("StartGoal with %s: err = %v; want a validation error", name, err)
		}
	}
	if _, err := svc.StartGoal(ctx, team.ID, strings.Repeat("a", maxGoalText)); err != nil {
		t.Fatalf("StartGoal with %d characters: %v", maxGoalText, err)
	}
	empty, err := svc.CreateTeam(ctx, "Empty", "", []string{ana.ID}, TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBot(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartGoal(ctx, empty.ID, "Plan."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("StartGoal for a team without members: err = %v; want ErrInvalid", err)
	}
}

func TestTheLeadStartsAGoalOnlyInATurnThatTheUserStarted(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, _ := newTestTeam(t, svc)

	if _, err := svc.StartGoalAsLead(ctx, Actor{BotID: bo.ID, TeamID: team.ID}, "Ship it."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("start_goal without a user post: err = %v; want ErrInvalid", err)
	}
	if _, err := svc.StartGoalAsLead(ctx, Actor{BotID: ana.ID, TeamID: team.ID, UserPost: true}, "Ship it."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("start_goal by a member who does not lead: err = %v; want ErrInvalid", err)
	}
	goal, err := svc.StartGoalAsLead(ctx, Actor{BotID: bo.ID, TeamID: team.ID, UserPost: true}, "Ship it.")
	if err != nil || goal.State != GoalActive {
		t.Fatalf("start_goal by the lead = %+v, %v; want an active goal", goal, err)
	}
	if turns, _ := svc.QueuedTurns(ctx, 10); len(turns) != 0 {
		t.Fatalf("queued turns = %+v; want none: the lead plans in the same turn", turns)
	}
	if text := lastEventText(t, svc, team.ID); text != "New goal: Ship it." {
		t.Fatalf("channel event = %q; want the new goal", text)
	}
}

func TestReadyItemsWakeTheirOwnersAndSubmittedOnesTheirReviewers(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()

	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	docs := f.create(t, f.lead, item("Docs", f.cy.ID, "", parse.ID, parse.ID))
	if !slices.Equal(docs.DependsOn, []string{parse.ID}) {
		t.Fatalf("Docs depends on %v; want Parse once", docs.DependsOn)
	}
	f.wantQueued(t, "the lead added Parse and Docs", "Ana work Parse")
	if text := lastEventText(t, f.svc, f.team.ID); text != `Bo added "Docs" for Cy.` {
		t.Fatalf("channel event = %q", text)
	}

	f.start(t, parse, f.ana)
	if got := f.state(t, parse); got.State != ItemDoing {
		t.Fatalf("Parse after its work turn started = %s; want doing", got.State)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Parsed in parse.go."); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "Ana submitted Parse", "Cy review Parse")
	if got := f.state(t, parse); got.State != ItemReview || got.Result != "Parsed in parse.go." {
		t.Fatalf("Parse after submit = %+v; want it in review with the result", got)
	}
	finishQueued(t, f.svc)
	if _, err := f.svc.PassItem(ctx, f.as(f.cy, RoleReview), parse.ID, "Looks good."); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "Cy passed Parse", "Cy work Docs")
	if text := lastEventText(t, f.svc, f.team.ID); text != `Cy passed "Parse".` {
		t.Fatalf("channel event = %q", text)
	}

	f.start(t, docs, f.cy)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.cy, RoleWork), docs.ID, "Wrote docs.md."); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, docs); got.State != ItemDone {
		t.Fatalf("Docs without a reviewer after submit = %s; want done", got.State)
	}
	f.wantQueued(t, "the last item was done", "Bo lead")
}

func TestReviewRoundsEndInABlockForTheLead(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.start(t, parse, f.ana)

	for round := 1; round <= maxReviewRounds; round++ {
		if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, fmt.Sprintf("Try %d.", round)); err != nil {
			t.Fatal(err)
		}
		finishQueued(t, f.svc)
		got, err := f.svc.RequestChanges(ctx, f.as(f.cy, RoleReview), parse.ID, "Handle empty lines.")
		if err != nil {
			t.Fatal(err)
		}
		if got.ReviewRounds != int64(round) {
			t.Fatalf("review rounds after round %d = %d", round, got.ReviewRounds)
		}
		if round < maxReviewRounds {
			if got.State != ItemDoing || got.Note != "Handle empty lines." {
				t.Fatalf("Parse after changes in round %d = %+v; want doing with the review's note", round, got)
			}
			f.wantQueued(t, fmt.Sprintf("changes in round %d", round), "Ana work Parse")
			finishQueued(t, f.svc)
		}
	}
	got := f.state(t, parse)
	if got.State != ItemBlocked || got.Note != "3 review rounds without a pass. The last review: Handle empty lines." {
		t.Fatalf("Parse after %d rounds = %+v; want it blocked for the lead", maxReviewRounds, got)
	}
	f.wantQueued(t, "the last round", "Bo lead")
}

func TestOnlyTheRightMemberActsOnAnItem(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.start(t, parse, f.ana)
	other, err := f.svc.CreateTeam(ctx, "Docs", "", []string{f.ana.ID}, TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	dee, err := f.svc.CreateBot(ctx, "Dee", "")
	if err != nil {
		t.Fatal(err)
	}
	ana := f.as(f.ana, RoleWork)
	cases := map[string]func() error{
		"a member assigns an owner on create": func() error {
			_, err := f.svc.CreateItem(ctx, ana, item("Mine", f.ana.ID, ""))
			return err
		},
		"a reviewer that owns the item": func() error { _, err := f.svc.CreateItem(ctx, f.lead, item("Self", f.ana.ID, f.ana.ID)); return err },
		"a reviewer without an owner":   func() error { _, err := f.svc.CreateItem(ctx, f.lead, item("Nobody", "", f.cy.ID)); return err },
		"an owner outside the team":     func() error { _, err := f.svc.CreateItem(ctx, f.lead, item("Out", dee.ID, "")); return err },
		"a dependency outside the goal": func() error { _, err := f.svc.CreateItem(ctx, f.lead, item("Dep", "", "", "missing")); return err },
		"a title used twice":            func() error { _, err := f.svc.CreateItem(ctx, f.lead, item("Parse", "", "")); return err },
		"an empty title":                func() error { _, err := f.svc.CreateItem(ctx, f.lead, item(" ", "", "")); return err },
		"an empty brief": func() error {
			_, err := f.svc.CreateItem(ctx, f.lead, NewItem{Title: "A", Acceptance: "A works."})
			return err
		},
		"empty acceptance": func() error {
			_, err := f.svc.CreateItem(ctx, f.lead, NewItem{Title: "A", Brief: "Do A."})
			return err
		},
		"a long title": func() error {
			_, err := f.svc.CreateItem(ctx, f.lead, item(strings.Repeat("a", maxItemTitle+1), "", ""))
			return err
		},
		"a turn in another team": func() error {
			_, err := f.svc.SubmitItem(ctx, Actor{BotID: f.ana.ID, TeamID: other.ID, Role: RoleWork}, parse.ID, "Done.")
			return err
		},
		"a bot outside the team":   func() error { _, err := f.svc.SubmitItem(ctx, f.as(dee, RoleWork), parse.ID, "Done."); return err },
		"another member submits":   func() error { _, err := f.svc.SubmitItem(ctx, f.as(f.cy, RoleWork), parse.ID, "Done."); return err },
		"a submit without result":  func() error { _, err := f.svc.SubmitItem(ctx, ana, parse.ID, " "); return err },
		"a pass before the review": func() error { _, err := f.svc.PassItem(ctx, f.as(f.cy, RoleReview), parse.ID, ""); return err },
		"changes before the review": func() error {
			_, err := f.svc.RequestChanges(ctx, f.as(f.cy, RoleReview), parse.ID, "No.")
			return err
		},
		"a member assigns":     func() error { _, err := f.svc.AssignItem(ctx, ana, parse.ID, f.ana.ID, "", ""); return err },
		"assign while doing":   func() error { _, err := f.svc.AssignItem(ctx, f.lead, parse.ID, f.ana.ID, "", ""); return err },
		"a member unblocks":    func() error { _, err := f.svc.UnblockItem(ctx, ana, parse.ID, "Go on."); return err },
		"unblock while doing":  func() error { _, err := f.svc.UnblockItem(ctx, f.lead, parse.ID, "Go on."); return err },
		"a member drops":       func() error { _, err := f.svc.DropItem(ctx, ana, parse.ID, ""); return err },
		"a reviewer blocks":    func() error { _, err := f.svc.BlockItem(ctx, f.as(f.cy, RoleReview), parse.ID, "Why?"); return err },
		"a block without note": func() error { _, err := f.svc.BlockItem(ctx, ana, parse.ID, " "); return err },
		"a long block note": func() error {
			_, err := f.svc.BlockItem(ctx, ana, parse.ID, strings.Repeat("a", maxItemNote+1))
			return err
		},
		"work on another's item": func() error { return f.svc.StartItemWork(ctx, parse.ID, f.cy.ID) },
	}
	for name, act := range cases {
		if err := act(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v; want ErrInvalid", name, err)
		}
	}
	if _, err := f.svc.SubmitItem(ctx, ana, "missing", "Done."); !errors.Is(err, ErrNotFound) {
		t.Errorf("submit a missing item: err = %v; want ErrNotFound", err)
	}
	if got := f.state(t, parse); got.State != ItemDoing || got.Result != "" {
		t.Fatalf("Parse after the refused actions = %+v; want it unchanged", got)
	}
}

func TestAGoalHoldsAtMostThirtyItems(t *testing.T) {
	f := newBoardFixture(t, 30)
	for i := range maxGoalItems {
		f.create(t, f.lead, item(fmt.Sprintf("Item %d", i), "", ""))
	}
	if _, err := f.svc.CreateItem(context.Background(), f.lead, item("One more", "", "")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("item %d: err = %v; want ErrInvalid", maxGoalItems+1, err)
	}
}

func TestProposalsBlocksAndUnblocksWakeTheLeadOrTheOwner(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()

	idea := f.create(t, f.as(f.ana, ""), item("Idea", "", ""))
	f.wantQueued(t, "Ana proposed an item", "Bo lead")
	if text := lastEventText(t, f.svc, f.team.ID); text != `Ana added "Idea" without an owner.` {
		t.Fatalf("channel event = %q", text)
	}
	finishQueued(t, f.svc)
	if _, err := f.svc.AssignItem(ctx, f.lead, idea.ID, f.ana.ID, f.cy.ID, ""); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "the lead assigned Idea", "Ana work Idea")
	if got := f.state(t, idea); got.OwnerID != f.ana.ID || got.ReviewerID != f.cy.ID {
		t.Fatalf("Idea after assign = %+v; want owner Ana and reviewer Cy", got)
	}
	if text := lastEventText(t, f.svc, f.team.ID); text != `Bo assigned "Idea" to Ana.` {
		t.Fatalf("channel event = %q", text)
	}

	f.start(t, idea, f.ana)
	if _, err := f.svc.BlockItem(ctx, f.as(f.ana, RoleWork), idea.ID, "Which format?"); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "Ana blocked Idea", "Bo lead")
	if text := lastEventText(t, f.svc, f.team.ID); text != `"Idea" is blocked: Which format?` {
		t.Fatalf("channel event = %q", text)
	}
	if _, err := f.svc.UnblockItem(ctx, f.as(f.ana, RoleWork), idea.ID, "CSV."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the owner unblocks: err = %v; want ErrInvalid", err)
	}
	finishQueued(t, f.svc)
	got, err := f.svc.UnblockItem(ctx, f.lead, idea.ID, "Use CSV.")
	if err != nil || got.State != ItemDoing || got.Note != "Use CSV." {
		t.Fatalf("unblock = %+v, %v; want Ana to work on it again with the answer", got, err)
	}
	f.wantQueued(t, "the lead unblocked Idea", "Ana work Idea")
	if text := lastEventText(t, f.svc, f.team.ID); text != `Bo unblocked "Idea".` {
		t.Fatalf("channel event = %q", text)
	}

	got, err = f.svc.DropItem(ctx, f.lead, idea.ID, "Not needed.")
	if err != nil || got.State != ItemDropped {
		t.Fatalf("drop = %+v, %v; want it dropped", got, err)
	}
	f.wantQueued(t, "the lead dropped Idea")
	if _, err := f.svc.DropItem(ctx, f.lead, idea.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("drop a dropped item: err = %v; want ErrInvalid", err)
	}
}

func TestUnblockingAnItemWithoutAnOwnerLeavesItToTheLead(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	idea := f.create(t, f.lead, item("Idea", "", ""))
	if _, err := f.svc.AssignItem(ctx, f.lead, idea.ID, "", "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("assign without an owner: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.AssignItem(ctx, f.lead, idea.ID, f.ana.ID, f.ana.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("assign the owner as reviewer: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.moveItem(ctx, f.lead, idea.ID, leaving(EventBlocked, fixed("Unclear."), func(_ Bot, it *Item) error { return block(it, "Unclear.") })); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.UnblockItem(ctx, f.lead, idea.ID, "Now clear.")
	if err != nil || got.State != ItemTodo {
		t.Fatalf("unblock an item without an owner = %+v, %v; want it to wait for an owner", got, err)
	}
	if _, err := f.svc.UnblockItem(ctx, f.lead, idea.ID, " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unblock without a note: err = %v; want ErrInvalid", err)
	}
}

func TestTheLeadWakesOnceForSeveralChanges(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	a := f.create(t, f.lead, item("A", f.ana.ID, ""))
	b := f.create(t, f.lead, item("B", f.cy.ID, ""))
	f.start(t, a, f.ana)
	if err := f.svc.StartItemWork(ctx, b.ID, f.cy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.BlockItem(ctx, f.as(f.ana, RoleWork), a.ID, "Help?"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.BlockItem(ctx, f.as(f.cy, RoleWork), b.ID, "Help too?"); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "two blocks", "Bo lead")
}

func TestTheLeadNeedsNoTurnForItsOwnChangesButDoesForItsWorkAsAMember(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	mine := f.create(t, f.lead, item("Mine", f.bo.ID, ""))
	f.wantQueued(t, "the lead added an item for itself", "Bo work Mine")
	f.start(t, mine, f.bo)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.bo, RoleWork), mine.ID, "Done."); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "the lead finished its item in a work turn", "Bo lead")
	if text := lastEventText(t, f.svc, f.team.ID); text != `Bo finished "Mine".` {
		t.Fatalf("channel event = %q", text)
	}
	finishQueued(t, f.svc)
	f.create(t, f.lead, item("Later", "", ""))
	f.wantQueued(t, "the lead added an item without an owner")
}

func TestTheBudgetPausesTheGoalAndMoreTurnsResumeIt(t *testing.T) {
	f := newBoardFixture(t, 2)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	turns, _ := f.svc.QueuedTurns(ctx, 10)
	work := turns[0]

	for i := range 2 {
		if ok, err := f.svc.GoalAllows(ctx, work); err != nil || !ok {
			t.Fatalf("GoalAllows with %d of 2 turns used = %v, %v; want true", i, ok, err)
		}
		id, err := f.svc.queueTurn(ctx, f.svc.q, Turn{BotID: f.bo.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead})
		if err != nil {
			t.Fatal(err)
		}
		if started, err := f.svc.StartTurn(ctx, id); err != nil || !started {
			t.Fatal(err)
		}
	}
	if ok, err := f.svc.GoalAllows(ctx, work); err != nil || ok {
		t.Fatalf("GoalAllows with 2 of 2 turns used = %v, %v; want false", ok, err)
	}
	board, _ := f.svc.Board(ctx, f.team.ID)
	if board.Goal.State != GoalPaused || board.Goal.Used != 2 {
		t.Fatalf("goal at its budget = %+v; want it paused with 2 turns used", board.Goal)
	}
	if text := lastEventText(t, f.svc, f.team.ID); !strings.HasPrefix(text, "The goal used its budget of 2 turns and is paused.") {
		t.Fatalf("channel event = %q", text)
	}
	if got := attentionOf(t, f.svc, f.team.ID); got != AttentionGoal {
		t.Fatalf("team attention = %q; want %q", got, AttentionGoal)
	}
	if ok, _ := f.svc.GoalAllows(ctx, work); ok {
		t.Fatal("GoalAllows of a paused goal = true; want false")
	}
	if ok, _ := f.svc.GoalAllows(ctx, Turn{GoalID: f.goal.ID, BoardRole: RoleRetro}); !ok {
		t.Fatal("GoalAllows of a retro turn = false; want true")
	}

	for _, turns := range []int64{-1, maxGoalBudget} {
		if _, err := f.svc.ExtendGoal(ctx, f.goal.ID, turns); !errors.Is(err, ErrInvalid) {
			t.Errorf("ExtendGoal(%d): err = %v; want ErrInvalid", turns, err)
		}
	}
	g, err := f.svc.ExtendGoal(ctx, f.goal.ID, 5)
	if err != nil || g.State != GoalActive || g.Budget != 7 {
		t.Fatalf("ExtendGoal(5) = %+v, %v; want an active goal with 7 turns", g, err)
	}
	f.wantQueued(t, "the goal resumed", "Ana work Parse", "Bo lead")
	if ok, err := f.svc.GoalAllows(ctx, work); err != nil || !ok {
		t.Fatalf("GoalAllows after more turns = %v, %v; want true", ok, err)
	}
	if got := attentionOf(t, f.svc, f.team.ID); got != "" {
		t.Fatalf("team attention after resume = %q; want none", got)
	}
	if g, err := f.svc.ExtendGoal(ctx, f.goal.ID, 3); err != nil || g.State != GoalActive || g.Budget != 10 {
		t.Fatalf("ExtendGoal of an active goal = %+v, %v; want 10 turns", g, err)
	}
	if g, err := f.svc.ExtendGoal(ctx, f.goal.ID, maxGoalBudget-10); err != nil || g.Budget != maxGoalBudget {
		t.Fatalf("ExtendGoal to %d turns = %+v, %v; want it accepted", maxGoalBudget, g, err)
	}
	if got := f.state(t, parse); got.State != ItemTodo {
		t.Fatalf("Parse = %s; want it to wait for its turn", got.State)
	}
}

func TestStopPausesTheGoalAndResumeGivesWaitingWorkItsTurnsAgain(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.create(t, f.lead, item("Docs", f.cy.ID, "", parse.ID))
	f.create(t, f.lead, item("Notes", f.ana.ID, ""))
	spec := f.create(t, f.lead, item("Spec", f.cy.ID, ""))
	f.start(t, parse, f.ana)
	if err := f.svc.StartItemWork(ctx, spec.ID, f.cy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.cy, RoleWork), spec.ID, "spec.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Parsed."); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.StopTeam(ctx, f.team.ID); err != nil {
		t.Fatal(err)
	}
	if text := lastEventText(t, f.svc, f.team.ID); text != "The goal is paused." {
		t.Fatalf("channel event after Stop = %q; want the pause", text)
	}
	if err := f.svc.CancelQueuedRoomTurns(ctx, f.team.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SettleItems(ctx, func(string, string) bool { return false }, LeadState{}); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, parse); got.State != ItemReview {
		t.Fatalf("Parse in a paused goal after settle = %s; want review: a paused goal blocks nothing", got.State)
	}
	if err := f.svc.StopTeam(ctx, f.team.ID); err != nil {
		t.Fatalf("pausing a paused goal: %v; want nothing to happen", err)
	}
	if _, err := f.svc.ExtendGoal(ctx, f.goal.ID, 0); err != nil {
		t.Fatal(err)
	}
	// Notes's work turn ended before the pause; Spec is done and needs none.
	f.wantQueued(t, "the goal resumed", "Cy review Parse", "Ana work Notes", "Bo lead")
}

func TestSettleBlocksItemsThatNobodyWorksOn(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	later := f.create(t, f.lead, item("Later", f.cy.ID, "", parse.ID))
	docs := f.create(t, f.lead, item("Docs", f.bo.ID, ""))
	none := func(string, string) bool { return false }

	if err := f.svc.SettleItems(ctx, none, LeadState{}); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, parse); got.State != ItemTodo {
		t.Fatalf("Parse with a queued turn after settle = %s; want todo", got.State)
	}
	f.start(t, parse, f.ana)
	if err := f.svc.StartItemWork(ctx, docs.ID, f.bo.ID); err != nil {
		t.Fatal(err)
	}
	live := func(botID, teamID string) bool { return botID != f.cy.ID && teamID == f.team.ID }
	if err := f.svc.SettleItems(ctx, live, LeadState{}); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, parse); got.State != ItemDoing {
		t.Fatalf("Parse while Ana's process runs = %s; want doing", got.State)
	}
	if err := f.svc.SettleItems(ctx, none, LeadState{}); err != nil {
		t.Fatal(err)
	}
	got := f.state(t, parse)
	if got.State != ItemBlocked || got.Note != "Ana stopped before handing it on; see the channel for why." {
		t.Fatalf("Parse after Ana stopped = %+v; want it blocked for the lead", got)
	}
	if got := f.state(t, later); got.State != ItemTodo {
		t.Fatalf("Later, which waits for Parse = %s; want todo", got.State)
	}
	if got := f.state(t, docs); got.State != ItemBlocked || got.Note != "Bo stopped before handing it on; see the channel for why." {
		t.Fatalf("Docs after Bo stopped = %+v; want it blocked too", got)
	}
	f.wantQueued(t, "settle", "Bo lead")
	if text := lastEventText(t, f.svc, f.team.ID); text != `"Docs" is blocked: Bo stopped before handing it on; see the channel for why.` {
		t.Fatalf("channel event = %q", text)
	}
}

func TestEndGoalDropsItsTurnsAndKeepsItsBoard(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))

	if err := f.svc.EndGoal(ctx, f.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "the goal ended")
	if text := lastEventText(t, f.svc, f.team.ID); text != "The goal ended." {
		t.Fatalf("channel event = %q; want the end of the goal", text)
	}
	board, err := f.svc.Board(ctx, f.team.ID)
	if err != nil || board.Goal.State != GoalEnded || len(board.Items) != 1 {
		t.Fatalf("board after the end = %+v, %v; want the ended goal with its item", board, err)
	}
	if err := f.svc.EndGoal(ctx, f.goal.ID, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("end an ended goal: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.DropItem(ctx, f.lead, parse.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("drop an item of an ended goal: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.ExtendGoal(ctx, f.goal.ID, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("extend an ended goal: err = %v; want ErrInvalid", err)
	}
	if ok, err := f.svc.GoalAllows(ctx, Turn{GoalID: f.goal.ID, BoardRole: RoleLead}); err != nil || ok {
		t.Fatalf("GoalAllows of an ended goal = %v, %v; want false", ok, err)
	}
	if _, err := f.svc.CreateItem(ctx, f.lead, item("More", "", "")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("add an item after the end: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.StartGoal(ctx, f.team.ID, "Next goal."); err != nil {
		t.Fatalf("a new goal after the end: %v", err)
	}
	if err := f.svc.EndGoal(ctx, "missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("end a missing goal: err = %v; want ErrNotFound", err)
	}
}

func TestCreateItemNeedsAGoal(t *testing.T) {
	svc := newTestService(t)
	team, _, bo, _ := newTestTeam(t, svc)
	if _, err := svc.CreateItem(context.Background(), Actor{BotID: bo.ID, TeamID: team.ID}, item("A", "", "")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("create without a goal: err = %v; want ErrInvalid", err)
	}
}

func TestItemTextsAtTheirLimitsAreAccepted(t *testing.T) {
	f := newBoardFixture(t, 30)
	n := NewItem{Title: strings.Repeat("t", maxItemTitle), Brief: strings.Repeat("b", maxItemText), Acceptance: strings.Repeat("a", maxItemNote)}
	if _, err := f.svc.CreateItem(context.Background(), f.lead, n); err != nil {
		t.Fatalf("an item with texts at their limits: %v", err)
	}
}

func TestADoneItemStartsEveryItemThatWaitedForIt(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	f.create(t, f.lead, item("Docs", f.cy.ID, "", parse.ID))
	f.create(t, f.lead, item("Notes", f.ana.ID, "", parse.ID))
	f.start(t, parse, f.ana)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Parsed."); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "Parse was done", "Cy work Docs", "Ana work Notes")
}

func TestAProposalWakesTheLeadWhileOtherWorkGoesOn(t *testing.T) {
	f := newBoardFixture(t, 30)
	f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	f.create(t, f.as(f.cy, ""), item("Idea", "", ""))
	f.wantQueued(t, "Cy proposed an item while Ana works", "Ana work Parse", "Bo lead")
}

func TestAReviewerBlocksAnItemInReviewForTheLead(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.create(t, f.lead, item("Docs", f.ana.ID, ""))
	f.start(t, parse, f.ana)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Parsed."); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	got, err := f.svc.BlockItem(ctx, f.as(f.cy, RoleReview), parse.ID, "Which CSV dialect?")
	if err != nil || got.State != ItemBlocked || got.Note != "Which CSV dialect?" {
		t.Fatalf("the reviewer blocks = %+v, %v; want it blocked with the question", got, err)
	}
	f.wantQueued(t, "the reviewer blocked Parse", "Bo lead")
}

func TestStartItemWorkStartsOnlyTheOwnersOpenItem(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	f.start(t, parse, f.ana)
	if err := f.svc.StartItemWork(ctx, parse.ID, f.ana.ID); err != nil {
		t.Fatalf("start an item that is being done: %v; want nothing to happen", err)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Parsed."); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartItemWork(ctx, parse.ID, f.ana.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("start a done item: err = %v; want ErrInvalid", err)
	}
	if got := f.state(t, parse); got.State != ItemDone {
		t.Fatalf("Parse = %s; want done", got.State)
	}
	if err := f.svc.StartItemWork(ctx, "missing", f.ana.ID); err == nil {
		t.Fatal("start a missing item: no error")
	}
}

func TestAPostToATeamReturnsItsTurnAndIsIdempotent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, _, _, _ := newTestTeam(t, svc)
	id, dup, err := svc.SubmitUserMessage(ctx, team.ID, "Hello team.", "n1")
	if err != nil || id == "" || dup {
		t.Fatalf("post = %q, %v, %v; want a new turn id", id, dup, err)
	}
	again, dup, err := svc.SubmitUserMessage(ctx, team.ID, "Hello team.", "n1")
	if err != nil || again != id || !dup {
		t.Fatalf("the same post again = %q, %v, %v; want turn %q as a duplicate", again, dup, err, id)
	}
}

// finish takes an item from todo to done by its owner, without a reviewer.
func (f *boardFixture) finish(t *testing.T, it Item, owner Bot) {
	t.Helper()
	f.start(t, it, owner)
	if _, err := f.svc.SubmitItem(context.Background(), f.as(owner, RoleWork), it.ID, "Done: "+it.Title); err != nil {
		t.Fatal(err)
	}
}

func lastEntry(t *testing.T, svc *Service, id string) Entry {
	t.Helper()
	entries, err := svc.Entries(context.Background(), id, 0, 0)
	if err != nil || len(entries) == 0 {
		t.Fatalf("entries = %v, %v", entries, err)
	}
	return entries[len(entries)-1]
}

func TestTheLeadDeliversWhenEveryItemIsClosed(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	docs := f.create(t, f.lead, item("Docs", f.cy.ID, ""))
	lead := f.lead
	lead.TurnID = "turn-9"

	if _, err := f.svc.DeliverGoal(ctx, lead, "Done."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("deliver with open items: err = %v; want ErrInvalid", err)
	}
	f.finish(t, parse, f.ana)
	if _, err := f.svc.DropItem(ctx, f.lead, docs.ID, "Not needed."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeliverGoal(ctx, f.as(f.ana, ""), "Done."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("deliver by a member: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.DeliverGoal(ctx, lead, " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("deliver without a result: err = %v; want ErrInvalid", err)
	}
	g, err := f.svc.DeliverGoal(ctx, lead, "CSV export is in reports.tsx.")
	if err != nil || g.State != GoalDelivered {
		t.Fatalf("deliver = %+v, %v; want a delivered goal", g, err)
	}
	e := lastEntry(t, f.svc, f.team.ID)
	var body struct {
		Text        string `json:"text"`
		NeedsReply  bool   `json:"needsReply"`
		SpeakerName string `json:"speakerName"`
	}
	if err := json.Unmarshal(e.Body, &body); err != nil || e.Kind != KindBot || e.TurnID != "turn-9" || body.Text != "CSV export is in reports.tsx." || !body.NeedsReply || body.SpeakerName != "Bo" {
		t.Fatalf("delivery entry = %+v %s; want Bo's question with the result", e, e.Body)
	}
	if got := attentionOf(t, f.svc, f.team.ID); got != AttentionQuestion {
		t.Fatalf("team attention after delivery = %q; want %q", got, AttentionQuestion)
	}
	if _, err := f.svc.DeliverGoal(ctx, lead, "Again."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("deliver a delivered goal: err = %v; want ErrInvalid", err)
	}
}

func TestAGoalWithoutADoneItemIsNotDelivered(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	idea := f.create(t, f.lead, item("Idea", "", ""))
	if _, err := f.svc.DropItem(ctx, f.lead, idea.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Nothing."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("deliver with only dropped items: err = %v; want ErrInvalid", err)
	}
	other := newBoardFixture(t, 30)
	if _, err := other.svc.DeliverGoal(ctx, Actor{BotID: other.bo.ID, TeamID: "missing"}, "Done."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("deliver without a goal: err = %v; want ErrInvalid", err)
	}
}

func TestAChangeAfterDeliveryOpensTheGoalAgain(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	f.create(t, f.lead, item("Fix", f.ana.ID, ""))
	board, _ := f.svc.Board(ctx, f.team.ID)
	if board.Goal.State != GoalActive {
		t.Fatalf("goal after a new item = %s; want active again", board.Goal.State)
	}
	f.wantQueued(t, "the lead added a fix after delivery", "Ana work Fix")
}

func TestAcceptMakesTheGoalDoneAndGivesTheLeadOneRetroTurn(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	if _, err := f.svc.AcceptGoal(ctx, f.goal.ID, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accept an active goal: err = %v; want ErrInvalid", err)
	}
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)

	g, err := f.svc.AcceptGoal(ctx, f.goal.ID, true)
	if err != nil || g.State != GoalDone {
		t.Fatalf("accept = %+v, %v; want a done goal", g, err)
	}
	f.wantQueued(t, "the user accepted", "Bo retro")
	if e := lastEntry(t, f.svc, f.team.ID); e.Kind != KindUser || !strings.Contains(string(e.Body), `"text":"Accepted the goal."`) {
		t.Fatalf("last entry = %s %s; want the user's acceptance", e.Kind, e.Body)
	}
	if got := attentionOf(t, f.svc, f.team.ID); got != "" {
		t.Fatalf("team attention after Accept = %q; want none: the acceptance answers the delivery", got)
	}
	turns, _ := f.svc.QueuedTurns(ctx, 10)
	if ok, err := f.svc.GoalAllows(ctx, turns[0]); err != nil || !ok {
		t.Fatalf("GoalAllows of the retro turn of a done goal = %v, %v; want true", ok, err)
	}
	before, _ := f.svc.Board(ctx, f.team.ID)
	if started, err := f.svc.StartTurn(ctx, turns[0].ID); err != nil || !started {
		t.Fatal(err)
	}
	after, _ := f.svc.Board(ctx, f.team.ID)
	if after.Goal.Used != before.Goal.Used {
		t.Fatalf("turns used after the retro started = %d; want %d: a retro turn is free", after.Goal.Used, before.Goal.Used)
	}
	if _, err := f.svc.AcceptGoal(ctx, f.goal.ID, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accept a done goal: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.AcceptGoal(ctx, "missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("accept a missing goal: err = %v; want ErrNotFound", err)
	}
}

// docsTeam adds team "Docs" of Dee (the lead) and Eve to the fixture.
func (f *boardFixture) docsTeam(t *testing.T) (team, dee, eve Bot) {
	t.Helper()
	ctx := context.Background()
	dee, err := f.svc.CreateBot(ctx, "Dee", "")
	if err != nil {
		t.Fatal(err)
	}
	eve, err = f.svc.CreateBot(ctx, "Eve", "")
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	team, err = f.svc.CreateTeam(ctx, "Docs", "Write docs.", []string{dee.ID, eve.ID}, TeamSettings{LeadID: dee.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.names[dee.ID], f.names[eve.ID] = "Dee", "Eve"
	return team, dee, eve
}

// queuedAll lists every queued turn as "who role item @team".
func (f *boardFixture) queuedAll(t *testing.T, teams map[string]string) []string {
	t.Helper()
	turns, err := f.svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tr := range turns {
		if tr.GoalID != f.goal.ID {
			t.Fatalf("queued turn %+v; want only turns that the goal pays for", tr)
		}
		out = append(out, strings.TrimSpace(fmt.Sprintf("%s %s %s", f.names[tr.BotID], tr.BoardRole, f.items[tr.ItemID]))+" @"+teams[tr.RoomID])
	}
	return out
}

func TestALeadAsksAnotherTeamAndItsResultUnblocksTheWaitingItems(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, eve := f.docsTeam(t)
	teams := map[string]string{f.team.ID: "Ship", docs.ID: "Docs"}

	// Publish waits on the request: needed_by makes it depend on it, and its
	// work, ready a moment ago, waits.
	publish := f.create(t, f.lead, item("Publish", f.ana.ID, ""))
	req, err := f.svc.RequestWork(ctx, f.lead, docs.ID, NewItem{Title: "Guide", Brief: "Write the export guide.", Acceptance: "guide.md exists.", OwnerID: f.ana.ID, NeededBy: []string{publish.ID}})
	if err != nil {
		t.Fatal(err)
	}
	f.items[req.ID] = req.Title
	if req.TeamID != docs.ID || req.GoalID != f.goal.ID || req.OwnerID != "" {
		t.Fatalf("request = %+v; want an item of Ship's goal in team Docs, without an owner", req)
	}
	if got := f.queuedAll(t, teams); !slices.Equal(got, []string{"Dee lead @Docs"}) {
		t.Fatalf("queued after the request = %q; want Docs's lead to triage it", got)
	}
	if text := lastEventText(t, f.svc, docs.ID); text != `Team Ship asked for "Guide".` {
		t.Fatalf("Docs channel event = %q", text)
	}
	if text := lastEventText(t, f.svc, f.team.ID); text != `Bo asked team Docs for "Guide".` {
		t.Fatalf("Ship channel event = %q", text)
	}
	board, err := f.svc.Board(ctx, docs.ID)
	if err != nil || board.Goal != nil || len(board.Requests) != 1 || board.Requests[0].ID != req.ID || board.Requests[0].FromTeamID != f.team.ID || board.Teams[f.team.ID] != "Ship" {
		t.Fatalf("Docs's board = %+v, %v; want Ship's request", board, err)
	}
	if ship, _ := f.svc.Board(ctx, f.team.ID); len(ship.Items) != 2 || ship.Items[1].TeamID != docs.ID || ship.Teams[docs.ID] != "Docs" ||
		!slices.Equal(ship.Items[0].DependsOn, []string{req.ID}) {
		t.Fatalf("Ship's board = %+v; want the request among its goal's items, and Publish waiting on it", ship)
	}
	finishQueued(t, f.svc)
	if _, err := f.svc.AssignItem(ctx, f.lead, req.ID, f.ana.ID, "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the asking lead assigns the request: err = %v; want ErrInvalid", err)
	}
	docsLead := Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}
	if _, err := f.svc.AssignItem(ctx, docsLead, req.ID, eve.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.queuedAll(t, teams); !slices.Equal(got, []string{"Eve work Guide @Docs"}) {
		t.Fatalf("queued after the assign = %q; want Eve's work in Docs", got)
	}
	if text := lastEventText(t, f.svc, f.team.ID); text != `Team Docs: Dee assigned "Guide" to Eve.` {
		t.Fatalf("Ship channel event = %q; want the request's move", text)
	}
	finishQueued(t, f.svc)
	if err := f.svc.StartItemWork(ctx, req.ID, eve.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitItem(ctx, Actor{BotID: eve.ID, TeamID: docs.ID, Role: RoleWork}, req.ID, "docs/guide.md"); err != nil {
		t.Fatal(err)
	}
	if got := f.queuedAll(t, teams); !slices.Equal(got, []string{"Ana work Publish @Ship"}) {
		t.Fatalf("queued after the request was done = %q; want Ship's waiting item to start", got)
	}
	f.finish(t, publish, f.ana)
	if got := f.queuedAll(t, teams); !slices.Equal(got, []string{"Bo lead @Ship"}) {
		t.Fatalf("queued after the last item = %q; want Ship's lead, whose goal it is", got)
	}
}

func TestOnlyALeadAsksAnotherTeamWithMembersForAnActiveGoal(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, _, _ := f.docsTeam(t)
	room, err := f.svc.CreateRoom(ctx, "Chat", "", []string{f.ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	fay, err := f.svc.CreateBot(ctx, "Fay", "")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := f.svc.CreateTeam(ctx, "Empty", "", []string{fay.ID}, TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteBot(ctx, fay.ID); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	guide := item("Guide", "", "")
	for name, c := range map[string]struct {
		a    Actor
		team string
	}{
		"a member who does not lead": {f.as(f.ana, ""), docs.ID},
		"the lead's own team":        {f.lead, f.team.ID},
		"a plain room":               {f.lead, room.ID},
		"a team in another turn":     {Actor{BotID: f.bo.ID, TeamID: docs.ID, Role: RoleLead}, docs.ID},
		"a team without members":     {f.lead, empty.ID},
	} {
		if _, err := f.svc.RequestWork(ctx, c.a, c.team, guide); !errors.Is(err, ErrInvalid) {
			t.Errorf("request from %s: err = %v; want ErrInvalid", name, err)
		}
	}
	if _, err := f.svc.RequestWork(ctx, f.lead, "missing", guide); !errors.Is(err, ErrNotFound) {
		t.Errorf("request to a missing team: err = %v; want ErrNotFound", err)
	}
	if _, err := f.svc.RequestWork(ctx, f.lead, docs.ID, NewItem{Title: "Guide"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("request without a brief: err = %v; want ErrInvalid", err)
	}
	if err := f.svc.EndGoal(ctx, f.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RequestWork(ctx, f.lead, docs.ID, guide); !errors.Is(err, ErrInvalid) {
		t.Errorf("request without an active goal: err = %v; want ErrInvalid", err)
	}
}

func TestADeclinedRequestWakesTheAskingLead(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, _ := f.docsTeam(t)
	req, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("Guide", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	got, err := f.svc.DropItem(ctx, Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}, req.ID, "We have no time this week.")
	if err != nil || got.State != ItemDropped {
		t.Fatalf("decline = %+v, %v; want the request dropped", got, err)
	}
	if got := f.queuedAll(t, map[string]string{f.team.ID: "Ship", docs.ID: "Docs"}); !slices.Equal(got, []string{"Bo lead @Ship"}) {
		t.Fatalf("queued after the decline = %q; want Ship's lead to decide", got)
	}
}

func TestABlockedRequestWakesBothLeadsWhenNothingElseRuns(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, eve := f.docsTeam(t)
	req, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("Guide", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	f.items[req.ID] = req.Title
	if _, err := f.svc.AssignItem(ctx, Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}, req.ID, eve.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	if err := f.svc.StartItemWork(ctx, req.ID, eve.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.BlockItem(ctx, Actor{BotID: eve.ID, TeamID: docs.ID, Role: RoleWork}, req.ID, "Which format?"); err != nil {
		t.Fatal(err)
	}
	want := []string{"Dee lead @Docs", "Bo lead @Ship"}
	if got := f.queuedAll(t, map[string]string{f.team.ID: "Ship", docs.ID: "Docs"}); !slices.Equal(got, want) {
		t.Fatalf("queued after the request was blocked = %q; want %q", got, want)
	}
}

// queuedOf lists the queued turns of the team as "who role goal", where goal
// is "old" for the fixture's goal and "new" for any other.
func (f *boardFixture) queuedOf(t *testing.T) []string {
	t.Helper()
	turns, err := f.svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, tr := range turns {
		goal := "new"
		if tr.GoalID == f.goal.ID {
			goal = "old"
		}
		out = append(out, fmt.Sprintf("%s %s %s", f.names[tr.BotID], tr.BoardRole, goal))
	}
	return out
}

func TestALeftoverTurnOfAFinishedGoalDoesNotHoldBackTheNextGoal(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	f.wantQueued(t, "the last item was done", "Bo lead")
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "the lead delivered: a delivered goal drops its queued turns")
	if _, err := f.svc.AcceptGoal(ctx, f.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StartGoal(ctx, f.team.ID, "The next goal."); err != nil {
		t.Fatal(err)
	}
	if got, want := f.queuedOf(t), []string{"Bo retro old", "Bo lead new"}; !slices.Equal(got, want) {
		t.Fatalf("queued turns = %q; want %q: the retro of the old goal does not stand in for the new goal's plan", got, want)
	}
	board, err := f.svc.GoalBoard(ctx, f.goal.ID)
	if err != nil || board.Goal.State != GoalDone || len(board.Items) != 1 {
		t.Fatalf("GoalBoard of the accepted goal = %+v, %v; want it done with its item", board, err)
	}
	if _, err := f.svc.GoalBoard(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GoalBoard of a missing goal: err = %v; want ErrNotFound", err)
	}
}

func TestATurnOfAGoalThatIsOverIsCancelled(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	if err := f.svc.EndGoal(ctx, f.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	// A turn of a goal that is over can still be queued, for example by a
	// change that raced with the end.
	id, err := f.svc.queueTurn(ctx, f.svc.q, Turn{BotID: f.bo.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := f.svc.queueTurn(ctx, f.svc.q, Turn{BotID: f.bo.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: "missing", BoardRole: RoleLead})
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range []Turn{{ID: id, GoalID: f.goal.ID}, {ID: gone, GoalID: "missing"}} {
		if ok, err := f.svc.GoalAllows(ctx, turn); ok || err != nil {
			t.Fatalf("GoalAllows of a turn of goal %s = %v, %v; want false", turn.GoalID, ok, err)
		}
	}
	if turns, _ := f.svc.QueuedTurns(ctx, 10); len(turns) != 0 {
		t.Fatalf("queued turns = %+v; want the turns of the ended and the missing goal cancelled", turns)
	}
	if started, err := f.svc.StartTurn(ctx, gone); err != nil || started {
		t.Fatalf("StartTurn of a cancelled turn = %v, %v; want false", started, err)
	}
}

func TestStopBlocksTheRequestsATeamWorksOnWithoutWakingItsLead(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, eve := f.docsTeam(t)
	teams := map[string]string{f.team.ID: "Ship", docs.ID: "Docs"}
	guide, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("Guide", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	spare, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("Spare", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	f.items[guide.ID], f.items[spare.ID] = guide.Title, spare.Title
	faq, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("FAQ", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	f.items[faq.ID] = faq.Title
	docsLead := Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}
	for _, it := range []Item{guide, faq} {
		if _, err := f.svc.AssignItem(ctx, docsLead, it.ID, eve.ID, "", "Spare can wait."); err != nil {
			t.Fatal(err)
		}
	}
	finishQueued(t, f.svc)
	if err := f.svc.StartItemWork(ctx, guide.ID, eve.ID); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.StopTeam(ctx, docs.ID); err != nil {
		t.Fatal(err)
	}
	board, _ := f.svc.Board(ctx, docs.ID)
	states := map[string]string{}
	for _, r := range board.Requests {
		states[r.Title] = r.State + ": " + r.Note
	}
	if states["Guide"] != "blocked: The user stopped team Docs." || states["FAQ"] != "blocked: The user stopped team Docs." || states["Spare"] != "todo: " {
		t.Fatalf("requests after Stop = %v; want Eve's work and her next request blocked, the unassigned one left", states)
	}
	if got := f.queuedAll(t, teams); slices.Contains(got, "Dee lead @Docs") {
		t.Fatalf("queued after Stop = %q; want no turn of the stopped team's lead", got)
	}
	if err := f.svc.StopTeam(ctx, docs.ID); err != nil {
		t.Fatalf("Stop again: %v; want nothing to do", err)
	}
}

func TestAMemberWhoLeavesFreesItsItems(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	docs := f.create(t, f.lead, item("Docs", f.cy.ID, f.ana.ID))
	later := f.create(t, f.lead, item("Later", f.cy.ID, "", parse.ID))
	f.start(t, parse, f.ana)
	if err := f.svc.StartItemWork(ctx, docs.ID, f.cy.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.cy, RoleWork), docs.ID, "docs.md"); err != nil {
		t.Fatal(err)
	}
	f.wantQueued(t, "Cy submitted Docs", "Ana review Docs")

	if _, err := f.svc.SetRoomMembers(ctx, f.team.ID, []string{f.bo.ID, f.cy.ID}); err != nil {
		t.Fatal(err)
	}
	got := f.state(t, parse)
	if got.State != ItemTodo || got.OwnerID != "" || got.ReviewerID != "" || got.Note != "Ana left the team; the item waits for a new owner." {
		t.Fatalf("Ana's item after she left = %+v; want it to wait for a new owner", got)
	}
	if got := f.state(t, docs); got.State != ItemBlocked || got.ReviewerID != "" || got.Note != "Ana left the team before reviewing it." {
		t.Fatalf("the item Ana reviewed = %+v; want it blocked for the lead without a reviewer", got)
	}
	f.wantQueued(t, "Ana left", "Bo lead")
	if text := lastEventText(t, f.svc, f.team.ID); text != "Members: Bo, Cy. Lead: Bo" {
		t.Fatalf("channel event = %q", text)
	}

	finishQueued(t, f.svc)
	if _, err := f.svc.AssignItem(ctx, f.lead, docs.ID, f.cy.ID, f.bo.ID, ""); err != nil {
		t.Fatalf("assign the blocked item again: %v", err)
	}
	f.wantQueued(t, "the lead assigned the blocked item", "Cy work Docs")
	if got := f.state(t, later); got.State != ItemTodo || got.OwnerID != f.cy.ID {
		t.Fatalf("Later = %+v; want it to keep waiting for Parse", got)
	}
}

func TestADeletedOwnerDoesNotBreakTheWorkOfOthers(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.cy.ID, ""))
	later := f.create(t, f.lead, item("Later", f.ana.ID, "", parse.ID))
	if err := f.svc.DeleteBot(ctx, f.ana.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, later); got.OwnerID != "" || got.State != ItemTodo {
		t.Fatalf("the deleted bot's item = %+v; want it to wait for a new owner", got)
	}
	f.start(t, parse, f.cy)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.cy, RoleWork), parse.ID, "Parsed."); err != nil {
		t.Fatalf("submit after the owner of a waiting item was deleted: %v", err)
	}
}

func TestUnblockSendsAnItemBackToWhereItStopped(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.start(t, parse, f.ana)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Parsed."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.BlockItem(ctx, f.as(f.cy, RoleReview), parse.ID, "Which dialect?"); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	got, err := f.svc.UnblockItem(ctx, f.lead, parse.ID, "RFC 4180.")
	if err != nil || got.State != ItemReview || got.BlockedFrom != "" {
		t.Fatalf("unblock a review = %+v, %v; want it back in review", got, err)
	}
	f.wantQueued(t, "the lead answered the reviewer", "Cy review Parse")
	if text := lastEventText(t, f.svc, f.team.ID); text != `Bo unblocked "Parse".` {
		t.Fatalf("channel event = %q", text)
	}
}

func TestUnblockedStateFollowsTheOwnerAndTheReviewer(t *testing.T) {
	cases := []struct {
		it   Item
		want string
	}{
		{Item{BlockedFrom: ItemReview, OwnerID: "a", ReviewerID: "r"}, ItemReview},
		{Item{BlockedFrom: ItemReview, OwnerID: "a"}, ItemDoing},
		{Item{BlockedFrom: ItemDoing, OwnerID: "a"}, ItemDoing},
		{Item{BlockedFrom: ItemDoing}, ItemTodo},
		{Item{BlockedFrom: ItemTodo, OwnerID: "a"}, ItemTodo},
		{Item{OwnerID: "a"}, ItemTodo},
	}
	for _, c := range cases {
		if got := unblockedState(c.it); got != c.want {
			t.Errorf("unblockedState(%+v) = %s; want %s", c.it, got, c.want)
		}
	}
}

func TestReviewRoundsBlockEveryThirdRound(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.start(t, parse, f.ana)
	round := func(want string) Item {
		t.Helper()
		if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Try."); err != nil {
			t.Fatal(err)
		}
		got, err := f.svc.RequestChanges(ctx, f.as(f.cy, RoleReview), parse.ID, "Not yet.")
		if err != nil || got.State != want {
			t.Fatalf("changes in round %d = %+v, %v; want %s", got.ReviewRounds, got, err, want)
		}
		return got
	}
	round(ItemDoing)
	round(ItemDoing)
	if got := round(ItemBlocked); got.BlockedFrom != ItemDoing {
		t.Fatalf("blocked after 3 rounds = %+v; want it to go back to its owner when unblocked", got)
	}
	if got, err := f.svc.UnblockItem(ctx, f.lead, parse.ID, "Ship what passes the tests."); err != nil || got.State != ItemDoing {
		t.Fatalf("unblock = %+v, %v; want the owner to work again", got, err)
	}
	round(ItemDoing)
	round(ItemDoing)
	round(ItemBlocked)
}

func TestWorkStartsOnlyForTheCurrentOwner(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	if _, err := f.svc.AssignItem(ctx, f.lead, parse.ID, f.cy.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.StartItemWork(ctx, parse.ID, f.ana.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Ana starts the item that went to Cy: err = %v; want ErrInvalid", err)
	}
	if got := f.state(t, parse); got.State != ItemTodo || got.OwnerID != f.cy.ID {
		t.Fatalf("Parse = %+v; want it to wait for Cy", got)
	}
}

func TestThePauseAtTheBudgetYieldsToMoreTurns(t *testing.T) {
	f := newBoardFixture(t, 2)
	ctx := context.Background()
	stale := f.goal
	stale.Used = 2
	if _, err := f.svc.ExtendGoal(ctx, f.goal.ID, 5); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.pauseAtBudget(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if board, _ := f.svc.Board(ctx, f.team.ID); board.Goal.State != GoalActive {
		t.Fatalf("goal after a pause with an old budget = %+v; want it still active", board.Goal)
	}
}

func TestSettleLeavesAnItemThatMovedSinceItWasRead(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.start(t, parse, f.ana)
	read := f.state(t, parse)
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Parsed."); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.moveSettled(ctx, Actor{}, read, "Ana stopped."); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, parse); got.State != ItemReview {
		t.Fatalf("Parse after a settle on an old read = %s; want review", got.State)
	}
}

func TestSettleWakesTheLeadOfAGoalThatStalled(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	none := func(string, string) bool { return false }
	settle := func(step string, want ...string) {
		t.Helper()
		if err := f.svc.SettleItems(ctx, none, LeadState{}); err != nil {
			t.Fatal(err)
		}
		f.wantQueued(t, step, want...)
	}
	settle("settle after the lead's plan turn ended well")

	stopped, err := f.svc.queueTurn(ctx, f.svc.q, Turn{BotID: f.bo.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StartTurn(ctx, stopped); err != nil {
		t.Fatal(err)
	}
	settle("settle while the lead's turn runs")
	if err := f.svc.FinishTurn(ctx, stopped, "interrupted", "server restarted"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.svc.TurnsReady():
	default:
	}
	settle("settle after a restart stopped the lead's turn", "Bo lead")
	select {
	case <-f.svc.TurnsReady():
	default:
		t.Fatal("the lead's new turn was queued without a signal to the runner")
	}
	settle("settle while the lead's new turn waits", "Bo lead")

	finishQueued(t, f.svc)
	again, err := f.svc.queueTurn(ctx, f.svc.q, Turn{BotID: f.bo.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StartTurn(ctx, again); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.FinishTurn(ctx, again, "failed", "boom"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.SubmitUserMessage(ctx, f.team.ID, "Status?", "n1"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SettleItems(ctx, none, LeadState{}); err != nil {
		t.Fatal(err)
	}
	if got := f.queuedOf(t); !slices.Equal(got, []string{"Bo  new"}) {
		t.Fatalf("queued while the lead has a chat turn = %q; want only the chat turn", got)
	}
}

func TestOnlyTheLeadReopensADeliveredGoalAndDroppedItemsLeaveRoom(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateItem(ctx, f.as(f.ana, ""), item("More", "", "")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a member adds to a delivered goal: err = %v; want ErrInvalid", err)
	}
	if board, _ := f.svc.Board(ctx, f.team.ID); board.Goal.State != GoalDelivered {
		t.Fatalf("goal = %s; want it still delivered", board.Goal.State)
	}

	g := newBoardFixture(t, 30)
	for i := range maxGoalItems - 1 {
		g.create(t, g.lead, item(fmt.Sprintf("Item %d", i), "", ""))
	}
	dropped := g.create(t, g.lead, item("Dropped", "", ""))
	if _, err := g.svc.DropItem(ctx, g.lead, dropped.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := g.svc.CreateItem(ctx, g.lead, item("One more", "", "")); err != nil {
		t.Fatalf("item %d with one dropped: %v; want it accepted", maxGoalItems+1, err)
	}
}

func TestReviewsItemFindsAnOpenReview(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	f.start(t, parse, f.ana)
	if got, err := f.svc.ReviewsItem(ctx, f.team.ID, f.cy.ID); err != nil || got {
		t.Fatalf("ReviewsItem before the submit = %v, %v; want false", got, err)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.ana, RoleWork), parse.ID, "Parsed."); err != nil {
		t.Fatal(err)
	}
	if got, err := f.svc.ReviewsItem(ctx, f.team.ID, f.cy.ID); err != nil || !got {
		t.Fatalf("ReviewsItem in the review = %v, %v; want true", got, err)
	}
	if got, _ := f.svc.ReviewsItem(ctx, f.team.ID, f.ana.ID); got {
		t.Fatal("ReviewsItem of the owner = true; want false")
	}
}

func TestOnlyAUserPostInARoomCountsAsOne(t *testing.T) {
	cases := []struct {
		t    Turn
		want bool
	}{
		{Turn{Source: SourceRoom}, true},
		{Turn{Source: SourceRoom, Hops: 1}, false},
		{Turn{Source: SourceRoom, BoardRole: RoleLead}, false},
		{Turn{Source: SourceUser}, false},
	}
	for _, c := range cases {
		if got := c.t.FromUserPost(); got != c.want {
			t.Errorf("FromUserPost(%+v) = %v; want %v", c.t, got, c.want)
		}
	}
}

func TestSettleWakesTheLeadOfEveryStalledGoal(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, _ := f.docsTeam(t)
	docsGoal, err := f.svc.StartGoal(ctx, docs.ID, "Docs of our own.")
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	for _, g := range []Goal{f.goal, docsGoal} {
		team, lead := f.team, f.bo
		if g.ID == docsGoal.ID {
			team, lead = docs, dee
		}
		id, err := f.svc.queueTurn(ctx, f.svc.q, Turn{BotID: lead.ID, Source: SourceRoom, RoomID: team.ID, GoalID: g.ID, BoardRole: RoleLead})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.StartTurn(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := f.svc.FinishTurn(ctx, id, "interrupted", "server restarted"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.SettleItems(ctx, func(string, string) bool { return false }, LeadState{}); err != nil {
		t.Fatal(err)
	}
	turns, _ := f.svc.QueuedTurns(ctx, 10)
	woken := map[string]bool{}
	for _, tr := range turns {
		woken[tr.GoalID] = tr.BoardRole == RoleLead
	}
	if !woken[f.goal.ID] || !woken[docsGoal.ID] || len(turns) != 2 {
		t.Fatalf("queued after a restart = %+v; want a lead turn for each stalled goal", turns)
	}
}

func TestADeletedBotIsReleasedInEveryTeam(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, _ := f.docsTeam(t)
	if _, err := f.svc.SetRoomMembers(ctx, docs.ID, []string{dee.ID, f.ana.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StartGoal(ctx, docs.ID, "Docs of our own."); err != nil {
		t.Fatal(err)
	}
	ship := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	guide, err := f.svc.CreateItem(ctx, Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}, item("Guide", f.ana.ID, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteBot(ctx, f.ana.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.state(t, ship); got.OwnerID != "" {
		t.Fatalf("Ship's item after Ana was deleted = %+v; want no owner", got)
	}
	board, _ := f.svc.Board(ctx, docs.ID)
	for _, it := range board.Items {
		if it.ID == guide.ID && it.OwnerID != "" {
			t.Fatalf("Docs's item after Ana was deleted = %+v; want no owner", it)
		}
	}
}

// otherTeam adds a team of two new bots, the first its lead, to the fixture.
func (f *boardFixture) otherTeam(t *testing.T, name, leadName, memberName string) (team, lead Bot) {
	t.Helper()
	ctx := context.Background()
	lead, err := f.svc.CreateBot(ctx, leadName, "")
	if err != nil {
		t.Fatal(err)
	}
	member, err := f.svc.CreateBot(ctx, memberName, "")
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	team, err = f.svc.CreateTeam(ctx, name, "", []string{lead.ID, member.ID}, TeamSettings{LeadID: lead.ID})
	if err != nil {
		t.Fatal(err)
	}
	f.names[lead.ID], f.names[member.ID] = leadName, memberName
	return team, lead
}

func TestATeamServingARequestPassesWorkOnToAThirdTeamUnderTheAskingGoal(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, eve := f.docsTeam(t)
	dev, cal := f.otherTeam(t, "Dev", "Cal", "Dan")
	teams := map[string]string{f.team.ID: "Ship", docs.ID: "Docs", dev.ID: "Dev"}

	spec, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("Spec", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	f.items[spec.ID] = spec.Title
	docsLead := Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}
	if _, err := f.svc.AssignItem(ctx, docsLead, spec.ID, eve.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	if err := f.svc.StartItemWork(ctx, spec.ID, eve.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitItem(ctx, Actor{BotID: eve.ID, TeamID: docs.ID, Role: RoleWork}, spec.ID, "spec.md"); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)

	if _, err := f.svc.RequestWork(ctx, docsLead, f.team.ID, item("Back", "", "")); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "submit your result") {
		t.Fatalf("pass work back to the goal's team: err = %v; want ErrInvalid that says to submit", err)
	}
	build, err := f.svc.RequestWork(ctx, docsLead, dev.ID, item("Build", "", "", spec.ID))
	if err != nil {
		t.Fatalf("chained request: %v", err)
	}
	f.items[build.ID] = build.Title
	if build.GoalID != f.goal.ID || build.TeamID != dev.ID || build.CreatedBy != dee.ID || build.ForItemID != spec.ID || !slices.Equal(build.DependsOn, []string{spec.ID}) {
		t.Fatalf("chained request = %+v; want an item of Ship's goal in Dev, by Dee, after Spec", build)
	}
	// Nothing of Ship's goal is in flight, so Ship's lead hears of the new item too.
	if got := f.queuedAll(t, teams); !slices.Equal(got, []string{"Cal lead @Dev", "Bo lead @Ship"}) {
		t.Fatalf("queued after the chained request = %q; want Dev's and Ship's leads, paid by Ship's goal", got)
	}
	for id, want := range map[string]string{
		dev.ID:    `Team Docs asked for "Build", for team Ship's goal.`,
		docs.ID:   `Dee asked team Dev for "Build", for team Ship's goal.`,
		f.team.ID: `Team Docs asked team Dev for "Build", for this goal.`,
	} {
		if text := lastEventText(t, f.svc, id); text != want {
			t.Errorf("%s channel event = %q; want %q", teams[id], text, want)
		}
	}
	ship, err := f.svc.Board(ctx, f.team.ID)
	if err != nil || len(ship.Items) != 3 || ship.Items[2].ID != build.ID || ship.Teams[dev.ID] != "Dev" {
		t.Fatalf("Ship's board = %+v, %v; want the chained request among its goal's items", ship, err)
	}
	if board, _ := f.svc.Board(ctx, dev.ID); len(board.Requests) != 1 || board.Requests[0].FromTeamID != f.team.ID {
		t.Fatalf("Dev's board = %+v; want Ship's request", board)
	}

	release := f.create(t, f.lead, item("Release", f.ana.ID, "", build.ID))
	if _, err := f.svc.AssignItem(ctx, Actor{BotID: cal.ID, TeamID: dev.ID, Role: RoleLead}, build.ID, cal.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	if err := f.svc.StartItemWork(ctx, build.ID, cal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubmitItem(ctx, Actor{BotID: cal.ID, TeamID: dev.ID, Role: RoleWork}, build.ID, "PR 1"); err != nil {
		t.Fatal(err)
	}
	// queuedAll also checks that Ship's goal pays for every turn, Dev's work included.
	if got := f.queuedAll(t, teams); !slices.Equal(got, []string{"Ana work Release @Ship"}) {
		t.Fatalf("queued after the chained request was done = %q; want Ship's waiting item", got)
	}
	f.finish(t, release, f.ana)
}

func TestARequestWithoutAGoalSaysHowToGoOn(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, _ := f.docsTeam(t)
	dev, _ := f.otherTeam(t, "Dev", "Cal", "Dan")
	docsLead := Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}

	_, err := f.svc.RequestWork(ctx, docsLead, dev.ID, item("Build", "", ""))
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "the user starts a goal") {
		t.Fatalf("request without any goal: err = %v; want ErrInvalid that says how to go on", err)
	}
	spec, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("Spec", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DropItem(ctx, docsLead, spec.ID, "Not ours."); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RequestWork(ctx, docsLead, dev.ID, item("Build", "", "")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("request for a dropped request: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("Spec 2", "", ""))); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.EndGoal(ctx, f.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RequestWork(ctx, docsLead, dev.ID, item("Build", "", "")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("request for an ended goal's request: err = %v; want ErrInvalid", err)
	}
}

func TestALeadServingSeveralGoalsNamesTheRequestItsWorkIsFor(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	docs, dee, _ := f.docsTeam(t)
	dev, _ := f.otherTeam(t, "Dev", "Cal", "Dan")
	ops, olga := f.otherTeam(t, "Ops", "Olga", "Oz")
	opsGoal, err := f.svc.StartGoal(ctx, ops.ID, "Run it.")
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	docsLead := Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}

	shipSpec, err := f.svc.RequestWork(ctx, f.lead, docs.ID, f.needs(t, item("Spec", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	opsLead := Actor{BotID: olga.ID, TeamID: ops.ID, Role: RoleLead}
	opsSpec, err := f.svc.RequestWork(ctx, opsLead, docs.ID, waitedOn(t, f.svc, opsLead, item("Runbook", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.RequestWork(ctx, docsLead, dev.ID, item("Build", "", ""))
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), shipSpec.ID) || !strings.Contains(err.Error(), opsSpec.ID) {
		t.Fatalf("request while serving two goals: err = %v; want ErrInvalid that lists both requests", err)
	}
	build, err := f.svc.RequestWork(ctx, docsLead, dev.ID, NewItem{Title: "Build", Brief: "Do it.", Acceptance: "Works.", ForItemID: opsSpec.ID})
	if err != nil || build.GoalID != opsGoal.ID {
		t.Fatalf("request for Ops's request = %+v, %v; want an item of Ops's goal", build, err)
	}
	if _, err := f.svc.RequestWork(ctx, docsLead, dev.ID, NewItem{Title: "X", Brief: "Do it.", Acceptance: "Works.", ForItemID: build.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("request for an item Docs doesn't serve: err = %v; want ErrInvalid", err)
	}

	// The team's own active goal comes first, unless the lead names a request.
	if _, err := f.svc.StartGoal(ctx, docs.ID, "Write docs."); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	own, err := f.svc.RequestWork(ctx, docsLead, dev.ID, waitedOn(t, f.svc, docsLead, item("Own", "", "")))
	if err != nil || own.GoalID == f.goal.ID || own.GoalID == opsGoal.ID {
		t.Fatalf("request with an own goal = %+v, %v; want an item of Docs's goal", own, err)
	}
	forShip, err := f.svc.RequestWork(ctx, docsLead, dev.ID, NewItem{Title: "Build", Brief: "Do it.", Acceptance: "Works.", ForItemID: shipSpec.ID})
	if err != nil || forShip.GoalID != f.goal.ID {
		t.Fatalf("request for Ship's request = %+v, %v; want an item of Ship's goal", forShip, err)
	}
}
