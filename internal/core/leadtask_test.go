package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// leadFixture is bots Ana, Bo and Cy, team Ship led by Ana with Bo, and team Docs led by Cy with
// Ana.
func leadFixture(t *testing.T) (svc *Service, ana, bo, cy, ship, docs Bot) {
	t.Helper()
	ctx := context.Background()
	svc = newTestService(t)
	bot := func(name string) Bot {
		t.Helper()
		b, err := svc.CreateBot(ctx, name, "")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	ana, bo, cy = bot("Ana"), bot("Bo"), bot("Cy")
	var err error
	if ship, err = svc.CreateTeam(ctx, "Ship", "", []string{ana.ID, bo.ID}, TeamSettings{LeadID: ana.ID}); err != nil {
		t.Fatal(err)
	}
	if docs, err = svc.CreateTeam(ctx, "Docs", "", []string{cy.ID, ana.ID}, TeamSettings{LeadID: cy.ID}); err != nil {
		t.Fatal(err)
	}
	return svc, ana, bo, cy, ship, docs
}

func wantRefused(t *testing.T, what string, err error, want string) {
	t.Helper()
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), want) {
		t.Errorf("%s: %v; want it refused with %q", what, err, want)
	}
}

// A lead adds the task the user asked for in its own chat to its team's board, as the user's own
// task: in the inbox, or as the Now task with startNow.
func TestALeadAddsTheUsersTaskFromItsOwnChat(t *testing.T) {
	ctx := context.Background()
	svc, ana, _, _, ship, _ := leadFixture(t)
	chat := LeadChat{BotID: ana.ID, FromUser: true}
	g, team, err := svc.LeadUserTask(ctx, chat, ship.ID, NewTask{Text: "Add CSV export.", Brief: "Reports page."}, false)
	if err != nil || team.ID != ship.ID || g.TeamID != ship.ID || g.State != GoalIncoming || g.Origin != OriginUser || g.FromTeamID != "" || g.FromBotID != "" || g.Brief != "Reports page." {
		t.Fatalf("user_task = %+v, %+v, %v; want the user's task in Ship's inbox", g, team, err)
	}
	now, _, err := svc.LeadUserTask(ctx, chat, ship.ID, NewTask{Text: "Fix login."}, true)
	if err != nil || now.State != GoalActive || now.Origin != OriginUser {
		t.Fatalf("user_task with startNow = %+v, %v; want the user's task as Now", now, err)
	}
	board, err := svc.Board(ctx, ship.ID)
	if err != nil || board.Goal == nil || board.Goal.ID != now.ID || len(board.Inbox) != 1 || board.Inbox[0].ID != g.ID {
		t.Fatalf("Ship's board = %+v, %v; want the started task Now and the other in the inbox", board, err)
	}
}

// A lead of one team may leave team_id out; a lead of several must name one it leads.
func TestALeadNamesTheTeamWhenItLeadsSeveral(t *testing.T) {
	ctx := context.Background()
	svc, ana, _, cy, ship, docs := leadFixture(t)
	if team, err := svc.LedTeam(ctx, LeadChat{BotID: ana.ID, FromUser: true}, ""); err != nil || team.ID != ship.ID {
		t.Fatalf("Ana's team without team_id = %+v, %v; want Ship, the one she leads", team, err)
	}
	_, err := svc.LedTeam(ctx, LeadChat{BotID: ana.ID, FromUser: true}, docs.ID)
	wantRefused(t, "Ana and Docs, which Cy leads", err, "only Cy, the lead of Docs, adds tasks to its board")
	ops, err := svc.CreateTeam(ctx, "Ops", "", []string{ana.ID}, TeamSettings{LeadID: ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.LedTeam(ctx, LeadChat{BotID: ana.ID, FromUser: true}, "")
	wantRefused(t, "Ana, the lead of two teams, without team_id", err, "you lead several teams; name one with team_id")
	if team, err := svc.LedTeam(ctx, LeadChat{BotID: ana.ID, FromUser: true}, ops.ID); err != nil || team.ID != ops.ID {
		t.Fatalf("Ana with team_id Ops = %+v, %v; want Ops", team, err)
	}
	_, err = svc.LedTeam(ctx, LeadChat{BotID: cy.ID, FromUser: true}, ship.ID)
	wantRefused(t, "Cy and Ship, which Cy isn't in", err, "you are in no team with id "+ship.ID)
}

// A member who leads no team is refused with the name of its lead; a bot in no team gets the
// refusal of every board action outside a team; a turn the user's message didn't start is refused.
func TestOnlyALeadInTheUsersOwnTurnAddsATask(t *testing.T) {
	ctx := context.Background()
	svc, ana, bo, _, ship, _ := leadFixture(t)
	_, _, err := svc.LeadUserTask(ctx, LeadChat{BotID: bo.ID, FromUser: true}, "", NewTask{Text: "X."}, false)
	wantRefused(t, "Bo, a member", err, "you lead no team; Ana leads Ship and adds its tasks")
	_, _, err = svc.LeadUserTask(ctx, LeadChat{BotID: bo.ID, FromUser: true}, ship.ID, NewTask{Text: "X."}, false)
	wantRefused(t, "Bo, a member, with team_id", err, "only Ana, the lead of Ship, adds tasks to its board")
	loner, err := svc.CreateBot(ctx, "Lone", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.LeadUserTask(ctx, LeadChat{BotID: loner.ID, FromUser: true}, "", NewTask{Text: "X."}, false)
	wantRefused(t, "a bot in no team", err, "update_board works only in a turn in your team")
	_, _, err = svc.LeadUserTask(ctx, LeadChat{BotID: ana.ID}, "", NewTask{Text: "X."}, false)
	wantRefused(t, "Ana in a turn the user didn't start", err, "only when the user's own message to you started the turn")
	if board, _ := svc.Board(ctx, ship.ID); len(board.Inbox) != 0 || board.Goal != nil {
		t.Fatalf("Ship's board after the refusals = %+v; want no task", board)
	}
}
