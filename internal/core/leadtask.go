package core

import (
	"context"
	"fmt"
	"strings"
)

// LeadChat is a turn of a bot in its own chat, as far as its team's board cares: BotID is the
// bot, and FromUser says the user's own message started the turn. A routine, a message from
// another bot, a first run or a task that Claude Code started is not from the user.
type LeadChat struct {
	BotID    string
	FromUser bool
}

// errNotInTeam is the refusal of a board action outside the turns of a team the bot is in.
const errNotInTeam = "update_board works only in a turn in your team"

// LedTeam returns the team whose board lead c may use from its own chat: teamID, or the one team
// the bot leads when teamID is "". Only the lead of a team, in a turn the user's own message
// started, may: the user asked for it in their own words.
func (s *Service) LedTeam(ctx context.Context, c LeadChat, teamID string) (Bot, error) {
	rooms, err := s.RoomsOf(ctx, c.BotID)
	if err != nil {
		return Bot{}, err
	}
	var teams, led []Bot
	for _, r := range rooms {
		if !r.IsTeam() {
			continue
		}
		teams = append(teams, r)
		if lead, _ := r.Lead(); lead.ID == c.BotID {
			led = append(led, r)
		}
	}
	if len(teams) == 0 {
		return Bot{}, invalid("%s", errNotInTeam)
	}
	if !c.FromUser {
		return Bot{}, invalid("outside a turn in your team, update_board works only in your own chat, in a turn that the user's own message to you started")
	}
	if teamID != "" {
		return ledOne(teams, teamID, c.BotID)
	}
	switch len(led) {
	case 0:
		return Bot{}, invalid("you lead no team; %s", leadsOf(teams))
	case 1:
		return led[0], nil
	}
	return Bot{}, invalid("you lead several teams; name one with team_id: %s", teamList(led))
}

// ledOne returns team teamID of teams when botID leads it.
func ledOne(teams []Bot, teamID, botID string) (Bot, error) {
	for _, t := range teams {
		if t.ID != teamID {
			continue
		}
		if lead, _ := t.Lead(); lead.ID != botID {
			return Bot{}, invalid("only %s, the lead of %s, uses its board from its own chat", lead.Name, t.Name)
		}
		return t, nil
	}
	return Bot{}, invalid("you are in no team with id %s", teamID)
}

// leadsOf names the lead of each team, for a member who asks for a task.
func leadsOf(teams []Bot) string {
	parts := make([]string, len(teams))
	for i, t := range teams {
		lead, _ := t.Lead()
		parts[i] = fmt.Sprintf("%s leads %s and adds its tasks", lead.Name, t.Name)
	}
	return strings.Join(parts, "; ")
}

func teamList(teams []Bot) string {
	parts := make([]string, len(teams))
	for i, t := range teams {
		parts[i] = fmt.Sprintf("%s (id %s)", t.Name, t.ID)
	}
	return strings.Join(parts, ", ")
}

// LeadUserTask adds task t, which the user asked lead c for in its own chat, to the board of
// teamID as the user's own task, as the user's UserTask does: to the inbox, or as the Now task
// (or first in the plan) with startNow.
func (s *Service) LeadUserTask(ctx context.Context, c LeadChat, teamID string, t NewTask, startNow bool) (Goal, Bot, error) {
	team, err := s.LedTeam(ctx, c, teamID)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	g, err := s.UserTask(ctx, team.ID, t, startNow)
	return g, team, err
}
