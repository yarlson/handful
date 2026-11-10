package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// Where a task comes from.
const (
	OriginUser     = "user"     // the user gave the team the task
	OriginHandoff  = "handoff"  // another team, or the user for it, sent the task
	OriginProposal = "proposal" // a member proposed the task for its own team
)

// Limits of tasks between teams, so that work cannot loop between them.
const (
	MaxHandoffDepth = 3 // teams that a task came through, at most
	MaxAskRounds    = 3 // questions of the receiving lead to the sender
	MaxPasses       = 3 // times a task is passed on to another team

	// DefaultInboxMax and DefaultInboxPerSender are a team's inbox limits
	// until the user sets others: open tasks from all senders, and from one
	// sending team. The user's tasks are exempt.
	DefaultInboxMax       = 10
	DefaultInboxPerSender = 3
	maxInboxMax           = 50

	maxHandoffArtifacts = 10
)

// Handoff is what a task keeps about where it came from and the lead's
// decisions on it.
type Handoff struct {
	Origin string `json:"origin"`
	// FromTeamID is the team that sent the task, "" when the user did for
	// no team, or for the user's own and proposed tasks. FromBotID is its
	// lead, or the member who proposed it; "" for the user. FromGoalID is
	// the sender's task at the time, for context only.
	FromTeamID  string   `json:"fromTeamId,omitempty"`
	FromBotID   string   `json:"fromBotId,omitempty"`
	FromGoalID  string   `json:"fromGoalId,omitempty"`
	Brief       string   `json:"brief,omitempty"`
	ArtifactIDs []string `json:"artifactIds,omitempty"`
	// IntakeNote is the lead's latest question, deferral note or push-back reason.
	IntakeNote string `json:"intakeNote,omitempty"`
	// AskedAt > 0 while a question waits for the sender's answer.
	AskedAt   int64 `json:"askedAt,omitempty"`
	AskRounds int64 `json:"askRounds,omitempty"`
	Depth     int64 `json:"depth,omitempty"`
	// NeedsUser: the leads did not settle it in MaxAskRounds questions, or
	// it was passed on MaxPasses times, so only the user decides on it now.
	NeedsUser bool `json:"needsUser,omitempty"`
	// Pinned holds a planned task where the user put it: its lead cannot move it.
	Pinned bool `json:"pinned,omitempty"`
	// LaterUntil is when a deferred task returns to the inbox, in ms.
	LaterUntil int64 `json:"laterUntil,omitempty"`
	// Passes counts how often a lead passed the task on; PassedBy says the
	// latest, as "Passed to Dev by Product Lead".
	Passes   int64  `json:"passes,omitempty"`
	PassedBy string `json:"passedBy,omitempty"`
	// WaitsOnItemID is the sender's step that waits on the task.
	WaitsOnItemID string `json:"waitsOnItemId,omitempty"`
}

func toHandoff(row db.Goal) Handoff {
	var ids []string
	if err := json.Unmarshal([]byte(row.ArtifactIds), &ids); err != nil || len(ids) == 0 {
		ids = nil
	}
	return Handoff{
		Origin: row.Origin, FromTeamID: row.FromTeamID, FromBotID: row.FromBotID, FromGoalID: row.FromGoalID, Brief: row.Brief,
		ArtifactIDs: ids, IntakeNote: row.IntakeNote, AskedAt: row.AskedAt, AskRounds: row.AskRounds, Depth: row.Depth,
		NeedsUser: row.AskRounds > MaxAskRounds || row.Passes > MaxPasses, Pinned: row.Pinned == 1, LaterUntil: row.LaterUntil,
		Passes: row.Passes, PassedBy: row.PassedBy, WaitsOnItemID: row.WaitsOnItemID,
	}
}

// NewTask is the work that a task carries: what to do, the context the
// receiver needs, the artifacts that go with it, and the sender's step that
// waits on it, if any.
type NewTask struct {
	Text        string
	Brief       string
	ArtifactIDs []string
	WaitsOn     string
}

// NewHandoff is a task sent to another team.
type NewHandoff = NewTask

// sender is who sends a task: a team's lead in a turn in the team, or the
// user, for a team or for none.
type sender struct {
	team   Bot // the zero Bot when the user sends for no team
	botID  string
	goalID string
	depth  int64
}

// name names the sender in a channel note.
func (s sender) name() string {
	if s.botID == "" && s.team.ID == "" {
		return "The user"
	}
	if s.botID == "" {
		return "The user, for team " + s.team.Name + ","
	}
	return "Team " + s.team.Name
}

// SendTask sends team toTeamID a task from the lead's own team: it waits in
// that team's inbox until its lead decides on it, and costs the sender
// nothing. It works in any turn in the team, in any state of its board.
func (s *Service) SendTask(ctx context.Context, a Actor, toTeamID string, t NewTask) (Goal, error) {
	return s.HandOver(ctx, a, "", toTeamID, t)
}

