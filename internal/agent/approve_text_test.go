package agent

import (
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

// The board says who approves each done task, as core's approver rule decides, and the lead is
// told to approve its team's own proposals, since no one else is asked to.
func TestTheBoardSaysWhoApprovesEachDoneTask(t *testing.T) {
	t.Parallel()
	dev := core.Bot{ID: "dev", Name: "Dev", Members: []core.Member{{ID: "dl", Name: "Dee"}, {ID: "mo", Name: "Mo"}}, Team: &core.Team{LeadID: "dl"}}
	done := func(id string, h core.Handoff) core.Goal {
		return core.Goal{ID: id, Text: "Task " + id + ".", TeamID: "dev", State: core.GoalDelivered, Handoff: h}
	}
	proposed := done("p", core.Handoff{Origin: core.OriginProposal})
	fromTeam := done("s", core.Handoff{Origin: core.OriginHandoff, FromTeamID: "sup", FromBotID: "sl"})
	fromUser := done("u", core.Handoff{Origin: core.OriginUser})
	// The user handed this task over from Support's board: no lead sent it, so the user approves it.
	forTeam := done("f", core.Handoff{Origin: core.OriginHandoff, FromTeamID: "sup"})
	teams := map[string]string{"sup": "Support"}
	board := core.Board{Done: []core.Goal{proposed, fromTeam, fromUser, forTeam}, Teams: teams}
	lead := boardPrompt(core.Bot{ID: "dl"}, dev, board, core.Turn{}, nil, time.Time{})
	for _, want := range []string{
		"Done, waiting for approval:\n",
		`- "Task p." (task id p), from a proposal of your team — the team's lead approves it` + "\n",
		`- "Task s." (task id s), from team "Support" — team "Support"'s lead approves it` + "\n",
		`- "Task u." (task id u), from the user — the user approves it` + "\n",
		`- "Task f." (task id f), from the user, for team "Support" — the user approves it` + "\n",
		approveBrief,
	} {
		if !strings.Contains(lead, want) {
			t.Errorf("the lead's board lacks %q:\n%s", want, lead)
		}
	}
	if !strings.Contains(approveBrief, "update_board approve with its goal_id") || !strings.Contains(approveBrief, "propose or make_task") {
		t.Errorf("approve brief = %q", approveBrief)
	}
	if member := boardPrompt(core.Bot{ID: "mo"}, dev, board, core.Turn{}, nil, time.Time{}); strings.Contains(member, approveBrief) {
		t.Error("a member is told to approve the team's proposals")
	}
	others := core.Board{Done: []core.Goal{fromTeam, fromUser, forTeam}, Teams: teams}
	if got := boardPrompt(core.Bot{ID: "dl"}, dev, others, core.Turn{}, nil, time.Time{}); strings.Contains(got, approveBrief) {
		t.Error("the lead is told to approve when no done task is its team's proposal")
	}
}

// The lead's brief and the update_board tool say who approves each kind of done task.
func TestTheLeadsBriefAndTheBoardToolSayWhoApproves(t *testing.T) {
	t.Parallel()
	for _, want := range []string{
		"the user approves a task from the user, and the sending team's lead one from another team",
		"A task your team proposed, with propose or make_task, you approve yourself.",
	} {
		if !strings.Contains(leadBrief, want) {
			t.Errorf("lead brief lacks %q", want)
		}
	}
	if want := "the user for the user's tasks, the sending team's lead for a task from another team, and the team's own lead for a task the team proposed (propose or make_task)"; !strings.Contains(updateBoardDescription, want) {
		t.Errorf("update_board description lacks %q", want)
	}
	for _, old := range []string{"Whoever asked approves it or asks for changes;", "whoever asked approves it or asks for changes, and"} {
		if strings.Contains(leadBrief, old) || strings.Contains(updateBoardDescription, old) {
			t.Errorf("old text %q is left", old)
		}
	}
}

// A task the lead sent and the other team closed shows on the sender's board with how to approve
// it; one the user handed over doesn't, and a task still being done doesn't either.
func TestTheSendersBoardSaysToApproveAClosedTask(t *testing.T) {
	t.Parallel()
	dev := core.Bot{ID: "dev", Name: "Dev", Members: []core.Member{{ID: "dl", Name: "Dee"}}, Team: &core.Team{LeadID: "dl"}}
	sent := func(id, state, fromBot string) core.Goal {
		g := core.Goal{ID: id, Text: "Task " + id + ".", TeamID: "docs", State: state}
		g.Origin, g.FromTeamID, g.FromBotID = core.OriginHandoff, "dev", fromBot
		return g
	}
	board := core.Board{Sent: []core.Goal{sent("a", core.GoalDelivered, "dl"), sent("b", core.GoalDelivered, ""), sent("c", core.GoalActive, "dl")}, Teams: map[string]string{"docs": "Docs"}}
	got := boardPrompt(core.Bot{ID: "dl"}, dev, board, core.Turn{}, nil, time.Time{})
	approve := " — your team's lead approves it in a lead turn: update_board approve with its goal_id, or changes"
	for _, want := range []string{
		`- "Task a." (task id a) to team "Docs" [done, waiting for approval]` + approve + "\n",
		`- "Task b." (task id b) to team "Docs" [done, waiting for approval]` + "\n",
		`- "Task c." (task id c) to team "Docs" [in progress]` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the sender's board lacks %q:\n%s", want, got)
		}
	}
	// A member's work turn shows the same board: the line names the lead, not the reader.
	dev.Members = append(dev.Members, core.Member{ID: "mo", Name: "Mo"})
	board.Items = []core.Item{{ID: "i1", Title: "Docs", State: core.ItemDoing, OwnerID: "mo"}}
	member := boardPrompt(core.Bot{ID: "mo"}, dev, board, core.Turn{BoardRole: core.RoleWork, ItemID: "i1"}, nil, time.Time{})
	if !strings.Contains(member, `[done, waiting for approval]`+approve) || strings.Contains(member, "you approve it") {
		t.Errorf("a member's work turn tells the reader to approve, or lacks the lead's approval:\n%s", member)
	}
}
