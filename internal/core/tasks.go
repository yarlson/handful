package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// Kinds of task events: every decision on a task leaves one, with who, when
// and the text.
const (
	TaskSent       = "sent"
	TaskProposed   = "proposed"
	TaskTaken      = "taken"
	TaskOrdered    = "ordered"
	TaskPinned     = "pinned"
	TaskUnpinned   = "unpinned"
	TaskAsked      = "asked"
	TaskAnswered   = "answered"
	TaskLater      = "later"
	TaskDue        = "due"
	TaskPassed     = "passed"
	TaskPushedBack = "pushed_back"
	TaskClosed     = "closed"
	TaskApproved   = "approved"
	TaskChanges    = "changes"
	TaskStopped    = "stopped"
	TaskReopened   = "reopened"
	TaskWithdrawn  = "withdrawn"
)

// maxLaterDays is how many days a lead defers a task, at most.
const maxLaterDays = 7

// boolInt stores a bool as SQLite's 0 or 1.
func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// taskEvent stores, with q, the event that actor a's decision kind on task g
// leaves.
func (s *Service) taskEvent(ctx context.Context, q *db.Queries, a Actor, g Goal, kind, text string) error {
	name, err := eventActorName(ctx, q, a)
	if err != nil {
		return err
	}
	if err := q.InsertGoalEvent(ctx, db.InsertGoalEventParams{
		ID: uuid.NewString(), GoalID: g.ID, Kind: kind, ActorID: a.BotID, ActorName: name, Text: cut(text, maxItemText), CreatedAt: s.nowMs(),
	}); err != nil {
		return fmt.Errorf("record the %s event of task %s: %w", kind, g.ID, err)
	}
	return nil
}

// TaskEvents returns the history of a task, oldest first: the decisions on
// it, and the events of its steps.
func (s *Service) TaskEvents(ctx context.Context, goalID string) ([]ItemEvent, error) {
	if _, err := s.goal(ctx, s.q, goalID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListGoalEvents(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list the events of task %s: %w", goalID, err)
	}
	out := make([]ItemEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, ItemEvent{ID: r.ID, GoalID: r.GoalID, Kind: r.Kind, ActorID: r.ActorID, ActorName: r.ActorName, Text: r.Text, CreatedAt: r.CreatedAt})
	}
	items, err := goalItems(ctx, s.q, goalID)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		events, err := s.ItemEvents(ctx, it.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, events...)
	}
	slices.SortStableFunc(out, func(a, b ItemEvent) int { return int(a.CreatedAt - b.CreatedAt) })
	return out, nil
}

// wakeDecision queues a decision turn of team's lead on task g with q,
// unless one waits already: a lead turn that runs whatever the task's state
// and that no goal pays for.
func (s *Service) wakeDecision(ctx context.Context, q *db.Queries, team Bot, g Goal, reason string) error {
	lead, ok := team.Lead()
	if !ok {
		return nil
	}
	n, err := q.CountQueuedDecisionTurns(ctx, db.CountQueuedDecisionTurnsParams{BotID: lead.ID, RoomID: team.ID, GoalID: g.ID})
	if err != nil {
		return fmt.Errorf("look for a queued decision turn of the lead: %w", err)
	}
	if n > 0 {
		return nil
	}
	_, err = s.queueTurn(ctx, q, Turn{BotID: lead.ID, Source: SourceRoom, RoomID: team.ID, GoalID: g.ID, BoardRole: RoleLead, Decision: true, Prompt: reason})
	return err
}

