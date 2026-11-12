package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// budgetFixture is the handoff fixture on a clock the test moves, in EET,
// with Ship's budget watch begun ten days before.
type budgetFixture struct {
	*handoffFixture
	clock time.Time
}

func newBudgetFixture(t *testing.T) *budgetFixture {
	t.Helper()
	f := &budgetFixture{handoffFixture: newHandoffFixture(t)}
	f.clock = time.Date(2027, 1, 15, 15, 0, 0, 0, time.FixedZone("EET", 2*60*60))
	f.svc.now = func() time.Time { return f.clock }
	f.watchSince(t, f.team.ID, f.clock.AddDate(0, 0, -10))
	return f
}

func (f *budgetFixture) watchSince(t *testing.T, teamID string, since time.Time) {
	t.Helper()
	if _, err := f.svc.sqlDB.ExecContext(context.Background(), `UPDATE teams SET budget_since = ? WHERE team_id = ?`, since.UnixMilli(), teamID); err != nil {
		t.Fatal(err)
	}
}

func (f *budgetFixture) setBudget(t *testing.T, teamID string, turns int64) {
	t.Helper()
	if _, err := f.svc.UpdateTeam(context.Background(), teamID, TeamPatch{DailyBudget: &turns}); err != nil {
		t.Fatal(err)
	}
}

// turns stores n chat turns in Ship's room that started at the given time.
func (f *budgetFixture) turns(t *testing.T, prefix string, n int, at time.Time) {
	t.Helper()
	for i := range n {
		storeTurn(t, f.svc, turnSpec{id: fmt.Sprintf("%s%d", prefix, i), bot: f.ana.ID, room: f.team.ID, hops: 1, started: at})
	}
}

