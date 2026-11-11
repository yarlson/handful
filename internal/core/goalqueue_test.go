package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
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
	if err != nil || board.Goal.ID != f.goal.ID || len(board.Plan) != 2 || board.Plan[0].ID != next.ID || board.Plan[1].ID != byLead.ID {
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
		for i, g := range board.Plan {
			if g.QueuePos != int64(i+1) {
				t.Fatalf("queue = %+v; want positions 1 to n", board.Plan)
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