// OrderPlan puts a team's planned tasks in the given order, which must list
// exactly the tasks that are planned now. a is the team's lead, in a turn in
// it, or the user. A lead cannot move a task that the user pinned.
func (s *Service) OrderPlan(ctx context.Context, a Actor, teamID string, order []string) error {
	team, err := s.team(ctx, teamID)
	if err != nil {
		return err
	}
	if a.Role != RoleUser && !a.leadActs(team) {
		return invalid("only the lead of %s orders its plan, in a turn in the team", team.Name)
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		queue, err := queuedGoals(ctx, q, teamID)
		if err != nil {
			return err
		}
		sorted, err := orderedPlan(a, team, queue, order)
		if err != nil {
			return err
		}
		if err := renumber(ctx, q, sorted); err != nil {
			return err
		}
		for i, g := range sorted {
			if g.QueuePos == int64(i+1) {
				continue
			}
			if err := s.taskEvent(ctx, q, a, g, TaskOrdered, fmt.Sprintf("#%d → #%d", g.QueuePos, i+1)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.hub.Publish(Event{Type: "bots"})
	return s.boardNote(ctx, teamID, actorName(team, a)+" put the plan in a new order.")
}

// orderedPlan returns queue in order, which must list exactly its tasks. A
// lead's order must keep every pinned task where it is.
func orderedPlan(a Actor, team Bot, queue []Goal, order []string) ([]Goal, error) {
	ids := make([]string, len(queue))
	byID := map[string]Goal{}
	for i, g := range queue {
		ids[i] = g.ID
		byID[g.ID] = g
	}
	if len(order) != len(ids) || !sameIDs(order, ids) {
		return nil, conflict("the plan of %s changed; list its tasks again", team.Name)
	}
	sorted := make([]Goal, len(order))
	for i, id := range order {
		g := byID[id]
		if g.Pinned && a.Role != RoleUser && g.QueuePos != int64(i+1) {
			return nil, conflict("%q is pinned by the user at #%d; it stays there", g.Text, g.QueuePos)
		}
		sorted[i] = g
	}
	return sorted, nil
}

// PinTask pins a planned task where it is, or unpins it: the user's own
// order, which the lead cannot change.
func (s *Service) PinTask(ctx context.Context, goalID string, pinned bool) (Goal, error) {
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return Goal{}, err
	}
	if g.State != GoalQueued {
		return Goal{}, invalid("%q is %s; only a planned task is pinned", g.Text, g.State)
	}
	kind := TaskUnpinned
	if pinned {
		kind = TaskPinned
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.SetGoalPinned(ctx, db.SetGoalPinnedParams{Pinned: boolInt(pinned), ID: g.ID})
		if err != nil {
			return fmt.Errorf("pin task %s: %w", g.ID, err)
		}
		if n == 0 {
			return invalid("the task changed meanwhile; look at the board again")
		}
		return s.taskEvent(ctx, q, Actor{Role: RoleUser}, g, kind, fmt.Sprintf("#%d", g.QueuePos))
	})
	if err != nil {
		return Goal{}, err
	}
	g.Pinned = pinned
	s.hub.Publish(Event{Type: "bots"})
	return g, nil
}

// LaterTask defers a task in the inbox until a time (ms) within maxLaterDays,
// with a note. It returns to the inbox when due, and its lead decides again.
func (s *Service) LaterTask(ctx context.Context, a Actor, goalID, note string, until int64) (Goal, error) {
	g, team, err := s.incoming(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	if note, err = textOf("the note", note, maxItemNote, true); err != nil {
		return Goal{}, err
	}
	now := s.now()
	if until <= now.UnixMilli() || until > now.AddDate(0, 0, maxLaterDays).UnixMilli() {
		return Goal{}, invalid("Pick a time in the future, at most 7 days from now.")
	}
	when := time.UnixMilli(until).In(s.loc).Format("Mon Jan 2 15:04")
	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.SetGoalLater(ctx, db.SetGoalLaterParams{LaterUntil: until, IntakeNote: note, ID: g.ID})
		if err != nil {
			return fmt.Errorf("defer task %s: %w", g.ID, err)
		}
		if n == 0 {
			return invalid("the task changed meanwhile; look at the board again")
		}
		return s.taskEvent(ctx, q, a, g, TaskLater, fmt.Sprintf("until %s: %s", when, note))
	})
	if err != nil {
		return Goal{}, err
	}
	g.State, g.LaterUntil, g.IntakeNote, g.AskedAt = GoalLater, until, note, 0
	s.hub.Publish(Event{Type: "bots"})
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, err
	}
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("%s deferred the task %q until %s: %s", actorName(team, a), g.Text, when, note)},
		{from.ID, fmt.Sprintf("Team %s deferred your task %q until %s: %s", team.Name, g.Text, when, note)},
	})
}

// dueLater returns the deferred tasks that came due to their inbox, and
// wakes their leads to decide again.
func (s *Service) dueLater(ctx context.Context) error {
	rows, err := s.q.ListDueLaterGoals(ctx, s.nowMs())
	if err != nil {
		return fmt.Errorf("list the deferred tasks that are due: %w", err)
	}
	for _, row := range rows {
		if err := s.returnDue(ctx, toGoal(row, 0)); err != nil {
			return err
		}
	}
	return nil
}

