package core

import (
	"context"
	"testing"

	"yargpt/internal/db"
)

// queuedOf returns the one queued turn of bot, and finishes every other.
func queuedOf(t *testing.T, svc *Service, botID string) Turn {
	t.Helper()
	turns, err := svc.QueuedTurns(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var found []Turn
	for _, tr := range turns {
		if tr.BotID == botID {
			found = append(found, tr)
		}
	}
	if len(found) != 1 {
		t.Fatalf("queued turns of %s = %+v; want one", botID, found)
	}
	return found[0]
}

// AC13: a turn woken by an @mention from a turn on task T works on T, and on
// its step when the woken member owns or reviews it.
func TestAMentionFromWorkOnATaskCarriesTheTaskAndTheStep(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	it := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	work := queuedOf(t, f.svc, f.ana.ID)
	if work.BoardRole != RoleWork || work.ItemID != it.ID {
		t.Fatalf("Ana's turn = %+v; want her work on Parse", work)
	}
	finishQueued(t, f.svc)
	if _, err := f.svc.PostToRoom(ctx, f.team.ID, f.ana.ID, work.ID, "@Cy @Bo look at this", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	cy, bo := queuedOf(t, f.svc, f.cy.ID), queuedOf(t, f.svc, f.bo.ID)
	if cy.GoalID != f.goal.ID || cy.ItemID != it.ID || cy.BoardRole != "" {
		t.Errorf("Cy's turn = %+v; want the task and Parse, which Cy reviews, as a conversation turn", cy)
	}
	if bo.GoalID != f.goal.ID || bo.ItemID != "" {
		t.Errorf("Bo's turn = %+v; want the task only, since Bo neither owns nor reviews Parse", bo)
	}

	// A turn on no task passes nothing on.
	finishQueued(t, f.svc)
	if _, err := f.svc.PostToRoom(ctx, f.team.ID, f.ana.ID, "", "@Cy hi", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	if got := queuedOf(t, f.svc, f.cy.ID); got.GoalID != "" || got.ItemID != "" {
		t.Errorf("a mention from no turn = %+v; want no task", got)
	}
}

func TestDecisionsAndRetrosPassNoTaskOn(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	for _, from := range []Turn{
		{BotID: f.bo.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleLead, Decision: true, Prompt: "decide"},
		{BotID: f.bo.ID, Source: SourceRoom, RoomID: f.team.ID, GoalID: f.goal.ID, BoardRole: RoleRetro, Prompt: "retro"},
	} {
		id, err := f.svc.queueTurn(ctx, f.svc.q, from)
		if err != nil {
			t.Fatal(err)
		}
		finishQueued(t, f.svc)
		if _, err := f.svc.PostToRoom(ctx, f.team.ID, f.bo.ID, id, "@Ana go", nil, 0, false); err != nil {
			t.Fatal(err)
		}
		if got := queuedOf(t, f.svc, f.ana.ID); got.GoalID != "" {
			t.Errorf("a mention from a %s turn (decision %v) = %+v; want no task", from.BoardRole, from.Decision, got)
		}
		finishQueued(t, f.svc)
	}
}

// AC13: a task turn that Claude Code starts works on the task and step of
// the turn that started the background task.
func TestATaskTurnCarriesTheWorkOfTheTurnThatStartedTheBackgroundTask(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	it := f.create(t, f.lead, item("Parse", f.ana.ID, ""))
	work := queuedOf(t, f.svc, f.ana.ID)
	got, err := f.svc.StartTaskTurn(ctx, f.ana.ID, f.team.ID, "task", 0, work.ID)
	if err != nil || got.GoalID != f.goal.ID || got.ItemID != it.ID {
		t.Fatalf("task turn = %+v, %v; want the task and Parse", got, err)
	}
	if none, err := f.svc.StartTaskTurn(ctx, f.ana.ID, f.team.ID, "task", 0, ""); err != nil || none.GoalID != "" || none.ItemID != "" {
		t.Fatalf("task turn of no turn = %+v, %v; want no task", none, err)
	}
	// The step stays with its owner: a bot that neither owns nor reviews it
	// gets the task only.
	if other, err := f.svc.StartTaskTurn(ctx, f.cy.ID, f.team.ID, "task", 0, work.ID); err != nil || other.GoalID != f.goal.ID || other.ItemID != "" {
		t.Fatalf("Cy's task turn = %+v, %v; want the task only", other, err)
	}
}

// A member the user @mentions in a team works on the Now task; the lead
// woken by a post without mentions, and a plain room, carry none.
func TestAUserMentionInATeamCarriesTheNowTask(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	if _, _, err := f.svc.SubmitUserMessage(ctx, f.team.ID, "@Ana how is it going?", "n1"); err != nil {
		t.Fatal(err)
	}
	if got := queuedOf(t, f.svc, f.ana.ID); got.GoalID != f.goal.ID || got.ItemID != "" {
		t.Errorf("Ana's turn = %+v; want the Now task", got)
	}
	finishQueued(t, f.svc)
	if _, _, err := f.svc.SubmitUserMessage(ctx, f.team.ID, "How is it going?", "n2"); err != nil {
		t.Fatal(err)
	}
	if got := queuedOf(t, f.svc, f.bo.ID); got.GoalID != "" {
		t.Errorf("the lead's turn for a post without mentions = %+v; want no task", got)
	}
	finishQueued(t, f.svc)
	room, err := f.svc.CreateRoom(ctx, "Plain", "", []string{f.ana.ID, f.cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.SubmitUserMessage(ctx, room.ID, "@Ana hi", "n3"); err != nil {
		t.Fatal(err)
	}
	if got := queuedOf(t, f.svc, f.ana.ID); got.GoalID != "" {
		t.Errorf("a plain room's turn = %+v; want no task", got)
	}
}

// A conversation turn that inherits a task neither uses its budget nor is
// cancelled when the task or its step changes; the board's own turns are.
func TestInheritedTurnsStayOffTheTasksBudgetAndCancels(t *testing.T) {
	f := newBoardFixture(t, 30)
	ctx := context.Background()
	it := f.create(t, f.lead, item("Parse", f.ana.ID, f.cy.ID))
	work := queuedOf(t, f.svc, f.ana.ID)
	finishQueued(t, f.svc)
	if _, err := f.svc.PostToRoom(ctx, f.team.ID, f.ana.ID, work.ID, "@Cy look", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	chat := queuedOf(t, f.svc, f.cy.ID)
	if _, err := f.svc.StartTurn(ctx, chat.ID); err != nil {
		t.Fatal(err)
	}
	if used, err := f.svc.q.CountGoalTurns(ctx, f.goal.ID); err != nil || used != 0 {
		t.Fatalf("goal turns used = %d, %v; want the inherited turn left out", used, err)
	}
	if n, err := f.svc.q.CountOpenGoalTurns(ctx, f.goal.ID); err != nil || n != 0 {
		t.Fatalf("open goal turns = %d, %v; want the inherited turn left out", n, err)
	}
	if n, err := f.svc.q.CountOpenItemTurns(ctx, db.CountOpenItemTurnsParams{ItemID: it.ID, BotID: f.cy.ID}); err != nil || n != 0 {
		t.Fatalf("open item turns = %d, %v; want the inherited turn left out", n, err)
	}
	finishQueued(t, f.svc)
	if _, err := f.svc.PostToRoom(ctx, f.team.ID, f.ana.ID, work.ID, "@Cy again", nil, 0, false); err != nil {
		t.Fatal(err)
	}
	waiting := queuedOf(t, f.svc, f.cy.ID)
	if _, err := f.svc.DropItem(ctx, f.lead, it.ID, "Not needed."); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.EndGoal(ctx, f.goal.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := queuedOf(t, f.svc, f.cy.ID); got.ID != waiting.ID {
		t.Fatalf("Cy's waiting turn after the step and the task ended = %+v; want it still queued", got)
	}
}
