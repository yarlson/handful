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

// nowTask is the team's Now task: its active goal; "" when it has none.
func (s *Service) nowTask(ctx context.Context, room Bot) (string, error) {
	if !room.IsTeam() {
		return "", nil
	}
	g, ok, err := s.latestGoal(ctx, room.ID)
	if err != nil || !ok || g.State != GoalActive {
		return "", err
	}
	return g.ID, nil
}