// returnDue returns deferred task g to its team's inbox.
func (s *Service) returnDue(ctx context.Context, g Goal) error {
	team, err := s.team(ctx, g.TeamID)
	if err != nil {
		return err
	}
	returned := false
	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.ReturnToInbox(ctx, db.ReturnToInboxParams{ID: g.ID, FromState: GoalLater})
		if err != nil || n == 0 {
			return err
		}
		returned = true
		if err := s.taskEvent(ctx, q, Actor{}, g, TaskDue, g.IntakeNote); err != nil {
			return err
		}
		return s.wakeDecision(ctx, q, team, g, fmt.Sprintf("The task %q that you deferred is due: decide on it again. Your note was: %s", g.Text, g.IntakeNote))
	})
	if err != nil || !returned {
		return err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return s.boardNote(ctx, team.ID, fmt.Sprintf("The deferred task %q is back in the inbox.", g.Text))
}

// PassOn passes a task in the inbox on to the team it belongs to, with a
// note, at most MaxPasses times. Then the user settles it: the next pass
// fails and asks the user in the team's channel. The sender sees who passed
// it where.
func (s *Service) PassOn(ctx context.Context, a Actor, goalID, toTeamID, note string) (Goal, error) {
	g, team, err := s.incoming(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	to, note, err := s.passTarget(ctx, team, g, toTeamID, note)
	if err != nil {
		return Goal{}, err
	}
	if g.Passes >= MaxPasses {
		return Goal{}, s.passedTooOften(ctx, a, team, g)
	}
	passedBy := fmt.Sprintf("Passed to %s by %s", to.Name, actorName(team, a))
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, err
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.pass(ctx, q, a, g, team, to, from, passedBy, note); err != nil {
			return err
		}
		return s.wakeDecision(ctx, q, to, g, fmt.Sprintf("Team %s passed your team a task: %q. Its note: %s Take it into your plan, ask its sender, defer it, pass it on, or push it back.", team.Name, g.Text, note))
	})
	if err != nil {
		return Goal{}, err
	}
	g.TeamID, g.Passes, g.PassedBy, g.IntakeNote, g.AskedAt = to.ID, g.Passes+1, passedBy, note, 0
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("%s passed the task %q on to team %s: %s", actorName(team, a), g.Text, to.Name, note)},
		{to.ID, fmt.Sprintf("Team %s passed on a task: %s", team.Name, g.Text)},
		{from.ID, fmt.Sprintf("%s: your task %q. %s", passedBy, g.Text, note)},
	})
}

// passTarget checks where team passes task g on to, with what note: a team
// with members, other than team and the task's sender. A member's proposal
// is not passed on: it is the team's own idea, with no other team to see
// where it went, so the lead sends it as a task of the team instead.
func (s *Service) passTarget(ctx context.Context, team Bot, g Goal, toTeamID, note string) (Bot, string, error) {
	if g.Origin == OriginProposal {
		return Bot{}, "", invalid("%q is a proposal of your team's member; to give it to another team, send it with send_task, then push the proposal back", g.Text)
	}
	note, err := textOf("the note", note, maxItemNote, true)
	if err != nil {
		return Bot{}, "", err
	}
	to, err := s.team(ctx, toTeamID)
	if err != nil {
		return Bot{}, "", err
	}
	if _, ok := to.Lead(); !ok || to.ID == team.ID || to.ID == g.FromTeamID {
		return Bot{}, "", invalid("pass the task on to a team with members, other than yours and its sender's")
	}
	return to, note, nil
}

// pass moves task g from team to team to with q, within to's inbox limits
// for its sender, and records the event.
func (s *Service) pass(ctx context.Context, q *db.Queries, a Actor, g Goal, team, to, from Bot, passedBy, note string) error {
	if g.Origin == OriginHandoff {
		if err := checkInbox(ctx, q, to, sender{team: from, botID: g.FromBotID}); err != nil {
			return err
		}
	}
	n, err := q.PassGoal(ctx, db.PassGoalParams{TeamID: to.ID, PassedBy: passedBy, IntakeNote: note, ID: g.ID, FromTeamID: team.ID})
	if err != nil {
		return fmt.Errorf("pass task %s on: %w", g.ID, err)
	}
	if n == 0 {
		return invalid("the task changed meanwhile; look at the board again")
	}
	return s.taskEvent(ctx, q, a, g, TaskPassed, passedBy+": "+note)
}

