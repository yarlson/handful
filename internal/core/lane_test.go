package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// turns lists every queued turn as "who role item", oldest first.
func (f *boardFixture) turns(t *testing.T) []string {
	t.Helper()
	turns, err := f.svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, tr := range turns {
		out = append(out, strings.TrimSpace(fmt.Sprintf("%s %s %s", f.names[tr.BotID], tr.BoardRole, f.items[tr.ItemID])))
	}
	return out
}

func (f *boardFixture) wantTurns(t *testing.T, step string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := f.turns(t); !slices.Equal(got, want) {
		t.Fatalf("after %s, queued turns = %q; want %q", step, got, want)
	}
}

// ask has the fixture's lead ask team other for an item.
func (f *boardFixture) ask(t *testing.T, other Bot, n NewItem) Item {
	t.Helper()
	it, err := f.svc.RequestWork(context.Background(), f.lead, other.ID, f.needs(t, n))
	if err != nil {
		t.Fatalf("request %q: %v", n.Title, err)
	}
	f.items[it.ID] = it.Title
	return it
}

// request returns the view of a request on the board of team.
func (f *boardFixture) request(t *testing.T, teamID string, it Item) Request {
	t.Helper()
	board, err := f.svc.Board(context.Background(), teamID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range board.Requests {
		if r.ID == it.ID {
			return r
		}
	}
	t.Fatalf("request %q is not on the board of %s", it.Title, teamID)
	return Request{}
}

// asked returns the view of a request on the asking board of team.
func (f *boardFixture) asked(t *testing.T, teamID string, it Item) Request {
	t.Helper()
	board, err := f.svc.Board(context.Background(), teamID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range board.Asked {
		if r.ID == it.ID {
			return r
		}
	}
	t.Fatalf("request %q is not among the asks of %s", it.Title, teamID)
	return Request{}
}

// runQueued starts and finishes every queued turn, as the runner would.
func runQueued(t *testing.T, svc *Service) {
	t.Helper()
	ctx := context.Background()
	turns, err := svc.QueuedTurns(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range turns {
		if _, err := svc.StartTurn(ctx, tr.ID); err != nil {
			t.Fatal(err)
		}
		if err := svc.FinishTurn(ctx, tr.ID, "done", ""); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *boardFixture) goalNow(t *testing.T, id string) Goal {
	t.Helper()
	g, err := f.svc.goal(context.Background(), f.svc.q, id)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func (f *boardFixture) itemNow(t *testing.T, id string) Item {
	t.Helper()
	row, err := f.svc.q.GetItem(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	it, err := toItem(row)
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestASecondGoalIsQueuedAndAQueueHoldsTen(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	next, err := f.svc.StartGoal(ctx, f.team.ID, "Ship JSON export.")
	if err != nil || next.State != GoalQueued || next.QueuePos != 1 || next.Budget != 0 {
		t.Fatalf("a goal while one is active = %+v, %v; want it queued first, without a budget", next, err)
	}
	f.wantTurns(t, "a queued goal")
	if text := lastEventText(t, f.svc, f.team.ID); text != "Queued goal #1: Ship JSON export." {
		t.Fatalf("channel event = %q", text)
	}
	lead := Actor{BotID: f.bo.ID, TeamID: f.team.ID, Role: RoleLead, UserPost: true}
	byLead, err := f.svc.StartGoalAsLead(ctx, lead, "Ship XML export.")
	if err != nil || byLead.State != GoalQueued || byLead.QueuePos != 2 {
		t.Fatalf("the lead's goal while one is active = %+v, %v; want it queued second", byLead, err)
	}
	board, err := f.svc.Board(ctx, f.team.ID)
	if err != nil || board.Goal.ID != f.goal.ID || len(board.Queue) != 2 || board.Queue[0].ID != next.ID || board.Queue[1].ID != byLead.ID {
		t.Fatalf("board = %+v, %v; want the active goal and the queue in order", board, err)
	}
	if _, err := f.svc.CreateItem(ctx, f.lead, item("Parse", "", "")); err != nil {
		t.Fatal(err)
	}
	if items, _ := goalItems(ctx, f.svc.q, next.ID); len(items) != 0 {
		t.Fatalf("items of the queued goal = %+v; want none: items go to the active goal", items)
	}
	for i := 3; i <= MaxQueuedGoals; i++ {
		if _, err := f.svc.StartGoal(ctx, f.team.ID, fmt.Sprintf("Goal %d.", i)); err != nil {
			t.Fatal(err)
		}
	}
	_, err = f.svc.StartGoal(ctx, f.team.ID, "One too many.")
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "Ship has 10 goals queued; cancel one first") {
		t.Fatalf("the 11th queued goal: err = %v; want ErrInvalid", err)
	}
}

func TestConcurrentStartsMakeOneActiveGoalAndQueueTheRest(t *testing.T) {
	svc := newTestService(t)
	team, _, _, _ := newTestTeam(t, svc)
	const n = 5
	goals := make([]Goal, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { goals[i], errs[i] = svc.StartGoal(context.Background(), team.ID, fmt.Sprintf("Goal %d.", i)) })
	}
	wg.Wait()
	active, positions := 0, []int64{}
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("start %d: %v; want no error", i, errs[i])
		}
		if goals[i].State == GoalActive {
			active++
		} else {
			positions = append(positions, goals[i].QueuePos)
		}
	}
	slices.Sort(positions)
	if active != 1 || !slices.Equal(positions, []int64{1, 2, 3, 4}) {
		t.Fatalf("goals = %+v; want one active and the others queued 1 to 4", goals)
	}
}

func TestClosingATaskStartsTheNextAtOnceAndApprovalAddsTheRetro(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	next, err := f.svc.StartGoal(ctx, f.team.ID, "Ship JSON export.")
	if err != nil {
		t.Fatal(err)
	}
	later, err := f.svc.StartGoal(ctx, f.team.ID, "Ship XML export.")
	if err != nil {
		t.Fatal(err)
	}
	budget := int64(12)
	if _, err := f.svc.UpdateTeam(ctx, f.team.ID, TeamPatch{GoalBudget: &budget}); err != nil {
		t.Fatal(err)
	}
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	finishQueued(t, f.svc)
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	g := f.goalNow(t, next.ID)
	if g.State != GoalActive || g.Budget != 12 || g.QueuePos != 0 {
		t.Fatalf("next goal after the close = %+v; want it active at once with the team's budget now", g)
	}
	if got := f.queuedOf(t); !slices.Equal(got, []string{"Bo lead new"}) {
		t.Fatalf("queued after the close = %q; want the next goal's planning", got)
	}
	if text := lastEventText(t, f.svc, f.team.ID); text != "Next goal started: Ship JSON export." {
		t.Fatalf("channel event = %q", text)
	}
	if done := f.goalNow(t, f.goal.ID); done.State != GoalDelivered {
		t.Fatalf("the closed goal = %s; want it waiting for approval", done.State)
	}
	finishQueued(t, f.svc)
	if _, err := f.svc.AcceptGoal(ctx, f.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := f.queuedOf(t); !slices.Equal(got, []string{"Bo retro old"}) {
		t.Fatalf("queued after the approval = %q; want the retro", got)
	}
	// The next goal runs, so the queue is not on hold.
	if text := lastEventText(t, f.svc, f.team.ID); strings.Contains(text, "on hold") {
		t.Fatalf("channel event after the approval = %q; want no hold note while a goal is Now", text)
	}
	if l := f.goalNow(t, later.ID); l.State != GoalQueued || l.QueuePos != 1 {
		t.Fatalf("later goal = %+v; want it first in the queue now", l)
	}
}

func TestEndingAGoalStartsTheNextOrHoldsTheQueue(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	next, err := f.svc.StartGoal(ctx, f.team.ID, "Ship JSON export.")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.EndGoal(ctx, next.ID, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("end a queued goal: err = %v; want ErrInvalid", err)
	}
	if _, err := f.svc.StartNextGoal(ctx, f.team.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("start the next goal while one is open: err = %v; want ErrConflict", err)
	}
	if err := f.svc.EndGoal(ctx, f.goal.ID, false); err != nil {
		t.Fatal(err)
	}
	if g := f.goalNow(t, next.ID); g.State != GoalQueued {
		t.Fatalf("next goal after an end that holds = %+v; want it queued", g)
	}
	if text := lastEventText(t, f.svc, f.team.ID); text != "The queue is on hold: start the next goal when you are ready." {
		t.Fatalf("channel event = %q", text)
	}
	appended, err := f.svc.StartGoal(ctx, f.team.ID, "Ship XML export.")
	if err != nil || appended.State != GoalQueued || appended.QueuePos != 2 {
		t.Fatalf("a goal while the queue holds = %+v, %v; want it queued behind", appended, err)
	}
	f.wantTurns(t, "goals while the queue holds")
	started, err := f.svc.StartNextGoal(ctx, f.team.ID)
	if err != nil || started.ID != next.ID || started.State != GoalActive {
		t.Fatalf("start the next goal = %+v, %v; want the head active", started, err)
	}
	f.wantTurns(t, "the next goal started", "Bo lead")
	finishQueued(t, f.svc)
	if err := f.svc.EndGoal(ctx, next.ID, true); err != nil {
		t.Fatal(err)
	}
	if g := f.goalNow(t, appended.ID); g.State != GoalActive || g.QueuePos != 0 {
		t.Fatalf("goal after an end = %+v; want it active", g)
	}
	if _, err := f.svc.StartNextGoal(ctx, f.team.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("start the next goal of an empty queue: err = %v; want ErrConflict", err)
	}
	if err := f.svc.EndGoal(ctx, appended.ID, false); err != nil {
		t.Fatal(err)
	}
	if text := lastEventText(t, f.svc, f.team.ID); strings.Contains(text, "on hold") {
		t.Fatalf("channel event after an end with an empty queue = %q; want no hold note", text)
	}
	if _, err := f.svc.StartNextGoal(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("start the next goal of a missing team: err = %v; want ErrNotFound", err)
	}
}

func TestADoneTaskDoesNotHoldItsTeam(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	// With nothing planned, a new goal starts at once beside the done one.
	next, err := f.svc.StartGoal(ctx, f.team.ID, "Ship JSON export.")
	if err != nil || next.State != GoalActive {
		t.Fatalf("a goal beside a done one = %+v, %v; want it active", next, err)
	}
	board, err := f.svc.Board(ctx, f.team.ID)
	if err != nil || board.Goal == nil || board.Goal.ID != next.ID || len(board.Done) != 1 || board.Done[0].ID != f.goal.ID {
		t.Fatalf("board = %+v, %v; want the new goal Now and the old one done", board, err)
	}
	// A new item goes to the Now goal, not the done one.
	more := f.create(t, f.lead, item("More", "", ""))
	if more.GoalID != next.ID || f.goalNow(t, f.goal.ID).State != GoalDelivered {
		t.Fatalf("item %+v; want it on the Now goal, and the done one untouched", more)
	}
}

func TestTheUserReordersAndCancelsQueuedGoals(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	var queue []Goal
	for _, text := range []string{"One.", "Two.", "Three."} {
		g, err := f.svc.StartGoal(ctx, f.team.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		queue = append(queue, g)
	}
	order := func() []string {
		board, err := f.svc.Board(ctx, f.team.ID)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for i, g := range board.Queue {
			if g.QueuePos != int64(i+1) {
				t.Fatalf("queue = %+v; want positions 1 to n", board.Queue)
			}
			out = append(out, g.Text)
		}
		return out
	}
	if err := f.svc.ReorderGoalQueue(ctx, f.team.ID, []string{queue[2].ID, queue[0].ID, queue[1].ID}); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []string{"Three.", "One.", "Two."}) {
		t.Fatalf("queue after the reorder = %q", got)
	}
	if text := lastEventText(t, f.svc, f.team.ID); text != "The user reordered the queued goals." {
		t.Fatalf("channel event = %q", text)
	}
	for name, ids := range map[string][]string{
		"a missing goal":  {queue[0].ID, queue[1].ID},
		"a goal twice":    {queue[0].ID, queue[0].ID, queue[1].ID},
		"an active goal":  {queue[0].ID, queue[1].ID, f.goal.ID},
		"one goal more":   {queue[0].ID, queue[1].ID, queue[2].ID, f.goal.ID},
		"a goal replaced": {queue[0].ID, queue[1].ID, "other"},
	} {
		if err := f.svc.ReorderGoalQueue(ctx, f.team.ID, ids); !errors.Is(err, ErrConflict) {
			t.Errorf("reorder with %s: err = %v; want ErrConflict", name, err)
		}
	}
	if err := f.svc.ReorderGoalQueue(ctx, "missing", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("reorder a missing team: err = %v; want ErrNotFound", err)
	}
	if err := f.svc.CancelGoal(ctx, queue[2].ID); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []string{"One.", "Two."}) {
		t.Fatalf("queue after the cancel = %q", got)
	}
	if g := f.goalNow(t, queue[2].ID); g.State != GoalEnded {
		t.Fatalf("cancelled goal = %+v; want it ended", g)
	}
	if text := lastEventText(t, f.svc, f.team.ID); text != "Cancelled before it started: Three." {
		t.Fatalf("channel event = %q", text)
	}
	if err := f.svc.CancelGoal(ctx, f.goal.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "end it instead") {
		t.Fatalf("cancel the active goal: err = %v; want ErrInvalid", err)
	}
	if err := f.svc.CancelGoal(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel a missing goal: err = %v; want ErrNotFound", err)
	}
}

// laneFixture is the board fixture with team Docs serving Ship, Docs working
// on its own active goal.
type laneFixture struct {
	*boardFixture
	docs, dee, eve Bot
	docsLead       Actor
	docsGoal       Goal
}

func newLaneFixture(t *testing.T, ownGoal bool) *laneFixture {
	t.Helper()
	f := newBoardFixture(t, 30)
	docs, dee, eve := f.docsTeam(t)
	l := &laneFixture{boardFixture: f, docs: docs, dee: dee, eve: eve, docsLead: Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead}}
	if ownGoal {
		g, err := f.svc.StartGoal(context.Background(), docs.ID, "Write docs.")
		if err != nil {
			t.Fatal(err)
		}
		l.docsGoal = g
	}
	finishQueued(t, f.svc)
	return l
}

func (l *laneFixture) accept(t *testing.T, it Item, owner Bot, note string) {
	t.Helper()
	if _, err := l.svc.AssignItem(context.Background(), l.docsLead, it.ID, owner.ID, "", note); err != nil {
		t.Fatalf("accept %q: %v", it.Title, err)
	}
}

func (l *laneFixture) work(t *testing.T, it Item, owner Bot) {
	t.Helper()
	ctx := context.Background()
	finishQueued(t, l.svc)
	if err := l.svc.StartItemWork(ctx, it.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.svc.SubmitItem(ctx, Actor{BotID: owner.ID, TeamID: l.docs.ID, Role: RoleWork}, it.ID, "Done: "+it.Title); err != nil {
		t.Fatal(err)
	}
}

func TestOneSlotRunsOneRequestWhileTheOwnGoalIsActive(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	guide, faq := l.ask(t, l.docs, item("Guide", "", "")), l.ask(t, l.docs, item("FAQ", "", ""))
	finishQueued(t, l.svc)
	l.accept(t, guide, l.eve, "")
	l.wantTurns(t, "the first accept", "Eve work Guide")
	l.accept(t, faq, l.dee, "")
	l.wantTurns(t, "the second accept", "Eve work Guide")

	board, err := l.svc.Board(ctx, l.docs.ID)
	if err != nil || board.Slots != (Slots{Limit: 1, Used: 1, Applies: true}) {
		t.Fatalf("Docs's slots = %+v, %v; want 1 of 1 in use", board.Slots, err)
	}
	if r := l.request(t, l.docs.ID, guide); r.Reason != (Reason{Code: ReasonInSlot, Text: "In progress (Eve)."}) || r.Intake != IntakeAccepted {
		t.Fatalf("Guide = %+v; want it in progress", r)
	}
	want := Reason{Code: ReasonOwnGoalFirst, Text: `Accepted, waiting for a slot: Docs's own goal comes first; 1 of 1 request slots in use by "Guide".`}
	if r := l.request(t, l.docs.ID, faq); r.Reason != want || r.Position != 1 || r.Total != 1 {
		t.Fatalf("FAQ = %+v; want it waiting first for a slot", r)
	}
	l.work(t, guide, l.eve)
	l.wantTurns(t, "Guide is done", "Dee work FAQ")
	if r := l.itemNow(t, guide.ID); r.SlotAt != 0 {
		t.Fatalf("Guide after it is done = %+v; want its slot free", r)
	}
	if r := l.request(t, l.docs.ID, guide); r.Reason.Code != ReasonDone || r.Reason.Text != "Done." {
		t.Fatalf("Guide = %+v; want it done", r)
	}
}

func TestRequestsRunWithoutLimitWithoutAnActiveOwnGoal(t *testing.T) {
	l := newLaneFixture(t, false)
	ctx := context.Background()
	guide, faq := l.ask(t, l.docs, item("Guide", "", "")), l.ask(t, l.docs, item("FAQ", "", ""))
	l.accept(t, guide, l.eve, "")
	l.accept(t, faq, l.dee, "")
	finishQueued(t, l.svc)
	l.wantTurns(t, "accepts without a goal of Docs's own")
	for _, it := range []Item{guide, faq} {
		if got := l.itemNow(t, it.ID); got.SlotAt == 0 {
			t.Fatalf("%q = %+v; want a slot without a limit", it.Title, got)
		}
	}
	board, _ := l.svc.Board(ctx, l.docs.ID)
	if board.Slots != (Slots{Limit: 1, Used: 2, Applies: false}) {
		t.Fatalf("slots = %+v; want 2 in use, no limit", board.Slots)
	}

	// A goal of Docs's own that starts pre-empts nothing; a new request then waits.
	own, err := l.svc.StartGoal(ctx, l.docs.ID, "Write docs.")
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, l.svc)
	api := l.ask(t, l.docs, item("API", "", ""))
	l.accept(t, api, l.eve, "")
	finishQueued(t, l.svc)
	if got := l.itemNow(t, api.ID); got.SlotAt != 0 {
		t.Fatalf("API = %+v; want it waiting: 2 run, the limit is 1", got)
	}
	// A paused goal of Docs's own lifts the limit.
	if err := l.svc.StopTeam(ctx, l.docs.ID); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, api.ID); got.SlotAt == 0 {
		t.Fatalf("API with Docs's goal paused = %+v; want a slot", got)
	}
	if g := l.goalNow(t, own.ID); g.State != GoalPaused {
		t.Fatalf("Docs's goal = %+v; want it paused", g)
	}
}

func TestTriageNeedsNoSlot(t *testing.T) {
	l := newLaneFixture(t, true)
	guide := l.ask(t, l.docs, item("Guide", "", ""))
	finishQueued(t, l.svc)
	l.accept(t, guide, l.eve, "")
	finishQueued(t, l.svc)
	faq := l.ask(t, l.docs, item("FAQ", "", ""))
	l.wantTurns(t, "a request while every slot is in use", "Dee lead")
	want := Reason{Code: ReasonTriage, Text: "Waiting for Dee to accept, decline or defer it (position 1 of 1)."}
	if r := l.request(t, l.docs.ID, faq); r.Reason != want || r.Position != 1 || r.Total != 1 || r.Intake != IntakeRequested {
		t.Fatalf("FAQ = %+v; want it waiting for triage", r)
	}
}

func TestAcceptingOutOfOrderNeedsANoteAndGetsTheNextSlot(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	guide := l.ask(t, l.docs, item("Guide", "", ""))
	finishQueued(t, l.svc)
	l.accept(t, guide, l.eve, "")
	faq, api, hotfix := l.ask(t, l.docs, item("FAQ", "", "")), l.ask(t, l.docs, item("API", "", "")), l.ask(t, l.docs, item("Hotfix", "", ""))
	_, err := l.svc.AssignItem(ctx, l.docsLead, hotfix.ID, l.dee.ID, "", "")
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), `Earlier requests wait: "FAQ", "API". Accept them first, or give a note on why this one goes first.`) {
		t.Fatalf("accept out of order without a note: err = %v", err)
	}
	l.accept(t, faq, l.dee, "")
	l.accept(t, hotfix, l.dee, "Prod is down.")
	if got := l.itemNow(t, hotfix.ID); got.JumpedAt == 0 || got.IntakeNote != "Prod is down." {
		t.Fatalf("Hotfix = %+v; want it taken out of order with the note", got)
	}
	if text := lastEventText(t, l.svc, l.docs.ID); text != `Dee took "Hotfix" out of order for Dee: Prod is down.` {
		t.Fatalf("Docs channel event = %q", text)
	}
	jumped := Reason{Code: ReasonJumped, Text: "Taken out of order by Dee: Prod is down."}
	if r := l.asked(t, l.team.ID, hotfix); r.Reason != jumped || r.Position != 1 || r.Total != 2 {
		t.Fatalf("Hotfix for the asking team = %+v; want the jump, first in line", r)
	}
	if r := l.request(t, l.docs.ID, faq); r.Position != 2 || r.Reason.Code != ReasonOwnGoalFirst {
		t.Fatalf("FAQ = %+v; want it second behind the hotfix", r)
	}
	if r := l.request(t, l.docs.ID, api); r.Position != 1 || r.Total != 1 || r.Reason.Code != ReasonTriage {
		t.Fatalf("API = %+v; want it first in triage", r)
	}
	finishQueued(t, l.svc)
	l.work(t, guide, l.eve)
	l.wantTurns(t, "Guide is done", "Dee work Hotfix")
	if r := l.request(t, l.docs.ID, hotfix); r.Reason != jumped {
		t.Fatalf("Hotfix in its slot = %+v; want the jump shown", r)
	}

	// A note that skips nobody is kept, but takes nothing out of order.
	l2 := newLaneFixture(t, true)
	only := l2.ask(t, l2.docs, item("Only", "", ""))
	l2.accept(t, only, l2.eve, "Easy one.")
	if got := l2.itemNow(t, only.ID); got.JumpedAt != 0 || got.IntakeNote != "Easy one." {
		t.Fatalf("Only = %+v; want the note without a jump", got)
	}
}

