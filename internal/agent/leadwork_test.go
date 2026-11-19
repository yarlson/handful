package agent

import (
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

// The lead texts of spec bd0b39af rev 1.2 §3, as the spec words them.
const (
	ownChatLeadLine = `- You lead team "Ship" (id t). Work for your team goes to its board: when the user asks you here for work that is more than a quick question, add it with update_board user_task (team_id when you lead several) before you read or run anything for it, so it is tracked as the user's task, and tell the user it is on the board; start_now only when the user asks for it. Answer a quick question yourself: one you can answer in a handful of tool calls. Going through many items, such as issues, files or logs, is work, even when one command lists them. Do the work here only when the user asks you to do it yourself. Plan board work in your turns in the team, where you give it to a member, or own it when no member fits. The Delegating section covers only work you keep. list_members shows your team's members with their model, effort and current work; update_board list shows the board.
`
	teamLeadLine = `- You are the lead, the team's front door: a post from the user without @mentions comes to you. You plan and assign; the members do the work. Answer a quick question yourself. Give other work to the member who fits it, with a self-contained request: the goal, the inputs and what to report. Use the fewest members that fit the task. The Delegating section covers only work you keep.
`
	splitLineNoReviewer = `- Split the task into steps with create. Give each step a self-contained brief (what to achieve, the inputs, the constraints and where to put the result) and acceptance criteria that a reviewer can check. Give it one owner (owner_id). Put each part of the task, such as a check against the code, in a step's brief, never only in a review. The team has no reviewer: make yourself the reviewer (reviewer_id) of each step. The working agreement can name other reviewers or no review; a step that also does other work the task asks for gets the reviewer that the other work needs.
`
	splitLineReviewerBo = `- Split the task into steps with create. Give each step a self-contained brief (what to achieve, the inputs, the constraints and where to put the result) and acceptance criteria that a reviewer can check. Give it one owner (owner_id). Put each part of the task, such as a check against the code, in a step's brief, never only in a review. The team's reviewer is Bo (id b): make it the reviewer (reviewer_id) of each step, and make yourself the reviewer of a step that Bo owns. The working agreement can name other reviewers or no review; a step that also does other work the task asks for gets the reviewer that the other work needs.
`
	ownerLines = `- Give each step to the member who fits it; when several fit, to the one on the cheaper model or lower effort. Own a step yourself only when no member fits.
- When the reviewer passes a step, don't check it again: close the task from the step results.
`
)

var leadTestNow = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)

func shipTeam() core.Bot {
	return core.Bot{ID: "t", Name: "Ship", Kind: core.KindRoom, Members: []core.Member{{ID: "a", Name: "Ana"}, {ID: "b", Name: "Bo"}}, Team: &core.Team{LeadID: "a"}}
}

// AC 1: only a lead's own chat tells it to put work for its team on the board.
func TestALeadsOwnChatSendsTeamWorkToTheBoard(t *testing.T) {
	t.Parallel()
	team := shipTeam()
	plain := core.Bot{ID: "r", Name: "Plan", Kind: core.KindRoom, Members: team.Members}
	prompts := map[string]struct {
		prompt string
		want   bool
	}{
		"the lead's own chat":   {systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Rooms: []core.Bot{team, plain}, Now: leadTestNow}), true},
		"a member's own chat":   {systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Bo"}, Rooms: []core.Bot{team, plain}, Now: leadTestNow}), false},
		"the lead's team turn":  {systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Team: &team, InRoom: true, Rooms: []core.Bot{team}, Now: leadTestNow}), false},
		"the lead's plain room": {systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, InRoom: true, Rooms: []core.Bot{team, plain}, Now: leadTestNow}), false},
	}
	for name, c := range prompts {
		if got := strings.Contains(c.prompt, ownChatLeadLine); got != c.want {
			t.Errorf("%s holds the own-chat lead line = %v; want %v", name, got, c.want)
		}
		if strings.Contains(c.prompt, "When the user asks you here to give work to your team") {
			t.Errorf("%s still holds the old own-chat lead line", name)
		}
	}
	other := core.Bot{ID: "u", Name: "Docs", Kind: core.KindRoom, Members: team.Members, Team: &core.Team{LeadID: "a"}}
	two := systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Rooms: []core.Bot{team, other}, Now: leadTestNow})
	if want := `- You lead team "Ship" (id t), "Docs" (id u). Work for your team goes to its board:`; !strings.Contains(two, want) {
		t.Errorf("the own chat of a lead of two teams lacks %q", want)
	}
}

