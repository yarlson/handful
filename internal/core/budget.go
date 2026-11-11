package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"handful/internal/db"
)

// A team's daily budget (spec 9cf81412 section 6).
const (
	maxDailyBudget = 10000
	// budgetWatchDays is how long a team's use is watched before Handful
	// suggests a budget for it.
	budgetWatchDays = 7
	// budgetWarnPercent is the share of the budget that warns the user.
	budgetWarnPercent = 80
	// minSuggestedBudget is the smallest budget Handful suggests.
	minSuggestedBudget = 100
)

// The kinds of a team's budget that need the user.
const (
	NeedBudgetWarning = "budget_warning" // the team used budgetWarnPercent of today's turns
	NeedBudgetReached = "budget_reached" // the team used all of today's turns and waits
)

// BudgetView is a team's use of its daily budget today. Limit 0 is no limit:
// nothing pauses. Suggested is 0 until the team was watched for
// budgetWatchDays, then 1.5 times its busiest day of the last week, at least
// minSuggestedBudget, rounded up to 50. WatchUntil is when that is (ms).
type BudgetView struct {
	Today      int64 `json:"today"`
	Limit      int64 `json:"limit"`
	Suggested  int64 `json:"suggested"`
	WatchUntil int64 `json:"watchUntil"`
	// Paused: the team used all of today's turns, so only its lead's
	// decisions start until midnight or a higher budget. Warn: it used
	// budgetWarnPercent of them.
	Paused bool `json:"paused"`
	Warn   bool `json:"warn"`
}

// dayStart is the start of the user's local day that t falls in.
func (s *Service) dayStart(t time.Time) time.Time {
	y, m, d := t.In(s.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, s.loc)
}

// dayKey names the user's local day of t, as budget_noted_on and
// budget_waited_on store it.
func (s *Service) dayKey(t time.Time) string {
	return t.In(s.loc).Format(time.DateOnly)
}

// teamTurnsOn counts the turns of team that started on the local day that
// starts at day.
func (s *Service) teamTurnsOn(ctx context.Context, teamID string, day time.Time) (int64, error) {
	n, err := s.q.CountTeamTurnsBetween(ctx, db.CountTeamTurnsBetweenParams{FromMs: day.UnixMilli(), ToMs: day.AddDate(0, 0, 1).UnixMilli(), TeamID: teamID})
	if err != nil {
		return 0, fmt.Errorf("count the turns of team %s: %w", teamID, err)
	}
	return n, nil
}

// TeamBudget is a team's use of its daily budget today; the zero view for a
// bot that is no team.
func (s *Service) TeamBudget(ctx context.Context, teamID string) (BudgetView, error) {
	row, err := s.q.GetTeam(ctx, teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return BudgetView{}, nil
	}
	if err != nil {
		return BudgetView{}, fmt.Errorf("get team %s: %w", teamID, err)
	}
	return s.budgetOf(ctx, row)
}

// budgetOf is the budget view of team row.
func (s *Service) budgetOf(ctx context.Context, row db.Team) (BudgetView, error) {
	now := s.now()
	today := s.dayStart(now)
	used, err := s.teamTurnsOn(ctx, row.TeamID, today)
	if err != nil {
		return BudgetView{}, err
	}
	v := BudgetView{Today: used, Limit: row.DailyBudget, WatchUntil: time.UnixMilli(row.BudgetSince).AddDate(0, 0, budgetWatchDays).UnixMilli()}
	if v.Limit > 0 {
		v.Paused = used >= v.Limit
		v.Warn = !v.Paused && used*100 >= v.Limit*budgetWarnPercent
	}
	if now.UnixMilli() < v.WatchUntil {
		return v, nil
	}
	busiest, err := s.busiestDay(ctx, row.TeamID, today)
	v.Suggested = suggestedBudget(busiest)
	return v, err
}

// busiestDay is the most turns team used on one of the budgetWatchDays local
// days before today.
func (s *Service) busiestDay(ctx context.Context, teamID string, today time.Time) (int64, error) {
	var busiest int64
	for back := 1; back <= budgetWatchDays; back++ {
		n, err := s.teamTurnsOn(ctx, teamID, today.AddDate(0, 0, -back))
		if err != nil {
			return 0, err
		}
		busiest = max(busiest, n)
	}
	return busiest, nil
}

// suggestedBudget is 1.5 times busiest, at least minSuggestedBudget, rounded
// up to a multiple of 50.
func suggestedBudget(busiest int64) int64 {
	half := (busiest*3 + 1) / 2 // 1.5 times, rounded up
	return max(minSuggestedBudget, (half+49)/50*50)
}