// HandOver sends team toTeamID a task. a is the lead of team fromTeamID in a
// turn in it, or the user, for fromTeamID or for no team (""). The user's
// tasks are exempt from the inbox limits.
func (s *Service) HandOver(ctx context.Context, a Actor, fromTeamID, toTeamID string, t NewTask) (Goal, error) {
	from, err := s.handoffSender(ctx, a, fromTeamID)
	if err != nil {
		return Goal{}, err
	}
	to, err := s.team(ctx, toTeamID)
	if err != nil {
		return Goal{}, err
	}
	if to.ID == from.team.ID {
		return Goal{}, invalid("send a task to another team; for your own team, propose it with update_board propose")
	}
	if _, ok := to.Lead(); !ok {
		return Goal{}, invalid("%s has no members", to.Name)
	}
	if t, err = s.validTask(ctx, t); err != nil {
		return Goal{}, err
	}
	if err := s.checkWaitsOn(ctx, from, t.WaitsOn); err != nil {
		return Goal{}, err
	}
	var g Goal
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := checkInbox(ctx, q, to, from); err != nil {
			return err
		}
		var err error
		g, err = s.insertTask(ctx, q, to, taskRow{origin: OriginHandoff, from: from, task: t})
		if err != nil {
			return err
		}
		if err := s.taskEvent(ctx, q, a, g, TaskSent, g.Text); err != nil {
			return err
		}
		return s.wakeDecision(ctx, q, to, g, fmt.Sprintf("%s sent your team a task: %q. Take it into your plan, ask its sender, defer it with later, pass it on, or push it back.", from.name(), g.Text))
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.postNotes(ctx, []noteTo{
		{to.ID, fmt.Sprintf("%s sent a task: %s", from.name(), g.Text)},
		{from.team.ID, fmt.Sprintf("%s sent team %s a task: %s", s.senderName(from), to.Name, g.Text)},
	})
}

// UserTask gives team teamID a task from the user. It lands in the inbox,
// where the lead decides on it, unless startNow: then it starts at once, or
// goes to the top of the plan while another task is Now, and the lead is told.
func (s *Service) UserTask(ctx context.Context, teamID string, t NewTask, startNow bool) (Goal, error) {
	team, err := s.team(ctx, teamID)
	if err != nil {
		return Goal{}, err
	}
	if _, ok := team.Lead(); !ok {
		return Goal{}, invalid("%s has no members", team.Name)
	}
	if t.WaitsOn != "" {
		return Goal{}, invalid("only a team's lead links a task to a step that waits on it")
	}
	if t, err = s.validTask(ctx, t); err != nil {
		return Goal{}, err
	}
	user := Actor{Role: RoleUser}
	var g Goal
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if startNow {
			g, err = s.placeTop(ctx, q, team, t)
		} else {
			g, err = s.insertTask(ctx, q, team, taskRow{origin: OriginUser, task: t})
		}
		if err != nil {
			return err
		}
		if err := s.taskEvent(ctx, q, user, g, TaskSent, g.Text); err != nil {
			return err
		}
		return s.userTaskWake(ctx, q, team, g)
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.boardNote(ctx, team.ID, userTaskNote(g))
}

// userTaskWake wakes the lead of team about the user's task g with q: to
// plan it when it started, to hear that it tops the plan, or to decide on it
// in the inbox.
func (s *Service) userTaskWake(ctx context.Context, q *db.Queries, team Bot, g Goal) error {
	if g.State == GoalActive {
		return s.wakeLead(ctx, q, team, g, "The user started a new task: plan it.")
	}
	if g.State == GoalQueued {
		return s.wakeDecision(ctx, q, team, g, fmt.Sprintf("The user put a task at the top of your plan: %q. It starts when the task you work on now is closed.", g.Text))
	}
	return s.wakeDecision(ctx, q, team, g, fmt.Sprintf("The user sent your team a task: %q. Take it into your plan, ask the user, defer it with later, pass it on, or push it back.", g.Text))
}

// userTaskNote shows a task of the user in its team's channel.
func userTaskNote(g Goal) string {
	if g.State == GoalActive {
		return "New task, started at once: " + g.Text
	}
	if g.State == GoalQueued {
		return "New task, at the top of the plan: " + g.Text
	}
	return "The user sent a task: " + g.Text
}

// placeTop inserts the user's task t for team with q: active when the team
// has no Now task, else first in its plan.
func (s *Service) placeTop(ctx context.Context, q *db.Queries, team Bot, t NewTask) (Goal, error) {
	_, open, err := openGoalWith(ctx, q, team.ID)
	if err != nil {
		return Goal{}, err
	}
	if !open {
		return s.insertTask(ctx, q, team, taskRow{origin: OriginUser, task: t, state: GoalActive})
	}
	queue, err := queuedGoals(ctx, q, team.ID)
	if err != nil {
		return Goal{}, err
	}
	if len(queue) >= MaxQueuedGoals {
		return Goal{}, invalid("%s has %d tasks planned; take one out first", team.Name, MaxQueuedGoals)
	}
	g, err := s.insertTask(ctx, q, team, taskRow{origin: OriginUser, task: t, state: GoalQueued, queuePos: 1})
	if err != nil {
		return Goal{}, err
	}
	return g, renumber(ctx, q, append([]Goal{g}, queue...))
}

