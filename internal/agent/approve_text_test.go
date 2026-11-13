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
	fromTeam := done("s", core.Handoff{Origin: core.OriginHandoff, FromTeamID: "sup"})
	fromUser := done("u", core.Handoff{Origin: core.OriginUser})
	teams := map[string]string{"sup": "Support"}
	board := core.Board{Done: []core.Goal{proposed, fromTeam, fromUser}, Teams: teams}
	lead := boardPrompt(core.Bot{ID: "dl"}, dev, board, core.Turn{}, nil, time.Time{})
	for _, want := range []string{
		"Done, waiting for approval:\n",
		`- "Task p." (task id p), from a proposal of your team — the team's lead approves it` + "\n",
		`- "Task s." (task id s), from team "Support" — team "Support"'s lead approves it` + "\n",
		`- "Task u." (task id u), from the user — the user approves it` + "\n",
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
	others := core.Board{Done: []core.Goal{fromTeam, fromUser}, Teams: teams}
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