// passedTooOften turns a pass after MaxPasses into a question for the user,
// once, and returns the error that the lead gets.
func (s *Service) passedTooOften(ctx context.Context, a Actor, team Bot, g Goal) error {
	refused := invalid("%q was passed on %d times; take it, push it back, or ask the user.", g.Text, MaxPasses)
	if g.NeedsUser {
		return refused
	}
	err := s.inTx(ctx, func(q *db.Queries) error {
		_, err := q.PassGoal(ctx, db.PassGoalParams{TeamID: team.ID, PassedBy: g.PassedBy, IntakeNote: g.IntakeNote, ID: g.ID, FromTeamID: team.ID})
		return err
	})
	if err != nil {
		return fmt.Errorf("mark task %s for the user: %w", g.ID, err)
	}
	s.hub.Publish(Event{Type: "bots"})
	question := fmt.Sprintf("The task %q was passed on %d times and still has no team; %s couldn't settle it. Take it here, push it back, or tell the leads where it belongs?",
		g.Text, MaxPasses, memberName(team, a.BotID))
	return errors.Join(refused, s.askUser(ctx, a, team, g.ID, question))
}

// CloseTask closes the team's Now task with a result: it is done and waits
// for approval by whoever asked, and the next planned task starts at once.
func (s *Service) CloseTask(ctx context.Context, a Actor, goalID, result string) (Goal, error) {
	g, ok, err := s.latestGoal(ctx, a.TeamID)
	if err != nil {
		return Goal{}, err
	}
	if !ok || g.ID != goalID {
		return Goal{}, invalid("close the task your team works on now")
	}
	return s.DeliverGoal(ctx, a, result)
}

// approver checks that a approves or asks for changes to task g of team:
// the user for the user's own tasks, the sending lead (or the user) for a
// task from another team, the team's lead (or the user) for a proposal.
func (s *Service) approver(ctx context.Context, a Actor, team Bot, g Goal) error {
	if a.Role == RoleUser {
		return nil
	}
	if g.Origin == OriginProposal && a.leadActs(team) {
		return nil
	}
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return err
	}
	if g.Origin == OriginHandoff && g.FromBotID != "" && from.ID != "" && a.leadActs(from) {
		return nil
	}
	return invalid("only whoever asked for %q approves it or asks for changes", g.Text)
}

