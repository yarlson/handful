package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"yargpt/internal/db"
)

// workOn is the task and step a turn works on; "" for none.
type workOn struct {
	goalID string
	itemID string
}

// inheritedWork is the task, and the step, that a turn of bot started by
// turnID inherits (spec 9cf81412 §5): the task of the turn that woke it, and
// its step when bot owns or reviews that step. A lead's decision on an inbox
// task, a retro and a turn on no task pass nothing on.
func inheritedWork(ctx context.Context, q *db.Queries, turnID, botID string) (workOn, error) {
	if turnID == "" {
		return workOn{}, nil
	}
	row, err := q.GetTurnTask(ctx, turnID)
	if errors.Is(err, sql.ErrNoRows) {
		return workOn{}, nil
	}
	if err != nil {
		return workOn{}, fmt.Errorf("read the task of turn %s: %w", turnID, err)
	}
	if row.GoalID == "" || row.Decision == 1 || row.BoardRole == RoleRetro {
		return workOn{}, nil
	}
	w := workOn{goalID: row.GoalID}
	if row.ItemID == "" {
		return w, nil
	}
	it, err := q.GetItem(ctx, row.ItemID)
	if err != nil {
		return workOn{}, fmt.Errorf("read the step of turn %s: %w", turnID, err)
	}
	if it.OwnerID == botID || it.ReviewerID == botID {
		w.itemID = it.ID
	}
	return w, nil
}

// nowTask is the team's Now task, its active goal, and the members who own or
// review one of its steps; "" and none when the team has no active goal.
func (s *Service) nowTask(ctx context.Context, room Bot) (string, map[string]bool, error) {
	if !room.IsTeam() {
		return "", nil, nil
	}
	g, ok, err := s.latestGoal(ctx, room.ID)
	if err != nil || !ok || g.State != GoalActive {
		return "", nil, err
	}
	items, err := s.q.ListGoalItems(ctx, g.ID)
	if err != nil {
		return "", nil, fmt.Errorf("list the steps of task %s: %w", g.ID, err)
	}
	onSteps := map[string]bool{}
	for _, it := range items {
		onSteps[it.OwnerID], onSteps[it.ReviewerID] = true, true
	}
	return g.ID, onSteps, nil
}