// ProposeTask is a member's proposal of a task for its own team, in a turn in
// the team: it waits in the inbox, marked as proposed by the member, until
// the lead decides on it.
func (s *Service) ProposeTask(ctx context.Context, a Actor, t NewTask) (Goal, error) {
	team, err := s.team(ctx, a.TeamID)
	if err != nil {
		return Goal{}, err
	}
	if _, err := roomMember(team, a.BotID); err != nil {
		return Goal{}, err
	}
	if t.WaitsOn != "" {
		return Goal{}, invalid("a proposal waits on no step; ask your lead to link it")
	}
	if t, err = s.validTask(ctx, t); err != nil {
		return Goal{}, err
	}
	var g Goal
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := checkInbox(ctx, q, team, sender{botID: a.BotID}); err != nil {
			return err
		}
		var err error
		if g, err = s.insertTask(ctx, q, team, taskRow{origin: OriginProposal, from: sender{botID: a.BotID}, task: t}); err != nil {
			return err
		}
		if err := s.taskEvent(ctx, q, a, g, TaskProposed, g.Text); err != nil {
			return err
		}
		if a.leadActs(team) {
			return nil
		}
		return s.wakeDecision(ctx, q, team, g, fmt.Sprintf("%s proposed a task for your team: %q. Take it into your plan, defer it with later, or push it back.", memberName(team, a.BotID), g.Text))
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.boardNote(ctx, team.ID, fmt.Sprintf("%s proposed a task: %s", memberName(team, a.BotID), g.Text))
}

// senderName names who sent a task, in the sender's channel.
func (s *Service) senderName(from sender) string {
	if from.botID == "" {
		return "The user"
	}
	return memberName(from.team, from.botID)
}

// taskRow is a new task for insertTask: where it comes from, what it
// carries, and where it lands; the zero state is the inbox.
type taskRow struct {
	origin   string
	from     sender
	task     NewTask
	state    string
	queuePos int64
}

// insertTask adds task r for team to with q. An active task gets the team's
// goal budget now; any other gets it when it starts.
func (s *Service) insertTask(ctx context.Context, q *db.Queries, to Bot, r taskRow) (Goal, error) {
	ids, err := json.Marshal(append([]string{}, r.task.ArtifactIDs...))
	if err != nil {
		return Goal{}, fmt.Errorf("encode the artifacts: %w", err)
	}
	state, budget := r.state, int64(0)
	if state == "" {
		state = GoalIncoming
	}
	if state == GoalActive {
		budget = to.Team.GoalBudget
	}
	row, err := q.InsertTask(ctx, db.InsertTaskParams{
		ID: uuid.NewString(), TeamID: to.ID, Text: r.task.Text, State: state, Budget: budget, QueuePos: r.queuePos, CreatedAt: s.nowMs(),
		Origin: r.origin, FromTeamID: r.from.team.ID, FromBotID: r.from.botID, FromGoalID: r.from.goalID, Brief: r.task.Brief,
		ArtifactIds: string(ids), Depth: r.from.depth, WaitsOnItemID: r.task.WaitsOn,
	})
	if err != nil {
		return Goal{}, fmt.Errorf("add a task for team %s: %w", to.ID, err)
	}
	return toGoal(row, 0), nil
}

// checkInbox checks with q that team's inbox takes one more task from
// sender: at most InboxMax open tasks, and at most InboxPerSender from one
// sending team. The user's tasks are exempt.
func checkInbox(ctx context.Context, q *db.Queries, team Bot, from sender) error {
	if from.botID == "" {
		return nil
	}
	inbox, err := q.ListInboxGoals(ctx, team.ID)
	if err != nil {
		return fmt.Errorf("list the inbox of team %s: %w", team.ID, err)
	}
	if int64(len(inbox)) >= team.Team.InboxMax {
		return invalid("%s's inbox is full (%d waiting). Wait for its lead's decisions, or ask the user.", team.Name, len(inbox))
	}
	if from.team.ID == "" {
		return nil
	}
	fromTeam := 0
	for _, g := range inbox {
		if g.FromTeamID == from.team.ID {
			fromTeam++
		}
	}
	if int64(fromTeam) >= team.Team.InboxPerSender {
		return invalid("You have %d tasks waiting in %s's inbox. Wait for decisions, or withdraw one.", fromTeam, team.Name)
	}
	return nil
}

// handoffSender checks who sends a task for team fromTeamID: its lead in a
// turn in the team, or the user. A lead's task is one deeper than the task
// that its turn works on.
func (s *Service) handoffSender(ctx context.Context, a Actor, fromTeamID string) (sender, error) {
	if a.Role == RoleUser {
		return s.userSender(ctx, fromTeamID)
	}
	if fromTeamID == "" {
		fromTeamID = a.TeamID
	}
	if a.TeamID != fromTeamID {
		return sender{}, invalid("send work to another team only in a turn in your team")
	}
	team, err := s.team(ctx, fromTeamID)
	if err != nil {
		return sender{}, err
	}
	if err := a.mustLead(team); err != nil {
		return sender{}, err
	}
	g, err := s.turnGoal(ctx, a.TurnID, team.ID)
	if err != nil {
		return sender{}, err
	}
	if g.Depth >= MaxHandoffDepth {
		return sender{}, invalid("This work came through %d teams; ask the user before sending it on.", MaxHandoffDepth)
	}
	return sender{team: team, botID: a.BotID, goalID: g.ID, depth: g.Depth + 1}, nil
}