// AC 3: the lead's team turn holds the plan-and-assign line; a member's holds neither lead line.
func TestALeadsTeamTurnSaysItPlansAndMembersDoTheWork(t *testing.T) {
	t.Parallel()
	team := shipTeam()
	const old = "Answer it yourself when you can"
	lead := systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Team: &team, InRoom: true, Rooms: []core.Bot{team}, Now: leadTestNow})
	member := systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Bo"}, Team: &team, InRoom: true, Rooms: []core.Bot{team}, Now: leadTestNow})
	ownChat := systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Rooms: []core.Bot{team}, Now: leadTestNow})
	if !strings.Contains(lead, teamLeadLine) || strings.Contains(lead, old) {
		t.Errorf("the lead's team prompt: want the plan-and-assign line and no %q", old)
	}
	for name, prompt := range map[string]string{"a member's team prompt": member, "the lead's own chat": ownChat} {
		if strings.Contains(prompt, teamLeadLine) || strings.Contains(prompt, old) {
			t.Errorf("%s holds a team lead line", name)
		}
	}
}

// AC 4: the lead's board turn on a Now task names who reviews each step and keeps the lead off the steps.
func TestALeadsBoardTurnNamesAReviewerAndKeepsTheLeadOffTheSteps(t *testing.T) {
	t.Parallel()
	team := shipTeam()
	board := core.Board{Goal: &core.Goal{ID: "g", Text: "Ship it.", TeamID: "t", State: core.GoalActive}, Items: []core.Item{
		{ID: "i1", Title: "Parse", State: core.ItemDoing, OwnerID: "b", ReviewerID: "a"},
	}}
	lead := boardPrompt(core.Bot{ID: "a", Name: "Ana"}, team, board, core.Turn{}, nil, leadTestNow)
	for _, want := range []string{splitLineNoReviewer, ownerLines} {
		if !strings.Contains(lead, want) {
			t.Errorf("the lead's board turn in a team with no reviewer lacks %q", want)
		}
	}
	for _, old := range []string{"You can own small steps yourself.", "when the working agreement asks for a check", "the team's reviewer, or you when the team has none"} {
		if strings.Contains(lead, old) {
			t.Errorf("the lead's board turn still holds %q", old)
		}
	}
	for name, turn := range map[string]core.Turn{"a member's board turn": {}, "a member's work turn": {BoardRole: core.RoleWork, ItemID: "i1"}} {
		if got := boardPrompt(core.Bot{ID: "b", Name: "Bo"}, team, board, turn, nil, leadTestNow); strings.Contains(got, "- Split the task into steps") || strings.Contains(got, ownerLines) {
			t.Errorf("%s holds the lead's step lines", name)
		}
	}
}

// AC 4: the lead's next board turn after a Reviewer change names the team's reviewer as it is then.
func TestALeadsNextBoardTurnFollowsTheTeamsReviewerSetting(t *testing.T) {
	t.Parallel()
	team := shipTeam()
	board := core.Board{Goal: &core.Goal{ID: "g", Text: "Ship it.", TeamID: "t", State: core.GoalActive}}
	ana := core.Bot{ID: "a", Name: "Ana"}
	for _, c := range []struct {
		reviewer, want, not string
	}{
		{"", splitLineNoReviewer, splitLineReviewerBo},
		{"b", splitLineReviewerBo, splitLineNoReviewer},
		{"", splitLineNoReviewer, splitLineReviewerBo},
	} {
		team.Team = &core.Team{LeadID: "a", ReviewerID: c.reviewer}
		got := boardPrompt(ana, team, board, core.Turn{}, nil, leadTestNow)
		if !strings.Contains(got, c.want) || strings.Contains(got, c.not) {
			t.Errorf("the lead's board turn with Reviewer %q = %q; want %q and not %q", c.reviewer, got, c.want, c.not)
		}
	}
}
