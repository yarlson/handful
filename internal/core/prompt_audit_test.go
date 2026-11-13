package core

import (
	"context"
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