// userSender is the user sending a task, for team fromTeamID or for none.
func (s *Service) userSender(ctx context.Context, fromTeamID string) (sender, error) {
	if fromTeamID == "" {
		return sender{}, nil
	}
	team, err := s.team(ctx, fromTeamID)
	if err != nil {
		return sender{}, err
	}
	g, err := s.turnGoal(ctx, "", team.ID)
	return sender{team: team, goalID: g.ID}, err
}

// turnGoal returns the task that a turn in team works on: the turn's own
// task of the team, else the team's Now task; the zero Goal for none. A
// decision turn on a task of the inbox works on no task of the team yet.
func (s *Service) turnGoal(ctx context.Context, turnID, teamID string) (Goal, error) {
	id, err := s.q.GetTurnGoal(ctx, turnID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Goal{}, fmt.Errorf("get the goal of turn %s: %w", turnID, err)
	}
	if id != "" {
		g, err := s.goal(ctx, s.q, id)
		if err != nil || (g.TeamID == teamID && g.Open()) {
			return g, err
		}
	}
	g, ok, err := s.latestGoal(ctx, teamID)
	if err != nil || !ok || !g.Now() {
		return Goal{}, err
	}
	return g, nil
}

// checkWaitsOn checks that step itemID, when given, is a todo, doing or
// blocked step of the sender's task, so that a push back can block it.
func (s *Service) checkWaitsOn(ctx context.Context, from sender, itemID string) error {
	if itemID == "" {
		return nil
	}
	if from.goalID == "" || from.botID == "" {
		return invalid("waits_on names a step of the task your turn works on; your team works on none")
	}
	items, err := goalItems(ctx, s.q, from.goalID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(items, func(it Item) bool { return it.ID == itemID })
	if i < 0 || (items[i].State != ItemTodo && items[i].State != ItemDoing && items[i].State != ItemBlocked) {
		return invalid("%s is not an open step of the task your turn works on", itemID)
	}
	return nil
}

// validTask checks a task's texts and artifacts and cleans them.
func (s *Service) validTask(ctx context.Context, t NewTask) (NewTask, error) {
	var err error
	if t.Text, err = textOf("the task", t.Text, maxGoalText, true); err != nil {
		return NewTask{}, err
	}
	if t.Brief, err = textOf("the brief", t.Brief, maxItemText, false); err != nil {
		return NewTask{}, err
	}
	t.ArtifactIDs = slices.Compact(slices.Sorted(slices.Values(t.ArtifactIDs)))
	if len(t.ArtifactIDs) > maxHandoffArtifacts {
		return NewTask{}, invalid("a task carries at most %d artifacts", maxHandoffArtifacts)
	}
	for _, id := range t.ArtifactIDs {
		if _, err := s.GetArtifact(ctx, id); err != nil {
			return NewTask{}, err
		}
	}
	return t, nil
}

// task returns a task with its team.
func (s *Service) task(ctx context.Context, goalID string) (Goal, Bot, error) {
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	team, err := s.team(ctx, g.TeamID)
	return g, team, err
}

// decides returns a task of the actor's team that is in one of states. a is
// the team's lead, in a turn in it, or the user.
func (s *Service) decides(ctx context.Context, a Actor, goalID string, states ...string) (Goal, Bot, error) {
	g, team, err := s.task(ctx, goalID)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	if a.Role != RoleUser && !a.leadActs(team) {
		return Goal{}, Bot{}, invalid("only the lead of %s decides on its tasks, in a turn in the team", team.Name)
	}
	if !slices.Contains(states, g.State) {
		return Goal{}, Bot{}, invalid("%q is %s; this needs a task that is %s", g.Text, g.State, strings.Join(states, " or "))
	}
	return g, team, nil
}

// incoming returns a task that waits in the actor's team's inbox.
func (s *Service) incoming(ctx context.Context, a Actor, goalID string) (Goal, Bot, error) {
	return s.decides(ctx, a, goalID, GoalIncoming)
}

// sent returns a task with its sending team. a is the sending lead, in a
// turn in its team, or the user. The team is the zero Bot when the user sent
// the task, or the team is gone; then only the user acts.
func (s *Service) sent(ctx context.Context, a Actor, goalID string) (Goal, Bot, error) {
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	if a.Role != RoleUser && (g.Origin != OriginHandoff || from.ID == "" || !a.leadActs(from)) {
		return Goal{}, Bot{}, invalid("only the lead of the team that sent %q does this, in a turn in its team", g.Text)
	}
	return g, from, nil
}

// senderTeam returns the team that sent g, or the zero Bot when there is
// none (any more).
func (s *Service) senderTeam(ctx context.Context, g Goal) (Bot, error) {
	if g.FromTeamID == "" {
		return Bot{}, nil
	}
	team, err := s.GetBot(ctx, g.FromTeamID)
	if errors.Is(err, ErrNotFound) {
		return Bot{}, nil
	}
	return team, err
}

// AcceptHandoff takes a task from the inbox into the end of the plan.
func (s *Service) AcceptHandoff(ctx context.Context, a Actor, goalID string) (Goal, error) {
	return s.TakeTask(ctx, a, goalID, 0)
}

// TakeTask takes a task from the inbox into the team's plan, at position (1
// first; 0 the end). When the team has no Now task and nothing planned, it
// starts at once. A lead's position must not move a task that the user pinned.
func (s *Service) TakeTask(ctx context.Context, a Actor, goalID string, position int64) (Goal, error) {
	g, team, err := s.decides(ctx, a, goalID, GoalIncoming)
	if err != nil {
		return Goal{}, err
	}
	if a.Role == RoleUser && !g.NeedsUser {
		return Goal{}, invalid("%s's lead decides on %q; you take a task from an inbox when its leads couldn't settle it", team.Name, g.Text)
	}
	var started bool
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if g, started, err = s.take(ctx, q, a, team, g, position); err != nil {
			return err
		}
		return s.taskEvent(ctx, q, a, g, TaskTaken, takenText(g, started))
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	where := takenText(g, started)
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, err
	}
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("%s took the task %q; %s.", actorName(team, a), g.Text, where)},
		{from.ID, fmt.Sprintf("Team %s took your task %q; %s.", team.Name, g.Text, where)},
	})
}

