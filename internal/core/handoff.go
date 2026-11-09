package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// Where a goal comes from, and what an accepted handoff does when its team is free.
const (
	OriginUser    = "user"    // the user gave the team the goal
	OriginHandoff = "handoff" // another team, or the user for it, handed the goal over

	StartAuto = "auto" // an accepted handoff starts when the team is free
	StartAsk  = "ask"  // an accepted handoff waits in the queue for the user
)

// Limits of handoffs, so that work cannot loop between teams.
const (
	MaxHandoffDepth = 3  // handoffs that led to a goal, at most
	MaxIncoming     = 10 // handoffs that wait for a team to take them in
	MaxAskRounds    = 3  // questions of the receiving lead to the sender

	maxHandoffArtifacts = 10
)

// Handoff is what a goal that another team handed over keeps about it.
type Handoff struct {
	Origin string `json:"origin"`
	// FromTeamID is the team that handed the goal over, "" when the user did
	// for no team; FromBotID is its lead, "" for the user. FromGoalID is the
	// sender's goal at the time, for context only.
	FromTeamID  string   `json:"fromTeamId,omitempty"`
	FromBotID   string   `json:"fromBotId,omitempty"`
	FromGoalID  string   `json:"fromGoalId,omitempty"`
	Brief       string   `json:"brief,omitempty"`
	ArtifactIDs []string `json:"artifactIds,omitempty"`
	// IntakeNote is the receiver's latest question, or why it declined.
	IntakeNote string `json:"intakeNote,omitempty"`
	// AskedAt > 0 while a question waits for the sender's answer.
	AskedAt   int64 `json:"askedAt,omitempty"`
	AskRounds int64 `json:"askRounds,omitempty"`
	Depth     int64 `json:"depth,omitempty"`
	// NeedsUser: the leads did not settle it in MaxAskRounds questions, so
	// only the user answers it now.
	NeedsUser bool `json:"needsUser,omitempty"`
}

func toHandoff(row db.Goal) Handoff {
	var ids []string
	if err := json.Unmarshal([]byte(row.ArtifactIds), &ids); err != nil || len(ids) == 0 {
		ids = nil
	}
	return Handoff{
		Origin: row.Origin, FromTeamID: row.FromTeamID, FromBotID: row.FromBotID, FromGoalID: row.FromGoalID, Brief: row.Brief,
		ArtifactIDs: ids, IntakeNote: row.IntakeNote, AskedAt: row.AskedAt, AskRounds: row.AskRounds, Depth: row.Depth,
		NeedsUser: row.AskRounds > MaxAskRounds,
	}
}

// NewHandoff is the work that a team hands over: the goal, the context that
// the receiver needs, and the artifacts that go with it.
type NewHandoff struct {
	Text        string
	Brief       string
	ArtifactIDs []string
}