// TeamAllows reports whether turn t may start under its team's daily budget.
// A lead's decision always may; so may a turn of no team, and any turn of a
// team with no limit. It only reads, so the runner may call it holding its
// lock.
func (s *Service) TeamAllows(ctx context.Context, t Turn) (bool, error) {
	if t.Decision {
		return true, nil
	}
	row, ok, err := s.turnTeamRow(ctx, t)
	if err != nil || !ok || row.DailyBudget == 0 {
		return err == nil, err
	}
	used, err := s.teamTurnsOn(ctx, row.TeamID, s.dayStart(s.now()))
	return err == nil && used < row.DailyBudget, err
}

// turnTeamRow is the team whose budget turn t counts against: its room when
// that is a team, else the team of its task; false for none.
func (s *Service) turnTeamRow(ctx context.Context, t Turn) (db.Team, bool, error) {
	teamID := t.RoomID
	if teamID == "" && t.GoalID != "" {
		g, err := s.q.GetGoal(ctx, t.GoalID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return db.Team{}, false, fmt.Errorf("get the task of turn %s: %w", t.ID, err)
		}
		teamID = g.TeamID
	}
	if teamID == "" {
		return db.Team{}, false, nil
	}
	row, err := s.q.GetTeam(ctx, teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Team{}, false, nil
	}
	if err != nil {
		return db.Team{}, false, fmt.Errorf("get team %s: %w", teamID, err)
	}
	return row, true, nil
}

// noteBudgetPauses tells each team's channel, once a local day, that the
// team used all of today's turns, and the clients that it pauses.
func (s *Service) noteBudgetPauses(ctx context.Context) error {
	rows, err := s.q.ListTeams(ctx)
	if err != nil {
		return fmt.Errorf("list the teams: %w", err)
	}
	day := s.dayKey(s.now())
	for _, row := range rows {
		if row.DailyBudget == 0 {
			continue
		}
		if err := s.noteBudgetPause(ctx, row, day); err != nil {
			return err
		}
	}
	return nil
}

// noteBudgetPause posts the pause of team row on local day, once.
func (s *Service) noteBudgetPause(ctx context.Context, row db.Team, day string) error {
	used, err := s.teamTurnsOn(ctx, row.TeamID, s.dayStart(s.now()))
	if err != nil || used < row.DailyBudget {
		return err
	}
	n, err := s.q.NoteTeamBudgetPause(ctx, db.NoteTeamBudgetPauseParams{Day: day, TeamID: row.TeamID})
	if err != nil {
		return fmt.Errorf("note the budget pause of team %s: %w", row.TeamID, err)
	}
	if n == 0 {
		return nil
	}
	s.hub.Publish(Event{Type: "bots"})
	return s.boardNote(ctx, row.TeamID, fmt.Sprintf("The team used its %d turns for today. New turns wait until midnight or a higher budget; the lead's decisions still run.", row.DailyBudget))
}

// WaitForBudget waits out a team's budget pause until the local day ends:
// Needs you no longer lists it today.
func (s *Service) WaitForBudget(ctx context.Context, teamID string) error {
	if _, err := s.team(ctx, teamID); err != nil {
		return err
	}
	if _, err := s.q.WaitTeamBudget(ctx, db.WaitTeamBudgetParams{Day: s.dayKey(s.now()), TeamID: teamID}); err != nil {
		return fmt.Errorf("wait for the budget of team %s: %w", teamID, err)
	}
	s.hub.Publish(Event{Type: "bots"})
	return nil
}

// budgetsForTheUser lists the teams near or at their daily budget, except
// a pause that the user waits out today.
func (s *Service) budgetsForTheUser(ctx context.Context) ([]NeedsYou, error) {
	rows, err := s.q.ListTeams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the teams: %w", err)
	}
	out := []NeedsYou{}
	for _, row := range rows {
		n, ok, err := s.budgetForTheUser(ctx, row)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, n)
		}
	}
	return out, nil
}

// budgetForTheUser is the Needs you item of team row's budget, if it has one.
func (s *Service) budgetForTheUser(ctx context.Context, row db.Team) (NeedsYou, bool, error) {
	if row.DailyBudget == 0 {
		return NeedsYou{}, false, nil
	}
	v, err := s.budgetOf(ctx, row)
	if err != nil || (!v.Warn && !v.Paused) {
		return NeedsYou{}, false, err
	}
	if v.Paused && row.BudgetWaitedOn == s.dayKey(s.now()) {
		return NeedsYou{}, false, nil
	}
	team, err := s.team(ctx, row.TeamID)
	if err != nil {
		return NeedsYou{}, false, err
	}
	n := NeedsYou{
		Kind: NeedBudgetWarning, ChatID: row.TeamID, At: s.dayStart(s.now()).UnixMilli(),
		Text: fmt.Sprintf("%s used %d%% of today's turns (%d of %d)", team.Name, v.Today*100/v.Limit, v.Today, v.Limit),
	}
	if v.Paused {
		n.Kind, n.Text = NeedBudgetReached, fmt.Sprintf("Budget: %s used %d of %d turns today", team.Name, v.Today, v.Limit)
	}
	return n, true, nil
}
