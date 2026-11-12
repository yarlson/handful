package core

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// handoffFixture is the board fixture with team Docs (Dee leads) and team Dev
// (Cal leads), which have no goal of their own.
type handoffFixture struct {
	*boardFixture
	docs, dev        Bot
	dee, eve, cal    Bot
	docsLead, devLed Actor
	teamNames        map[string]string
}

func newHandoffFixture(t *testing.T) *handoffFixture {
	t.Helper()
	f := newBoardFixture(t, 30)
	docs, dee, eve := f.docsTeam(t)
	dev, cal := f.otherTeam(t, "Dev", "Cal", "Dan")
	return &handoffFixture{
		boardFixture: f, docs: docs, dev: dev, dee: dee, eve: eve, cal: cal,
		docsLead:  Actor{BotID: dee.ID, TeamID: docs.ID, Role: RoleLead},
		devLed:    Actor{BotID: cal.ID, TeamID: dev.ID, Role: RoleLead},
		teamNames: map[string]string{f.team.ID: "Ship", docs.ID: "Docs", dev.ID: "Dev"},
	}
}

func (h *handoffFixture) handOver(t *testing.T, a Actor, from, to Bot, text string) Goal {
	t.Helper()
	g, err := h.svc.HandOver(context.Background(), a, from.ID, to.ID, NewHandoff{Text: text, Brief: "Context for " + text + "."})
	if err != nil {
		t.Fatalf("hand over %q: %v", text, err)
	}
	return g
}

// queuedTurns lists every queued turn as "who role @team", with "+goal" when
// a goal pays for it and "+decision" for a lead's decision on a task.
func (h *handoffFixture) queuedTurns(t *testing.T) []string {
	t.Helper()
	turns, err := h.svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, tr := range turns {
		paid := ""
		if tr.GoalID != "" {
			paid = " +goal"
		}
		if tr.Decision {
			paid = " +decision"
		}
		out = append(out, fmt.Sprintf("%s %s @%s%s", h.names[tr.BotID], tr.BoardRole, h.teamNames[tr.RoomID], paid))
	}
	return out
}

func (h *handoffFixture) wantQueued(t *testing.T, step string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := h.queuedTurns(t); !slices.Equal(got, want) {
		t.Fatalf("after %s, queued turns = %q; want %q", step, got, want)
	}
}

func TestAHandoffDuringAnActiveGoalCostsTheSenderNothing(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	runQueued(t, h.svc)
	before := h.goalNow(t, f.goal.ID)

	g, err := h.svc.HandOver(ctx, f.lead, "", h.docs.ID, NewHandoff{Text: " Fix the export bug. ", Brief: "It drops rows.", ArtifactIDs: nil})
	if err != nil {
		t.Fatal(err)
	}
	want := Handoff{Origin: OriginHandoff, FromTeamID: f.team.ID, FromBotID: f.bo.ID, FromGoalID: f.goal.ID, Brief: "It drops rows.", Depth: 1}
	if g.State != GoalIncoming || g.TeamID != h.docs.ID || g.Text != "Fix the export bug." || g.Budget != 0 || !sameHandoff(g.Handoff, want) {
		t.Fatalf("handoff = %+v; want an incoming goal of Docs with %+v", g, want)
	}
	h.wantQueued(t, "the handoff", "Dee lead @Docs +decision")
	runQueued(t, h.svc)
	after := h.goalNow(t, f.goal.ID)
	if after.Used != before.Used || after.Budget != before.Budget || after.State != GoalActive {
		t.Fatalf("Ship's goal after the handoff = %+v; want it as before, %+v", after, before)
	}
	if board, _ := h.svc.Board(ctx, f.team.ID); len(board.Items) != 1 || board.Items[0].ID != parse.ID {
		t.Fatalf("Ship's items = %+v; want only Parse", board.Items)
	}
	if text := lastEventText(t, h.svc, h.docs.ID); text != "Team Ship sent a task: Fix the export bug." {
		t.Fatalf("Docs channel = %q", text)
	}
	if text := lastEventText(t, h.svc, f.team.ID); text != "Bo sent team Docs a task: Fix the export bug." {
		t.Fatalf("Ship channel = %q", text)
	}
	docsBoard, err := h.svc.Board(ctx, h.docs.ID)
	if err != nil || docsBoard.Goal != nil || len(docsBoard.Inbox) != 1 || docsBoard.Inbox[0].ID != g.ID || docsBoard.Teams[f.team.ID] != "Ship" {
		t.Fatalf("Docs's board = %+v, %v; want the incoming handoff and no goal", docsBoard, err)
	}
	shipBoard, _ := h.svc.Board(ctx, f.team.ID)
	if len(shipBoard.HandedOver) != 1 || shipBoard.HandedOver[0].ID != g.ID || shipBoard.Teams[h.docs.ID] != "Docs" {
		t.Fatalf("Ship's handed over = %+v; want the handoff", shipBoard.HandedOver)
	}
	bots, err := h.svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		want := int64(0)
		if b.ID == h.docs.ID {
			want = 1
		}
		if b.IncomingHandoffs != want {
			t.Errorf("%s incoming handoffs = %d; want %d", b.Name, b.IncomingHandoffs, want)
		}
	}
}

