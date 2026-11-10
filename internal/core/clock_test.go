package core

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// clockFixture is the handoff fixture on a clock that the test moves.
type clockFixture struct {
	*handoffFixture
	clock time.Time
}

func newClockFixture(t *testing.T) *clockFixture {
	t.Helper()
	c := &clockFixture{handoffFixture: newHandoffFixture(t), clock: time.UnixMilli(1_800_000_000_000)}
	c.svc.now = func() time.Time { return c.clock }
	return c
}

// tick moves the clock by d and runs one settle tick with state.
func (c *clockFixture) tick(t *testing.T, d time.Duration, state LeadState) {
	t.Helper()
	c.clock = c.clock.Add(d)
	if err := c.svc.SettleItems(context.Background(), func(string, string) bool { return true }, state); err != nil {
		t.Fatal(err)
	}
}

// ticks runs a tick every minute for d.
func (c *clockFixture) ticks(t *testing.T, d time.Duration, state LeadState) {
	t.Helper()
	for range int(d / time.Minute) {
		c.tick(t, time.Minute, state)
	}
}

// needs lists what needs the user as "kind text (lead)".
func (c *clockFixture) needs(t *testing.T) []string {
	t.Helper()
	got, err := c.svc.NeedsYou(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, n := range got {
		line := n.Kind + " " + n.Text
		if n.Lead != "" {
			line += " (" + n.Lead + ")"
		}
		if n.Reason != "" {
			line += " (" + n.Reason + ")"
		}
		out = append(out, line)
	}
	return out
}

func (c *clockFixture) wantNeeds(t *testing.T, step string, want ...string) {
	t.Helper()
	if got := c.needs(t); !slices.Equal(got, want) {
		t.Fatalf("needs you %s = %q; want %q", step, got, want)
	}
}

func TestATaskLeftUndecidedForTheDecisionTimeGoesToTheUser(t *testing.T) {
	c := newClockFixture(t)
	ctx := context.Background()
	idle := LeadState{}
	g := c.handOver(t, c.lead, c.team, c.docs, "Guide.")
	finishQueued(t, c.svc)

	c.tick(t, 0, idle) // the clock starts
	c.ticks(t, 29*time.Minute, idle)
	if got := c.goalNow(t, g.ID); got.NeedsUser || got.DecisionMs != (29*time.Minute).Milliseconds() {
		t.Fatalf("task after 29 min = %+v; want 29 min counted, still the lead's", got)
	}
	c.wantNeeds(t, "after 29 min")

	c.tick(t, time.Minute, idle)
	got := c.goalNow(t, g.ID)
	if !got.NeedsUser || got.MissedAt != c.clock.UnixMilli() {
		t.Fatalf("task after 30 min = %+v; want it left to the user now", got)
	}
	c.wantNeeds(t, "after 30 min", "missed Guide. (Dee)")
	e := lastEntry(t, c.svc, c.docs.ID)
	if e.Kind != KindBot || !strings.Contains(string(e.Body), `Dee didn't decide on \"Guide.\" within 30 min. Take it or push it back?`) || !strings.Contains(string(e.Body), `"needsReply":true`) {
		t.Fatalf("Docs channel = %s; want Dee's question to the user", e.Body)
	}
	if events := c.events(t, g.ID); events[len(events)-1] != `missed by YarGPT: Dee didn't decide on "Guide." within 30 min. Take it or push it back?` {
		t.Fatalf("events = %q; want the missed decision last", events)
	}
	c.wantWakes(t, "a missed decision")
	events := len(c.events(t, g.ID))
	c.tick(t, time.Hour, idle) // it is missed once
	if n := len(c.events(t, g.ID)); n != events || c.goalNow(t, g.ID).MissedAt != c.clock.Add(-time.Hour).UnixMilli() {
		t.Fatal("a later tick missed the task again")
	}

	// The user takes it, and its question closes.
	if _, err := c.svc.TakeTask(ctx, Actor{Role: RoleUser}, g.ID, 0); err != nil {
		t.Fatal(err)
	}
	c.wantNeeds(t, "after the user took it")
	if open, _ := c.svc.OpenQuestions(ctx, c.docs.ID); len(open) != 0 {
		t.Fatalf("open questions in Docs = %+v; want the missed one closed", open)
	}
}

func TestTheDecisionClockStandsWhileTheLeadCannotAct(t *testing.T) {
	c := newClockFixture(t)
	ctx := context.Background()
	idle := LeadState{}
	// Ship (Bo leads) works on its goal; a task waits in Ship's inbox.
	g := c.handOver(t, c.docsLead, c.docs, c.team, "Review it.")
	finishQueued(t, c.svc)
	c.tick(t, 0, idle)
	c.ticks(t, 10*time.Minute, idle)
	counted := func() int64 { return c.goalNow(t, g.ID).DecisionMs }
	if got := counted(); got != (10 * time.Minute).Milliseconds() {
		t.Fatalf("counted = %d; want 10 min", got)
	}
	for name, stand := range map[string]struct {
		state LeadState
		on    func()
		off   func()
	}{
		"a usage limit":  {state: LeadState{Paused: true}},
		"the lead's bot": {state: LeadState{Busy: func(id string) bool { return id == c.bo.ID }}},
		"a drain":        {on: func() { c.svc.SetDraining(true) }, off: func() { c.svc.SetDraining(false) }},
		"a stopped team": {
			on: func() {
				if err := c.svc.StopTeam(ctx, c.team.ID); err != nil {
					t.Fatal(err)
				}
			},
			off: func() {
				if _, err := c.svc.ExtendGoal(ctx, c.goal.ID, 0); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		before := counted()
		if stand.on != nil {
			stand.on()
		}
		c.ticks(t, 3*time.Hour, stand.state) // three hours of it
		if got := counted(); got != before {
			t.Fatalf("%s: counted %d, then %d; want the clock to stand", name, before, got)
		}
		if stand.off != nil {
			stand.off()
		}
		c.tick(t, time.Minute, idle) // the first tick after restarts the clock
		if got := counted(); got != before {
			t.Fatalf("%s: the first tick after counted %d; want none of the stop", name, got-before)
		}
		c.tick(t, time.Minute, idle)
		if got := counted(); got != before+time.Minute.Milliseconds() {
			t.Fatalf("%s: counted %d after a minute; want %d", name, got, before+time.Minute.Milliseconds())
		}
	}
	c.wantNeeds(t, "after the stops")

	// A question to the sender stops it too, and the answer goes on from there.
	if _, err := c.svc.AskTask(ctx, c.lead, g.ID, "Which parts?"); err != nil {
		t.Fatal(err)
	}
	before := counted()
	c.ticks(t, 3*time.Hour, idle)
	if got := counted(); got != before {
		t.Fatalf("while an ask waits: counted %d, then %d; want the clock to stand", before, got)
	}
	if _, err := c.svc.AnswerTask(ctx, c.docsLead, g.ID, "The parser."); err != nil {
		t.Fatal(err)
	}
	c.tick(t, time.Minute, idle)
	c.tick(t, time.Minute, idle)
	if got := counted(); got != before+time.Minute.Milliseconds() {
		t.Fatalf("after the answer: counted %d; want %d, the clock going on", got, before+time.Minute.Milliseconds())
	}
	c.wantNeeds(t, "after the ask")
}

func TestEveryInboxCountsItsOwnDecisionTime(t *testing.T) {
	c := newClockFixture(t)
	first := c.handOver(t, c.lead, c.team, c.docs, "Guide.")
	second := c.handOver(t, c.lead, c.team, c.dev, "Host it.")
	finishQueued(t, c.svc)
	c.tick(t, 0, LeadState{})
	c.ticks(t, 30*time.Minute, LeadState{})
	if !c.goalNow(t, first.ID).NeedsUser || !c.goalNow(t, second.ID).NeedsUser {
		t.Fatalf("Docs's task = %+v, Dev's = %+v; want both missed", c.goalNow(t, first.ID), c.goalNow(t, second.ID))
	}
	c.wantNeeds(t, "", "missed Guide. (Dee)", "missed Host it. (Cal)")
}

func TestTheDecisionTimeIsFiveMinutesToADay(t *testing.T) {
	c := newClockFixture(t)
	ctx := context.Background()
	for minutes, ok := range map[int64]bool{5: true, 1440: true, 45: true, 4: false, 1441: false, 0: false} {
		team, err := c.svc.UpdateTeam(ctx, c.docs.ID, TeamPatch{DecisionMinutes: &minutes})
		if ok && (err != nil || team.Team.DecisionMinutes != minutes) {
			t.Errorf("decision time %d = %+v, %v; want it set", minutes, team.Team, err)
		}
		if !ok && !errors.Is(err, ErrInvalid) {
			t.Errorf("decision time %d: err = %v; want ErrInvalid", minutes, err)
		}
	}
	for d, want := range map[time.Duration]string{5 * time.Minute: "5 min", 2 * time.Hour: "2 h", 135 * time.Minute: "2 h 15 min"} {
		if got := minutesText(d); got != want {
			t.Errorf("minutesText(%v) = %q; want %q", d, got, want)
		}
	}
}

func TestABudgetPauseDoesNotStopTheDecisionClock(t *testing.T) {
	c := newClockFixture(t)
	g := c.handOver(t, c.docsLead, c.docs, c.team, "Review it.")
	finishQueued(t, c.svc)
	if _, err := c.svc.sqlDB.ExecContext(context.Background(), `UPDATE goals SET state = 'paused', budget = 0 WHERE id = ?`, c.goal.ID); err != nil {
		t.Fatal(err)
	}
	c.tick(t, 0, LeadState{})
	c.ticks(t, 30*time.Minute, LeadState{})
	if got := c.goalNow(t, g.ID); !got.NeedsUser {
		t.Fatalf("task after 30 min with Ship paused at its budget = %+v; want it left to the user", got)
	}
}

func TestATaskBackInTheInboxStartsAFreshClock(t *testing.T) {
	c := newClockFixture(t)
	ctx := context.Background()
	idle := LeadState{}
	g := c.handOver(t, c.lead, c.team, c.docs, "Guide.")
	finishQueued(t, c.svc)
	c.tick(t, 0, idle)
	c.ticks(t, 20*time.Minute, idle)
	if _, err := c.svc.PassOn(ctx, c.docsLead, g.ID, c.dev.ID, "Dev hosts it."); err != nil {
		t.Fatal(err)
	}
	if got := c.goalNow(t, g.ID); got.DecisionMs != 0 {
		t.Fatalf("a passed task = %+v; want a fresh clock at Dev", got)
	}
	c.tick(t, 0, idle)
	c.ticks(t, 20*time.Minute, idle)
	if _, err := c.svc.LaterTask(ctx, c.devLed, g.ID, "After the release.", c.clock.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	c.tick(t, time.Hour, idle) // it comes due
	if got := c.goalNow(t, g.ID); got.State != GoalIncoming || got.DecisionMs != 0 {
		t.Fatalf("a task that came due = %+v; want it in the inbox with a fresh clock", got)
	}
}

func TestNeedsYouListsOnlyWhatTheUserDecides(t *testing.T) {
	c := newClockFixture(t)
	ctx := context.Background()
	user := Actor{Role: RoleUser}
	// The user's own task, done: the user approves it.
	if _, err := c.svc.CreateItem(ctx, c.lead, item("Parse", c.ana.ID, "")); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, c.svc)
	if _, err := c.svc.sqlDB.ExecContext(context.Background(), `UPDATE items SET state = 'done'`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.DeliverGoal(ctx, c.lead, "CSV export works."); err != nil {
		t.Fatal(err)
	}
	// Another team's task, done: its lead approves it, not the user.
	other := c.handOver(t, c.lead, c.team, c.docs, "Guide.")
	if _, err := c.svc.TakeTask(ctx, c.docsLead, other.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.CreateItem(ctx, c.docsLead, item("Write", c.eve.ID, "")); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, c.svc)
	if _, err := c.svc.sqlDB.ExecContext(context.Background(), `UPDATE items SET state = 'done'`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.DeliverGoal(ctx, c.docsLead, "Guide written."); err != nil {
		t.Fatal(err)
	}
	if e := lastEntry(t, c.svc, c.docs.ID); strings.Contains(string(e.Body), `"needsReply":true`) {
		t.Fatalf("Docs's delivery = %s; want no question to the user", e.Body)
	}
	// A task asked about three times waits for the user.
	stuck := c.handOver(t, c.lead, c.team, c.dev, "Host it.")
	for range MaxAskRounds {
		if _, err := c.svc.AskTask(ctx, c.devLed, stuck.ID, "Where?"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.svc.AnswerTask(ctx, c.lead, stuck.ID, "Anywhere."); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.svc.AskTask(ctx, c.devLed, stuck.ID, "Where?"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a 4th ask: err = %v; want ErrInvalid", err)
	}
	// A bot's question to the user.
	if _, err := c.svc.AppendBotMessage(ctx, c.ana.ID, "", "Which format?", nil, false, true); err != nil {
		t.Fatal(err)
	}
	c.wantNeeds(t, "", "approve Ship CSV export.", "stuck Host it. (asked 3 times)", "question Which format?")
	got, err := c.svc.NeedsYou(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].GoalID != c.goal.ID || got[0].EntryID == "" || got[1].EntryID == "" || got[2].ChatID != c.ana.ID {
		t.Fatalf("needs you = %+v; want the tasks with their questions, and the question's chat", got)
	}
	// Approving the task closes its question; deciding on the stuck one too.
	if _, err := c.svc.ApproveTask(ctx, user, c.goal.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.PushBack(ctx, user, stuck.ID, "Not now."); err != nil {
		t.Fatal(err)
	}
	c.wantNeeds(t, "after the decisions", "question Which format?")
}