func TestBehindReasonsShowTheOrderOfArrival(t *testing.T) {
	l := newLaneFixture(t, false)
	spec := l.create(t, l.lead, item("Spec", l.ana.ID, ""))
	a := l.ask(t, l.docs, item("A", "", "", spec.ID))
	b := l.ask(t, l.docs, item("B", "", "", spec.ID))
	l.accept(t, a, l.eve, "")
	l.accept(t, b, l.dee, "")
	ra, rb := l.request(t, l.docs.ID, a), l.request(t, l.docs.ID, b)
	if ra.Reason.Code != ReasonBehind || !strings.HasPrefix(ra.Reason.Text, "Accepted, position 1 of 2: arrived ") || strings.Contains(ra.Reason.Text, "after") {
		t.Fatalf("A = %+v; want it first by arrival", ra.Reason)
	}
	if rb.Reason.Code != ReasonBehind || !strings.HasSuffix(rb.Reason.Text, `, after "A".`) || rb.Position != 2 {
		t.Fatalf("B = %+v; want it after A", rb.Reason)
	}
	if !strings.Contains(ra.Reason.Text, time.UnixMilli(a.CreatedAt).UTC().Format("2006-01-02 15:04 UTC")) {
		t.Fatalf("A = %q; want its arrival time", ra.Reason.Text)
	}
}