func sameHandoff(a, b Handoff) bool {
	return a.Origin == b.Origin && a.FromTeamID == b.FromTeamID && a.FromBotID == b.FromBotID && a.FromGoalID == b.FromGoalID &&
		a.Brief == b.Brief && slices.Equal(a.ArtifactIDs, b.ArtifactIDs) && a.Depth == b.Depth && a.AskRounds == b.AskRounds && a.NeedsUser == b.NeedsUser
}

func TestATeamWithoutAGoalOrWithADeliveredOneHandsOver(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	// Docs has no goal at all.
	g := h.handOver(t, h.docsLead, h.docs, h.dev, "Build the docs site.")
	if g.FromGoalID != "" || g.Depth != 1 || g.FromTeamID != h.docs.ID {
		t.Fatalf("handoff without a goal = %+v; want no goal of the sender, depth 1", g)
	}
	// Ship's goal is delivered and waits for the user.
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	f.finish(t, parse, f.ana)
	if _, err := h.svc.DeliverGoal(ctx, f.lead, "Parsed."); err != nil {
		t.Fatal(err)
	}
	// A delivered task waits for approval without being Ship's work now.
	g = h.handOver(t, f.lead, f.team, h.dev, "Ship the parser.")
	if g.FromGoalID != "" || g.State != GoalIncoming {
		t.Fatalf("handoff while delivered = %+v; want it from no task of Ship's", g)
	}
	if got := h.goalNow(t, f.goal.ID); got.State != GoalDelivered {
		t.Fatalf("Ship's goal = %s; want it still delivered", got.State)
	}
}

func TestHandoffsLandInTheReceiversBudgetAndWakesInNone(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	runQueued(t, h.svc)
	shipUsed := h.goalNow(t, f.goal.ID).Used
	g := h.handOver(t, f.lead, f.team, h.docs, "Write the guide.")
	h.wantQueued(t, "the handoff", "Dee lead @Docs +decision")
	runQueued(t, h.svc)
	if _, err := h.svc.AskSender(ctx, h.docsLead, g.ID, "Which format?"); err != nil {
		t.Fatal(err)
	}
	h.wantQueued(t, "the question", "Bo lead @Ship")
	runQueued(t, h.svc)
	if _, err := h.svc.AnswerHandoff(ctx, f.lead, g.ID, "Markdown."); err != nil {
		t.Fatal(err)
	}
	h.wantQueued(t, "the answer", "Dee lead @Docs +decision")
	runQueued(t, h.svc)
	got, err := h.svc.AcceptHandoff(ctx, h.docsLead, g.ID)
	if err != nil || got.State != GoalActive || got.Budget != DefaultGoalBudget || got.Used != 0 {
		t.Fatalf("accept = %+v, %v; want it active with Docs's budget and nothing used", got, err)
	}
	h.wantQueued(t, "the accept", "Dee lead @Docs +goal")
	runQueued(t, h.svc)
	if used := h.goalNow(t, g.ID).Used; used != 1 {
		t.Fatalf("the handoff's goal used %d turns; want the planning turn", used)
	}
	if used := h.goalNow(t, f.goal.ID).Used; used != shipUsed {
		t.Fatalf("Ship's goal used %d turns; want %d: the handoff costs it nothing", used, shipUsed)
	}
}