// takenText says where a taken task went.
func takenText(g Goal, started bool) string {
	if started {
		return "it started"
	}
	return fmt.Sprintf("it is #%d in the plan", g.QueuePos)
}

// take puts task g into team's plan with q at position, and starts it when
// the team has no Now task and nothing planned. It returns the task as it is
// now and whether it started.
func (s *Service) take(ctx context.Context, q *db.Queries, a Actor, team Bot, g Goal, position int64) (Goal, bool, error) {
	_, open, err := openGoalWith(ctx, q, team.ID)
	if err != nil {
		return Goal{}, false, err
	}
	queue, err := queuedGoals(ctx, q, team.ID)
	if err != nil {
		return Goal{}, false, err
	}
	if len(queue) >= MaxQueuedGoals {
		return Goal{}, false, invalid("%s has %d tasks planned; close or stop one first", team.Name, MaxQueuedGoals)
	}
	at, err := planPosition(a, queue, position)
	if err != nil {
		return Goal{}, false, err
	}
	// renumber gives the task its place below.
	n, err := q.AcceptHandoffGoal(ctx, db.AcceptHandoffGoalParams{State: GoalQueued, ID: g.ID})
	if err != nil {
		return Goal{}, false, fmt.Errorf("take task %s: %w", g.ID, err)
	}
	if n == 0 {
		return Goal{}, false, invalid("the task changed meanwhile; look at the board again")
	}
	g.State, g.QueuePos, g.AskedAt = GoalQueued, at, 0
	if err := renumber(ctx, q, slices.Insert(queue, int(at-1), g)); err != nil {
		return Goal{}, false, err
	}
	if open || len(queue) > 0 {
		return g, false, nil
	}
	next, started, err := s.nextGoal(ctx, q, team.ID)
	if err != nil || !started {
		return g, false, err
	}
	return next, true, nil
}

// planPosition returns where a new task goes in plan queue: position, or
// the end for 0. A lead may not put it above a task that the user pinned,
// which would move that task down.
func planPosition(a Actor, queue []Goal, position int64) (int64, error) {
	end := int64(len(queue) + 1)
	if position == 0 {
		return end, nil
	}
	if position < 1 || position > end {
		return 0, invalid("position must be from 1 to %d, or 0 for the end of the plan", end)
	}
	if a.Role == RoleUser {
		return position, nil
	}
	for _, g := range queue[position-1:] {
		if g.Pinned {
			return 0, conflict("%q is pinned by the user at #%d; place the task below it", g.Text, g.QueuePos)
		}
	}
	return position, nil
}

// AskSender asks the sender of a task in the inbox a question.
func (s *Service) AskSender(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	return s.AskTask(ctx, a, goalID, text)
}

// AskTask asks the sender of a task in the inbox a question, at most
// MaxAskRounds times. Then the user settles it: the next question fails,
// wakes no one, and asks the user in the team's channel.
func (s *Service) AskTask(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	if a.Role == RoleUser {
		return Goal{}, invalid("the receiving lead asks the sender; answer the task instead")
	}
	g, team, err := s.incoming(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	if g.Origin == OriginProposal {
		return Goal{}, invalid("ask %s about the proposal in the team's channel", memberName(team, g.FromBotID))
	}
	if text, err = textOf("the question", text, maxItemText, true); err != nil {
		return Goal{}, err
	}
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, err
	}
	if g.AskRounds >= MaxAskRounds {
		return Goal{}, s.askedTooOften(ctx, a, team, from, g)
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.setAsk(ctx, q, g, text, g.AskRounds+1); err != nil {
			return err
		}
		if err := s.taskEvent(ctx, q, a, g, TaskAsked, text); err != nil {
			return err
		}
		if g.FromBotID == "" || from.ID == "" {
			return nil
		}
		return s.wakeLead(ctx, q, from, Goal{}, fmt.Sprintf("Team %s asks about your task %q: %s Answer it with answer_task.", team.Name, g.Text, text))
	})
	if err != nil {
		return Goal{}, err
	}
	g.AskRounds++
	g.IntakeNote, g.AskedAt = text, s.nowMs()
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.askNotes(ctx, a, team, from, g)
}