func TestRequestSlotsAreTheUsersWithinTheMembers(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	guide, faq := l.ask(t, l.docs, item("Guide", "", "")), l.ask(t, l.docs, item("FAQ", "", ""))
	l.accept(t, guide, l.eve, "")
	l.accept(t, faq, l.dee, "")
	finishQueued(t, l.svc)
	for _, bad := range []int64{0, 3, -1} {
		_, err := l.svc.UpdateTeam(ctx, l.docs.ID, TeamPatch{RequestSlots: &bad})
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "Requests in progress at once must be from 1 to 2.") {
			t.Errorf("request slots %d: err = %v; want ErrInvalid", bad, err)
		}
	}
	two := int64(2)
	team, err := l.svc.UpdateTeam(ctx, l.docs.ID, TeamPatch{RequestSlots: &two})
	if err != nil || team.Team.RequestSlots != 2 {
		t.Fatalf("request slots 2 = %+v, %v", team.Team, err)
	}
	l.wantTurns(t, "a second slot", "Dee work FAQ")
	one := int64(1)
	if _, err := l.svc.UpdateTeam(ctx, l.docs.ID, TeamPatch{RequestSlots: &one}); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, faq.ID); got.SlotAt == 0 {
		t.Fatalf("FAQ after fewer slots = %+v; want it to keep running", got)
	}
	if _, err := l.svc.UpdateTeam(ctx, l.docs.ID, TeamPatch{RequestSlots: &two}); err != nil {
		t.Fatal(err)
	}
	room, err := l.svc.SetRoomMembers(ctx, l.docs.ID, []string{l.dee.ID})
	if err != nil || room.Team.RequestSlots != 1 {
		t.Fatalf("team after a member left = %+v, %v; want 1 slot", room.Team, err)
	}
	row, err := l.svc.q.GetTeam(ctx, l.docs.ID)
	if err != nil || row.RequestSlots != 1 {
		t.Fatalf("stored request slots = %d, %v; want 1", row.RequestSlots, err)
	}
}

