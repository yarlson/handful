package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"handful/internal/core"
)

// listMembersDescription is the list_members tool's description.
const listMembersDescription = "List the members of a team you are in, each with its role (lead, reviewer or member), the model and effort of its turns in the team, its steps on the team's Now task and its full description. In a team turn it lists that team; from your own chat, give team_id when you are in several teams."

type membersInput struct {
	TeamID string `json:"team_id,omitempty" jsonschema:"the team's id; needed from your own chat when you are in several teams"`
}

// listMembers lists the team of a team turn, or from the bot's own chat a team the bot is in.
// It only reads, so any turn of the own chat may call it.
func (t tools) listMembers(ctx context.Context, rc runContext, in membersInput) (*mcp.CallToolResult, any, error) {
	team, err := t.membersTeam(ctx, rc, in.TeamID)
	if err != nil {
		return nil, nil, err
	}
	board, err := t.svc.Board(ctx, team.ID)
	if err != nil {
		return nil, nil, err
	}
	bots, err := t.svc.ListBots(ctx)
	if err != nil {
		return nil, nil, err
	}
	instructions := map[string]string{}
	for _, b := range bots {
		instructions[b.ID] = b.Instructions
	}
	return textResult("%s", membersList(team, board, instructions, t.svc.Defaults())), nil, nil
}

// membersTeam returns the team that list_members lists in run rc.
func (t tools) membersTeam(ctx context.Context, rc runContext, teamID string) (core.Bot, error) {
	if rc.RoomID == "" {
		return t.svc.MemberTeam(ctx, rc.BotID, teamID)
	}
	turnTeam, err := t.svc.TurnTeam(ctx, rc.RoomID)
	if err != nil {
		return core.Bot{}, err
	}
	if turnTeam == "" {
		return core.Bot{}, errors.New("list_members works in a team turn or in your own chat")
	}
	team, err := t.svc.GetBot(ctx, turnTeam)
	if err != nil {
		return core.Bot{}, err
	}
	if teamID != "" && teamID != turnTeam {
		return core.Bot{}, fmt.Errorf("in a turn in team %s, list_members lists only that team; list another team you are in from your own chat", team.Name)
	}
	return team, nil
}

// membersList shows a team's reviewer, then each member in the team's order with its role, the
// model and effort of its turns in the team, its open steps on the Now task and its full
// description. instructions maps bot ids to their instructions.
func membersList(team core.Bot, board core.Board, instructions map[string]string, d core.Defaults) string {
	lead, _ := team.Lead()
	reviewer := "none"
	var b strings.Builder
	for _, m := range team.Members {
		if m.ID == team.Team.ReviewerID {
			reviewer = m.Name
		}
	}
	fmt.Fprintf(&b, "Team %q (id %s). Reviewer: %s.\n", team.Name, team.ID, reviewer)
	for _, m := range team.Members {
		role := "member"
		switch m.ID {
		case lead.ID:
			role = "lead"
		case team.Team.ReviewerID:
			role = "reviewer"
		}
		model, effort := memberModel(m, team.Team, d)
		fmt.Fprintf(&b, "- %s (id %s): %s · %s · %s. Now: %s.\n", m.Name, m.ID, role, model, effort, nowSteps(board, m.ID))
		description := firstNonEmpty(strings.TrimSpace(instructions[m.ID]), "no instructions yet")
		b.WriteString("  " + strings.ReplaceAll(description, "\n", "\n  ") + "\n")
	}
	return b.String()
}

// nowSteps lists the steps of the team's Now task that member owns or reviews and that are not
// done or dropped, with their state; "no step" when there is none.
func nowSteps(board core.Board, member string) string {
	if board.Goal == nil || !board.Goal.Now() {
		return "no step"
	}
	var steps []string
	for _, it := range board.Items {
		if it.State == core.ItemDone || it.State == core.ItemDropped {
			continue
		}
		switch member {
		case it.OwnerID:
			steps = append(steps, fmt.Sprintf("owns %q (%s)", it.Title, it.State))
		case it.ReviewerID:
			steps = append(steps, fmt.Sprintf("reviews %q (%s)", it.Title, it.State))
		}
	}
	if len(steps) == 0 {
		return "no step"
	}
	return strings.Join(steps, "; ")
}