// setAsk records with q that the receiving lead asked about task g for the
// given round. It fails when another question came meanwhile.
func (s *Service) setAsk(ctx context.Context, q *db.Queries, g Goal, note string, rounds int64) error {
	n, err := q.AskAboutHandoff(ctx, db.AskAboutHandoffParams{IntakeNote: note, AskedAt: s.nowMs(), AskRounds: rounds, ID: g.ID, FromRounds: g.AskRounds})
	if err != nil {
		return fmt.Errorf("ask about task %s: %w", g.ID, err)
	}
	if n == 0 {
		return invalid("the task changed meanwhile; look at the board again")
	}
	return nil
}

// askNotes shows a question about task g: in the sender's channel, or as a
// question to the user when the user sent it.
func (s *Service) askNotes(ctx context.Context, a Actor, team, from Bot, g Goal) error {
	asked := fmt.Sprintf("%s asked about the task %q (%d of %d): %s", memberName(team, a.BotID), g.Text, g.AskRounds, MaxAskRounds, g.IntakeNote)
	if err := s.boardNote(ctx, team.ID, asked); err != nil {
		return err
	}
	if g.FromBotID == "" {
		return s.askUser(ctx, a, team, fmt.Sprintf("A question about the task %q: %s", g.Text, g.IntakeNote))
	}
	return s.postNotes(ctx, []noteTo{{from.ID, fmt.Sprintf("Team %s asks about your task %q: %s", team.Name, g.Text, g.IntakeNote)}})
}

// askUser posts the receiving lead's question to the user in its team's channel.
func (s *Service) askUser(ctx context.Context, a Actor, team Bot, text string) error {
	_, err := s.PostToRoomWithMeta(ctx, team.ID, a.BotID, a.TurnID, text, nil, maxAgentHops, true, PostMeta{})
	return err
}

// askedTooOften turns the receiver's question after MaxAskRounds into one
// for the user, once, and returns the error that the receiving lead gets.
func (s *Service) askedTooOften(ctx context.Context, a Actor, team, from Bot, g Goal) error {
	senderName := "the user"
	if lead, ok := from.Lead(); ok && g.FromBotID != "" {
		senderName = lead.Name
	}
	refused := invalid("You asked %s %d times about %s; take it, push it back, or ask the user.", senderName, MaxAskRounds, g.Text)
	if g.NeedsUser {
		return refused
	}
	if err := s.setAsk(ctx, s.q, g, g.IntakeNote, MaxAskRounds+1); err != nil {
		return err
	}
	s.hub.Publish(Event{Type: "bots"})
	question := fmt.Sprintf("%s and %s couldn't settle %q in %d rounds: %s Take it, push it back or answer it?",
		memberName(team, a.BotID), senderName, g.Text, MaxAskRounds, g.IntakeNote)
	return errors.Join(refused, s.askUser(ctx, a, team, question))
}

// AnswerHandoff answers the receiver's question about a task.
func (s *Service) AnswerHandoff(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	return s.AnswerTask(ctx, a, goalID, text)
}

// AnswerTask answers the receiving lead's question about a task: the answer
// joins its brief, and the task waits in the inbox for the lead's decision
// again. a is the sending lead, while a question waits, or the user at any
// time.
func (s *Service) AnswerTask(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	g, from, err := s.sent(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	if g.State != GoalIncoming {
		return Goal{}, invalid("%q is %s; only a task in the inbox is answered", g.Text, g.State)
	}
	if a.Role != RoleUser && (g.AskedAt == 0 || g.NeedsUser) {
		return Goal{}, invalid("no question about %q waits for you", g.Text)
	}
	who := actorName(from, a)
	text, brief, err := answeredBrief(g, who, text)
	if err != nil {
		return Goal{}, err
	}
	team, err := s.team(ctx, g.TeamID)
	if err != nil {
		return Goal{}, err
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.AnswerHandoffGoal(ctx, db.AnswerHandoffGoalParams{Brief: brief, ID: g.ID, FromBrief: g.Brief})
		if err != nil {
			return fmt.Errorf("answer task %s: %w", g.ID, err)
		}
		if n == 0 {
			return invalid("the task changed meanwhile; look at the board again")
		}
		if err := s.taskEvent(ctx, q, a, g, TaskAnswered, text); err != nil {
			return err
		}
		return s.wakeDecision(ctx, q, team, g, fmt.Sprintf("%s answered your question about the task %q: %s Take it, ask again, or push it back.", who, g.Text, text))
	})
	if err != nil {
		return Goal{}, err
	}
	g.Brief, g.AskedAt = brief, 0
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("%s answered about the task %q: %s", who, g.Text, text)},
		{from.ID, fmt.Sprintf("%s answered team %s about the task %q: %s", who, team.Name, g.Text, text)},
	})
}

// answeredBrief checks an answer to a question about task g and returns it
// with the task's brief that holds it.
func answeredBrief(g Goal, who, text string) (string, string, error) {
	text, err := textOf("the answer", text, maxItemText, true)
	if err != nil {
		return "", "", err
	}
	brief, err := textOf("the brief with the answer", g.Brief+"\n\nAnswer from "+who+": "+text, maxItemText, true)
	return text, brief, err
}