func TestADeclinedRequestBlocksItsDependentsAndWakesTheAskingLead(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	guide := l.ask(t, l.docs, item("Guide", "", ""))
	publish := l.create(t, l.lead, item("Publish", l.ana.ID, "", guide.ID))
	own := l.create(t, l.lead, item("Own", "", ""))
	after := l.create(t, l.lead, item("After own", l.cy.ID, "", own.ID))
	finishQueued(t, l.svc)
	if _, err := l.svc.DropItem(ctx, l.docsLead, guide.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("decline without a note: err = %v; want ErrInvalid", err)
	}
	if _, err := l.svc.DropItem(ctx, l.docsLead, guide.ID, "Out of scope."); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, guide.ID); got.Intake != IntakeDeclined || got.State != ItemDropped || got.IntakeNote != "Out of scope." {
		t.Fatalf("Guide = %+v; want it declined", got)
	}
	if got := l.itemNow(t, publish.ID); got.State != ItemBlocked || got.Note != `Team Docs declined "Guide": Out of scope.` {
		t.Fatalf("Publish = %+v; want it blocked with the decline", got)
	}
	l.wantTurns(t, "the decline", "Bo lead")
	turns, _ := l.svc.QueuedTurns(ctx, 10)
	if !strings.Contains(turns[0].Prompt, `Team Docs declined "Guide": Out of scope. Re-plan`) || turns[0].GoalID != l.goal.ID {
		t.Fatalf("lead turn = %+v; want the decline, paid by Ship's goal", turns[0])
	}
	if text := lastEventText(t, l.svc, l.team.ID); text != `"Publish" is blocked: Team Docs declined "Guide": Out of scope.` {
		t.Fatalf("Ship channel event = %q", text)
	}
	if text := lastEventText(t, l.svc, l.docs.ID); text != `Dee declined "Guide": Out of scope.` {
		t.Fatalf("Docs channel event = %q", text)
	}
	if r := l.asked(t, l.team.ID, guide); r.Reason != (Reason{Code: ReasonDeclined, Text: "Declined: Out of scope."}) {
		t.Fatalf("Guide for the asking team = %+v", r.Reason)
	}
	// A team's own dropped item still lets the items after it start, with no note.
	finishQueued(t, l.svc)
	if _, err := l.svc.DropItem(ctx, l.lead, own.ID, ""); err != nil {
		t.Fatal(err)
	}
	l.wantTurns(t, "an own drop", "Cy work After own")
	if got := l.itemNow(t, after.ID); got.State != ItemTodo {
		t.Fatalf("After own = %+v; want it ready, not blocked", got)
	}
}