// sender is who hands a goal over: a team's lead in a turn in the team, or
// the user, for a team or for none.
type sender struct {
	team   Bot // the zero Bot when the user hands over for no team
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

// HandOver gives team toTeamID a goal of its own: it waits as an incoming
// goal until its lead takes it in, and costs the sender nothing. a is the
// lead of team fromTeamID in a turn in it, in any state of its board, or the
// user, for fromTeamID or for no team ("").
func (s *Service) HandOver(ctx context.Context, a Actor, fromTeamID, toTeamID string, h NewHandoff) (Goal, error) {
	from, err := s.handoffSender(ctx, a, fromTeamID)
	if err != nil {
		return Goal{}, err
	}
	to, err := s.team(ctx, toTeamID)
	if err != nil {
		return Goal{}, err
	}
	if _, ok := to.Lead(); !ok || to.ID == from.team.ID {
		return Goal{}, invalid("hand over to a team other than yours that has members")
	}
	if h, err = s.validHandoff(ctx, h); err != nil {
		return Goal{}, err
	}
	var g Goal
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		g, err = s.insertHandoff(ctx, q, from, to, h)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.postNotes(ctx, []noteTo{
		{to.ID, fmt.Sprintf("%s handed over a goal: %s", from.name(), g.Text)},
		{from.team.ID, fmt.Sprintf("%s handed over to team %s: %s", s.senderName(from), to.Name, g.Text)},
	})
}

// senderName names who handed over, in the sender's channel.
func (s *Service) senderName(from sender) string {
	if from.botID == "" {
		return "The user"
	}
	return memberName(from.team, from.botID)
}

// insertHandoff adds an incoming goal for team to with q, and wakes its lead
// to take it in, in a turn that no goal pays for.
func (s *Service) insertHandoff(ctx context.Context, q *db.Queries, from sender, to Bot, h NewHandoff) (Goal, error) {
	incoming, err := q.ListIncomingGoals(ctx, to.ID)
	if err != nil {
		return Goal{}, fmt.Errorf("list the handoffs to team %s: %w", to.ID, err)
	}
	if len(incoming) >= MaxIncoming {
		return Goal{}, invalid("%s has %d handoffs waiting; ask the user.", to.Name, MaxIncoming)
	}
	ids, err := json.Marshal(append([]string{}, h.ArtifactIDs...))
	if err != nil {
		return Goal{}, fmt.Errorf("encode the artifacts: %w", err)
	}
	row, err := q.InsertHandoffGoal(ctx, db.InsertHandoffGoalParams{
		ID: uuid.NewString(), TeamID: to.ID, Text: h.Text, CreatedAt: s.nowMs(), FromTeamID: from.team.ID, FromBotID: from.botID,
		FromGoalID: from.goalID, Brief: h.Brief, ArtifactIds: string(ids), Depth: from.depth,
	})
	if err != nil {
		return Goal{}, fmt.Errorf("hand a goal over to team %s: %w", to.ID, err)
	}
	g := toGoal(row, 0)
	return g, s.wakeLead(ctx, q, to, Goal{}, fmt.Sprintf("%s handed over a goal to your team: %q. Accept it, ask its sender, or decline it.", from.name(), g.Text))
}

// handoffSender checks who hands a goal over for team fromTeamID: its lead
// in a turn in the team, or the user. A lead's handoff is one deeper than the
// goal that its turn works on.
func (s *Service) handoffSender(ctx context.Context, a Actor, fromTeamID string) (sender, error) {
	if a.Role == RoleUser {
		return s.userSender(ctx, fromTeamID)
	}
	if fromTeamID == "" {
		fromTeamID = a.TeamID
	}
	if a.TeamID != fromTeamID {
		return sender{}, invalid("hand work over only in a turn in your team")
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
		return sender{}, invalid("This work came through %d handoffs; ask the user before passing it on.", MaxHandoffDepth)
	}
	return sender{team: team, botID: a.BotID, goalID: g.ID, depth: g.Depth + 1}, nil
}

// userSender is the user handing a goal over, for team fromTeamID or for none.
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

// turnGoal returns the goal that a turn in team works on: the turn's own
// goal, else the team's open goal; the zero Goal for none.
func (s *Service) turnGoal(ctx context.Context, turnID, teamID string) (Goal, error) {
	id, err := s.q.GetTurnGoal(ctx, turnID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Goal{}, fmt.Errorf("get the goal of turn %s: %w", turnID, err)
	}
	if id != "" {
		return s.goal(ctx, s.q, id)
	}
	g, ok, err := s.latestGoal(ctx, teamID)
	if err != nil || !ok || !g.Open() {
		return Goal{}, err
	}
	return g, nil
}

// validHandoff checks a handoff's texts and artifacts and cleans them.
func (s *Service) validHandoff(ctx context.Context, h NewHandoff) (NewHandoff, error) {
	var err error
	if h.Text, err = textOf("the goal", h.Text, maxGoalText, true); err != nil {
		return NewHandoff{}, err
	}
	if h.Brief, err = textOf("the brief", h.Brief, maxItemText, false); err != nil {
		return NewHandoff{}, err
	}
	h.ArtifactIDs = slices.Compact(slices.Sorted(slices.Values(h.ArtifactIDs)))
	if len(h.ArtifactIDs) > maxHandoffArtifacts {
		return NewHandoff{}, invalid("a handoff carries at most %d artifacts", maxHandoffArtifacts)
	}
	for _, id := range h.ArtifactIDs {
		if _, err := s.GetArtifact(ctx, id); err != nil {
			return NewHandoff{}, err
		}
	}
	return h, nil
}

// handoff returns an incoming handoff with its team, the team that receives it.
func (s *Service) handoff(ctx context.Context, goalID string) (Goal, Bot, error) {
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	if g.Origin != OriginHandoff {
		return Goal{}, Bot{}, invalid("%q is not a handoff", g.Text)
	}
	team, err := s.team(ctx, g.TeamID)
	return g, team, err
}

// incoming returns a handoff that waits for the actor's team to take it in.
// a is the receiving lead, in a turn in its team, or the user.
func (s *Service) incoming(ctx context.Context, a Actor, goalID string) (Goal, Bot, error) {
	g, team, err := s.handoff(ctx, goalID)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	if a.Role != RoleUser && !a.leadActs(team) {
		return Goal{}, Bot{}, invalid("only the lead of %s takes its handoffs in, in a turn in the team", team.Name)
	}
	if g.State != GoalIncoming {
		return Goal{}, Bot{}, invalid("%q is %s; only an incoming handoff is taken in", g.Text, g.State)
	}
	return g, team, nil
}

// sent returns a handoff with its sending team. a is the sending lead, in a
// turn in its team, or the user. The team is the zero Bot when the user
// handed the goal over for no team, or the team is gone.
func (s *Service) sent(ctx context.Context, a Actor, goalID string) (Goal, Bot, error) {
	g, _, err := s.handoff(ctx, goalID)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, Bot{}, err
	}
	if a.Role != RoleUser && (from.ID == "" || !a.leadActs(from)) {
		return Goal{}, Bot{}, invalid("only the lead of the team that handed %q over does this, in a turn in its team", g.Text)
	}
	return g, from, nil
}