func (f *budgetFixture) budget(t *testing.T, teamID string) BudgetView {
	t.Helper()
	b, err := f.svc.TeamBudget(context.Background(), teamID)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (f *budgetFixture) allows(t *testing.T, turn Turn) bool {
	t.Helper()
	ok, err := f.svc.TeamAllows(context.Background(), turn)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func (f *budgetFixture) needs(t *testing.T) []string {
	t.Helper()
	got, err := f.svc.NeedsYou(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, n := range got {
		if strings.HasPrefix(n.Kind, "budget") {
			out = append(out, n.Kind+" "+n.Text)
		}
	}
	return out
}

func TestEveryTurnOfATeamCountsOnItsDayButItsLeadsDecisions(t *testing.T) {
	f := newBudgetFixture(t)
	now, team := f.clock, f.team.ID
	other, err := f.svc.CreateRoom(context.Background(), "Chat", "", []string{f.ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []turnSpec{
		{id: "chat", bot: f.ana.ID, room: team, hops: 1, started: now},
		{id: "inherited", bot: f.cy.ID, room: team, hops: 1, goal: f.goal.ID, started: now},
		{id: "work", bot: f.ana.ID, room: team, goal: f.goal.ID, role: RoleWork, started: now},
		{id: "review", bot: f.cy.ID, room: team, goal: f.goal.ID, role: RoleReview, started: now},
		{id: "planning", bot: f.bo.ID, room: team, source: SourceSystem, goal: f.goal.ID, role: RoleLead, started: now},
		{id: "retro", bot: f.bo.ID, room: team, source: SourceSystem, goal: f.goal.ID, role: RoleRetro, started: now},
		{id: "report", bot: f.cy.ID, room: team, source: "task", started: now},
		// Free or not Ship's:
		{id: "decision", bot: f.bo.ID, room: team, source: SourceSystem, goal: f.goal.ID, role: RoleLead, decision: 1, started: now},
		{id: "lead-wake", bot: f.bo.ID, room: team, source: SourceSystem, role: RoleLead, started: now},
		{id: "direct", bot: f.ana.ID, source: SourceUser, started: now},
		{id: "plain-room", bot: f.ana.ID, room: other.ID, hops: 1, started: now},
		{id: "docs", bot: f.dee.ID, room: f.docs.ID, hops: 1, started: now},
		// Not today in EET: 23:59 yesterday and 00:00 tomorrow.
		{id: "yesterday", bot: f.ana.ID, room: team, hops: 1, started: time.Date(2027, 1, 14, 23, 59, 0, 0, f.clock.Location())},
		{id: "tomorrow", bot: f.ana.ID, room: team, hops: 1, started: time.Date(2027, 1, 16, 0, 0, 0, 0, f.clock.Location())},
		// Midnight in EET is today.
		{id: "midnight", bot: f.ana.ID, room: team, hops: 1, started: time.Date(2027, 1, 15, 0, 0, 0, 0, f.clock.Location())},
	} {
		storeTurn(t, f.svc, s)
	}
	if got := f.budget(t, team).Today; got != 8 {
		t.Fatalf("Ship's turns today = %d; want 8: chat, inherited, work, review, planning, retro, report and midnight", got)
	}
	if got := f.budget(t, f.docs.ID).Today; got != 1 {
		t.Fatalf("Docs's turns today = %d; want its own one", got)
	}
}

func TestATeamAtItsBudgetStartsOnlyItsLeadsDecisions(t *testing.T) {
	f := newBudgetFixture(t)
	f.setBudget(t, f.team.ID, 10)
	f.turns(t, "a", 7, f.clock)
	chat := Turn{BotID: f.ana.ID, RoomID: f.team.ID}
	step := Turn{BotID: f.ana.ID, RoomID: f.team.ID, GoalID: f.goal.ID, ItemID: "i", BoardRole: RoleWork}
	planning := Turn{BotID: f.bo.ID, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead}
	decision := Turn{BotID: f.bo.ID, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead, Decision: true}
	if b := f.budget(t, f.team.ID); b.Warn || b.Paused || !f.allows(t, chat) {
		t.Fatalf("Ship at 7 of 10 = %+v; want neither a warning nor a pause", b)
	}
	if got := f.needs(t); len(got) != 0 {
		t.Fatalf("needs you at 70%% = %q; want nothing", got)
	}
	f.turns(t, "b", 1, f.clock)
	if b := f.budget(t, f.team.ID); !b.Warn || b.Paused {
		t.Fatalf("Ship at 8 of 10 = %+v; want a warning", b)
	}
	if got := f.needs(t); !slices.Equal(got, []string{"budget_warning Ship used 80% of today's turns (8 of 10)"}) {
		t.Fatalf("needs you at 80%% = %q", got)
	}
	f.turns(t, "c", 2, f.clock)
	if b := f.budget(t, f.team.ID); b.Warn || !b.Paused || b.Today != 10 {
		t.Fatalf("Ship at 10 of 10 = %+v; want it paused", b)
	}
	for name, turn := range map[string]Turn{"chat": chat, "a step": step, "planning": planning} {
		if f.allows(t, turn) {
			t.Errorf("%s at the budget: allowed; want it held", name)
		}
	}
	if !f.allows(t, decision) {
		t.Fatal("a decision at the budget: held; want it to start")
	}
	if !f.allows(t, Turn{BotID: f.dee.ID, RoomID: f.docs.ID}) || !f.allows(t, Turn{BotID: f.ana.ID}) {
		t.Fatal("another team's or a direct chat's turn was held by Ship's budget")
	}
	// A turn of Ship's task outside any room counts on Ship too.
	if f.allows(t, Turn{BotID: f.ana.ID, GoalID: f.goal.ID}) {
		t.Fatal("a turn of Ship's task with no room was allowed at Ship's budget")
	}
	if got := f.needs(t); !slices.Equal(got, []string{"budget_reached Budget: Ship used 10 of 10 turns today"}) {
		t.Fatalf("needs you at 100%% = %q", got)
	}
	// Raise: the pause ends.
	f.setBudget(t, f.team.ID, 20)
	if !f.allows(t, chat) {
		t.Fatal("chat under a raised budget: held")
	}
	if got := f.needs(t); len(got) != 0 {
		t.Fatalf("needs you after a raise = %q; want nothing", got)
	}
}

func TestWaitingUntilMidnightHidesTheBudgetPauseForTheDay(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	f.setBudget(t, f.team.ID, 2)
	f.turns(t, "a", 2, f.clock)
	if err := f.svc.WaitForBudget(ctx, f.team.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.needs(t); len(got) != 0 {
		t.Fatalf("needs you after Wait until midnight = %q; want nothing today", got)
	}
	if f.allows(t, Turn{BotID: f.ana.ID, RoomID: f.team.ID}) {
		t.Fatal("waiting lifted the pause; want only the item hidden")
	}
	// The next day the turns are new, and so is the pause.
	f.clock = f.clock.AddDate(0, 0, 1)
	f.turns(t, "b", 2, f.clock)
	if got := f.needs(t); !slices.Equal(got, []string{"budget_reached Budget: Ship used 2 of 2 turns today"}) {
		t.Fatalf("needs you the next day = %q; want the new pause", got)
	}
	if err := f.svc.WaitForBudget(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wait for a missing team: err = %v; want ErrNotFound", err)
	}
}

func TestTheSettleTickTellsTheChannelOfAPauseOnceADay(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	settle := func() {
		t.Helper()
		if err := f.svc.SettleItems(ctx, func(string, string) bool { return true }, LeadState{}); err != nil {
			t.Fatal(err)
		}
	}
	f.setBudget(t, f.team.ID, 2)
	f.turns(t, "a", 1, f.clock)
	settle()
	note := "The team used its 2 turns for today. New turns wait until midnight or a higher budget; the lead's decisions still run."
	if text := lastEventText(t, f.svc, f.team.ID); text == note {
		t.Fatal("a pause was posted below the budget")
	}
	f.turns(t, "b", 1, f.clock)
	// Dev, listed after Ship, is at its budget too; Docs has no limit.
	f.setBudget(t, f.dev.ID, 1)
	storeTurn(t, f.svc, turnSpec{id: "dev", bot: f.cal.ID, room: f.dev.ID, hops: 1, started: f.clock})
	events, stop := f.svc.Hub().Subscribe()
	defer stop()
	settle()
	if text := lastEventText(t, f.svc, f.team.ID); text != note {
		t.Fatalf("Ship channel = %q; want the pause", text)
	}
	if text := lastEventText(t, f.svc, f.dev.ID); !strings.HasPrefix(text, "The team used its 1 turns for today.") {
		t.Fatalf("Dev channel = %q; want its pause too", text)
	}
	if text := lastEventText(t, f.svc, f.docs.ID); strings.HasPrefix(text, "The team used") {
		t.Fatalf("Docs channel = %q; want no pause for a team with no limit", text)
	}
	if !gotEvent(events, "bots") {
		t.Fatal("no bots event for the pause")
	}
	entries := func() int {
		t.Helper()
		got, err := f.svc.Entries(ctx, f.team.ID, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return len(got)
	}
	before := entries()
	settle()
	if after := entries(); after != before {
		t.Fatalf("Ship entries %d → %d after a second tick; want the pause told once", before, after)
	}
	f.clock = f.clock.AddDate(0, 0, 1)
	f.turns(t, "c", 2, f.clock)
	settle()
	if after := entries(); after != before+1 {
		t.Fatalf("Ship entries the next day = %d; want one more pause note", after)
	}
}

// gotEvent reports whether events holds an event of kind, without waiting.
func gotEvent(events <-chan Event, kind string) bool {
	for {
		select {
		case e := <-events:
			if e.Type == kind {
				return true
			}
		default:
			return false
		}
	}
}

func TestTheTeamThatDoesASentTaskPaysForIt(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	g := f.handOver(t, f.lead, f.team, f.docs, "Guide.")
	if _, err := f.svc.TakeTask(ctx, f.docsLead, g.ID, 0); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	storeTurn(t, f.svc, turnSpec{id: "docs-work", bot: f.eve.ID, room: f.docs.ID, goal: g.ID, role: RoleWork, started: f.clock})
	if ship, docs := f.budget(t, f.team.ID).Today, f.budget(t, f.docs.ID).Today; ship != 0 || docs != 1 {
		t.Fatalf("turns today: Ship %d, Docs %d; want Docs to pay its work", ship, docs)
	}
	board, err := f.svc.Board(ctx, f.team.ID)
	if err != nil || len(board.Sent) != 1 || board.Sent[0].PaidBy != "Docs" {
		t.Fatalf("Ship's sent = %+v, %v; want it paid by Docs", board.Sent, err)
	}
}

func TestABudgetIsWatchedForAWeekThenSuggested(t *testing.T) {
	f := newBudgetFixture(t)
	day := func(back int) time.Time { return f.clock.AddDate(0, 0, -back) }
	f.watchSince(t, f.team.ID, f.clock.AddDate(0, 0, -6))
	f.turns(t, "busy", 80, day(2))
	f.turns(t, "today", 300, f.clock) // today doesn't count toward the suggestion
	b := f.budget(t, f.team.ID)
	if b.Suggested != 0 || b.WatchUntil != f.clock.AddDate(0, 0, 1).UnixMilli() || b.Limit != 0 || b.Paused || b.Warn {
		t.Fatalf("Ship in its 7-day watch = %+v; want no suggestion and no limit", b)
	}
	if !f.allows(t, Turn{BotID: f.ana.ID, RoomID: f.team.ID}) {
		t.Fatal("a team with no limit held a turn")
	}
	f.watchSince(t, f.team.ID, f.clock.AddDate(0, 0, -7))
	f.turns(t, "older", 300, day(8)) // more than 7 days back: not seen
	f.turns(t, "oldest-seen", 120, day(7))
	if b := f.budget(t, f.team.ID); b.Suggested != 200 || b.Limit != 0 || b.Paused {
		t.Fatalf("Ship after its watch = %+v; want 200 suggested (1.5 × 120, 7 days back) and still no limit", b)
	}
}

func TestTheSuggestedBudgetIsOneAndAHalfDaysRoundedUpToFifty(t *testing.T) {
	for busiest, want := range map[int64]int64{0: 100, 66: 100, 67: 150, 100: 150, 101: 200, 400: 600} {
		if got := suggestedBudget(busiest); got != want {
			t.Errorf("suggestedBudget(%d) = %d; want %d", busiest, got, want)
		}
	}
}

func TestATeamsDailyBudgetIsZeroToTenThousand(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	for _, turns := range []int64{-1, maxDailyBudget + 1} {
		if _, err := f.svc.UpdateTeam(ctx, f.team.ID, TeamPatch{DailyBudget: &turns}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "0 (no limit) to 10000") {
			t.Errorf("daily budget %d: err = %v; want ErrInvalid with the range", turns, err)
		}
	}
	for _, turns := range []int64{maxDailyBudget, 0} {
		team, err := f.svc.UpdateTeam(ctx, f.team.ID, TeamPatch{DailyBudget: &turns})
		if err != nil || team.Team.DailyBudget != turns {
			t.Fatalf("daily budget %d = %+v, %v", turns, team.Team, err)
		}
	}
	if b := f.budget(t, "missing"); b != (BudgetView{}) {
		t.Fatalf("budget of no team = %+v; want the zero view", b)
	}
}

func TestARaisedBudgetReachedTheSameDayPausesWithANoticeAgain(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	settle := func() {
		t.Helper()
		if err := f.svc.SettleItems(ctx, func(string, string) bool { return true }, LeadState{}); err != nil {
			t.Fatal(err)
		}
	}
	f.setBudget(t, f.team.ID, 3)
	f.turns(t, "a", 3, f.clock)
	settle()
	if err := f.svc.WaitForBudget(ctx, f.team.ID); err != nil {
		t.Fatal(err)
	}
	f.setBudget(t, f.team.ID, 5) // the user raises it later the same day
	f.turns(t, "b", 2, f.clock)
	if got := f.needs(t); !slices.Equal(got, []string{"budget_reached Budget: Ship used 5 of 5 turns today"}) {
		t.Fatalf("needs you at the raised limit = %q; want the pause again", got)
	}
	settle()
	if text := lastEventText(t, f.svc, f.team.ID); !strings.HasPrefix(text, "The team used its 5 turns for today.") {
		t.Fatalf("Ship channel = %q; want a note for the new limit", text)
	}
	// Saving other settings with the same limit keeps the wait.
	if err := f.svc.WaitForBudget(ctx, f.team.ID); err != nil {
		t.Fatal(err)
	}
	agreement := "Tests pass first."
	if _, err := f.svc.UpdateTeam(ctx, f.team.ID, TeamPatch{Agreement: &agreement}); err != nil {
		t.Fatal(err)
	}
	if got := f.needs(t); len(got) != 0 {
		t.Fatalf("needs you after another setting changed = %q; want the wait kept", got)
	}
}