func TestADeferredRequestComesBackWhenItsRevisitIsDue(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	guide, faq, api := l.ask(t, l.docs, item("Guide", "", "")), l.ask(t, l.docs, item("FAQ", "", "")), l.ask(t, l.docs, item("API", "", ""))
	publish := l.create(t, l.lead, item("Publish", l.ana.ID, "", faq.ID))
	l.accept(t, guide, l.eve, "")
	finishQueued(t, l.svc)
	frozen := l.svc.now()
	l.svc.now = func() time.Time { return frozen }
	for name, c := range map[string]struct {
		note, revisit string
		until         int64
	}{
		"without a note":   {"", RevisitManual, 0},
		"a bad revisit":    {"Later.", "someday", 0},
		"a time gone by":   {"Later.", RevisitDate, frozen.UnixMilli() - 1},
		"a time right now": {"Later.", RevisitDate, frozen.UnixMilli()},
	} {
		if _, err := l.svc.DeferItem(ctx, l.docsLead, faq.ID, c.note, c.revisit, c.until); !errors.Is(err, ErrInvalid) {
			t.Errorf("defer %s: err = %v; want ErrInvalid", name, err)
		}
	}
	if _, err := l.svc.DeferItem(ctx, l.as(l.ana, RoleLead), faq.ID, "Later.", RevisitManual, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the asking lead defers: err = %v; want ErrInvalid", err)
	}
	if _, err := l.svc.DeferItem(ctx, l.docsLead, faq.ID, "After our release.", RevisitGoalEnd, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := l.svc.DeferItem(ctx, l.docsLead, guide.ID, "Not now.", RevisitManual, 0); err != nil {
		t.Fatal(err)
	}
	soon := l.svc.nowMs() + 60_000
	if _, err := l.svc.DeferItem(ctx, l.docsLead, api.ID, "Tomorrow.", RevisitDate, soon); err != nil {
		t.Fatal(err)
	}
	if _, err := l.svc.DeferItem(ctx, l.docsLead, api.ID, "Again.", RevisitManual, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("defer a deferred request: err = %v; want ErrInvalid", err)
	}
	if got := l.itemNow(t, guide.ID); got.Intake != IntakeDeferred || got.State != ItemTodo || got.OwnerID != l.eve.ID || got.SlotAt != 0 {
		t.Fatalf("Guide = %+v; want it deferred, its owner kept and its slot free", got)
	}
	if got := l.itemNow(t, publish.ID); got.State != ItemTodo {
		t.Fatalf("Publish = %+v; want it still waiting", got)
	}
	l.wantTurns(t, "the deferrals", "Bo lead")
	turns, _ := l.svc.QueuedTurns(ctx, 10)
	if !strings.Contains(turns[0].Prompt, `Team Docs deferred "FAQ": After our release. Re-plan`) {
		t.Fatalf("lead turn = %q; want the first deferral", turns[0].Prompt)
	}
	if text := lastEventText(t, l.svc, l.docs.ID); text != `Dee deferred "API" until `+clock(soon)+`: Tomorrow.` {
		t.Fatalf("Docs channel event = %q", text)
	}
	if _, err := l.svc.AssignItem(ctx, l.docsLead, guide.ID, l.eve.ID, "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("assign a deferred request: err = %v; want ErrInvalid", err)
	}
	for _, c := range []struct {
		it   Item
		want string
	}{
		{faq, "Deferred until team Docs's current goal ends: After our release."},
		{guide, "Deferred until someone brings it back: Not now."},
		{api, "Deferred until " + clock(soon) + ": Tomorrow."},
	} {
		if r := l.asked(t, l.team.ID, c.it); r.Reason != (Reason{Code: ReasonDeferred, Text: c.want}) {
			t.Errorf("%q for the asking team = %+v; want %q", c.it.Title, r.Reason, c.want)
		}
	}
	finishQueued(t, l.svc)

	// Manual: the user brings it back, and the serving lead triages it.
	if _, err := l.svc.UndeferItem(ctx, l.as(l.bo, RoleLead), guide.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the asking lead brings it back: err = %v; want ErrInvalid", err)
	}
	if _, err := l.svc.UndeferItem(ctx, Actor{Role: RoleUser}, guide.ID); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, guide.ID); got.Intake != IntakeRequested || got.OwnerID != "" || got.IntakeNote != "" {
		t.Fatalf("Guide back = %+v; want it waiting for triage", got)
	}
	l.wantTurns(t, "the user brought Guide back", "Dee lead")
	if text := lastEventText(t, l.svc, l.docs.ID); text != `The user brought "Guide" back from deferral.` {
		t.Fatalf("Docs channel event = %q", text)
	}
	if _, err := l.svc.UndeferItem(ctx, l.docsLead, guide.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bring back a request that is not deferred: err = %v; want ErrInvalid", err)
	}
	if r := l.request(t, l.docs.ID, guide); r.Position != 1 || r.Total != 1 {
		t.Fatalf("Guide = %+v; want it first in triage, at its arrival place", r)
	}
	finishQueued(t, l.svc)

	// Date: the first settle after the time.
	live := func(string, string) bool { return true }
	if err := l.svc.SettleItems(ctx, live); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, api.ID); got.Intake != IntakeDeferred {
		t.Fatalf("API before its time = %+v; want it deferred", got)
	}
	l.svc.now = func() time.Time { return time.UnixMilli(soon) }
	if err := l.svc.SettleItems(ctx, live); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, api.ID); got.Intake != IntakeRequested {
		t.Fatalf("API at its time = %+v; want it back", got)
	}
	if text := lastEventText(t, l.svc, l.docs.ID); text != `"API" is back from deferral: its time came.` {
		t.Fatalf("Docs channel event = %q", text)
	}

	// Goal end: when Docs's own goal ends.
	if err := l.svc.EndGoal(ctx, l.docsGoal.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, faq.ID); got.Intake != IntakeRequested {
		t.Fatalf("FAQ after Docs's goal ended = %+v; want it back", got)
	}
	if text := lastEventText(t, l.svc, l.team.ID); text != `Team Docs: "FAQ" is back from deferral: the team's goal ended.` {
		t.Fatalf("Ship channel event = %q", text)
	}
	if _, err := l.svc.UndeferItem(ctx, l.docsLead, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bring back a missing item: err = %v; want ErrNotFound", err)
	}
	if _, err := l.svc.UndeferItem(ctx, l.docsLead, publish.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bring back an own item: err = %v; want ErrInvalid", err)
	}
}