// senderTeam returns the team that handed g over, or the zero Bot when there
// is none any more.
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

// AcceptHandoff takes a handoff in: it joins the team's goal queue, and when
// the team is free it starts at once, unless the team waits for the user to
// start handoffs.
func (s *Service) AcceptHandoff(ctx context.Context, a Actor, goalID string) (Goal, error) {
	g, team, err := s.incoming(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	var started bool
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		g, started, err = s.acceptHandoff(ctx, q, team, g)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	where := fmt.Sprintf("it is #%d in the queue", g.QueuePos)
	if started {
		where = "it started"
	}
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, err
	}
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("%s accepted the handoff %q; %s.", actorName(team, a), g.Text, where)},
		{from.ID, fmt.Sprintf("Team %s accepted your handoff %q; %s.", team.Name, g.Text, where)},
	})
}

// acceptHandoff puts handoff g at the end of team's queue with q, and starts
// it when the team has no open goal and no queue. It returns the goal as it
// is now and whether it started.
func (s *Service) acceptHandoff(ctx context.Context, q *db.Queries, team Bot, g Goal) (Goal, bool, error) {
	_, open, err := openGoalWith(ctx, q, team.ID)
	if err != nil {
		return Goal{}, false, err
	}
	queue, err := queuedGoals(ctx, q, team.ID)
	if err != nil {
		return Goal{}, false, err
	}
	if len(queue) >= MaxQueuedGoals {
		return Goal{}, false, invalid("%s has %d goals queued; cancel one first", team.Name, MaxQueuedGoals)
	}
	pos := int64(len(queue) + 1)
	n, err := q.AcceptHandoffGoal(ctx, db.AcceptHandoffGoalParams{State: GoalQueued, QueuePos: pos, ID: g.ID})
	if err != nil {
		return Goal{}, false, fmt.Errorf("accept handoff %s: %w", g.ID, err)
	}
	if n == 0 {
		return Goal{}, false, invalid("the handoff changed meanwhile; look at the board again")
	}
	g.State, g.QueuePos, g.AskedAt = GoalQueued, pos, 0
	if open || len(queue) > 0 {
		return g, false, nil
	}
	next, started, err := s.nextGoal(ctx, q, team.ID, false)
	if err != nil || !started {
		return g, false, err
	}
	return next, true, nil
}

// holds reports whether team waits for the user to start goal g, the head of
// its queue: a handoff, in a team that starts handoffs only when asked.
func holds(team Bot, g Goal) bool {
	return g.Origin == OriginHandoff && team.Team.StartHandoffs == StartAsk
}