func TestAHandoffOutlivesTheSendersGoal(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	queued := h.handOver(t, f.lead, f.team, h.docs, "Queue me.")
	active := h.handOver(t, f.lead, f.team, h.dev, "Run me.")
	finishQueued(t, h.svc)
	if _, err := h.svc.AcceptHandoff(ctx, h.devLed, active.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StopTeam(ctx, f.team.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.goalNow(t, f.goal.ID); got.State != GoalPaused {
		t.Fatalf("Ship's goal = %s; want paused", got.State)
	}
	if err := h.svc.EndGoal(ctx, f.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := h.goalNow(t, queued.ID); got.State != GoalIncoming {
		t.Fatalf("the incoming handoff = %s; want it untouched", got.State)
	}
	if got := h.goalNow(t, active.ID); got.State != GoalActive {
		t.Fatalf("the started handoff = %s; want it running", got.State)
	}
	h.wantQueued(t, "the sender's goal ended", "Cal lead @Dev +goal")
}

func TestATakenTaskStartsOrJoinsThePlanAtItsPlace(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	// Docs is free: the task starts, and its lead plans it.
	first := h.handOver(t, f.lead, f.team, h.docs, "First.")
	finishQueued(t, h.svc)
	g, err := h.svc.TakeTask(ctx, h.docsLead, first.ID, 0)
	if err != nil || g.State != GoalActive || g.QueuePos != 0 {
		t.Fatalf("take on a free team = %+v, %v; want it active", g, err)
	}
	h.wantQueued(t, "the take", "Dee lead @Docs +goal")
	if text := lastEventText(t, h.svc, f.team.ID); text != `Team Docs took your task "First."; it started.` {
		t.Fatalf("Ship channel = %q", text)
	}
	if text := lastEventText(t, h.svc, h.docs.ID); text != `Dee took the task "First."; it started.` {
		t.Fatalf("Docs channel = %q", text)
	}
	finishQueued(t, h.svc)
	// Docs is busy: the next ones go into the plan, at the end or at a place.
	second := h.handOver(t, f.lead, f.team, h.docs, "Second.")
	third := h.handOver(t, h.devLed, h.dev, h.docs, "Third.")
	if g, err = h.svc.TakeTask(ctx, h.docsLead, second.ID, 0); err != nil || g.State != GoalQueued || g.QueuePos != 1 {
		t.Fatalf("take on a busy team = %+v, %v; want it planned at #1", g, err)
	}
	if text := lastEventText(t, h.svc, h.docs.ID); text != `Dee took the task "Second."; it is #1 in the plan.` {
		t.Fatalf("Docs channel = %q", text)
	}
	if g, err = h.svc.TakeTask(ctx, h.docsLead, third.ID, 1); err != nil || g.QueuePos != 1 {
		t.Fatalf("take at #1 = %+v, %v; want it first", g, err)
	}
	if got := h.goalNow(t, second.ID); got.QueuePos != 2 {
		t.Fatalf("the task it went above = #%d; want #2", got.QueuePos)
	}
	for _, pos := range []int64{-1, 4} {
		fourth := h.handOver(t, f.lead, f.team, h.docs, fmt.Sprintf("Fourth %d.", pos))
		if _, err := h.svc.TakeTask(ctx, h.docsLead, fourth.ID, pos); !errors.Is(err, ErrInvalid) {
			t.Errorf("take at %d: err = %v; want ErrInvalid", pos, err)
		}
	}
	// When the Now task ends, the head of the plan starts on its own.
	if err := h.svc.EndGoal(ctx, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := h.goalNow(t, third.ID); got.State != GoalActive {
		t.Fatalf("the head after the end = %s; want it active", got.State)
	}
	if got := h.goalNow(t, second.ID); got.State != GoalQueued || got.QueuePos != 1 {
		t.Fatalf("the rest of the plan = %+v; want it first now", got)
	}
	if text := lastEventText(t, h.svc, h.docs.ID); text != "Next goal started: Third." {
		t.Fatalf("Docs channel after the end = %q", text)
	}
	// A place at the very end is a place too.
	last := h.handOver(t, h.devLed, h.dev, h.docs, "Last.")
	if g, err := h.svc.TakeTask(ctx, h.docsLead, last.ID, 2); err != nil || g.QueuePos != 2 {
		t.Fatalf("take at the end = %+v, %v; want it at #2", g, err)
	}
}

func TestOnlyTheReceivingLeadOrTheUserTakesAHandoffIn(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	for name, a := range map[string]Actor{
		"a member":           {BotID: h.eve.ID, TeamID: h.docs.ID, Role: RoleWork},
		"the sending lead":   f.lead,
		"the lead elsewhere": {BotID: h.dee.ID, TeamID: f.team.ID, Role: RoleLead},
	} {
		if _, err := h.svc.AcceptHandoff(ctx, a, g.ID); !errors.Is(err, ErrInvalid) {
			t.Errorf("accept by %s: err = %v; want ErrInvalid", name, err)
		}
	}
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, f.goal.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("accept a goal that is no handoff: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("accept a missing goal: err = %v; want ErrNotFound", err)
	}
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, g.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, g.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("accept twice: err = %v; want ErrInvalid", err)
	}
}

func TestAFullQueueTakesNoMoreHandoffs(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	if _, err := h.svc.StartGoal(ctx, h.docs.ID, "Own goal."); err != nil {
		t.Fatal(err)
	}
	for i := range MaxQueuedGoals {
		if _, err := h.svc.StartGoal(ctx, h.docs.ID, fmt.Sprintf("Goal %d.", i)); err != nil {
			t.Fatal(err)
		}
	}
	g := h.handOver(t, f.lead, f.team, h.docs, "One too many.")
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, g.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "10 tasks planned") {
		t.Fatalf("accept into a full queue: err = %v; want ErrInvalid", err)
	}
	if got := h.goalNow(t, g.ID); got.State != GoalIncoming {
		t.Fatalf("handoff after a failed accept = %s; want it incoming", got.State)
	}
}

func TestTheReceiverAsksTheSenderAndTheSenderAnswers(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	finishQueued(t, h.svc)
	if _, err := h.svc.AnswerHandoff(ctx, f.lead, g.ID, "Early."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an answer before any question: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.AskSender(ctx, Actor{Role: RoleUser}, g.ID, "Hm?"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the user asks: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.AskSender(ctx, h.docsLead, g.ID, " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an empty question: err = %v; want ErrInvalid", err)
	}
	asked, err := h.svc.AskSender(ctx, h.docsLead, g.ID, "Which format?")
	if err != nil || asked.AskRounds != 1 || asked.AskedAt == 0 || asked.IntakeNote != "Which format?" {
		t.Fatalf("ask = %+v, %v", asked, err)
	}
	if got := h.goalNow(t, g.ID); got.AskRounds != 1 || got.AskedAt == 0 || got.IntakeNote != "Which format?" {
		t.Fatalf("stored question = %+v", got.Handoff)
	}
	h.wantQueued(t, "the question", "Bo lead @Ship")
	if text := lastEventText(t, h.svc, f.team.ID); text != `Team Docs asks about your task "Guide.": Which format?` {
		t.Fatalf("Ship channel = %q", text)
	}
	if text := lastEventText(t, h.svc, h.docs.ID); text != `Dee asked about the task "Guide." (1 of 3): Which format?` {
		t.Fatalf("Docs channel = %q", text)
	}
	finishQueued(t, h.svc)
	if _, err := h.svc.AnswerHandoff(ctx, h.docsLead, g.ID, "Mine."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the receiver answers: err = %v; want ErrInvalid", err)
	}
	answered, err := h.svc.AnswerHandoff(ctx, f.lead, g.ID, "Markdown.")
	if err != nil || answered.AskedAt != 0 || answered.Brief != "Context for Guide..\n\nAnswer from Bo: Markdown." {
		t.Fatalf("answer = %+v, %v", answered, err)
	}
	if got := h.goalNow(t, g.ID); got.Brief != answered.Brief || got.AskedAt != 0 {
		t.Fatalf("stored answer = %+v", got.Handoff)
	}
	h.wantQueued(t, "the answer", "Dee lead @Docs +decision")
	if text := lastEventText(t, h.svc, h.docs.ID); text != `Bo answered about the task "Guide.": Markdown.` {
		t.Fatalf("Docs channel = %q", text)
	}
	if text := lastEventText(t, h.svc, f.team.ID); text != `Bo answered team Docs about the task "Guide.": Markdown.` {
		t.Fatalf("Ship channel = %q", text)
	}
}

func TestTheReceiverAsksTheUserAboutTheUsersHandoff(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	g, err := h.svc.HandOver(ctx, Actor{Role: RoleUser}, "", h.docs.ID, NewHandoff{Text: "From the user."})
	if err != nil || g.FromTeamID != "" || g.FromBotID != "" || g.Depth != 0 {
		t.Fatalf("the user's handoff = %+v, %v", g, err)
	}
	if text := lastEventText(t, h.svc, h.docs.ID); text != "The user sent a task: From the user." {
		t.Fatalf("Docs channel = %q", text)
	}
	finishQueued(t, h.svc)
	if _, err := h.svc.AskSender(ctx, h.docsLead, g.ID, "How soon?"); err != nil {
		t.Fatal(err)
	}
	h.wantQueued(t, "a question to the user")
	open, err := h.svc.OpenQuestions(ctx, h.docs.ID)
	if err != nil || len(open) != 1 || open[0].AskerID != h.dee.ID || !strings.Contains(open[0].Question, "How soon?") {
		t.Fatalf("open questions in Docs = %+v, %v; want Dee's question to the user", open, err)
	}
	if _, err := h.svc.AnswerHandoff(ctx, h.docsLead, g.ID, "Mine."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a lead answers the user's handoff: err = %v; want ErrInvalid", err)
	}
	got, err := h.svc.AnswerHandoff(ctx, Actor{Role: RoleUser}, g.ID, "Today.")
	if err != nil || !strings.HasSuffix(got.Brief, "Answer from The user: Today.") {
		t.Fatalf("the user's answer = %+v, %v", got, err)
	}
	h.wantQueued(t, "the user's answer", "Dee lead @Docs +decision")
	// The user hands over for team Ship.
	f := h.boardFixture
	g, err = h.svc.HandOver(ctx, Actor{Role: RoleUser}, f.team.ID, h.dev.ID, NewHandoff{Text: "For Ship."})
	if err != nil || g.FromTeamID != f.team.ID || g.FromBotID != "" || g.FromGoalID != f.goal.ID {
		t.Fatalf("the user's handoff for Ship = %+v, %v", g, err)
	}
	if text := lastEventText(t, h.svc, h.dev.ID); text != "The user, for team Ship, sent a task: For Ship." {
		t.Fatalf("Dev channel = %q", text)
	}
	if text := lastEventText(t, h.svc, f.team.ID); text != "The user sent team Dev a task: For Ship." {
		t.Fatalf("Ship channel = %q", text)
	}
	if _, err := h.svc.HandOver(ctx, Actor{Role: RoleUser}, "missing", h.dev.ID, NewHandoff{Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("for a missing team: err = %v; want ErrNotFound", err)
	}
}

func TestADeclinedHandoffNeedsAReasonAndTellsTheSender(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	finishQueued(t, h.svc)
	if _, err := h.svc.DeclineHandoff(ctx, h.docsLead, g.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a decline without a reason: err = %v; want ErrInvalid", err)
	}
	got, err := h.svc.DeclineHandoff(ctx, h.docsLead, g.ID, "Not ours.")
	if err != nil || got.State != GoalDeclined || got.IntakeNote != "Not ours." {
		t.Fatalf("decline = %+v, %v", got, err)
	}
	if stored := h.goalNow(t, g.ID); stored.State != GoalDeclined || stored.IntakeNote != "Not ours." || stored.Open() {
		t.Fatalf("declined goal = %+v; want declined, not open", stored)
	}
	h.wantQueued(t, "the decline", "Bo lead @Ship")
	if text := lastEventText(t, h.svc, f.team.ID); text != `Team Docs pushed back your task "Guide.": Not ours.` {
		t.Fatalf("Ship channel = %q", text)
	}
	if text := lastEventText(t, h.svc, h.docs.ID); text != `Dee pushed back the task "Guide.": Not ours.` {
		t.Fatalf("Docs channel = %q", text)
	}
	if _, err := h.svc.DeclineHandoff(ctx, h.docsLead, g.ID, "Again."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("decline twice: err = %v; want ErrInvalid", err)
	}
	own, err := h.svc.StartGoal(ctx, h.docs.ID, "Own goal.")
	if err != nil || own.State != GoalActive {
		t.Fatalf("a goal after the decline = %+v, %v; want it active: a declined goal is not open", own, err)
	}
	board, _ := h.svc.Board(ctx, h.docs.ID)
	if board.Goal == nil || board.Goal.ID != own.ID || len(board.Inbox) != 0 {
		t.Fatalf("Docs's board = %+v; want its own goal and nothing incoming", board)
	}
	// The user's handoff wakes no lead when it is declined.
	mine, err := h.svc.HandOver(ctx, Actor{Role: RoleUser}, f.team.ID, h.dev.ID, NewHandoff{Text: "Mine."})
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, h.svc)
	if _, err := h.svc.DeclineHandoff(ctx, Actor{Role: RoleUser}, mine.ID, "No."); err != nil {
		t.Fatal(err)
	}
	h.wantQueued(t, "a decline of the user's handoff")
}

func TestTheSenderWithdrawsAHandoffUntilItStarts(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	if _, err := h.svc.StartGoal(ctx, h.docs.ID, "Own goal."); err != nil {
		t.Fatal(err)
	}
	incoming := h.handOver(t, f.lead, f.team, h.docs, "Incoming.")
	first := h.handOver(t, f.lead, f.team, h.docs, "First.")
	second := h.handOver(t, f.lead, f.team, h.docs, "Second.")
	for _, g := range []Goal{first, second} {
		if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, g.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.WithdrawHandoff(ctx, h.docsLead, incoming.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the receiver withdraws: err = %v; want ErrInvalid", err)
	}
	got, err := h.svc.WithdrawHandoff(ctx, f.lead, incoming.ID)
	if err != nil || got.State != GoalEnded {
		t.Fatalf("withdraw an incoming handoff = %+v, %v", got, err)
	}
	if text := lastEventText(t, h.svc, h.docs.ID); text != `Withdrawn by Bo: the task "Incoming.".` {
		t.Fatalf("Docs channel = %q", text)
	}
	if text := lastEventText(t, h.svc, f.team.ID); text != `Bo withdrew the task "Incoming." from team Docs.` {
		t.Fatalf("Ship channel = %q", text)
	}
	if _, err := h.svc.WithdrawHandoff(ctx, Actor{Role: RoleUser}, first.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.goalNow(t, second.ID); got.QueuePos != 1 {
		t.Fatalf("Second after First was withdrawn = #%d; want #1", got.QueuePos)
	}
	// A started handoff only the user ends.
	active := h.handOver(t, f.lead, f.team, h.dev, "Active.")
	if _, err := h.svc.AcceptHandoff(ctx, h.devLed, active.ID); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Actor{f.lead, {Role: RoleUser}} {
		if _, err := h.svc.WithdrawHandoff(ctx, a, active.ID); !errors.Is(err, ErrInvalid) {
			t.Fatalf("withdraw a started handoff: err = %v; want ErrInvalid", err)
		}
	}
	if board, _ := h.svc.Board(ctx, h.dev.ID); board.Goal == nil || board.Goal.ID != active.ID {
		t.Fatalf("Dev's goal = %+v; want the handoff", board.Goal)
	}
	// A withdrawn handoff is no latest goal of its team.
	gone := h.handOver(t, f.lead, f.team, h.dev, "Gone.")
	if err := h.svc.EndGoal(ctx, active.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.WithdrawHandoff(ctx, f.lead, gone.ID); err != nil {
		t.Fatal(err)
	}
	if board, _ := h.svc.Board(ctx, h.dev.ID); board.Goal == nil || board.Goal.ID != active.ID {
		t.Fatalf("Dev's latest goal = %+v; want the ended handoff, not the withdrawn one", board.Goal)
	}
}

func TestTheSenderHearsWhatBecameOfItsHandoff(t *testing.T) {
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
	if text := lastEventText(t, h.svc, f.team.ID); text != `Team Docs delivered: The guide is in guide.md. — your task "Guide."` {
		t.Fatalf("Ship channel after the delivery = %q", text)
	}
	h.wantQueued(t, "the delivery", "Bo lead @Ship")
	if _, err := h.svc.AcceptGoal(ctx, g.ID, true); err != nil {
		t.Fatal(err)
	}
	if text := lastEventText(t, h.svc, f.team.ID); text != `Team Docs finished, and the user accepted, your task "Guide."` {
		t.Fatalf("Ship channel after the accept = %q", text)
	}
	other := h.handOver(t, f.lead, f.team, h.dev, "Build.")
	if _, err := h.svc.AcceptHandoff(ctx, h.devLed, other.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EndGoal(ctx, other.ID, true); err != nil {
		t.Fatal(err)
	}
	if text := lastEventText(t, h.svc, f.team.ID); text != `Team Dev stopped: the user ended your task "Build."` {
		t.Fatalf("Ship channel after the end = %q", text)
	}
	// A sender that is gone hears nothing, and the board names it.
	from := h.handOver(t, h.devLed, h.dev, h.docs, "From Dev.")
	if err := h.svc.DeleteBot(ctx, h.dev.ID); err != nil {
		t.Fatal(err)
	}
	board, err := h.svc.Board(ctx, h.docs.ID)
	if err != nil || len(board.Inbox) != 1 || board.Inbox[0].ID != from.ID || board.Teams[h.dev.ID] != "a deleted team" {
		t.Fatalf("Docs's board = %+v, %v; want the handoff of a deleted team", board, err)
	}
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, from.ID); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffsCannotLoopOrPileUp(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	ops, olga := f.otherTeam(t, "Ops", "Olga", "Oz")
	opsLead := Actor{BotID: olga.ID, TeamID: ops.ID, Role: RoleLead}
	if _, err := h.svc.HandOver(ctx, f.lead, f.team.ID, f.team.ID, NewHandoff{Text: "Me."}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hand over to the own team: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.HandOver(ctx, f.lead, h.docs.ID, h.dev.ID, NewHandoff{Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hand over for another team: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.HandOver(ctx, f.as(f.ana, RoleWork), "", h.dev.ID, NewHandoff{Text: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a member hands over: err = %v; want ErrInvalid", err)
	}
	// Ship (depth 0) → Docs (1) → Dev (2) → Ops (3), and Ops can't pass it on.
	chain := []struct {
		a        Actor
		from, to Bot
	}{{f.lead, f.team, h.docs}, {h.docsLead, h.docs, h.dev}, {h.devLed, h.dev, ops}}
	receivers := []Actor{h.docsLead, h.devLed, opsLead}
	for i, c := range chain {
		g := h.handOver(t, c.a, c.from, c.to, fmt.Sprintf("Step %d.", i+1))
		if g.Depth != int64(i+1) {
			t.Fatalf("step %d depth = %d; want %d", i+1, g.Depth, i+1)
		}
		if _, err := h.svc.AcceptHandoff(ctx, receivers[i], g.ID); err != nil {
			t.Fatal(err)
		}
	}
	_, err := h.svc.HandOver(ctx, opsLead, ops.ID, f.team.ID, NewHandoff{Text: "Step 4."})
	if !errors.Is(err, ErrInvalid) || err.Error() != "invalid input: This work came through 3 teams; ask the user before sending it on." {
		t.Fatalf("a fourth send: err = %v; want the depth limit", err)
	}
}

func TestAHandoffsTextsAndArtifactsAreChecked(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	kind, title, content := "plan", "Spec", "Do it."
	a, err := h.svc.CreateArtifact(ctx, f.bo.ID, ArtifactInput{Kind: &kind, Title: &title, Content: &content})
	if err != nil {
		t.Fatal(err)
	}
	for name, n := range map[string]NewHandoff{
		"no goal":            {Text: " "},
		"a long goal":        {Text: strings.Repeat("x", maxGoalText+1)},
		"a long brief":       {Text: "x", Brief: strings.Repeat("x", maxItemText+1)},
		"a missing paper":    {Text: "x", ArtifactIDs: []string{"missing"}},
		"too many artifacts": {Text: "x", ArtifactIDs: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}},
	} {
		if _, err := h.svc.HandOver(ctx, f.lead, "", h.docs.ID, n); err == nil {
			t.Errorf("hand over with %s: want an error", name)
		}
	}
	if _, err := h.svc.HandOver(ctx, f.lead, "", "missing", NewHandoff{Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("hand over to a missing team: err = %v; want ErrNotFound", err)
	}
	g, err := h.svc.HandOver(ctx, f.lead, "", h.docs.ID, NewHandoff{Text: "Build the spec.", ArtifactIDs: []string{a.ID, a.ID}})
	if err != nil || !slices.Equal(g.ArtifactIDs, []string{a.ID}) {
		t.Fatalf("handoff with an artifact = %+v, %v", g, err)
	}
	if got := h.goalNow(t, g.ID); !slices.Equal(got.ArtifactIDs, []string{a.ID}) {
		t.Fatalf("stored artifacts = %v", got.ArtifactIDs)
	}
}

func TestTheAskLoopEndsWithTheUserAfterThreeRounds(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	runQueued(t, h.svc)
	shipUsed := h.goalNow(t, f.goal.ID).Used
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	asks, answers := 0, 0
	for range 10 {
		runQueued(t, h.svc)
		if _, err := h.svc.AskSender(ctx, h.docsLead, g.ID, fmt.Sprintf("Question %d?", asks+1)); err != nil {
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), `You asked Bo 3 times about Guide.; take it, push it back, or ask the user.`) {
				t.Fatalf("the 4th question: err = %v; want the round limit", err)
			}
			break
		}
		asks++
		runQueued(t, h.svc)
		if _, err := h.svc.AnswerHandoff(ctx, f.lead, g.ID, fmt.Sprintf("Answer %d.", answers+1)); err != nil {
			t.Fatal(err)
		}
		answers++
	}
	if asks != 3 || answers != 3 {
		t.Fatalf("asks, answers = %d, %d; want 3 and 3", asks, answers)
	}
	h.wantQueued(t, "the 4th question")
	stored := h.goalNow(t, g.ID)
	if !stored.NeedsUser || stored.State != GoalIncoming {
		t.Fatalf("handoff after 3 rounds = %+v; want it to need the user", stored)
	}
	open, err := h.svc.OpenQuestions(ctx, h.docs.ID)
	want := `Dee and Bo couldn't settle "Guide." in 3 rounds: Question 3? Take it, push it back or answer it?`
	if err != nil || len(open) != 1 || open[0].AskerID != h.dee.ID || open[0].Question != want {
		t.Fatalf("open questions = %+v, %v; want %q", open, err, want)
	}
	// Asking again asks the user no second time.
	if _, err := h.svc.AskSender(ctx, h.docsLead, g.ID, "Again?"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a 5th question: err = %v; want ErrInvalid", err)
	}
	if open, _ := h.svc.OpenQuestions(ctx, h.docs.ID); len(open) != 1 {
		t.Fatalf("open questions after a 5th try = %d; want 1", len(open))
	}
	h.wantQueued(t, "the 5th question")
	if _, err := h.svc.AnswerHandoff(ctx, f.lead, g.ID, "Me again."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the sending lead answers after 3 rounds: err = %v; want ErrInvalid", err)
	}
	if _, err := h.svc.AnswerHandoff(ctx, Actor{Role: RoleUser}, g.ID, "Do it."); err != nil {
		t.Fatalf("the user answers: %v", err)
	}
	if used := h.goalNow(t, f.goal.ID).Used; used != shipUsed {
		t.Fatalf("Ship's goal used %d; want %d", used, shipUsed)
	}
	if used := h.goalNow(t, g.ID).Used; used != 0 {
		t.Fatalf("the handoff used %d; want 0", used)
	}
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, g.ID); err != nil {
		t.Fatalf("the receiving lead accepts after 3 rounds: %v", err)
	}
}

func TestTheInboxLimitsAreTheUsersTeamSettings(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	if got, _ := h.svc.GetBot(ctx, h.docs.ID); got.Team.InboxMax != 10 || got.Team.InboxPerSender != 3 {
		t.Fatalf("default limits = %d, %d; want 10 and 3", got.Team.InboxMax, got.Team.InboxPerSender)
	}
	inboxMax, perSender := int64(5), int64(2)
	team, err := h.svc.UpdateTeam(ctx, h.docs.ID, TeamPatch{InboxMax: &inboxMax, InboxPerSender: &perSender})
	if err != nil || team.Team.InboxMax != 5 || team.Team.InboxPerSender != 2 {
		t.Fatalf("set limits = %+v, %v", team.Team, err)
	}
	if got, _ := h.svc.GetBot(ctx, h.docs.ID); got.Team.InboxMax != 5 || got.Team.InboxPerSender != 2 {
		t.Fatalf("stored limits = %+v", got.Team)
	}
	for _, c := range []struct{ inboxMax, perSender int64 }{{51, 2}, {-1, 2}, {5, 6}, {5, -1}} {
		if _, err := h.svc.UpdateTeam(ctx, h.docs.ID, TeamPatch{InboxMax: &c.inboxMax, InboxPerSender: &c.perSender}); !errors.Is(err, ErrInvalid) {
			t.Errorf("limits %d, %d: err = %v; want ErrInvalid", c.inboxMax, c.perSender, err)
		}
	}
	for _, c := range []struct{ inboxMax, perSender int64 }{{50, 50}, {1, 1}} {
		if _, err := h.svc.UpdateTeam(ctx, h.docs.ID, TeamPatch{InboxMax: &c.inboxMax, InboxPerSender: &c.perSender}); err != nil {
			t.Errorf("limits %d, %d: %v; want them saved", c.inboxMax, c.perSender, err)
		}
	}
	created, err := h.svc.CreateTeam(ctx, "Small", "", []string{h.eve.ID}, TeamSettings{InboxMax: 4, InboxPerSender: 1})
	if err != nil || created.Team.InboxMax != 4 || created.Team.InboxPerSender != 1 {
		t.Fatalf("a team created with limits = %+v, %v", created.Team, err)
	}
}

func TestAHandoffFromABoardTurnIsOneDeeperThanItsGoal(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	finishQueued(t, h.svc)
	if _, err := h.svc.AcceptHandoff(ctx, h.docsLead, g.ID); err != nil {
		t.Fatal(err)
	}
	turns, err := h.svc.QueuedTurns(ctx, 10)
	if err != nil || len(turns) != 1 || turns[0].GoalID != g.ID {
		t.Fatalf("queued = %+v, %v; want Dee's planning turn", turns, err)
	}
	// The planning turn of the handoff's goal hands over from that goal.
	a := h.docsLead
	a.TurnID = turns[0].ID
	next, err := h.svc.HandOver(ctx, a, h.docs.ID, h.dev.ID, NewHandoff{Text: "Build it."})
	if err != nil || next.FromGoalID != g.ID || next.Depth != 2 {
		t.Fatalf("handoff from the planning turn = %+v, %v; want it from the handoff's goal, depth 2", next, err)
	}
}

func TestTheSenderSeesItsHandoffsOfTheLastThirtyDays(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	start := time.Now()
	for _, c := range []struct {
		after time.Duration
		n     int
	}{{29 * 24 * time.Hour, 1}, {30*24*time.Hour + time.Hour, 0}} {
		h.svc.now = func() time.Time { return start.Add(c.after) }
		board, err := h.svc.Board(ctx, f.team.ID)
		if err != nil || len(board.HandedOver) != c.n {
			t.Fatalf("handed over %s later = %+v, %v; want %d", c.after, board.HandedOver, err, c.n)
		}
		if c.n == 1 && board.HandedOver[0].ID != g.ID {
			t.Fatalf("handed over = %+v", board.HandedOver)
		}
	}
}

func TestAHandoffCarriesUpToTenArtifacts(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	var ids []string
	for i := range maxHandoffArtifacts + 1 {
		kind, title, content := "note", fmt.Sprintf("Note %d", i), "x"
		a, err := h.svc.CreateArtifact(ctx, f.bo.ID, ArtifactInput{Kind: &kind, Title: &title, Content: &content})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	if _, err := h.svc.HandOver(ctx, f.lead, "", h.docs.ID, NewHandoff{Text: "Ten.", ArtifactIDs: ids[:maxHandoffArtifacts]}); err != nil {
		t.Fatalf("a handoff with %d artifacts: %v", maxHandoffArtifacts, err)
	}
	if _, err := h.svc.HandOver(ctx, f.lead, "", h.docs.ID, NewHandoff{Text: "Eleven.", ArtifactIDs: ids}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a handoff with %d artifacts: err = %v; want ErrInvalid", len(ids), err)
	}
}

func TestThePushedBackTaskOfASendingTeamIsPutAwayOnce(t *testing.T) {
	h := newHandoffFixture(t)
	ctx := context.Background()
	f := h.boardFixture
	now := time.UnixMilli(1_800_000_000_000)
	h.svc.now = func() time.Time { return now }
	g := h.handOver(t, f.lead, f.team, h.docs, "Guide.")
	if _, err := h.svc.DismissTask(ctx, g.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("put away a task in the inbox: err = %v; want ErrConflict", err)
	}
	if _, err := h.svc.DeclineHandoff(ctx, h.docsLead, g.ID, "Not ours."); err != nil {
		t.Fatal(err)
	}
	docsBefore, err := h.svc.Board(ctx, h.docs.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, stop := h.svc.Hub().Subscribe()
	defer stop()
	got, err := h.svc.DismissTask(ctx, g.ID)
	if err != nil || got.DismissedAt != now.UnixMilli() || got.State != GoalDeclined {
		t.Fatalf("put away = %+v, %v; want it declined and put away now", got, err)
	}
	if !gotEvent(events, "bots") {
		t.Fatal("no bots event for the put-away task")
	}
	now = now.Add(time.Hour)
	if again, err := h.svc.DismissTask(ctx, g.ID); err != nil || again.DismissedAt != got.DismissedAt {
		t.Fatalf("put away again = %+v, %v; want the first time kept", again, err)
	}
	if gotEvent(events, "bots") {
		t.Fatal("a bots event for a task already put away; want none, nothing changed")
	}
	ship, err := h.svc.Board(ctx, f.team.ID)
	if err != nil || len(ship.HandedOver) != 1 || ship.HandedOver[0].DismissedAt != got.DismissedAt {
		t.Fatalf("Ship's sent tasks = %+v, %v; want the task, put away", ship.HandedOver, err)
	}
	if docsAfter, err := h.svc.Board(ctx, h.docs.ID); err != nil || !reflect.DeepEqual(docsAfter, docsBefore) {
		t.Fatalf("Docs's board = %+v, %v; want it as before: %+v", docsAfter, err, docsBefore)
	}
	own, err := h.svc.UserTask(ctx, h.docs.ID, NewTask{Text: "Own."}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.DeclineHandoff(ctx, h.docsLead, own.ID, "No."); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.DismissTask(ctx, own.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("put away a pushed-back task that no team sent: err = %v; want ErrConflict", err)
	}
	if _, err := h.svc.DismissTask(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("put away a missing task: err = %v; want ErrNotFound", err)
	}
}