// ApproveTask approves a done task, by whoever asked for it: it is closed,
// and its team's lead looks back on it in one retro turn.
func (s *Service) ApproveTask(ctx context.Context, a Actor, goalID string) (Goal, error) {
	g, team, err := s.task(ctx, goalID)
	if err != nil {
		return Goal{}, err
	}
	if err := s.approver(ctx, a, team, g); err != nil {
		return Goal{}, err
	}
	if a.Role == RoleUser {
		return s.AcceptGoal(ctx, goalID, true)
	}
	if g.State != GoalDelivered {
		return Goal{}, invalid("approve a task after its lead closes it")
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.setGoalState(ctx, q, g, GoalDone); err != nil {
			return err
		}
		if err := s.taskEvent(ctx, q, a, g, TaskApproved, ""); err != nil {
			return err
		}
		lead, ok := team.Lead()
		if !ok {
			return nil
		}
		_, err := s.queueTurn(ctx, q, Turn{BotID: lead.ID, Source: SourceRoom, RoomID: team.ID, GoalID: g.ID, BoardRole: RoleRetro, Prompt: "The task was approved."})
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	g.State = GoalDone
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	if err := s.closeTaskQuestions(ctx, g.TeamID, g.ID); err != nil {
		return Goal{}, err
	}
	return g, s.boardNote(ctx, team.ID, fmt.Sprintf("%s approved the task %q.", actorName(team, a), g.Text))
}

// ChangesTask asks for changes to a done task, by whoever asked for it: it
// is Now again when the team has no Now task, else first in the plan, and
// its lead plans the changes.
func (s *Service) ChangesTask(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	g, team, err := s.task(ctx, goalID)
	if err != nil {
		return Goal{}, err
	}
	if err := s.approver(ctx, a, team, g); err != nil {
		return Goal{}, err
	}
	if g.State != GoalDelivered {
		return Goal{}, invalid("ask for changes to a task after its lead closes it")
	}
	if text, err = textOf("the changes", text, maxItemText, true); err != nil {
		return Goal{}, err
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if g, err = s.reopenDone(ctx, q, team, g); err != nil {
			return err
		}
		if err := s.taskEvent(ctx, q, a, g, TaskChanges, text); err != nil {
			return err
		}
		reason := fmt.Sprintf("%s asked for changes to %q: %s Plan them as new steps.", actorName(team, a), g.Text, text)
		if g.State == GoalActive {
			return s.wakeLead(ctx, q, team, g, reason)
		}
		return s.wakeDecision(ctx, q, team, g, reason+" The task is first in your plan, after the task you work on now.")
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	if err := s.closeTaskQuestions(ctx, g.TeamID, g.ID); err != nil {
		return Goal{}, err
	}
	return g, s.boardNote(ctx, team.ID, fmt.Sprintf("%s asked for changes to the task %q: %s", actorName(team, a), g.Text, text))
}

// reopenDone moves done task g with q back to work: active when its team has
// no Now task, else first in the plan.
func (s *Service) reopenDone(ctx context.Context, q *db.Queries, team Bot, g Goal) (Goal, error) {
	_, open, err := openGoalWith(ctx, q, team.ID)
	if err != nil {
		return Goal{}, err
	}
	if !open {
		if err := s.setGoalState(ctx, q, g, GoalActive); err != nil {
			return Goal{}, err
		}
		g.State = GoalActive
		return g, nil
	}
	queue, err := queuedGoals(ctx, q, team.ID)
	if err != nil {
		return Goal{}, err
	}
	if err := s.setGoalState(ctx, q, g, GoalQueued); err != nil {
		return Goal{}, err
	}
	g.State = GoalQueued
	if err := renumber(ctx, q, append([]Goal{g}, queue...)); err != nil {
		return Goal{}, err
	}
	g.QueuePos = 1
	return g, nil
}

// StopTask stops a task: no new turns start for it, the running ones
// finish, and its steps keep their last state. When it was Now, the next
// planned task starts. a is the team's lead, in a turn in it, or the user.
func (s *Service) StopTask(ctx context.Context, a Actor, goalID string) (Goal, error) {
	g, team, err := s.decides(ctx, a, goalID, GoalActive, GoalPaused, GoalQueued)
	if err != nil {
		return Goal{}, err
	}
	var after goalAfter
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.setGoalState(ctx, q, g, GoalStopped); err != nil {
			return err
		}
		if err := s.taskEvent(ctx, q, a, g, TaskStopped, ""); err != nil {
			return err
		}
		queue, err := queuedGoals(ctx, q, team.ID)
		if err != nil {
			return err
		}
		if err := renumber(ctx, q, queue); err != nil {
			return err
		}
		if !g.Now() {
			return nil
		}
		after.next, after.started, err = s.nextGoal(ctx, q, team.ID)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	g.State, g.QueuePos = GoalStopped, 0
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	if err := s.boardNote(ctx, team.ID, fmt.Sprintf("%s stopped the task %q; its steps are kept, and it can be reopened.", actorName(team, a), g.Text)); err != nil {
		return Goal{}, err
	}
	if err := s.handoffResult(ctx, g, "stopped", false); err != nil {
		return Goal{}, err
	}
	return g, s.nextGoalNote(ctx, after.next, after.started)
}

// ReopenTask returns a stopped task to its team's inbox, with its history,
// for the lead to decide on again.
func (s *Service) ReopenTask(ctx context.Context, a Actor, goalID string) (Goal, error) {
	g, team, err := s.decides(ctx, a, goalID, GoalStopped)
	if err != nil {
		return Goal{}, err
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.ReturnToInbox(ctx, db.ReturnToInboxParams{ID: g.ID, FromState: GoalStopped})
		if err != nil {
			return fmt.Errorf("reopen task %s: %w", g.ID, err)
		}
		if n == 0 {
			return invalid("the task changed meanwhile; look at the board again")
		}
		if err := s.taskEvent(ctx, q, a, g, TaskReopened, ""); err != nil {
			return err
		}
		if a.leadActs(team) {
			return nil
		}
		return s.wakeDecision(ctx, q, team, g, fmt.Sprintf("The stopped task %q is back in your inbox: decide on it again.", g.Text))
	})
	if err != nil {
		return Goal{}, err
	}
	g.State = GoalIncoming
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.boardNote(ctx, team.ID, fmt.Sprintf("%s reopened the task %q; it is back in the inbox.", actorName(team, a), g.Text))
}