// DeclineHandoff pushes a task back.
func (s *Service) DeclineHandoff(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	return s.PushBack(ctx, a, goalID, text)
}

// PushBack closes a task in the inbox with a reason; it never runs. Its
// sender sees why. A step of the sender's that waits on it is blocked with
// the reason, and the sending lead wakes to re-plan.
func (s *Service) PushBack(ctx context.Context, a Actor, goalID, reason string) (Goal, error) {
	g, team, err := s.incoming(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	if reason, err = textOf("the reason", reason, maxItemText, true); err != nil {
		return Goal{}, err
	}
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, err
	}
	note := fmt.Sprintf("Team %s pushed back your task %q: %s", team.Name, g.Text, reason)
	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.DeclineHandoffGoal(ctx, db.DeclineHandoffGoalParams{IntakeNote: reason, ID: g.ID})
		if err != nil {
			return fmt.Errorf("push back task %s: %w", g.ID, err)
		}
		if n == 0 {
			return invalid("the task changed meanwhile; look at the board again")
		}
		if err := s.taskEvent(ctx, q, a, g, TaskPushedBack, reason); err != nil {
			return err
		}
		return s.wakeSender(ctx, q, g, from, note)
	})
	if err != nil {
		return Goal{}, err
	}
	g.State, g.IntakeNote, g.AskedAt = GoalDeclined, reason, 0
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	if err := s.blockWaiting(ctx, g, note); err != nil {
		return Goal{}, err
	}
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("%s pushed back the task %q: %s", actorName(team, a), g.Text, reason)},
		{from.ID, note},
	})
}

// blockWaiting blocks the sender's step that waits on task g with note, when
// it is still open.
func (s *Service) blockWaiting(ctx context.Context, g Goal, note string) error {
	if g.WaitsOnItemID == "" {
		return nil
	}
	_, err := s.moveItem(ctx, Actor{}, g.WaitsOnItemID, leaving(EventBlocked, fixed(note), func(_ Bot, it *Item) error {
		if it.State != ItemTodo && it.State != ItemDoing && it.State != ItemReview {
			return errNotWaiting
		}
		return block(it, cut(note, maxItemNote))
	}))
	if errors.Is(err, errNotWaiting) || errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// errNotWaiting is a step that no longer waits on a task: it is done,
// dropped or blocked already.
var errNotWaiting = errors.New("the step no longer waits")

// wakeSender wakes the lead of the team that sent g, with q, in a turn that
// no goal pays for. The user's tasks wake no lead.
func (s *Service) wakeSender(ctx context.Context, q *db.Queries, g Goal, from Bot, reason string) error {
	if g.FromBotID == "" || from.ID == "" {
		return nil
	}
	return s.wakeLead(ctx, q, from, Goal{}, reason+" Act on it if your team needs to; otherwise note it and stop.")
}

// WithdrawHandoff takes back a task that has not started.
func (s *Service) WithdrawHandoff(ctx context.Context, a Actor, goalID string) (Goal, error) {
	return s.WithdrawTask(ctx, a, goalID)
}

// WithdrawTask takes back a task that has not started: in the inbox,
// deferred or planned. a is the sending lead, or the user; a started task
// only the user ends.
func (s *Service) WithdrawTask(ctx context.Context, a Actor, goalID string) (Goal, error) {
	g, from, err := s.sent(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	if g.State != GoalIncoming && g.State != GoalLater && g.State != GoalQueued {
		return Goal{}, invalid("%q is %s; only a task that has not started is withdrawn, and the user ends one that has", g.Text, g.State)
	}
	team, err := s.team(ctx, g.TeamID)
	if err != nil {
		return Goal{}, err
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.setGoalState(ctx, q, g, GoalEnded); err != nil {
			return err
		}
		if err := s.taskEvent(ctx, q, a, g, TaskWithdrawn, ""); err != nil {
			return err
		}
		queue, err := queuedGoals(ctx, q, g.TeamID)
		if err != nil {
			return err
		}
		return renumber(ctx, q, queue)
	})
	if err != nil {
		return Goal{}, err
	}
	g.State, g.QueuePos = GoalEnded, 0
	s.hub.Publish(Event{Type: "bots"})
	who := actorName(from, a)
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("Withdrawn by %s: the task %q.", who, g.Text)},
		{from.ID, fmt.Sprintf("%s withdrew the task %q from team %s.", who, g.Text, team.Name)},
	})
}

// handoffResult tells the team that sent task g what became of it: it was
// delivered, approved or ended. On a delivery its lead wakes, in a turn that
// no goal pays for.
func (s *Service) handoffResult(ctx context.Context, g Goal, text string, wake bool) error {
	if g.Origin != OriginHandoff {
		return nil
	}
	from, err := s.senderTeam(ctx, g)
	if err != nil || from.ID == "" {
		return err
	}
	team, err := s.GetBot(ctx, g.TeamID)
	if err != nil {
		return err
	}
	note := fmt.Sprintf("Team %s %s your task %q", team.Name, text, g.Text)
	if wake {
		err = s.inTx(ctx, func(q *db.Queries) error { return s.wakeSender(ctx, q, g, from, note) })
		s.signalTurns()
	}
	return errors.Join(err, s.boardNote(ctx, from.ID, note))
}

