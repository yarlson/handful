package core

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"handful/internal/db"
)

// The kinds of what needs the user.
const (
	NeedApprove  = "approve"  // the user's own task is done and waits for approval
	NeedMissed   = "missed"   // a lead did not decide on a task in its inbox in time
	NeedStuck    = "stuck"    // a task was asked about or passed on too often
	NeedQuestion = "question" // a bot asked the user a question
)

// NeedsYou is one thing that waits for the user: a task to approve or to
// decide on, or an open question. ChatID is where it shows: the team, or
// the bot or room of a question.
type NeedsYou struct {
	Kind   string `json:"kind"`
	ChatID string `json:"chatId"`
	GoalID string `json:"goalId,omitempty"`
	// EntryID is the post that asks: a question, or a task's question to
	// the user, when there is one.
	EntryID string `json:"entryId,omitempty"`
	Text    string `json:"text"`
	// Lead is the lead who did not decide on a missed task; Reason says
	// why a stuck task waits for the user.
	Lead   string `json:"lead,omitempty"`
	Reason string `json:"reason,omitempty"`
	At     int64  `json:"at"` // ms
}

// NeedsYou lists what waits for the user, oldest first: the user's done
// tasks to approve, tasks whose leads did not decide in time or could not
// settle them, teams near or at their daily budget, and open questions. A
// question about a task that is listed shows as that task.
func (s *Service) NeedsYou(ctx context.Context) ([]NeedsYou, error) {
	out, err := s.tasksToApprove(ctx)
	if err != nil {
		return nil, err
	}
	stuck, err := s.tasksForTheUser(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, stuck...)
	budgets, err := s.budgetsForTheUser(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, budgets...)
	questions, err := s.questionsBesides(ctx, out)
	if err != nil {
		return nil, err
	}
	out = append(out, questions...)
	slices.SortStableFunc(out, func(a, b NeedsYou) int { return cmp.Compare(a.At, b.At) })
	return out, nil
}

// tasksToApprove lists the user's own tasks that are done.
func (s *Service) tasksToApprove(ctx context.Context) ([]NeedsYou, error) {
	rows, err := s.q.ListDeliveredUserGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the tasks to approve: %w", err)
	}
	out := []NeedsYou{}
	for _, row := range rows {
		at, err := s.decidedAt(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, NeedsYou{Kind: NeedApprove, ChatID: row.TeamID, GoalID: row.ID, Text: row.Text, At: at})
	}
	return out, nil
}

// tasksForTheUser lists the tasks in inboxes that only the user decides on
// now: missed by their lead, or asked about or passed on too often.
func (s *Service) tasksForTheUser(ctx context.Context) ([]NeedsYou, error) {
	rows, err := s.q.ListNeedsUserGoals(ctx, db.ListNeedsUserGoalsParams{MaxAskRounds: MaxAskRounds, MaxPasses: MaxPasses})
	if err != nil {
		return nil, fmt.Errorf("list the tasks left to the user: %w", err)
	}
	out := []NeedsYou{}
	for _, row := range rows {
		n, err := s.taskForTheUser(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// taskForTheUser says why task row waits for the user.
func (s *Service) taskForTheUser(ctx context.Context, row db.Goal) (NeedsYou, error) {
	n := NeedsYou{Kind: NeedStuck, ChatID: row.TeamID, GoalID: row.ID, Text: row.Text, Reason: fmt.Sprintf("passed on %d times", MaxPasses)}
	if askedOut(row.AskRounds) {
		n.Reason = fmt.Sprintf("asked %d times", MaxAskRounds)
	}
	if row.MissedAt > 0 {
		team, err := s.team(ctx, row.TeamID)
		if err != nil {
			return NeedsYou{}, err
		}
		lead, _ := team.Lead()
		n.Kind, n.Reason, n.Lead, n.At = NeedMissed, "", lead.Name, row.MissedAt
		return n, nil
	}
	at, err := s.decidedAt(ctx, row)
	n.At = at
	return n, err
}

// decidedAt is when the latest decision on task row was made, or when it
// was sent.
func (s *Service) decidedAt(ctx context.Context, row db.Goal) (int64, error) {
	at, err := s.q.LatestGoalEventAt(ctx, row.ID)
	if err != nil {
		return 0, fmt.Errorf("read the history of task %s: %w", row.ID, err)
	}
	return max(at, row.CreatedAt), nil
}

// questionsBesides lists the open questions, except those about the tasks
// that tasks lists; a task's own question becomes its EntryID.
func (s *Service) questionsBesides(ctx context.Context, tasks []NeedsYou) ([]NeedsYou, error) {
	listed := map[string]int{}
	for i, t := range tasks {
		listed[t.GoalID] = i
	}
	rows, err := s.q.ListAllOpenQuestionEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the open questions: %w", err)
	}
	var keep []db.Entry
	for _, row := range rows {
		b, err := parseQuestion(row)
		if err != nil {
			return nil, err
		}
		if i, ok := listed[b.GoalID]; ok && b.GoalID != "" {
			tasks[i].EntryID = row.ID
			continue
		}
		keep = append(keep, row)
	}
	questions, err := s.toQuestions(ctx, s.q, keep, "")
	if err != nil {
		return nil, err
	}
	out := make([]NeedsYou, 0, len(questions))
	for _, q := range questions {
		out = append(out, NeedsYou{Kind: NeedQuestion, ChatID: q.ChatID, EntryID: q.EntryID, Text: q.Question, At: q.CreatedAt})
	}
	return out, nil
}