// AskSender asks the sender of an incoming handoff a question, at most
// MaxAskRounds times. Then the user settles it: the next question fails,
// wakes no one, and asks the user in the team's channel.
func (s *Service) AskSender(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	if a.Role == RoleUser {
		return Goal{}, invalid("the receiving lead asks the sender; answer the handoff instead")
	}
	g, team, err := s.incoming(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
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
		if g.FromBotID == "" || from.ID == "" {
			return nil
		}
		return s.wakeLead(ctx, q, from, Goal{}, fmt.Sprintf("Team %s asks about your handoff %q: %s Answer it with answer_handoff.", team.Name, g.Text, text))
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

// setAsk records with q that the receiving lead asked about handoff g for
// the given round. It fails when another question came meanwhile.
func (s *Service) setAsk(ctx context.Context, q *db.Queries, g Goal, note string, rounds int64) error {
	n, err := q.AskAboutHandoff(ctx, db.AskAboutHandoffParams{IntakeNote: note, AskedAt: s.nowMs(), AskRounds: rounds, ID: g.ID, FromRounds: g.AskRounds})
	if err != nil {
		return fmt.Errorf("ask about handoff %s: %w", g.ID, err)
	}
	if n == 0 {
		return invalid("the handoff changed meanwhile; look at the board again")
	}
	return nil
}

// askNotes shows a question about handoff g: in the sender's channel, or as
// a question to the user when the user handed it over.
func (s *Service) askNotes(ctx context.Context, a Actor, team, from Bot, g Goal) error {
	asked := fmt.Sprintf("%s asked about the handoff %q (%d of %d): %s", memberName(team, a.BotID), g.Text, g.AskRounds, MaxAskRounds, g.IntakeNote)
	if err := s.boardNote(ctx, team.ID, asked); err != nil {
		return err
	}
	if g.FromBotID == "" {
		return s.askUser(ctx, a, team, fmt.Sprintf("A question about the handoff %q: %s", g.Text, g.IntakeNote))
	}
	return s.postNotes(ctx, []noteTo{{from.ID, fmt.Sprintf("Team %s asks about your handoff %q: %s", team.Name, g.Text, g.IntakeNote)}})
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
	refused := invalid("You asked %s %d times about %s; accept it, decline it, or ask the user.", senderName, MaxAskRounds, g.Text)
	if g.NeedsUser {
		return refused
	}
	if err := s.setAsk(ctx, s.q, g, g.IntakeNote, MaxAskRounds+1); err != nil {
		return err
	}
	s.hub.Publish(Event{Type: "bots"})
	question := fmt.Sprintf("%s and %s couldn't settle %q in %d rounds: %s Accept, decline or answer it?",
		memberName(team, a.BotID), senderName, g.Text, MaxAskRounds, g.IntakeNote)
	return errors.Join(refused, s.askUser(ctx, a, team, question))
}

// AnswerHandoff answers the receiver's question about a handoff: the answer
// joins its brief, and the receiving lead takes it in again. a is the
// sending lead, while a question waits, or the user at any time.
func (s *Service) AnswerHandoff(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	g, from, err := s.sent(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	if g.State != GoalIncoming {
		return Goal{}, invalid("%q is %s; only an incoming handoff is answered", g.Text, g.State)
	}
	if a.Role != RoleUser && (g.AskedAt == 0 || g.NeedsUser) {
		return Goal{}, invalid("no question about %q waits for you", g.Text)
	}
	if text, err = textOf("the answer", text, maxItemText, true); err != nil {
		return Goal{}, err
	}
	who := actorName(from, a)
	brief, err := textOf("the brief with the answer", g.Brief+"\n\nAnswer from "+who+": "+text, maxItemText, true)
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
			return fmt.Errorf("answer handoff %s: %w", g.ID, err)
		}
		if n == 0 {
			return invalid("the handoff changed meanwhile; look at the board again")
		}
		return s.wakeLead(ctx, q, team, Goal{}, fmt.Sprintf("%s answered your question about the handoff %q: %s Accept it, ask again, or decline it.", who, g.Text, text))
	})
	if err != nil {
		return Goal{}, err
	}
	g.Brief, g.AskedAt = brief, 0
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("%s answered about the handoff %q: %s", who, g.Text, text)},
		{from.ID, fmt.Sprintf("%s answered team %s about the handoff %q: %s", who, team.Name, g.Text, text)},
	})
}