// fillHandoffs adds a team's inbox, its done tasks waiting for approval, and
// the tasks it sent lately, to its board.
func (s *Service) fillHandoffs(ctx context.Context, teamID string, board *Board) error {
	inbox, err := s.q.ListInboxGoals(ctx, teamID)
	if err != nil {
		return fmt.Errorf("list the inbox of team %s: %w", teamID, err)
	}
	for _, row := range inbox {
		g := toGoal(row, 0)
		board.Inbox = append(board.Inbox, g)
		if g.State == GoalIncoming {
			board.Incoming = append(board.Incoming, g)
		}
	}
	done, err := s.q.ListDoneGoals(ctx, teamID)
	if err != nil {
		return fmt.Errorf("list the done tasks of team %s: %w", teamID, err)
	}
	for _, row := range done {
		g, err := s.goal(ctx, s.q, row.ID)
		if err != nil {
			return err
		}
		board.Done = append(board.Done, g)
	}
	if err := s.fillSent(ctx, teamID, board); err != nil {
		return err
	}
	board.Plan = board.Queue
	return s.nameHandoffTeams(ctx, board)
}

// fillSent adds the tasks that a team sent in the last 30 days: all of them
// as handedOver, and those still in another team's inbox or plan, or being
// done there, as sent.
func (s *Service) fillSent(ctx context.Context, teamID string, board *Board) error {
	since := s.now().Add(-30 * 24 * time.Hour).UnixMilli()
	rows, err := s.q.ListHandedOverGoals(ctx, db.ListHandedOverGoalsParams{FromTeamID: teamID, Since: since})
	if err != nil {
		return fmt.Errorf("list the tasks that team %s sent: %w", teamID, err)
	}
	for _, row := range rows {
		g, err := s.goal(ctx, s.q, row.ID)
		if err != nil {
			return err
		}
		board.HandedOver = append(board.HandedOver, g)
		if g.State != GoalDone && g.State != GoalEnded && g.State != GoalDeclined {
			board.Sent = append(board.Sent, g)
		}
	}
	return nil
}

// nameHandoffTeams names the teams that a board's tasks come from or went
// to; a team that is gone is "a deleted team".
func (s *Service) nameHandoffTeams(ctx context.Context, board *Board) error {
	ids := []string{}
	for _, g := range board.Inbox {
		ids = append(ids, g.FromTeamID)
	}
	for _, g := range board.HandedOver {
		ids = append(ids, g.TeamID)
	}
	for _, id := range ids {
		if _, known := board.Teams[id]; id == "" || known {
			continue
		}
		team, err := s.GetBot(ctx, id)
		if errors.Is(err, ErrNotFound) {
			board.Teams[id] = "a deleted team"
			continue
		}
		if err != nil {
			return err
		}
		board.Teams[id] = team.Name
	}
	return nil
}

// incomingCounts maps each team to how many tasks wait in its inbox.
func (s *Service) incomingCounts(ctx context.Context) (map[string]int64, error) {
	rows, err := s.q.CountIncomingGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("count the tasks in inboxes: %w", err)
	}
	out := map[string]int64{}
	for _, r := range rows {
		out[r.TeamID] = r.N
	}
	return out, nil
}

// errNeededBy is what a request without the items that wait on it gets.
const errNeededBy = "A request is for a part your goal's items wait on; name them in needed_by. For other work for that team, use hand_over."

// neededBy returns the items of the asking goal that wait on a new request:
// ids must name todo or blocked items of it. A chained request (forItem is
// the request that the team serves) needs none: the served request, which is
// in progress, waits on it.
func neededBy(ids []string, items []Item, forItem string) ([]Item, error) {
	if forItem != "" && len(ids) == 0 {
		return nil, nil
	}
	if len(ids) == 0 {
		return nil, invalid(errNeededBy)
	}
	var waiting []Item
	for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		i := slices.IndexFunc(items, func(it Item) bool { return it.ID == id })
		if i < 0 || (items[i].State != ItemTodo && items[i].State != ItemBlocked) {
			return nil, invalid("%s (%s is not a todo or blocked item of your goal)", errNeededBy, id)
		}
		waiting = append(waiting, items[i])
	}
	return waiting, nil
}

// waitOn makes items depend on request reqID with q. Their queued work waits
// for it.
func (s *Service) waitOn(ctx context.Context, q *db.Queries, items []Item, reqID string) error {
	for _, it := range items {
		deps, err := json.Marshal(slices.Sorted(slices.Values(append(slices.Clone(it.DependsOn), reqID))))
		if err != nil {
			return fmt.Errorf("encode the dependencies of item %s: %w", it.ID, err)
		}
		if err := q.SetItemDependsOn(ctx, db.SetItemDependsOnParams{DependsOn: string(deps), ID: it.ID}); err != nil {
			return fmt.Errorf("make item %s wait on %s: %w", it.ID, reqID, err)
		}
		if err := q.CancelQueuedItemTurns(ctx, db.CancelQueuedItemTurnsParams{FinishedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, ItemID: it.ID}); err != nil {
			return fmt.Errorf("cancel the turns of item %s: %w", it.ID, err)
		}
	}
	return nil
}
