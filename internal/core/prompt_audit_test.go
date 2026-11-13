package core

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// Audit finding 4: the errors name the actions that exist: send_task, propose and waits_for.
func TestErrorsNameTheBoardActionsThatExist(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := func(name string) Bot {
		t.Helper()
		b, err := svc.CreateBot(ctx, name, "")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	ana, bo, cy := bot("Ana"), bot("Bo"), bot("Cy")
	team, err := svc.CreateTeam(ctx, "Ship", "", []string{ana.ID, bo.ID}, TeamSettings{LeadID: ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := svc.CreateTeam(ctx, "Docs", "", []string{cy.ID}, TeamSettings{LeadID: cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, err error, want, gone string) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), gone) {
			t.Errorf("%s: %v; want it to say %q, not %q", what, err, want, gone)
		}
	}
	err = svc.CheckTeamTurnTarget(ctx, team.ID, ana.ID, bo.ID, "Help?", 0)
	check("a message to a bot in a team turn", err, "a lead gives another team a task with update_board send_task", "hand_over")
	_, err = svc.CreateItem(ctx, Actor{BotID: ana.ID, TeamID: team.ID, Role: RoleLead}, NewItem{Title: "Write", Brief: "b", Acceptance: "a", OwnerID: bo.ID})
	check("a step without a Now task", err, "the team has no Now task; propose one with update_board propose", "start_goal")
	_, err = svc.SendTask(ctx, Actor{BotID: ana.ID, TeamID: team.ID, Role: RoleLead}, docs.ID, NewTask{Text: "Write the guide.", WaitsOn: "step-1"})
	check("waits_for without a task", err, "waits_for names a step", "waits_on")
}

// Audit finding 9: the board's turn prompts and errors say task and step.
func TestBoardTurnsAndErrorsSayTaskAndStep(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	parse := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	turns, err := f.svc.QueuedTurns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tr := range turns {
		found = found || (tr.ItemID == parse.ID && tr.Prompt == "Work on your step.")
	}
	if !found {
		t.Fatalf("queued turns = %+v; want Ana's work turn with \"Work on your step.\"", turns)
	}
	if _, err := f.svc.SubmitItem(ctx, f.as(f.cy, RoleWork), parse.ID, "x"); err == nil || err.Error() != "invalid input: only the owner submits a step that is being done" {
		t.Fatalf("a submit by someone else = %v; want the step error", err)
	}
	if _, err := f.svc.CreateItem(ctx, f.lead, NewItem{Title: "Parse", Brief: "b", Acceptance: "a"}); err == nil || !strings.Contains(err.Error(), `the task already has a step "Parse"`) {
		t.Fatalf("a second Parse = %v; want the step error", err)
	}
}

// Audit findings 7 and 9: the user's approval gives the lead a retro turn that says the task was
// approved, and starts the next planned task when the plan is on hold.
func TestTheUsersApprovalStartsTheRetroThenTheNextTask(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	// While the done task waits for the approval, the user starts another one and plans a third,
	// then ends the second without starting the next: the plan is on hold.
	second, err := f.svc.UserTask(ctx, f.team.ID, NewTask{Text: "Then: CSV import."}, true)
	if err != nil {
		t.Fatal(err)
	}
	third, err := f.svc.UserTask(ctx, f.team.ID, NewTask{Text: "Next: JSON export."}, true)
	if err != nil || third.State != GoalQueued {
		t.Fatalf("the third task = %+v, %v; want it planned", third, err)
	}
	if err := f.svc.EndGoal(ctx, second.ID, false); err != nil {
		t.Fatal(err)
	}
	finishQueued(t, f.svc)
	if _, err := f.svc.AcceptGoal(ctx, f.goal.ID, true); err != nil {
		t.Fatal(err)
	}
	turns, err := f.svc.QueuedTurns(ctx, 10)
	if err != nil || len(turns) == 0 || turns[0].BoardRole != RoleRetro || turns[0].Prompt != "The user approved the task." {
		t.Fatalf("queued turns = %+v, %v; want the retro first, saying the user approved the task", turns, err)
	}
	if board, err := f.svc.Board(ctx, f.team.ID); err != nil || board.Goal == nil || board.Goal.ID != third.ID || board.Goal.State != GoalActive {
		t.Fatalf("Now task after the approval = %+v, %v; want %q in progress", board.Goal, err, third.Text)
	}
}

// Audit finding 9: the lead's wakes for a task that started from the plan, and for a Now task
// nobody works on, say task and step.
func TestTheLeadsWakesSayTaskAndStep(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	prompts := func() []string {
		t.Helper()
		turns, err := f.svc.QueuedTurns(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, tr := range turns {
			out = append(out, tr.Prompt)
		}
		return out
	}
	// A turn of the Now task that ended badly, and no step: the scan wakes the lead.
	stopped, err := f.svc.queueTurn(ctx, f.svc.q, Turn{BotID: f.bo.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.StartTurn(ctx, stopped); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.FinishTurn(ctx, stopped, "interrupted", "server restarted"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SettleItems(ctx, func(string, string) bool { return false }, LeadState{}); err != nil {
		t.Fatal(err)
	}
	if got := prompts(); !slices.Contains(got, "The Now task has no step in work or review.") {
		t.Fatalf("queued prompts after the scan = %q; want the stall wake", got)
	}
	finishQueued(t, f.svc)
	// A task first in the plan starts when the Now task is closed.
	if _, err := f.svc.UserTask(ctx, f.team.ID, NewTask{Text: "Next."}, true); err != nil {
		t.Fatal(err)
	}
	f.finish(t, f.create(t, f.lead, item("Parse", f.ana.ID, "")), f.ana)
	if _, err := f.svc.DeliverGoal(ctx, f.lead, "Done."); err != nil {
		t.Fatal(err)
	}
	if got := prompts(); !slices.Contains(got, "The next task of the plan started: split it into steps.") {
		t.Fatalf("queued prompts after the close = %q; want the next task's wake", got)
	}
}
