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

// wakes lists every queued turn of a lead as "who @team decides|plans <task>",
// or "who @team" for a lead turn of no task.
func (h *handoffFixture) wakes(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	turns, err := h.svc.QueuedTurns(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, tr := range turns {
		if tr.BoardRole != RoleLead {
			continue
		}
		line := fmt.Sprintf("%s @%s", h.names[tr.BotID], h.teamNames[tr.RoomID])
		if tr.GoalID != "" {
			g := h.goalNow(t, tr.GoalID)
			verb := "plans"
			if tr.Decision {
				verb = "decides"
			}
			line += " " + verb + " " + g.Text
		}
		out = append(out, line)
	}
	return out
}

func (h *handoffFixture) wantWakes(t *testing.T, step string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := h.wakes(t); !slices.Equal(got, want) {
		t.Fatalf("after %s, lead turns = %q; want %q", step, got, want)
	}
}

// wantPrompt checks that the newest queued turn's prompt starts with want.
func (h *handoffFixture) wantPrompt(t *testing.T, want string) {
	t.Helper()
	turns, err := h.svc.QueuedTurns(context.Background(), 100)
	if err != nil || len(turns) == 0 {
		t.Fatalf("queued turns = %v, %v", turns, err)
	}
	if got := turns[len(turns)-1].Prompt; !strings.HasPrefix(got, want) {
		t.Fatalf("prompt = %q; want it to start with %q", got, want)
	}
}

// events lists a task's events as "kind by <actor>: <text>".
func (h *handoffFixture) events(t *testing.T, goalID string) []string {
	t.Helper()
	events, err := h.svc.TaskEvents(context.Background(), goalID)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range events {
		out = append(out, fmt.Sprintf("%s by %s: %s", e.Kind, e.ActorName, e.Text))
	}
	return out
}

func TestALeadSendsATaskWithOrWithoutATaskOfItsOwn(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	finishQueued(t, h.svc)
	// Ship works on a task; Docs has none. Both send.
	withTask, err := h.svc.SendTask(ctx, f.lead, h.docs.ID, NewTask{Text: "Write the guide."})
	if err != nil || withTask.State != GoalIncoming || withTask.FromGoalID != f.goal.ID {
		t.Fatalf("send while working = %+v, %v; want it in Docs's inbox, from Ship's task", withTask, err)
	}
	none, err := h.svc.SendTask(ctx, h.docsLead, h.dev.ID, NewTask{Text: "Host the docs."})
	if err != nil || none.State != GoalIncoming || none.FromGoalID != "" {
		t.Fatalf("send with no task = %+v, %v; want it in Dev's inbox", none, err)
	}
	h.wantWakes(t, "the sends", "Dee @Docs decides Write the guide.", "Cal @Dev decides Host the docs.")
	ship, err := h.svc.Board(ctx, f.team.ID)
	if err != nil || len(ship.Sent) != 1 || ship.Sent[0].ID != withTask.ID || ship.Teams[h.docs.ID] != "Docs" {
		t.Fatalf("Ship's sent tasks = %+v, %v; want the guide", ship.Sent, err)
	}
	docs, _ := h.svc.Board(ctx, h.docs.ID)
	if len(docs.Inbox) != 1 || docs.Inbox[0].ID != withTask.ID || len(docs.Sent) != 1 || docs.Sent[0].ID != none.ID {
		t.Fatalf("Docs's board = inbox %+v, sent %+v; want the guide in, the docs host out", docs.Inbox, docs.Sent)
	}
	if got := h.events(t, withTask.ID); !slices.Equal(got, []string{"sent by Bo: Write the guide."}) {
		t.Fatalf("events = %q", got)
	}
}

func TestTheUsersTaskGoesThroughTheInboxUnlessStartedNow(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	finishQueued(t, h.svc)
	g, err := h.svc.UserTask(ctx, f.team.ID, NewTask{Text: "Ship XML export.", Brief: "Like CSV."}, false)
	if err != nil || g.State != GoalIncoming || g.Origin != OriginUser || g.Brief != "Like CSV." {
		t.Fatalf("user task = %+v, %v; want it in the inbox", g, err)
	}
	h.wantWakes(t, "the user's task", "Bo @Ship decides Ship XML export.")
	h.wantPrompt(t, "The user sent your team a task: \"Ship XML export.\". Take it into your plan")
	if text := lastEventText(t, h.svc, f.team.ID); text != "The user sent a task: Ship XML export." {
		t.Fatalf("Ship channel = %q", text)
	}
	finishQueued(t, h.svc)
	// Start now, while another task is Now: first in the plan, and the lead is told.
	other, _ := h.svc.StartGoal(ctx, f.team.ID, "Ship JSON export.")
	top, err := h.svc.UserTask(ctx, f.team.ID, NewTask{Text: "Fix the crash."}, true)
	if err != nil || top.State != GoalQueued || top.QueuePos != 1 || h.goalNow(t, other.ID).QueuePos != 2 {
		t.Fatalf("start now while busy = %+v, %v; want it first in the plan", top, err)
	}
	h.wantWakes(t, "a task started now", "Bo @Ship decides Fix the crash.")
	h.wantPrompt(t, "The user put a task at the top of your plan: \"Fix the crash.\".")
	if text := lastEventText(t, h.svc, f.team.ID); text != "New task, at the top of the plan: Fix the crash." {
		t.Fatalf("Ship channel = %q", text)
	}
	// Start now on a free team: it starts, and its lead plans it.
	finishQueued(t, h.svc)
	now, err := h.svc.UserTask(ctx, h.docs.ID, NewTask{Text: "Write the FAQ."}, true)
	if err != nil || now.State != GoalActive || now.Budget != DefaultGoalBudget {
		t.Fatalf("start now on a free team = %+v, %v; want it active", now, err)
	}
	h.wantWakes(t, "a task started at once", "Dee @Docs plans Write the FAQ.")
	h.wantPrompt(t, "The user started a new task: plan it.")
	// A full plan takes no task started now; one place left still does.
	for i := range MaxQueuedGoals - 1 {
		if _, err := h.svc.StartGoal(ctx, h.docs.ID, fmt.Sprintf("Planned %d.", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.UserTask(ctx, h.docs.ID, NewTask{Text: "The 10th."}, true); err != nil {
		t.Fatalf("start now into the last place: %v", err)
	}
	if _, err := h.svc.UserTask(ctx, h.docs.ID, NewTask{Text: "An 11th."}, true); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "10 tasks planned") {
		t.Fatalf("start now into a full plan: err = %v; want ErrInvalid", err)
	}
	for name, c := range map[string]struct {
		team string
		task NewTask
	}{
		"no text":           {f.team.ID, NewTask{Text: " "}},
		"a step to wait on": {f.team.ID, NewTask{Text: "x", WaitsOn: "item"}},
		"a missing team":    {"missing", NewTask{Text: "x"}},
	} {
		if _, err := h.svc.UserTask(ctx, c.team, c.task, false); err == nil {
			t.Errorf("user task with %s: want an error", name)
		}
	}
}

func TestOnlyTheLeadDecidesAndTheUserTakesOnlyWhatTheLeadsCouldNotSettle(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	member := Actor{BotID: h.eve.ID, TeamID: h.docs.ID, Role: RoleWork}
	user := Actor{Role: RoleUser}
	if _, err := h.svc.TakeTask(ctx, member, g.ID, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a member takes a task: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.TakeTask(ctx, user, g.ID, 0); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "Docs's lead decides on") {
		t.Fatalf("the user takes a task its lead decides on: err = %v; want ErrInvalid", err)
	}
	// Nothing else takes it: settling the board leaves it in the inbox.
	if err := h.svc.SettleItems(ctx, func(string, string) bool { return false }, LeadState{}); err != nil {
		t.Fatal(err)
	}
	if got := h.goalNow(t, g.ID); got.State != GoalIncoming {
		t.Fatalf("task after a settle = %s; want it in the inbox", got.State)
	}
	// After three questions, only the user settles it.
	for i := range MaxAskRounds {
		if _, err := h.svc.AskTask(ctx, h.docsLead, g.ID, fmt.Sprintf("Question %d?", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.AskTask(ctx, h.docsLead, g.ID, "Question 4?"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a 4th question: err = %v; want ErrInvalid", err)
	}
	taken, err := h.svc.TakeTask(ctx, user, g.ID, 0)
	if err != nil || taken.State != GoalActive {
		t.Fatalf("the user takes the unsettled task = %+v, %v; want it active", taken, err)
	}
}

func TestAnAskGoesToTheSenderAndTheAnswerBringsItBack(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	finishQueued(t, h.svc)
	if _, err := h.svc.AskTask(ctx, h.docsLead, g.ID, "Which format?"); err != nil {
		t.Fatal(err)
	}
	h.wantWakes(t, "the question", "Bo @Ship")
	finishQueued(t, h.svc)
	if _, err := h.svc.AnswerTask(ctx, f.lead, g.ID, "Markdown."); err != nil {
		t.Fatal(err)
	}
	h.wantWakes(t, "the answer", "Dee @Docs decides Guide.")
	if got := h.goalNow(t, g.ID); got.State != GoalIncoming || got.AskedAt != 0 || !strings.HasSuffix(got.Brief, "Answer from Bo: Markdown.") {
		t.Fatalf("task after the answer = %+v; want it back in the inbox with the answer", got)
	}
	if got := h.events(t, g.ID); !slices.Equal(got, []string{"sent by Bo: Guide.", "asked by Dee: Which format?", "answered by Bo: Markdown."}) {
		t.Fatalf("events = %q", got)
	}
	// A proposal is asked about in the team's channel.
	p, err := h.svc.ProposeTask(ctx, Actor{BotID: h.eve.ID, TeamID: h.docs.ID}, NewTask{Text: "Tidy the docs."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AskTask(ctx, h.docsLead, p.ID, "Why?"); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "ask Eve about the proposal") {
		t.Fatalf("ask about a proposal: err = %v; want ErrInvalid", err)
	}
}

func TestAPushBackBlocksTheSendersWaitingStepAndWakesItsLead(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	publish := f.create(t, f.lead, item("Publish", f.ana.ID, ""))
	finishQueued(t, h.svc)
	g, err := h.svc.SendTask(ctx, f.lead, h.docs.ID, NewTask{Text: "Guide.", WaitsOn: publish.ID})
	if err != nil || g.WaitsOnItemID != publish.ID {
		t.Fatalf("send with a waiting step = %+v, %v", g, err)
	}
	for name, id := range map[string]string{"a missing step": "missing", "another team's step": "x"} {
		if _, err := h.svc.SendTask(ctx, f.lead, h.dev.ID, NewTask{Text: "Other.", WaitsOn: id}); !errors.Is(err, ErrInvalid) {
			t.Errorf("send waiting on %s: err = %v; want ErrInvalid", name, err)
		}
	}
	finishQueued(t, h.svc)
	if _, err := h.svc.PushBack(ctx, h.docsLead, g.ID, " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("push back without a reason: err = %v; want ErrInvalid", err)
	}
	got, err := h.svc.PushBack(ctx, h.docsLead, g.ID, "Not ours.")
	if err != nil || got.State != GoalDeclined || got.IntakeNote != "Not ours." {
		t.Fatalf("push back = %+v, %v", got, err)
	}
	step := f.itemNow(t, publish.ID)
	want := `Team Docs pushed back your task "Guide.": Not ours.`
	if step.State != ItemBlocked || step.Note != want {
		t.Fatalf("the waiting step = %+v; want it blocked with the reason", step)
	}
	h.wantWakes(t, "the push back", "Bo @Ship")
	if text := lastEventText(t, h.svc, f.team.ID); text != want {
		t.Fatalf("Ship channel = %q", text)
	}
	if got := h.events(t, g.ID); got[len(got)-1] != "pushed_back by Dee: Not ours." {
		t.Fatalf("events = %q", got)
	}
}

func TestTheLeadOrdersThePlanAroundWhatTheUserPinned(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	one, _ := h.svc.StartGoal(ctx, f.team.ID, "One.")
	two, _ := h.svc.StartGoal(ctx, f.team.ID, "Two.")
	three, _ := h.svc.StartGoal(ctx, f.team.ID, "Three.")
	if err := h.svc.OrderPlan(ctx, f.lead, f.team.ID, []string{three.ID, one.ID, two.ID}); err != nil {
		t.Fatal(err)
	}
	if h.goalNow(t, three.ID).QueuePos != 1 || h.goalNow(t, one.ID).QueuePos != 2 {
		t.Fatalf("plan after the lead's order = %d, %d; want Three first", h.goalNow(t, three.ID).QueuePos, h.goalNow(t, one.ID).QueuePos)
	}
	if got := h.events(t, three.ID); !slices.Equal(got, []string{"ordered by Bo: #3 → #1"}) {
		t.Fatalf("events of Three = %q", got)
	}
	if got := h.events(t, two.ID); !slices.Equal(got, []string{"ordered by Bo: #2 → #3"}) {
		t.Fatalf("events of Two = %q; want each moved task's move", got)
	}
	// A task that keeps its place leaves no event.
	if err := h.svc.OrderPlan(ctx, f.lead, f.team.ID, []string{three.ID, two.ID, one.ID}); err != nil {
		t.Fatal(err)
	}
	if got := h.events(t, three.ID); len(got) != 1 {
		t.Fatalf("events of Three after an order that kept it first = %q; want no new one", got)
	}
	if err := h.svc.OrderPlan(ctx, f.lead, f.team.ID, []string{three.ID, one.ID, two.ID}); err != nil {
		t.Fatal(err)
	}
	if pinned, err := h.svc.PinTask(ctx, one.ID, true); err != nil || !pinned.Pinned || pinned.ID != one.ID {
		t.Fatalf("pin = %+v, %v; want it pinned", pinned, err)
	}
	// One is pinned at #2: the lead cannot move it, nor place a task above it.
	if err := h.svc.OrderPlan(ctx, f.lead, f.team.ID, []string{one.ID, three.ID, two.ID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("the lead moves a pinned task: err = %v; want ErrConflict", err)
	}
	if err := h.svc.OrderPlan(ctx, f.lead, f.team.ID, []string{three.ID, one.ID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("an order that misses a task: err = %v; want ErrConflict", err)
	}
	g := h.handOver(t, h.docsLead, h.docs, f.team, "Four.")
	if _, err := h.svc.TakeTask(ctx, f.lead, g.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("take above a pinned task: err = %v; want ErrConflict", err)
	}
	if taken, err := h.svc.TakeTask(ctx, f.lead, g.ID, 3); err != nil || taken.QueuePos != 3 {
		t.Fatalf("take below the pinned task = %+v, %v; want it at #3", taken, err)
	}
	if err := h.svc.OrderPlan(ctx, f.lead, f.team.ID, []string{three.ID, one.ID, two.ID, g.ID}); err != nil {
		t.Fatalf("an order that keeps the pin: %v", err)
	}
	// The user may move it.
	if err := h.svc.OrderPlan(ctx, Actor{Role: RoleUser}, f.team.ID, []string{one.ID, three.ID, two.ID, g.ID}); err != nil {
		t.Fatalf("the user moves a pinned task: %v", err)
	}
	if unpinned, err := h.svc.PinTask(ctx, one.ID, false); err != nil || unpinned.Pinned {
		t.Fatalf("unpin = %+v, %v", unpinned, err)
	}
	if err := h.svc.OrderPlan(ctx, Actor{BotID: h.eve.ID, TeamID: f.team.ID}, f.team.ID, []string{one.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a non-lead orders: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.PinTask(ctx, f.goal.ID, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pin the Now task: err = %v; want ErrInvalid", err)
	}
	if got := h.events(t, one.ID); !slices.Contains(got, "pinned by the user: #2") || !slices.Contains(got, "unpinned by the user: #1") {
		t.Fatalf("events of One = %q", got)
	}
}

func TestLaterDefersATaskWithinAWeekAndItComesBackWhenDue(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	start := time.Now()
	now := start
	h.svc.now = func() time.Time { return now }
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	other := h.handOver(t, h.devLed, h.dev, h.docs, "FAQ.")
	finishQueued(t, h.svc)
	if _, err := h.svc.LaterTask(ctx, h.docsLead, other.ID, "Soon.", start.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	for name, until := range map[string]time.Time{"a past time": start.Add(-time.Minute), "now": start, "past a week": start.AddDate(0, 0, maxLaterDays).Add(time.Minute)} {
		if _, err := h.svc.LaterTask(ctx, h.docsLead, g.ID, "Busy.", until.UnixMilli()); !errors.Is(err, ErrInvalid) {
			t.Errorf("later to %s: err = %v; want ErrInvalid", name, err)
		}
	}
	if _, err := h.svc.LaterTask(ctx, h.docsLead, g.ID, " ", start.Add(time.Hour).UnixMilli()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("later without a note: err = %v; want ErrInvalid", err)
	}
	due := start.AddDate(0, 0, maxLaterDays)
	got, err := h.svc.LaterTask(ctx, h.docsLead, g.ID, "After the release.", due.UnixMilli())
	if err != nil || got.State != GoalLater || got.LaterUntil != due.UnixMilli() {
		t.Fatalf("later = %+v, %v", got, err)
	}
	index := h.handOver(t, h.devLed, h.dev, h.docs, "Index.")
	if _, err := h.svc.LaterTask(ctx, h.docsLead, index.ID, "With the guide.", due.Add(-time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	if board, _ := h.svc.Board(ctx, h.docs.ID); len(board.Inbox) != 3 || board.Inbox[0].State != GoalLater || len(board.Incoming) != 0 {
		t.Fatalf("Docs's inbox = %+v; want the deferred task", board.Inbox)
	}
	live := func(string, string) bool { return false }
	now = due.Add(-2 * time.Second)
	if err := h.svc.SettleItems(ctx, live, LeadState{}); err != nil {
		t.Fatal(err)
	}
	if h.goalNow(t, g.ID).State != GoalLater || h.goalNow(t, other.ID).State != GoalIncoming {
		t.Fatal("want only the task that was due back in the inbox")
	}
	h.wantWakes(t, "a settle before the guide is due", "Dee @Docs decides FAQ.")
	finishQueued(t, h.svc)
	now = due
	if err := h.svc.SettleItems(ctx, live, LeadState{}); err != nil {
		t.Fatal(err)
	}
	if back := h.goalNow(t, g.ID); back.State != GoalIncoming || back.LaterUntil != 0 {
		t.Fatalf("task when due = %+v; want it in the inbox", back)
	}
	h.wantWakes(t, "the guide and the index came due", "Dee @Docs decides Index.", "Dee @Docs decides Guide.")
	if text := lastEventText(t, h.svc, h.docs.ID); text != `The deferred task "Guide." is back in the inbox.` {
		t.Fatalf("Docs channel = %q", text)
	}
	if got := h.events(t, g.ID); got[len(got)-1] != "due by YarGPT: After the release." {
		t.Fatalf("events = %q", got)
	}
}

func TestPassOnMovesATaskToAnotherInboxAtMostThreeTimes(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	ops, olga := f.otherTeam(t, "Ops", "Olga", "Oz")
	h.teamNames[ops.ID] = "Ops"
	opsLead := Actor{BotID: olga.ID, TeamID: ops.ID, Role: RoleLead}
	g := h.handOver(t, f.lead, f.team, h.docs, "Host it.")
	finishQueued(t, h.svc)
	for name, to := range map[string]string{"its own team": h.docs.ID, "its sender": f.team.ID} {
		if _, err := h.svc.PassOn(ctx, h.docsLead, g.ID, to, "Not ours."); !errors.Is(err, ErrInvalid) {
			t.Errorf("pass on to %s: err = %v; want ErrInvalid", name, err)
		}
	}
	if _, err := h.svc.PassOn(ctx, h.docsLead, g.ID, h.dev.ID, " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("pass on without a note: err = %v; want ErrInvalid", err)
	}
	// Dev's inbox holds 3 from Ship already: the pass waits, as a send would.
	full := make([]Goal, DefaultInboxPerSender)
	for i := range full {
		full[i] = h.handOver(t, f.lead, f.team, h.dev, fmt.Sprintf("For Dev %d.", i))
	}
	if _, err := h.svc.PassOn(ctx, h.docsLead, g.ID, h.dev.ID, "Dev hosts things."); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "You have 3 tasks waiting in Dev's inbox.") {
		t.Fatalf("pass into a full share of the inbox: err = %v", err)
	}
	if _, err := h.svc.WithdrawTask(ctx, f.lead, full[0].ID); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	got, err := h.svc.PassOn(ctx, h.docsLead, g.ID, h.dev.ID, "Dev hosts things.")
	if err != nil || got.TeamID != h.dev.ID || got.State != GoalIncoming || got.Passes != 1 || got.PassedBy != "Passed to Dev by Dee" {
		t.Fatalf("pass on = %+v, %v", got, err)
	}
	h.wantWakes(t, "the pass", "Cal @Dev decides Host it.")
	if text := lastEventText(t, h.svc, f.team.ID); text != `Passed to Dev by Dee: your task "Host it.". Dev hosts things.` {
		t.Fatalf("Ship channel = %q", text)
	}
	if ship, _ := h.svc.Board(ctx, f.team.ID); len(ship.Sent) != 3 || ship.Sent[2].ID != g.ID || ship.Sent[2].TeamID != h.dev.ID || ship.Sent[2].PassedBy != "Passed to Dev by Dee" {
		t.Fatalf("Ship's sent = %+v; want it at Dev, passed by Dee", ship.Sent)
	}
	if _, err := h.svc.PassOn(ctx, h.devLed, g.ID, ops.ID, "Ops runs it."); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PassOn(ctx, opsLead, g.ID, h.docs.ID, "Docs, after all."); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	_, err = h.svc.PassOn(ctx, h.docsLead, g.ID, h.dev.ID, "Again.")
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), `"Host it." was passed on 3 times; take it, push it back, or ask the user.`) {
		t.Fatalf("a 4th pass: err = %v; want the pass limit", err)
	}
	stored := h.goalNow(t, g.ID)
	if stored.TeamID != h.docs.ID || !stored.NeedsUser {
		t.Fatalf("task after a 4th pass = %+v; want it at Docs, needing the user", stored)
	}
	if e := lastEntry(t, h.svc, h.docs.ID); e.Kind != KindBot || !strings.Contains(string(e.Body), "was passed on 3 times and still has no team") {
		t.Fatalf("Docs channel = %s; want a question to the user", e.Body)
	}
	h.wantWakes(t, "a 4th pass")
}

func TestAClosedTaskWaitsForWhoeverAskedWithoutBlockingThePlan(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	// Docs does a task of Ship's and has another planned.
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	if _, err := h.svc.TakeTask(ctx, h.docsLead, g.ID, 0); err != nil {
		t.Fatal(err)
	}
	next, _ := h.svc.StartGoal(ctx, h.docs.ID, "FAQ.")
	step, err := h.svc.CreateItem(ctx, h.docsLead, item("Write", h.eve.ID, ""))
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	if err := h.svc.StartItemWork(ctx, step.ID, h.eve.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.SubmitItem(ctx, Actor{BotID: h.eve.ID, TeamID: h.docs.ID, Role: RoleWork}, step.ID, "guide.md"); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	if _, err := h.svc.CloseTask(ctx, h.docsLead, next.ID, "Done."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("close a task that is not Now: err = %v; want ErrInvalid", err)
	}
	closed, err := h.svc.CloseTask(ctx, h.docsLead, g.ID, "The guide is in guide.md.")
	if err != nil || closed.State != GoalDelivered {
		t.Fatalf("close = %+v, %v", closed, err)
	}
	if got := h.goalNow(t, next.ID); got.State != GoalActive {
		t.Fatalf("the next task after the close = %s; want it started at once", got.State)
	}
	// Only whoever asked approves it: Ship's lead, or the user.
	for name, a := range map[string]Actor{"Docs's lead": h.docsLead, "Dev's lead": h.devLed} {
		if _, err := h.svc.ApproveTask(ctx, a, g.ID); !errors.Is(err, ErrInvalid) {
			t.Errorf("approve by %s: err = %v; want ErrInvalid", name, err)
		}
	}
	finishQueued(t, h.svc)
	approved, err := h.svc.ApproveTask(ctx, f.lead, g.ID)
	if err != nil || approved.State != GoalDone {
		t.Fatalf("approve by the sender = %+v, %v", approved, err)
	}
	if got := h.queuedTurns(t); !slices.Equal(got, []string{"Dee retro @Docs +goal"}) {
		t.Fatalf("queued after the approval = %q; want Docs's retro", got)
	}
	if got := h.events(t, g.ID); !slices.Contains(got, "closed by Dee: The guide is in guide.md.") || got[len(got)-1] != "approved by Bo: " {
		t.Fatalf("events = %q", got)
	}
	if _, err := h.svc.ApproveTask(ctx, f.lead, g.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("approve twice: err = %v; want ErrInvalid", err)
	}
}

func TestChangesToADoneTaskComeFirst(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	if _, err := h.svc.DeliverGoal(ctx, f.lead, "Parsed."); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ChangesTask(ctx, f.lead, f.goal.ID, "Also JSON."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the lead asks for changes to the user's task: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.ChangesTask(ctx, Actor{Role: RoleUser}, f.goal.ID, " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changes without a text: err = %v; want ErrInvalid", err)
	}
	finishQueued(t, h.svc)
	// No Now task: it is Now again, and the lead plans the changes.
	g, err := h.svc.ChangesTask(ctx, Actor{Role: RoleUser}, f.goal.ID, "Also JSON.")
	if err != nil || g.State != GoalActive {
		t.Fatalf("changes with no Now task = %+v, %v; want it active", g, err)
	}
	h.wantWakes(t, "the changes", "Bo @Ship plans Ship CSV export.")
	// With another task Now, it goes first in the plan.
	f.finish(t, f.create(t, f.lead, item("JSON", f.ana.ID, "")), f.ana)
	other, _ := h.svc.StartGoal(ctx, f.team.ID, "Other.")
	queued, _ := h.svc.StartGoal(ctx, f.team.ID, "Queued.")
	if _, err := h.svc.DeliverGoal(ctx, f.lead, "JSON too."); err != nil {
		t.Fatal(err)
	}
	if h.goalNow(t, other.ID).State != GoalActive {
		t.Fatal("the next task did not start at the close")
	}
	finishQueued(t, h.svc)
	g, err = h.svc.ChangesTask(ctx, Actor{Role: RoleUser}, f.goal.ID, "And XML.")
	if err != nil || g.State != GoalQueued || g.QueuePos != 1 || h.goalNow(t, queued.ID).QueuePos != 2 {
		t.Fatalf("changes while busy = %+v, %v; want it first in the plan", g, err)
	}
	h.wantWakes(t, "the changes while busy", "Bo @Ship decides Ship CSV export.")
}

func TestStopKeepsTheStepsAndReopenReturnsTheTaskToTheInbox(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	next, _ := h.svc.StartGoal(ctx, f.team.ID, "Next.")
	h.wantQueued(t, "the plan", "Ana work @Ship +goal")
	if _, err := h.svc.StopTask(ctx, f.as(f.ana, RoleWork), f.goal.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a member stops a task: err = %v; want ErrInvalid", err)
	}
	stopped, err := h.svc.StopTask(ctx, f.lead, f.goal.ID)
	if err != nil || stopped.State != GoalStopped {
		t.Fatalf("stop = %+v, %v", stopped, err)
	}
	if got := f.itemNow(t, parse.ID); got.State != ItemTodo || got.OwnerID != f.ana.ID {
		t.Fatalf("the step after the stop = %+v; want it kept", got)
	}
	h.wantQueued(t, "the stop", "Bo lead @Ship +goal")
	if got := h.goalNow(t, next.ID); got.State != GoalActive {
		t.Fatalf("the next task = %s; want it started", got.State)
	}
	finishQueued(t, h.svc)
	if _, err := h.svc.ReopenTask(ctx, f.lead, next.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reopen a task that is not stopped: err = %v; want ErrInvalid", err)
	}
	back, err := h.svc.ReopenTask(ctx, Actor{Role: RoleUser}, f.goal.ID)
	if err != nil || back.State != GoalIncoming {
		t.Fatalf("reopen = %+v, %v; want it in the inbox", back, err)
	}
	h.wantWakes(t, "the reopen", "Bo @Ship decides Ship CSV export.")
	if got := h.events(t, f.goal.ID); !slices.Contains(got, "stopped by Bo: ") || got[len(got)-1] != "reopened by the user: " {
		t.Fatalf("events = %q", got)
	}
	// A planned task stops too, and leaves the plan.
	later, _ := h.svc.StartGoal(ctx, f.team.ID, "Later.")
	if _, err := h.svc.StopTask(ctx, Actor{Role: RoleUser}, later.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.goalNow(t, later.ID); got.State != GoalStopped || got.QueuePos != 0 {
		t.Fatalf("stopped planned task = %+v", got)
	}
}

func TestTheSenderWithdrawsATaskThatHasNotStarted(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	if _, err := h.svc.LaterTask(ctx, h.docsLead, g.ID, "Busy.", h.svc.now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.WithdrawTask(ctx, h.docsLead, g.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the receiver withdraws: err = %v; want ErrInvalid", err)
	}
	got, err := h.svc.WithdrawTask(ctx, f.lead, g.ID)
	if err != nil || got.State != GoalEnded {
		t.Fatalf("withdraw a deferred task = %+v, %v", got, err)
	}
	if events := h.events(t, g.ID); events[len(events)-1] != "withdrawn by Bo: " {
		t.Fatalf("events = %q", events)
	}
}

func TestAMemberProposesATaskForItsOwnTeam(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	eve := Actor{BotID: h.eve.ID, TeamID: h.docs.ID, Role: RoleWork}
	g, err := h.svc.ProposeTask(ctx, eve, NewTask{Text: "Tidy the docs."})
	if err != nil || g.State != GoalIncoming || g.Origin != OriginProposal || g.FromBotID != h.eve.ID || g.TeamID != h.docs.ID {
		t.Fatalf("proposal = %+v, %v", g, err)
	}
	h.wantWakes(t, "the proposal", "Dee @Docs decides Tidy the docs.")
	if text := lastEventText(t, h.svc, h.docs.ID); text != "Eve proposed a task: Tidy the docs." {
		t.Fatalf("Docs channel = %q", text)
	}
	if _, err := h.svc.ProposeTask(ctx, Actor{BotID: h.eve.ID, TeamID: h.dev.ID}, NewTask{Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("propose in a team the bot is not in: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.ProposeTask(ctx, eve, NewTask{Text: "x", WaitsOn: "y"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a proposal that waits on a step: err = %v; want ErrInvalid", err)
	}
	// The lead's own proposal wakes no one: the lead is in its turn.
	finishQueued(t, h.svc)
	if _, err := h.svc.ProposeTask(ctx, h.docsLead, NewTask{Text: "Index."}); err != nil {
		t.Fatal(err)
	}
	h.wantWakes(t, "the lead's proposal")
	// The lead approves the team's own proposal.
	if _, err := h.svc.TakeTask(ctx, h.docsLead, g.ID, 0); err != nil {
		t.Fatal(err)
	}
	step, _ := h.svc.CreateItem(ctx, h.docsLead, item("Tidy", h.eve.ID, ""))
	finishQueued(t, h.svc)
	_ = h.svc.StartItemWork(ctx, step.ID, h.eve.ID)
	if _, err := h.svc.SubmitItem(ctx, eve, step.ID, "Tidied."); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.CloseTask(ctx, h.docsLead, g.ID, "Tidied."); err != nil {
		t.Fatal(err)
	}
	if got, err := h.svc.ApproveTask(ctx, h.docsLead, g.ID); err != nil || got.State != GoalDone {
		t.Fatalf("the lead approves a proposal = %+v, %v", got, err)
	}
}

func TestTheInboxLimitsHoldForSendersButNotForTheUser(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	// Three from Ship, then a fourth fails.
	for i := range DefaultInboxPerSender {
		h.handOver(t, f.lead, f.team, h.docs, fmt.Sprintf("From Ship %d.", i))
	}
	_, err := h.svc.SendTask(ctx, f.lead, h.docs.ID, NewTask{Text: "One more."})
	if !errors.Is(err, ErrInvalid) || err.Error() != "invalid input: You have 3 tasks waiting in Docs's inbox. Wait for decisions, or withdraw one." {
		t.Fatalf("a 4th from one team: err = %v", err)
	}
	// Other teams fill it to 10, then an 11th fails, from any team.
	limit := int64(10)
	perSender := int64(10)
	if _, err := h.svc.UpdateTeam(ctx, h.docs.ID, TeamPatch{InboxMax: &limit, InboxPerSender: &perSender}); err != nil {
		t.Fatal(err)
	}
	for i := range 7 {
		h.handOver(t, h.devLed, h.dev, h.docs, fmt.Sprintf("From Dev %d.", i))
	}
	_, err = h.svc.SendTask(ctx, h.devLed, h.docs.ID, NewTask{Text: "An 11th."})
	if !errors.Is(err, ErrInvalid) || err.Error() != "invalid input: Docs's inbox is full (10 waiting). Wait for its lead's decisions, or ask the user." {
		t.Fatalf("an 11th: err = %v", err)
	}
	// A failed send stores nothing and asks no one.
	board, _ := h.svc.Board(ctx, h.docs.ID)
	if len(board.Inbox) != 10 {
		t.Fatalf("inbox = %d tasks; want 10", len(board.Inbox))
	}
	if open, err := h.svc.AllOpenQuestions(ctx); err != nil || len(open) != 0 {
		t.Fatalf("questions after the failed sends = %+v, %v; want none", open, err)
	}
	// The user's tasks always land, and so do members' proposals elsewhere.
	if _, err := h.svc.UserTask(ctx, h.docs.ID, NewTask{Text: "The user's."}, false); err != nil {
		t.Fatalf("the user's task into a full inbox: %v", err)
	}
	if _, err := h.svc.HandOver(ctx, Actor{Role: RoleUser}, f.team.ID, h.docs.ID, NewTask{Text: "The user's, for Ship."}); err != nil {
		t.Fatalf("the user's task for Ship into a full inbox: %v", err)
	}
	if _, err := h.svc.ProposeTask(ctx, Actor{BotID: h.eve.ID, TeamID: h.docs.ID}, NewTask{Text: "Tidy."}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a proposal into a full inbox: err = %v; want ErrInvalid", err)
	}
	// No send to the team itself.
	_, err = h.svc.SendTask(ctx, h.docsLead, h.docs.ID, NewTask{Text: "Me."})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "propose it with update_board propose") {
		t.Fatalf("send to the own team: err = %v", err)
	}
}

func TestEveryInboxWakeIsAFreeDecisionTurnAndPlanningCountsAgainstTheTask(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	finishQueued(t, h.svc)
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	turns, err := h.svc.QueuedTurns(ctx, 10)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns = %+v, %v", turns, err)
	}
	decision := turns[0]
	if !decision.Decision || decision.GoalID != g.ID || decision.BoardRole != RoleLead || decision.RoomID != h.docs.ID {
		t.Fatalf("the arrival wake = %+v; want a lead decision turn on the task", decision)
	}
	// It may run although the task is no open goal, and it costs the task nothing.
	if ok, err := h.svc.GoalAllows(ctx, decision); err != nil || !ok {
		t.Fatalf("GoalAllows(decision) = %v, %v; want it to run", ok, err)
	}
	runQueued(t, h.svc)
	if used := h.goalNow(t, g.ID).Used; used != 0 {
		t.Fatalf("the task used %d turns after its decision turn; want 0", used)
	}
	// A second wake on the same task while one waits is the same turn.
	if _, err := h.svc.AskTask(ctx, h.docsLead, g.ID, "Format?"); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	if _, err := h.svc.AnswerTask(ctx, f.lead, g.ID, "Markdown."); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AnswerTask(ctx, Actor{Role: RoleUser}, g.ID, "Short."); err != nil {
		t.Fatal(err)
	}
	h.wantWakes(t, "two answers", "Dee @Docs decides Guide.")
	runQueued(t, h.svc)
	// Taking it starts it, and the planning turn counts against it.
	if _, err := h.svc.TakeTask(ctx, h.docsLead, g.ID, 0); err != nil {
		t.Fatal(err)
	}
	h.wantWakes(t, "the take", "Dee @Docs plans Guide.")
	runQueued(t, h.svc)
	if used := h.goalNow(t, g.ID).Used; used != 1 {
		t.Fatalf("the task used %d turns after its planning; want 1", used)
	}
}

func TestAPushBackBlocksOnlyAStepThatStillWaits(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	doing := f.create(t, f.lead, item("Doing", f.ana.ID, ""))
	review := f.create(t, f.lead, item("Review", f.cy.ID, f.ana.ID))
	done := f.create(t, f.lead, item("Done", f.ana.ID, ""))
	finishQueued(t, h.svc)
	f.start(t, doing, f.ana)
	tasks := map[string]Goal{}
	for _, it := range []Item{doing, review, done} {
		g, err := h.svc.SendTask(ctx, f.lead, h.docs.ID, NewTask{Text: "For " + it.Title + ".", WaitsOn: it.ID})
		if err != nil {
			t.Fatal(err)
		}
		tasks[it.ID] = g
	}
	f.start(t, review, f.cy)
	if _, err := h.svc.SubmitItem(ctx, f.as(f.cy, RoleWork), review.ID, "Reviewed?"); err != nil {
		t.Fatal(err)
	}
	f.finish(t, done, f.ana)
	for _, it := range []Item{doing, review, done} {
		if _, err := h.svc.PushBack(ctx, h.docsLead, tasks[it.ID].ID, "Not ours."); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		it   Item
		want string
	}{{doing, ItemBlocked}, {review, ItemBlocked}, {done, ItemDone}} {
		if got := f.itemNow(t, c.it.ID); got.State != c.want {
			t.Errorf("step %s after the push back = %s; want %s", c.it.Title, got.State, c.want)
		}
	}
}

func TestASendFromADecisionTurnComesFromNoTaskYet(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	finishQueued(t, h.svc)
	in := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	turns, err := h.svc.QueuedTurns(ctx, 10)
	if err != nil || len(turns) != 1 || !turns[0].Decision {
		t.Fatalf("turns = %+v, %v; want Docs's decision turn", turns, err)
	}
	decision := Actor{BotID: h.dee.ID, TeamID: h.docs.ID, Role: RoleLead, TurnID: turns[0].ID}
	g, err := h.svc.SendTask(ctx, decision, h.dev.ID, NewTask{Text: "Host the guide."})
	if err != nil || g.FromGoalID != "" || g.Depth != 1 {
		t.Fatalf("send from a decision turn on %q = %+v, %v; want it from no task of Docs's, depth 1", in.Text, g, err)
	}
}

func TestATaskWaitsOnlyOnAnOpenStep(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	blocked := f.create(t, f.lead, item("Blocked", f.ana.ID, ""))
	done := f.create(t, f.lead, item("Done", f.ana.ID, ""))
	f.start(t, blocked, f.ana)
	if _, err := h.svc.BlockItem(ctx, f.as(f.ana, RoleWork), blocked.ID, "Waiting on Docs."); err != nil {
		t.Fatal(err)
	}
	f.finish(t, done, f.ana)
	if _, err := h.svc.SendTask(ctx, f.lead, h.docs.ID, NewTask{Text: "Unblock it.", WaitsOn: blocked.ID}); err != nil {
		t.Fatalf("a task that a blocked step waits on: %v", err)
	}
	if _, err := h.svc.SendTask(ctx, f.lead, h.dev.ID, NewTask{Text: "Too late.", WaitsOn: done.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a task that a done step waits on: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.HandOver(ctx, Actor{Role: RoleUser}, f.team.ID, h.dev.ID, NewTask{Text: "The user's.", WaitsOn: blocked.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the user links a waiting step: err = %v; want ErrInvalid", err)
	}
}

func TestAProposalIsNotPassedOnButSentAsTheTeamsTask(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	p, err := h.svc.ProposeTask(ctx, Actor{BotID: h.eve.ID, TeamID: h.docs.ID}, NewTask{Text: "Tidy the docs."})
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	_, err = h.svc.PassOn(ctx, h.docsLead, p.ID, h.dev.ID, "Dev tidies.")
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "send it with send_task, then push the proposal back") {
		t.Fatalf("pass on a proposal: err = %v; want ErrInvalid that says to send it", err)
	}
	if got := h.goalNow(t, p.ID); got.TeamID != h.docs.ID || got.State != GoalIncoming || got.Passes != 0 {
		t.Fatalf("proposal after a refused pass = %+v; want it unchanged in Docs's inbox", got)
	}
	h.wantWakes(t, "a refused pass")
}

func TestATaskWithdrawnBeforeItStartedIsNotTheTeamsLatestGoal(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	user := Actor{Role: RoleUser}
	for name, send := range map[string]func() Goal{
		"the user's task": func() Goal {
			g, err := h.svc.UserTask(ctx, h.docs.ID, NewTask{Text: "Write the FAQ."}, false)
			if err != nil {
				t.Fatal(err)
			}
			return g
		},
		"a proposal": func() Goal {
			g, err := h.svc.ProposeTask(ctx, Actor{BotID: h.eve.ID, TeamID: h.docs.ID}, NewTask{Text: "Tidy the docs."})
			if err != nil {
				t.Fatal(err)
			}
			return g
		},
		"another team's task": func() Goal { return h.handOver(t, f.lead, f.team, h.docs, "Guide.") },
	} {
		g := send()
		withdrawer := user
		if g.Origin == OriginHandoff {
			withdrawer = f.lead
		}
		if _, err := h.svc.WithdrawTask(ctx, withdrawer, g.ID); err != nil {
			t.Fatalf("%s: withdraw: %v", name, err)
		}
		if board, err := h.svc.Board(ctx, h.docs.ID); err != nil || board.Goal != nil {
			t.Fatalf("%s: Docs's goal after a withdrawal = %+v, %v; want none", name, board.Goal, err)
		}
		finishQueued(t, h.svc)
	}
}
