package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// MaxQueuedGoals is how many goals a team keeps in its queue.
const MaxQueuedGoals = 10

// ErrConflict is a change that does not fit the current state, for example
// a reorder of a queue that changed meanwhile.
var ErrConflict = errors.New("conflict")

func conflict(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrConflict, fmt.Sprintf(format, args...))
}

// openGoalWith returns the open goal of a team with q, if any.
func openGoalWith(ctx context.Context, q *db.Queries, teamID string) (db.Goal, bool, error) {
	row, err := q.GetLatestGoal(ctx, teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Goal{}, false, nil
	}
	if err != nil {
		return db.Goal{}, false, fmt.Errorf("get the goal of team %s: %w", teamID, err)
	}
	return row, toGoal(row, 0).Open(), nil
}

func queuedGoals(ctx context.Context, q *db.Queries, teamID string) ([]Goal, error) {
	rows, err := q.ListQueuedGoals(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list the queued goals of team %s: %w", teamID, err)
	}
	goals := make([]Goal, 0, len(rows))
	for _, r := range rows {
		goals = append(goals, toGoal(r, 0))
	}
	return goals, nil
}

// placeGoal inserts a new goal of team with q: active when the team has no
// open goal and no queue, else queued at the end of the queue.
func (s *Service) placeGoal(ctx context.Context, q *db.Queries, team Bot, text string) (Goal, error) {
	_, open, err := openGoalWith(ctx, q, team.ID)
	if err != nil {
		return Goal{}, err
	}
	queue, err := queuedGoals(ctx, q, team.ID)
	if err != nil {
		return Goal{}, err
	}
	p := db.InsertGoalParams{ID: uuid.NewString(), TeamID: team.ID, Text: text, State: GoalActive, Budget: team.Team.GoalBudget, CreatedAt: s.nowMs()}
	if open || len(queue) > 0 {
		if len(queue) >= MaxQueuedGoals {
			return Goal{}, invalid("%s has %d goals queued; cancel one first", team.Name, MaxQueuedGoals)
		}
		p.State, p.Budget, p.QueuePos = GoalQueued, 0, int64(len(queue)+1)
	}
	row, err := q.InsertGoal(ctx, p)
	if err != nil {
		return Goal{}, fmt.Errorf("start a goal for team %s: %w", team.ID, err)
	}
	return toGoal(row, 0), nil
}

// renumber gives the queued goals positions 1 to n in their order.
func renumber(ctx context.Context, q *db.Queries, queue []Goal) error {
	for i, g := range queue {
		if err := q.SetQueuePos(ctx, db.SetQueuePosParams{QueuePos: int64(i + 1), ID: g.ID}); err != nil {
			return fmt.Errorf("move goal %s in the queue: %w", g.ID, err)
		}
	}
	return nil
}

// nextGoal makes the head of a team's queue its active goal with q, when the
// team has no open goal, and wakes the lead to plan it. It returns false when
// no goal started.
// Unless force, a handoff at the head waits for the user in a team that
// starts handoffs only when asked.
func (s *Service) nextGoal(ctx context.Context, q *db.Queries, teamID string, force bool) (Goal, bool, error) {
	_, open, err := openGoalWith(ctx, q, teamID)
	if err != nil || open {
		return Goal{}, false, err
	}
	queue, err := queuedGoals(ctx, q, teamID)
	if err != nil || len(queue) == 0 {
		return Goal{}, false, err
	}
	team, err := teamWith(ctx, q, teamID)
	if err != nil {
		return Goal{}, false, err
	}
	g := queue[0]
	if !force && holds(team, g) {
		return Goal{}, false, nil
	}
	if _, err := q.ActivateGoal(ctx, db.ActivateGoalParams{Budget: team.Team.GoalBudget, ID: g.ID}); err != nil {
		return Goal{}, false, fmt.Errorf("start goal %s: %w", g.ID, err)
	}
	g.State, g.Budget, g.QueuePos = GoalActive, team.Team.GoalBudget, 0
	if err := renumber(ctx, q, queue[1:]); err != nil {
		return Goal{}, false, err
	}
	if err := s.grantSlots(ctx, q, teamID); err != nil {
		return Goal{}, false, err
	}
	return g, true, s.wakeLead(ctx, q, team, g, "The next goal from the queue started: plan it.")
}

// nextGoalNote shows a goal that the queue started.
func (s *Service) nextGoalNote(ctx context.Context, g Goal, started bool) error {
	if !started {
		return nil
	}
	return s.boardNote(ctx, g.TeamID, "Next goal started: "+g.Text)
}

// ReorderGoalQueue puts the queued goals of a team in the given order. order
// must list exactly the goals that are queued now.
func (s *Service) ReorderGoalQueue(ctx context.Context, teamID string, order []string) error {
	team, err := s.team(ctx, teamID)
	if err != nil {
		return err
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		queue, err := queuedGoals(ctx, q, teamID)
		if err != nil {
			return err
		}
		ids := make([]string, len(queue))
		for i, g := range queue {
			ids[i] = g.ID
		}
		if len(order) != len(ids) || !sameIDs(order, ids) {
			return conflict("the queue of %s changed; list its goals again", team.Name)
		}
		byID := map[string]Goal{}
		for _, g := range queue {
			byID[g.ID] = g
		}
		sorted := make([]Goal, len(order))
		for i, id := range order {
			sorted[i] = byID[id]
		}
		return renumber(ctx, q, sorted)
	})
	if err != nil {
		return err
	}
	s.hub.Publish(Event{Type: "bots"})
	return s.boardNote(ctx, teamID, "The user reordered the queued goals.")
}

// sameIDs reports whether a and b hold the same ids, each once.
func sameIDs(a, b []string) bool {
	a, b = slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b))
	return slices.Equal(slices.Compact(a), b)
}

// StartNextGoal starts the head of a team's queue now, when the team has no
// open goal: the queue was on hold.
func (s *Service) StartNextGoal(ctx context.Context, teamID string) (Goal, error) {
	team, err := s.team(ctx, teamID)
	if err != nil {
		return Goal{}, err
	}
	var g Goal
	err = s.inTx(ctx, func(q *db.Queries) error {
		var started bool
		var err error
		if g, started, err = s.nextGoal(ctx, q, teamID, true); err != nil {
			return err
		}
		if !started {
			return conflict("%s works on a goal or has none queued", team.Name)
		}
		return nil
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.nextGoalNote(ctx, g, true)
}

// CancelGoal takes a queued goal out of the queue before it starts.
func (s *Service) CancelGoal(ctx context.Context, goalID string) error {
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return err
	}
	if g.State != GoalQueued {
		return invalid("only a queued goal is cancelled; end it instead")
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.setGoalState(ctx, q, g, GoalEnded); err != nil {
			return err
		}
		queue, err := queuedGoals(ctx, q, g.TeamID)
		if err != nil {
			return err
		}
		return renumber(ctx, q, queue)
	})
	if err != nil {
		return err
	}
	s.hub.Publish(Event{Type: "bots"})
	return s.boardNote(ctx, g.TeamID, "Cancelled before it started: "+g.Text)
}