func TestAcceptingTheOwnGoalBringsBackItsDeferrals(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	faq := l.ask(t, l.docs, item("FAQ", "", ""))
	if _, err := l.svc.DeferItem(ctx, l.docsLead, faq.ID, "After our release.", RevisitGoalEnd, 0); err != nil {
		t.Fatal(err)
	}
	own := Actor{BotID: l.dee.ID, TeamID: l.docs.ID, Role: RoleLead}
	if _, err := l.svc.CreateItem(ctx, own, item("Write", l.eve.ID, "")); err != nil {
		t.Fatal(err)
	}
	write, _ := goalItems(ctx, l.svc.q, l.docsGoal.ID)
	l.work(t, write[0], l.eve)
	if _, err := l.svc.DeliverGoal(ctx, own, "Written."); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, faq.ID); got.Intake != IntakeDeferred {
		t.Fatalf("FAQ after the delivery = %+v; want it deferred still", got)
	}
	if _, err := l.svc.AcceptGoal(ctx, l.docsGoal.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, faq.ID); got.Intake != IntakeRequested {
		t.Fatalf("FAQ after the accept = %+v; want it back", got)
	}
}

func TestEndingTheAskingGoalWithdrawsItsRequests(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	dev, cal := l.otherTeam(t, "Dev", "Cal", "Dan")
	other := newGoalOf(t, l.svc, "Ops", "Olga", "Oz", "Run it.")
	guide := l.ask(t, l.docs, item("Guide", "", ""))
	l.accept(t, guide, l.eve, "")
	waiting, err := l.svc.RequestWork(ctx, other.lead, l.docs.ID, waitedOn(t, l.svc, other.lead, item("Runbook", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	l.items[waiting.ID] = waiting.Title
	l.accept(t, waiting, l.dee, "")
	build, err := l.svc.RequestWork(ctx, l.docsLead, dev.ID, NewItem{Title: "Build", Brief: "Build it.", Acceptance: "Built.", ForItemID: guide.ID})
	if err != nil {
		t.Fatal(err)
	}
	l.items[build.ID] = build.Title
	if build.ForItemID != guide.ID || build.Intake != IntakeRequested {
		t.Fatalf("chained request = %+v; want it to name the request it serves", build)
	}
	finishQueued(t, l.svc)
	if got := l.itemNow(t, waiting.ID); got.SlotAt != 0 {
		t.Fatalf("Runbook = %+v; want it waiting behind Guide", got)
	}
	if err := l.svc.EndGoal(ctx, l.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, guide.ID); got.Intake != IntakeWithdrawn || got.State != ItemDropped || got.IntakeNote != "Withdrawn: team Ship ended its goal." {
		t.Errorf("Guide = %+v; want it withdrawn", got)
	}
	if got := l.itemNow(t, build.ID); got.Intake != IntakeWithdrawn || got.State != ItemDropped {
		t.Errorf("Build = %+v; want the chained request withdrawn too", got)
	}
	if text := lastEventText(t, l.svc, dev.ID); text != `Team Docs withdrew "Build": "Guide", which it served, was withdrawn.` {
		t.Fatalf("Dev channel event = %q", text)
	}
	if !hasEventText(t, l.svc, l.docs.ID, `Team Ship ended its goal; "Guide" is withdrawn.`) {
		t.Fatalf("Docs channel has no note of the withdrawal")
	}
	if got := l.itemNow(t, waiting.ID); got.SlotAt == 0 {
		t.Fatalf("Runbook = %+v; want the freed slot", got)
	}
	l.wantTurns(t, "the end", "Dee work Runbook")
	board, err := l.svc.Board(ctx, l.docs.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range board.Requests {
		if r.ID == guide.ID {
			found = r.Reason == (Reason{Code: ReasonWithdrawn, Text: "Withdrawn: team Ship ended its goal."})
		}
	}
	if !found {
		t.Fatalf("Docs's requests = %+v; want the withdrawn Guide among the closed ones", board.Requests)
	}
	later := l.svc.now().Add(23 * time.Hour)
	l.svc.now = func() time.Time { return later }
	if r := l.request(t, l.docs.ID, guide); r.Intake != IntakeWithdrawn {
		t.Fatalf("Guide after 23 hours = %+v; want it still among the closed requests", r)
	}
	l.svc.now = func() time.Time { return later.Add(2 * time.Hour) }
	if board, _ := l.svc.Board(ctx, l.docs.ID); slices.ContainsFunc(board.Requests, func(r Request) bool { return r.ID == guide.ID }) {
		t.Fatalf("Docs's requests after a day = %+v; want the withdrawn Guide gone", board.Requests)
	}
	l.svc.now = time.Now
	if _, err := l.svc.AssignItem(ctx, Actor{BotID: cal.ID, TeamID: dev.ID, Role: RoleLead}, build.ID, cal.ID, "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accept a request of an ended goal: err = %v; want ErrInvalid", err)
	}
}

// hasEventText reports whether a channel has a board event with text.
func hasEventText(t *testing.T, svc *Service, id, text string) bool {
	t.Helper()
	entries, err := svc.Entries(context.Background(), id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		var body struct {
			Text string `json:"text"`
		}
		if e.Kind == KindEvent && json.Unmarshal(e.Body, &body) == nil && body.Text == text {
			return true
		}
	}
	return false
}

// goalOwner is a team with an active goal and the actor of its lead.
type goalOwner struct {
	team Bot
	lead Actor
	goal Goal
}

func newGoalOf(t *testing.T, svc *Service, name, leadName, memberName, text string) goalOwner {
	t.Helper()
	ctx := context.Background()
	lead, err := svc.CreateBot(ctx, leadName, "")
	if err != nil {
		t.Fatal(err)
	}
	member, err := svc.CreateBot(ctx, memberName, "")
	if err != nil {
		t.Fatal(err)
	}
	team, err := svc.CreateTeam(ctx, name, "", []string{lead.ID, member.ID}, TeamSettings{LeadID: lead.ID})
	if err != nil {
		t.Fatal(err)
	}
	g, err := svc.StartGoal(ctx, team.ID, text)
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, svc)
	return goalOwner{team: team, lead: Actor{BotID: lead.ID, TeamID: team.ID, Role: RoleLead}, goal: g}
}

func TestTheAskingLeadWithdrawsARequestAndItsChain(t *testing.T) {
	l := newLaneFixture(t, false)
	ctx := context.Background()
	dev, _ := l.otherTeam(t, "Dev", "Cal", "Dan")
	guide, faq := l.ask(t, l.docs, item("Guide", "", "")), l.ask(t, l.docs, item("FAQ", "", ""))
	l.accept(t, guide, l.eve, "")
	build, err := l.svc.RequestWork(ctx, l.docsLead, dev.ID, NewItem{Title: "Build", Brief: "Build it.", Acceptance: "Built.", ForItemID: guide.ID})
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, l.svc)
	for name, a := range map[string]Actor{
		"the serving lead":    l.docsLead,
		"a member of Ship":    l.as(l.ana, ""),
		"Ship's lead in Docs": {BotID: l.bo.ID, TeamID: l.docs.ID, Role: RoleLead},
	} {
		if _, err := l.svc.WithdrawItem(ctx, a, guide.ID); !errors.Is(err, ErrInvalid) {
			t.Errorf("withdraw by %s: err = %v; want ErrInvalid", name, err)
		}
	}
	got, err := l.svc.WithdrawItem(ctx, l.lead, guide.ID)
	if err != nil || got.Intake != IntakeWithdrawn || got.IntakeNote != "Withdrawn by team Ship." {
		t.Fatalf("withdraw = %+v, %v", got, err)
	}
	if b := l.itemNow(t, build.ID); b.Intake != IntakeWithdrawn || b.IntakeNote != `Withdrawn: "Guide", which it served, was withdrawn.` {
		t.Fatalf("Build = %+v; want it withdrawn with Guide", b)
	}
	if text := lastEventText(t, l.svc, l.team.ID); text != `Bo withdrew "Guide" from team Docs.` {
		t.Fatalf("Ship channel event = %q", text)
	}
	if text := lastEventText(t, l.svc, l.docs.ID); text != `Team Ship withdrew "Guide".` {
		t.Fatalf("Docs channel event = %q", text)
	}
	if text := lastEventText(t, l.svc, dev.ID); text != `Team Docs withdrew "Build": "Guide", which it served, was withdrawn.` {
		t.Fatalf("Dev channel event = %q", text)
	}
	l.wantTurns(t, "the withdrawal")
	if _, err := l.svc.WithdrawItem(ctx, l.lead, guide.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("withdraw twice: err = %v; want ErrInvalid", err)
	}
	// The lead of a chained request's asking team withdraws it.
	api := l.ask(t, l.docs, item("API", "", ""))
	chained, err := l.svc.RequestWork(ctx, l.docsLead, dev.ID, NewItem{Title: "SDK", Brief: "Do it.", Acceptance: "Done.", ForItemID: api.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.svc.WithdrawItem(ctx, l.lead, chained.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the goal's lead withdraws a request Docs made: err = %v; want ErrInvalid", err)
	}
	if _, err := l.svc.WithdrawItem(ctx, l.docsLead, chained.ID); err != nil {
		t.Fatalf("Docs's lead withdraws its chained request: %v", err)
	}
	if got, err := l.svc.WithdrawItem(ctx, Actor{Role: RoleUser}, faq.ID); err != nil || got.IntakeNote != "Withdrawn by the user." {
		t.Fatalf("the user withdraws = %+v, %v", got, err)
	}
	if text := lastEventText(t, l.svc, l.docs.ID); text != `The user withdrew "FAQ".` {
		t.Fatalf("Docs channel event = %q", text)
	}
	own := l.create(t, l.lead, item("Own", "", ""))
	if _, err := l.svc.WithdrawItem(ctx, l.lead, own.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("withdraw an own item: err = %v; want ErrInvalid", err)
	}
	if _, err := l.svc.WithdrawItem(ctx, l.lead, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdraw a missing item: err = %v; want ErrNotFound", err)
	}
}

func TestWaitingAndDeferredRequestsBillNothing(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	guide, faq, api := l.ask(t, l.docs, item("Guide", "", "")), l.ask(t, l.docs, item("FAQ", "", "")), l.ask(t, l.docs, item("API", "", ""))
	runQueued(t, l.svc) // Dee's triage: one turn
	l.accept(t, guide, l.eve, "")
	l.accept(t, faq, l.dee, "")
	if _, err := l.svc.DeferItem(ctx, l.docsLead, api.ID, "Later.", RevisitManual, 0); err != nil {
		t.Fatal(err)
	}
	runQueued(t, l.svc) // Eve's work on Guide and Bo's wake on the deferral
	used := l.goalNow(t, l.goal.ID).Used
	if used != 3 {
		t.Fatalf("used = %d; want 3: the triage, Guide's work and the asking lead's wake", used)
	}
	for range 3 {
		if err := l.svc.SettleItems(ctx, func(string, string) bool { return false }); err != nil {
			t.Fatal(err)
		}
		runQueued(t, l.svc)
	}
	for _, it := range []Item{faq, api} {
		if got := l.itemNow(t, it.ID); got.State == ItemBlocked {
			t.Fatalf("%q = %+v; want it not blocked while it waits", it.Title, got)
		}
	}
	if got := l.goalNow(t, l.goal.ID).Used; got != used+1 {
		t.Fatalf("used after the settles = %d; want %d: only Guide, whose owner stopped, wakes a lead", got, used+1)
	}
}

func TestPausingTheAskingGoalFreesItsSlots(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	other := newGoalOf(t, l.svc, "Ops", "Olga", "Oz", "Run it.")
	guide := l.ask(t, l.docs, item("Guide", "", ""))
	runbook, err := l.svc.RequestWork(ctx, other.lead, l.docs.ID, waitedOn(t, l.svc, other.lead, item("Runbook", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	l.items[runbook.ID] = runbook.Title
	faq := l.ask(t, l.docs, item("FAQ", "", ""))
	l.accept(t, guide, l.eve, "")
	l.accept(t, runbook, l.dee, "")
	l.accept(t, faq, l.eve, "")
	finishQueued(t, l.svc)
	if err := l.svc.StopTeam(ctx, l.team.ID); err != nil {
		t.Fatal(err)
	}
	if got := l.itemNow(t, guide.ID); got.SlotAt != 0 {
		t.Fatalf("Guide with Ship's goal paused = %+v; want its slot free", got)
	}
	if got := l.itemNow(t, runbook.ID); got.SlotAt == 0 {
		t.Fatalf("Runbook = %+v; want the freed slot", got)
	}
	if r := l.request(t, l.docs.ID, faq); r.Reason != (Reason{Code: ReasonAskingPaused, Text: "On hold: the asking goal is paused at its budget."}) || r.Position != 2 {
		t.Fatalf("FAQ = %+v; want it on hold behind Guide", r)
	}
	l.work(t, runbook, l.dee)
	if _, err := l.svc.ExtendGoal(ctx, l.goal.ID, 0); err != nil {
		t.Fatal(err)
	}
	if g, f := l.itemNow(t, guide.ID), l.itemNow(t, faq.ID); g.SlotAt == 0 || f.SlotAt != 0 {
		t.Fatalf("after the resume Guide = %+v, FAQ = %+v; want Guide, first to arrive, in the slot", g, f)
	}
}

func TestTheRequestersSeeWhatTheirRequestsWaitOn(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	dev, _ := l.otherTeam(t, "Dev", "Cal", "Dan")
	api := l.ask(t, dev, item("API", "", ""))
	guide := l.ask(t, l.docs, item("Guide", "", "", api.ID))
	if r := l.asked(t, l.team.ID, guide); r.Reason.WaitsOn != "… waiting on team Dev." || r.Intake != IntakeRequested {
		t.Fatalf("Guide = %+v; want it waiting on Dev", r)
	}
	if r := l.asked(t, l.team.ID, api); r.Reason.WaitsOn != "" || r.Reason.Code != ReasonTriage {
		t.Fatalf("API = %+v; want it waiting for Dev's triage only", r)
	}
	// Docs asks Ship back: the two teams wait on each other.
	back, err := l.svc.RequestWork(ctx, l.docsLead, l.team.ID, waitedOn(t, l.svc, l.docsLead, item("Review", "", "")))
	if err != nil {
		t.Fatal(err)
	}
	r := l.request(t, l.docs.ID, guide)
	if r.Reason.WaitsOn != `… waiting on team Dev. Team Ship is also waiting on your request "Review".` {
		t.Fatalf("Guide on Docs's board = %+v", r.Reason)
	}
	if a := l.asked(t, l.docs.ID, back); a.TeamID != l.team.ID || a.Reason.Code != ReasonTriage {
		t.Fatalf("Docs's ask = %+v; want its request to Ship", a)
	}
	board, err := l.svc.Board(ctx, l.docs.ID)
	if err != nil || board.Teams[l.team.ID] != "Ship" {
		t.Fatalf("Docs's board teams = %+v, %v", board.Teams, err)
	}
	// A request Docs chained for Ship's request shows among Docs's asks.
	sdk, err := l.svc.RequestWork(ctx, l.docsLead, dev.ID, NewItem{Title: "SDK", Brief: "Do it.", Acceptance: "Done.", ForItemID: guide.ID})
	if err != nil {
		t.Fatal(err)
	}
	if a := l.asked(t, l.docs.ID, sdk); a.FromTeamID != l.team.ID || a.Reason.Code != ReasonTriage {
		t.Fatalf("Docs's chained ask = %+v", a)
	}
}

func TestAGoalPausedAtItsBudgetFreesTheSlotsOfEveryServingTeam(t *testing.T) {
	l := newLaneFixture(t, true)
	ctx := context.Background()
	ops := newGoalOf(t, l.svc, "Ops", "Olga", "Oz", "Run it.")
	guide := l.ask(t, l.docs, item("Guide", "", ""))
	runbook := l.ask(t, ops.team, item("Runbook", "", ""))
	l.accept(t, guide, l.eve, "")
	if _, err := l.svc.AssignItem(ctx, ops.lead, runbook.ID, ops.lead.BotID, "", ""); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, l.svc)
	if g, r := l.itemNow(t, guide.ID), l.itemNow(t, runbook.ID); g.SlotAt == 0 || r.SlotAt == 0 {
		t.Fatalf("Guide = %+v, Runbook = %+v; want both in a slot", g, r)
	}
	spent := l.goalNow(t, l.goal.ID)
	spent.Used = spent.Budget
	if err := l.svc.pauseAtBudget(ctx, spent); err != nil {
		t.Fatal(err)
	}
	if g := l.goalNow(t, l.goal.ID); g.State != GoalPaused {
		t.Fatalf("goal = %+v; want it paused at its budget", g)
	}
	if g, r := l.itemNow(t, guide.ID), l.itemNow(t, runbook.ID); g.SlotAt != 0 || r.SlotAt != 0 {
		t.Fatalf("after the pause Guide = %+v, Runbook = %+v; want both slots free", g, r)
	}
	if _, err := l.svc.ExtendGoal(ctx, l.goal.ID, 5); err != nil {
		t.Fatal(err)
	}
	if g, r := l.itemNow(t, guide.ID), l.itemNow(t, runbook.ID); g.SlotAt == 0 || r.SlotAt == 0 {
		t.Fatalf("after the resume Guide = %+v, Runbook = %+v; want both in a slot again", g, r)
	}
}
