package core

import (
	"context"
	"fmt"
	"time"

	"yargpt/internal/db"
)

// A team's decision time, in minutes: how long a task may wait in its inbox
// for the lead's decision, counting only time when the lead could act.
const (
	DefaultDecisionMinutes = 30
	minDecisionMinutes     = 5
	maxDecisionMinutes     = 1440 // a day
)

// TaskMissed is the event of a task whose lead did not decide in time.
const TaskMissed = "missed"

// LeadState is what the runner knows about whether a lead could act now:
// the runner is paused for a usage limit, and which bots run a turn. A nil
// Busy means that no bot does.
type LeadState struct {
	Paused bool
	Busy   func(botID string) bool
}

func (l LeadState) busy(botID string) bool { return l.Busy != nil && l.Busy(botID) }

// runDecisionClocks counts, on one settle tick, the time that each task in
// an inbox waits for its lead's decision. Only time when the lead could act
// counts: the clock stands while the runner is paused, the server drains,
// the team is stopped, a question to the sender waits, or the lead's bot
// runs another turn. A task whose time runs out waits for the user.
func (s *Service) runDecisionClocks(ctx context.Context, state LeadState) error {
	rows, err := s.q.ListClockedGoals(ctx)
	if err != nil {
		return fmt.Errorf("list the tasks waiting for a decision: %w", err)
	}
	draining, _ := s.Draining()
	stopped := map[string]bool{}
	for _, row := range rows {
		team, err := s.team(ctx, row.TeamID)
		if err != nil {
			return err
		}
		lead, ok := team.Lead()
		if _, seen := stopped[team.ID]; !seen {
			if stopped[team.ID], err = s.teamStopped(ctx, team.ID); err != nil {
				return err
			}
		}
		canAct := ok && !state.Paused && !draining && !stopped[team.ID] && !state.busy(lead.ID)
		if err := s.tickDecision(ctx, team, lead, row, canAct); err != nil {
			return err
		}
	}
	return nil
}

// teamStopped reports whether the user stopped a team: its Now task is
// paused with turns of its budget left. A team paused at its budget is not
// stopped, because its lead's decisions still run.
func (s *Service) teamStopped(ctx context.Context, teamID string) (bool, error) {
	g, ok, err := s.latestGoal(ctx, teamID)
	if err != nil || !ok {
		return false, err
	}
	return g.State == GoalPaused && g.Used < g.Budget, nil
}

// tickDecision counts the time since the last tick against task row when its
// lead could act, and starts or stops its clock. When the decision time is
// up while the lead could act, the task goes to the user.
func (s *Service) tickDecision(ctx context.Context, team Bot, lead Member, row db.Goal, canAct bool) error {
	now := s.nowMs()
	spent, tick := row.DecisionMs, int64(0)
	if canAct {
		if row.DecisionTick > 0 {
			spent += now - row.DecisionTick
		}
		tick = now
	}
	limit := time.Duration(team.Team.DecisionMinutes) * time.Minute
	if canAct && spent >= limit.Milliseconds() {
		return s.missDecision(ctx, team, lead, toGoal(row, 0), spent, limit)
	}
	if _, err := s.q.SetDecisionClock(ctx, db.SetDecisionClockParams{DecisionMs: spent, DecisionTick: tick, ID: row.ID}); err != nil {
		return fmt.Errorf("count the decision time of task %s: %w", row.ID, err)
	}
	return nil
}

// missDecision leaves task g to the user: its lead did not decide on it
// within limit. The user takes it or pushes it back; no one wakes.
func (s *Service) missDecision(ctx context.Context, team Bot, lead Member, g Goal, spent int64, limit time.Duration) error {
	text := fmt.Sprintf("%s didn't decide on %q within %s. Take it or push it back?", lead.Name, g.Text, minutesText(limit))
	missed := false
	err := s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.MissDecision(ctx, db.MissDecisionParams{DecisionMs: spent, MissedAt: s.nowMs(), ID: g.ID})
		if err != nil {
			return fmt.Errorf("leave task %s to the user: %w", g.ID, err)
		}
		if missed = n == 1; !missed {
			return nil // the lead decided meanwhile
		}
		return s.taskEvent(ctx, q, Actor{}, g, TaskMissed, text)
	})
	if err != nil || !missed {
		return err
	}
	s.hub.Publish(Event{Type: "bots"})
	return s.askUser(ctx, Actor{BotID: lead.ID}, team, g.ID, text)
}

// minutesText says a duration of whole minutes, as "30 min" or "2 h 15 min".
func minutesText(d time.Duration) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h == 0 {
		return fmt.Sprintf("%d min", m)
	}
	if m == 0 {
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}