// DeclineHandoff declines an incoming handoff with a reason. It never runs;
// the sending lead hears why.
func (s *Service) DeclineHandoff(ctx context.Context, a Actor, goalID, text string) (Goal, error) {
	g, team, err := s.incoming(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	if text, err = textOf("the reason", text, maxItemText, true); err != nil {
		return Goal{}, err
	}
	from, err := s.senderTeam(ctx, g)
	if err != nil {
		return Goal{}, err
	}
	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.DeclineHandoffGoal(ctx, db.DeclineHandoffGoalParams{IntakeNote: text, ID: g.ID})
		if err != nil {
			return fmt.Errorf("decline handoff %s: %w", g.ID, err)
		}
		if n == 0 {
			return invalid("the handoff changed meanwhile; look at the board again")
		}
		return s.wakeSender(ctx, q, g, from, fmt.Sprintf("Team %s declined your handoff %q: %s", team.Name, g.Text, text))
	})
	if err != nil {
		return Goal{}, err
	}
	g.State, g.IntakeNote, g.AskedAt = GoalDeclined, text, 0
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("%s declined the handoff %q: %s", actorName(team, a), g.Text, text)},
		{from.ID, fmt.Sprintf("Team %s declined your handoff %q: %s", team.Name, g.Text, text)},
	})
}

// wakeSender wakes the lead of the team that handed g over, with q, in a
// turn that no goal pays for. The user's handoffs wake no lead.
func (s *Service) wakeSender(ctx context.Context, q *db.Queries, g Goal, from Bot, reason string) error {
	if g.FromBotID == "" || from.ID == "" {
		return nil
	}
	return s.wakeLead(ctx, q, from, Goal{}, reason+" Act on it if your team needs to; otherwise note it and stop.")
}

// WithdrawHandoff takes back a handoff that has not started. a is the
// sending lead or the user; a started one only the user ends.
func (s *Service) WithdrawHandoff(ctx context.Context, a Actor, goalID string) (Goal, error) {
	g, from, err := s.sent(ctx, a, goalID)
	if err != nil {
		return Goal{}, err
	}
	if g.State != GoalIncoming && g.State != GoalQueued {
		return Goal{}, invalid("%q is %s; only a handoff that has not started is withdrawn, and the user ends one that has", g.Text, g.State)
	}
	team, err := s.team(ctx, g.TeamID)
	if err != nil {
		return Goal{}, err
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
		return Goal{}, err
	}
	g.State, g.QueuePos = GoalEnded, 0
	s.hub.Publish(Event{Type: "bots"})
	who := actorName(from, a)
	return g, s.postNotes(ctx, []noteTo{
		{team.ID, fmt.Sprintf("Withdrawn by %s: the handoff %q.", who, g.Text)},
		{from.ID, fmt.Sprintf("%s withdrew the handoff %q from team %s.", who, g.Text, team.Name)},
	})
}

// handoffResult tells the team that handed goal g over what became of it:
// it was delivered, accepted or ended. On a delivery its lead wakes, in a
// turn that no goal pays for.
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
	note := fmt.Sprintf("Team %s %s your handoff %q", team.Name, text, g.Text)
	if wake {
		err = s.inTx(ctx, func(q *db.Queries) error { return s.wakeSender(ctx, q, g, from, note) })
		s.signalTurns()
	}
	return errors.Join(err, s.boardNote(ctx, from.ID, note))
}

// fillHandoffs adds the handoffs that wait for a team, and those that it
// handed over lately, to its board.
func (s *Service) fillHandoffs(ctx context.Context, teamID string, board *Board) error {
	incoming, err := s.q.ListIncomingGoals(ctx, teamID)
	if err != nil {
		return fmt.Errorf("list the handoffs to team %s: %w", teamID, err)
	}
	for _, row := range incoming {
		board.Incoming = append(board.Incoming, toGoal(row, 0))
	}
	// The handoffs of the last 30 days.
	since := s.now().Add(-30 * 24 * time.Hour).UnixMilli()
	sent, err := s.q.ListHandedOverGoals(ctx, db.ListHandedOverGoalsParams{FromTeamID: teamID, Since: since})
	if err != nil {
		return fmt.Errorf("list the handoffs of team %s: %w", teamID, err)
	}
	for _, row := range sent {
		g, err := s.goal(ctx, s.q, row.ID)
		if err != nil {
			return err
		}
		board.HandedOver = append(board.HandedOver, g)
	}
	return s.nameHandoffTeams(ctx, board)
}

// nameHandoffTeams names the teams that a board's handoffs come from or went
// to; a team that is gone is "a deleted team".
func (s *Service) nameHandoffTeams(ctx context.Context, board *Board) error {
	ids := []string{}
	for _, g := range board.Incoming {
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

// incomingCounts maps each team to how many handoffs wait for it.
func (s *Service) incomingCounts(ctx context.Context) (map[string]int64, error) {
	rows, err := s.q.CountIncomingGoals(ctx)
	if err != nil {
		return nil, fmt.Errorf("count the incoming handoffs: %w", err)
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
