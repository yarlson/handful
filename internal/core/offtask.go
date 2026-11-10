package core

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"yargpt/internal/db"
)

// OffTaskWindow is how far back a team's board shows the turns that were on
// no task.
const OffTaskWindow = time.Hour

// maxMadeFromTurns caps the turns that one Make a task attaches.
const maxMadeFromTurns = 50

// maxFirstLine is the length, in characters, of a turn's first line.
const maxFirstLine = 140

// OffTaskTurn is a team's turn that was on no task: who ran it, when, and
// what it was about.
type OffTaskTurn struct {
	TurnID    string `json:"turnId"`
	BotID     string `json:"botId"`
	BotName   string `json:"botName"`
	StartedAt int64  `json:"startedAt"` // ms
	// FirstLine is the first line of the bot's first post in the turn, or of
	// the turn's prompt when the bot posted nothing.
	FirstLine string `json:"firstLine"`
}

// OffTask is the work of a team that was on no task in the last
// OffTaskWindow, oldest first.
type OffTask struct {
	Count int64         `json:"count"`
	Turns []OffTaskTurn `json:"turns"`
}

// OffTaskTurns lists the turns in team teamID that started at or after since
// and are on no task. Lead decisions, retros and the lead's turns that a
// user post woke are left out: they are board work, or the lead's talk with
// the user.
func (s *Service) OffTaskTurns(ctx context.Context, teamID string, since time.Time) ([]OffTaskTurn, error) {
	team, err := s.team(ctx, teamID)
	if err != nil {
		return nil, err
	}
	lead, _ := team.Lead()
	rows, err := s.q.ListOffTaskTurns(ctx, db.ListOffTaskTurnsParams{TeamID: teamID, Since: since.UnixMilli(), LeadID: lead.ID})
	if err != nil {
		return nil, fmt.Errorf("list the turns of %s on no task: %w", teamID, err)
	}
	out := make([]OffTaskTurn, 0, len(rows))
	for _, r := range rows {
		line := firstLine(r.FirstPost)
		if line == "" {
			line = firstLine(r.Prompt)
		}
		out = append(out, OffTaskTurn{TurnID: r.ID, BotID: r.BotID, BotName: r.Name, StartedAt: r.StartedAt, FirstLine: line})
	}
	return out, nil
}

// RecentOffTask is a team's work on no task in the last OffTaskWindow.
func (s *Service) RecentOffTask(ctx context.Context, teamID string) (OffTask, error) {
	turns, err := s.OffTaskTurns(ctx, teamID, s.now().Add(-OffTaskWindow))
	if err != nil {
		return OffTask{}, err
	}
	return OffTask{Count: int64(len(turns)), Turns: turns}, nil
}

// boardOffTask is RecentOffTask for a board: a bot that is no team has
// none. Board serves any bot, for example when the user stops a bot.
func (s *Service) boardOffTask(ctx context.Context, id string) (OffTask, error) {
	bot, err := s.GetBot(ctx, id)
	if err != nil {
		return OffTask{}, err
	}
	if !bot.IsTeam() {
		return OffTask{Turns: []OffTaskTurn{}}, nil
	}
	return s.RecentOffTask(ctx, id)
}

// firstLine is the first line of text that is not blank, at most
// maxFirstLine characters.
func firstLine(text string) string {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > maxFirstLine {
			line = string([]rune(line)[:maxFirstLine-1]) + "…"
		}
		return line
	}
	return ""
}

// MakeTask makes a task of the given turns of team teamID that were on no
// task: the task is taken at once, into the end of the plan, or started when
// nothing is Now, and the turns become its history. a is the team's lead in
// a turn in the team, or the user. The turns stay conversation turns, so
// they don't count against the task's budget.
func (s *Service) MakeTask(ctx context.Context, a Actor, teamID string, t NewTask, turnIDs []string) (Goal, error) {
	team, err := s.team(ctx, teamID)
	if err != nil {
		return Goal{}, err
	}
	origin, from, err := maker(a, team)
	if err != nil {
		return Goal{}, err
	}
	turnIDs = slices.Compact(slices.Sorted(slices.Values(turnIDs)))
	if len(turnIDs) == 0 || len(turnIDs) > maxMadeFromTurns {
		return Goal{}, invalid("make a task from 1 to %d turns that were on no task", maxMadeFromTurns)
	}
	if t.WaitsOn != "" {
		return Goal{}, invalid("a task made from turns waits on no step")
	}
	if t, err = s.validTask(ctx, t); err != nil {
		return Goal{}, err
	}
	var g Goal
	var started bool
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if g, err = s.insertTask(ctx, q, team, taskRow{origin: origin, from: from, task: t}); err != nil {
			return err
		}
		if err := attachTurns(ctx, q, team.ID, g.ID, turnIDs); err != nil {
			return err
		}
		if g, started, err = s.take(ctx, q, a, team, g, 0); err != nil {
			return err
		}
		return s.taskEvent(ctx, q, a, g, TaskTaken, fmt.Sprintf("made from %d turns that were on no task; %s", len(turnIDs), takenText(g, started)))
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.boardNote(ctx, team.ID, fmt.Sprintf("%s made the task %q from %d turns that were on no task; %s.", actorName(team, a), g.Text, len(turnIDs), takenText(g, started)))
}

// maker checks that a may make a task for team, and returns the task's
// origin and sender: the user's own task, or the team's proposal by its lead.
func maker(a Actor, team Bot) (string, sender, error) {
	if a.Role == RoleUser {
		return OriginUser, sender{}, nil
	}
	if a.leadActs(team) {
		return OriginProposal, sender{botID: a.BotID}, nil
	}
	return "", sender{}, invalid("only the lead of %s, or the user, makes a task from turns", team.Name)
}

// attachTurns makes the turns of team teamID with ids the history of task
// goalID with q. Each must be the team's turn on no task.
func attachTurns(ctx context.Context, q *db.Queries, teamID, goalID string, ids []string) error {
	for _, id := range ids {
		n, err := q.AttachOffTaskTurn(ctx, db.AttachOffTaskTurnParams{GoalID: goalID, ID: id, TeamID: teamID})
		if err != nil {
			return fmt.Errorf("attach turn %s to task %s: %w", id, goalID, err)
		}
		if n == 0 {
			return invalid("turn %s is not a turn of this team on no task", id)
		}
	}
	return nil
}
