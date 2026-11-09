package core

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"yargpt/internal/db"
)

// The intake of a request: how the serving team took it.
const (
	IntakeRequested = "requested" // waits for the serving lead's triage
	IntakeAccepted  = "accepted"  // has an owner; works while it holds a slot
	IntakeDeclined  = "declined"  // dropped by the serving lead, with a note
	IntakeDeferred  = "deferred"  // put off until its revisit, with a note
	IntakeWithdrawn = "withdrawn" // taken back by the asking team, or its goal ended
)

// When a deferred request comes back.
const (
	RevisitGoalEnd = "goal_end" // when the serving team's own goal is accepted or ended
	RevisitDate    = "date"     // at DeferUntil
	RevisitManual  = "manual"   // when the serving lead or the user brings it back
)

// The codes of a request's reason line. The UI and the prompts show Text.
const (
	ReasonTriage       = "triage"
	ReasonOwnGoalFirst = "own_goal_first"
	ReasonBehind       = "behind"
	ReasonJumped       = "jumped"
	ReasonInSlot       = "in_slot"
	ReasonAskingPaused = "asking_paused"
	ReasonDeferred     = "deferred"
	ReasonDeclined     = "declined"
	ReasonWithdrawn    = "withdrawn"
	ReasonDone         = "done"
)

// Reason says why a request is where it is.
type Reason struct {
	Code string `json:"code"`
	Text string `json:"text"`
	// WaitsOn adds what the request's work waits on in other teams.
	WaitsOn string `json:"waitsOn,omitempty"`
}

// Slots is how a team's request slots are used. Applies is false while the
// team has no active goal of its own: then every accepted request works.
type Slots struct {
	Limit   int64 `json:"limit"`
	Used    int64 `json:"used"`
	Applies bool  `json:"applies"`
}

// closed reports whether an item is done or dropped.
func closed(it Item) bool { return it.State == ItemDone || it.State == ItemDropped }

// holdsSlot reports whether a request works in one of its team's slots.
func holdsSlot(it Item) bool { return it.Intake == IntakeAccepted && it.SlotAt > 0 && !closed(it) }

// mayRun reports whether an item's owner and reviewer may get turns: a
// team's own item always, a request only while it holds a slot.
func mayRun(it Item) bool { return it.Intake == "" || holdsSlot(it) }

// waitsForSlot reports whether a request is accepted and waits for a slot.
func waitsForSlot(it Item) bool { return it.Intake == IntakeAccepted && it.SlotAt == 0 && !closed(it) }

// laneRequest is a request in a serving team's lane.
type laneRequest struct {
	Item
	from      string // the asking goal's team
	goalState string // the asking goal's state
}

// lane is the requests of other teams' open goals to a team.
type lane struct {
	team    Bot
	ownGoal string // the team's open goal, or ""
	slots   Slots
	reqs    []laneRequest // in arrival order
}

// laneWith loads the lane of a team with q; a bot that is no team has an
// empty lane.
func laneWith(ctx context.Context, q *db.Queries, teamID string) (lane, error) {
	if _, err := q.GetTeam(ctx, teamID); errors.Is(err, sql.ErrNoRows) {
		return lane{}, nil
	}
	team, err := teamWith(ctx, q, teamID)
	if err != nil {
		return lane{}, err
	}
	l := lane{team: team, slots: Slots{Limit: team.Team.RequestSlots}}
	row, open, err := openGoalWith(ctx, q, teamID)
	if err != nil {
		return lane{}, err
	}
	if open {
		l.ownGoal, l.slots.Applies = row.ID, row.State == GoalActive
	}
	rows, err := q.ListTeamRequests(ctx, teamID)
	if err != nil {
		return lane{}, fmt.Errorf("list the requests to team %s: %w", teamID, err)
	}
	for _, r := range rows {
		it, err := toItem(r.Item)
		if err != nil {
			return lane{}, err
		}
		l.reqs = append(l.reqs, laneRequest{Item: it, from: r.FromTeamID, goalState: r.GoalState})
		if holdsSlot(it) {
			l.slots.Used++
		}
	}
	return l, nil
}

// waiting returns the accepted requests that wait for a slot in the order
// they get one: those taken out of order first, then by arrival.
func (l lane) waiting() []laneRequest {
	var out []laneRequest
	for _, r := range l.reqs {
		if waitsForSlot(r.Item) {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b laneRequest) int {
		return cmp.Compare(jumpKey(a.Item), jumpKey(b.Item))
	})
	return out
}

// jumpKey orders a request taken out of order by when, before the others.
func jumpKey(it Item) int64 {
	if it.JumpedAt > 0 {
		return it.JumpedAt
	}
	return 1 << 62
}

// grantSlots gives the requests to a team that wait for a slot the slots
// that are free, with q, and queues their turns. While the team has no active
// goal of its own, every waiting request gets one. A request of a paused
// goal, or one that waits on nothing yet, gets none.
func (s *Service) grantSlots(ctx context.Context, q *db.Queries, teamID string) error {
	l, err := laneWith(ctx, q, teamID)
	if err != nil {
		return err
	}
	for _, r := range l.waiting() {
		if l.slots.Applies && l.slots.Used >= l.slots.Limit {
			return nil
		}
		ok, err := s.grantSlot(ctx, q, r)
		if err != nil {
			return err
		}
		if ok {
			l.slots.Used++
		}
	}
	return nil
}

// grantSlot gives one waiting request a slot when its goal is active and its
// work can start, and queues the turn of the member it waits on.
func (s *Service) grantSlot(ctx context.Context, q *db.Queries, r laneRequest) (bool, error) {
	if r.goalState != GoalActive {
		return false, nil
	}
	items, err := goalItems(ctx, q, r.GoalID)
	if err != nil {
		return false, err
	}
	if member, _ := waitsOn(r.Item, items); member == "" {
		return false, nil
	}
	it := r.Item
	it.SlotAt = s.nowMs()
	if err := q.SetItemSlot(ctx, db.SetItemSlotParams{SlotAt: it.SlotAt, ID: it.ID}); err != nil {
		return false, fmt.Errorf("give item %s a slot: %w", it.ID, err)
	}
	g, err := s.goal(ctx, q, it.GoalID)
	if err != nil {
		return false, err
	}
	return true, s.requeue(ctx, q, g, it, items)
}

// releaseSlot frees the slot of an item that no longer holds one with q.
func releaseSlot(ctx context.Context, q *db.Queries, it *Item) error {
	if holdsSlot(*it) {
		return nil
	}
	it.SlotAt = 0
	if err := q.SetItemSlot(ctx, db.SetItemSlotParams{SlotAt: 0, ID: it.ID}); err != nil {
		return fmt.Errorf("free the slot of item %s: %w", it.ID, err)
	}
	return nil
}

// servingTeams returns the other teams that work on items of goal g.
func servingTeams(g Goal, items []Item) []string {
	var ids []string
	for _, it := range items {
		if it.TeamID != g.TeamID && !slices.Contains(ids, it.TeamID) {
			ids = append(ids, it.TeamID)
		}
	}
	return ids
}

// grantGoalSlots runs grantSlots for the teams that serve goal g.
func (s *Service) grantGoalSlots(ctx context.Context, q *db.Queries, g Goal, items []Item) error {
	for _, id := range servingTeams(g, items) {
		if err := s.grantSlots(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

// goalChanged keeps the slots right after goal g moved to state, with q: a
// paused goal frees the slots of its requests, the goal's team gets or loses
// its slot limit, and the teams that serve the goal may start work.
func (s *Service) goalChanged(ctx context.Context, q *db.Queries, g Goal, state string) error {
	if state == GoalPaused {
		if err := q.ReleaseGoalSlots(ctx, g.ID); err != nil {
			return fmt.Errorf("free the slots of goal %s: %w", g.ID, err)
		}
	}
	if err := s.grantSlots(ctx, q, g.TeamID); err != nil {
		return err
	}
	items, err := goalItems(ctx, q, g.ID)
	if err != nil {
		return err
	}
	return s.grantGoalSlots(ctx, q, g, items)
}

// noteTo is a board note for a team's channel.
type noteTo struct{ teamID, text string }

func (s *Service) postNotes(ctx context.Context, notes []noteTo) error {
	for _, n := range notes {
		if err := s.boardNote(ctx, n.teamID, n.text); err != nil {
			return err
		}
	}
	return nil
}

// askingTeam returns the team that asked for request it of goal g: the team
// of the request that a chained request serves, else the goal's team.
func askingTeam(ctx context.Context, q *db.Queries, it Item, g Goal) (Bot, error) {
	id := g.TeamID
	if it.ForItemID != "" {
		row, err := q.GetItem(ctx, it.ForItemID)
		if err != nil {
			return Bot{}, fmt.Errorf("get item %s: %w", it.ForItemID, err)
		}
		id = row.TeamID
	}
	return teamWith(ctx, q, id)
}

// earlier is what a request skips when its lead accepts it: the titles of
// the requests to its team that arrived before it and wait for triage, and
// how many accepted ones wait for a slot.
type earlier struct {
	triage  []string
	waiting int
}

func (s *Service) earlierRequests(ctx context.Context, itemID string) (earlier, error) {
	row, err := s.q.GetItem(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return earlier{}, nil
	}
	if err != nil {
		return earlier{}, fmt.Errorf("get item %s: %w", itemID, err)
	}
	if row.Intake != IntakeRequested {
		return earlier{}, nil
	}
	l, err := laneWith(ctx, s.q, row.TeamID)
	if err != nil {
		return earlier{}, err
	}
	var e earlier
	for _, r := range l.reqs {
		if r.ID == itemID {
			break
		}
		if r.Intake == IntakeRequested {
			e.triage = append(e.triage, fmt.Sprintf("%q", r.Title))
		}
		if waitsForSlot(r.Item) {
			e.waiting++
		}
	}
	return e, nil
}

// accept takes a request in at the lead's assign. Skipping a request that
// waits for triage needs a note; a note that skips any waiting request takes
// this one out of order, so it gets the next slot.
func accept(it *Item, e earlier, note string, now int64) error {
	text, err := textOf("the note", note, maxItemNote, false)
	if err != nil {
		return err
	}
	if len(e.triage) > 0 && text == "" {
		return invalid("Earlier requests wait: %s. Accept them first, or give a note on why this one goes first.", strings.Join(e.triage, ", "))
	}
	it.Intake = IntakeAccepted
	if text != "" {
		it.IntakeNote = text
	}
	if text != "" && len(e.triage)+e.waiting > 0 {
		it.JumpedAt = now
	}
	return nil
}

// DeferItem puts a request off with a note until its revisit: the end of the
// serving team's own goal, the time until (ms, RevisitDate) or a manual
// undefer. Its owner stops, its slot is free, and the asking lead re-plans.
func (s *Service) DeferItem(ctx context.Context, a Actor, itemID, note, revisit string, until int64) (Item, error) {
	now := s.nowMs()
	return s.moveItem(ctx, a, itemID, func(team Bot, it *Item) error {
		if err := a.mustLead(team); err != nil {
			return err
		}
		if (it.Intake != IntakeRequested && it.Intake != IntakeAccepted) || closed(*it) {
			return invalid("only an open request of another team is deferred; %q is not", it.Title)
		}
		text, err := textOf("the note", note, maxItemNote, true)
		if err != nil {
			return err
		}
		if until, err = deferUntil(revisit, until, now); err != nil {
			return err
		}
		it.Intake, it.IntakeNote, it.DeferRevisit, it.DeferUntil = IntakeDeferred, text, revisit, until
		it.State, it.BlockedFrom = ItemTodo, ""
		return nil
	})
}

// deferUntil checks the revisit of a deferral and returns its time.
func deferUntil(revisit string, until, now int64) (int64, error) {
	switch revisit {
	case RevisitGoalEnd, RevisitManual:
		return 0, nil
	case RevisitDate:
		if until <= now {
			return 0, invalid("Pick a time in the future.")
		}
		return until, nil
	}
	return 0, invalid("revisit must be %s, %s or %s", RevisitGoalEnd, RevisitDate, RevisitManual)
}

// UndeferItem brings a deferred request back to triage at its arrival
// place: the serving lead does it, or the user from the serving board.
func (s *Service) UndeferItem(ctx context.Context, a Actor, itemID string) (Item, error) {
	it, team, g, err := s.loadRequest(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if a.Role != RoleUser {
		if err := s.leadOn(ctx, a, team, g); err != nil {
			return Item{}, err
		}
	}
	if it.Intake != IntakeDeferred {
		return Item{}, invalid("%q is not deferred", it.Title)
	}
	var notes []noteTo
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		it, notes, err = s.undeferWith(ctx, q, team, g, a, it, actorName(team, a)+" brought %q back from deferral.")
		return err
	})
	if err != nil {
		return Item{}, err
	}
	s.signalTurns()
	return it, s.postNotes(ctx, notes)
}

// leadOn checks that bot actor a is the lead of team, in a turn in the team,
// for open goal g.
func (s *Service) leadOn(ctx context.Context, a Actor, team Bot, g Goal) error {
	if _, _, err := s.boardOf(ctx, a, team.ID, g.ID); err != nil {
		return err
	}
	return a.mustLead(team)
}

// loadRequest loads a request of an open goal with its serving team.
func (s *Service) loadRequest(ctx context.Context, itemID string) (Item, Bot, Goal, error) {
	row, err := s.q.GetItem(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, Bot{}, Goal{}, notFound("item", itemID)
	}
	if err != nil {
		return Item{}, Bot{}, Goal{}, fmt.Errorf("get item %s: %w", itemID, err)
	}
	it, err := toItem(row)
	if err != nil {
		return Item{}, Bot{}, Goal{}, err
	}
	if it.Intake == "" {
		return Item{}, Bot{}, Goal{}, invalid("%q is not a request of another team", it.Title)
	}
	team, g, err := s.boardOf(ctx, Actor{}, it.TeamID, it.GoalID)
	if err != nil {
		return Item{}, Bot{}, Goal{}, err
	}
	return it, team, g, nil
}

// actorName names who acts for a channel note.
func actorName(team Bot, a Actor) string {
	if a.BotID != "" {
		return memberName(team, a.BotID)
	}
	if a.Role == RoleUser {
		return "The user"
	}
	return "The app"
}

// undeferWith brings deferred request it of goal g in serving team back to
// triage with q, and wakes the serving lead unless it acts itself. format
// gets the title for the note in both channels. It returns no notes when the
// item moved meanwhile.
func (s *Service) undeferWith(ctx context.Context, q *db.Queries, team Bot, g Goal, a Actor, it Item, format string) (Item, []noteTo, error) {
	from, fromIntake := it.State, it.Intake
	it.Intake, it.IntakeNote, it.DeferRevisit, it.DeferUntil, it.JumpedAt = IntakeRequested, "", "", 0, 0
	it.OwnerID, it.ReviewerID, it.State, it.BlockedFrom, it.UpdatedAt = "", "", ItemTodo, "", s.nowMs()
	n, err := updateItem(ctx, q, it, from, fromIntake)
	if err != nil || n == 0 {
		return it, nil, err
	}
	if err := releaseSlot(ctx, q, &it); err != nil {
		return it, nil, err
	}
	if a.Role == RoleUser {
		a = Actor{} // the user's change wakes the serving lead to triage it
	}
	if err := s.wake(ctx, q, team, g, a, it); err != nil {
		return it, nil, err
	}
	text := fmt.Sprintf(format, it.Title)
	return it, []noteTo{{team.ID, text}, {g.TeamID, "Team " + team.Name + ": " + text}}, nil
}

// updateItem stores an item's changes with q when its state and intake are
// still from and fromIntake, and returns how many rows changed.
func updateItem(ctx context.Context, q *db.Queries, it Item, from, fromIntake string) (int64, error) {
	n, err := q.UpdateItem(ctx, db.UpdateItemParams{
		State: it.State, OwnerID: it.OwnerID, ReviewerID: it.ReviewerID, Result: it.Result, Note: it.Note,
		BlockedFrom: it.BlockedFrom, ReviewRounds: it.ReviewRounds, UpdatedAt: it.UpdatedAt,
		Intake: it.Intake, IntakeNote: it.IntakeNote, DeferRevisit: it.DeferRevisit, DeferUntil: it.DeferUntil, JumpedAt: it.JumpedAt,
		ID: it.ID, FromState: from, FromIntake: fromIntake,
	})
	if err != nil {
		return 0, fmt.Errorf("update item %s: %w", it.ID, err)
	}
	return n, nil
}

// revisitGoalEnd brings back, with q, the requests to a team that were
// deferred until its own goal ends.
func (s *Service) revisitGoalEnd(ctx context.Context, q *db.Queries, teamID string) ([]noteTo, error) {
	rows, err := q.ListGoalEndDeferrals(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list the deferrals to team %s: %w", teamID, err)
	}
	return s.revisit(ctx, q, rows, "%q is back from deferral: the team's goal ended.")
}

// revisit brings deferred requests back to triage with q.
func (s *Service) revisit(ctx context.Context, q *db.Queries, rows []db.Item, format string) ([]noteTo, error) {
	var notes []noteTo
	for _, row := range rows {
		it, err := toItem(row)
		if err != nil {
			return nil, err
		}
		team, err := teamWith(ctx, q, it.TeamID)
		if err != nil {
			return nil, err
		}
		g, err := s.goal(ctx, q, it.GoalID)
		if err != nil {
			return nil, err
		}
		_, more, err := s.undeferWith(ctx, q, team, g, Actor{}, it, format)
		if err != nil {
			return nil, err
		}
		notes = append(notes, more...)
	}
	return notes, nil
}

// revisitDue brings back the requests whose deferral date has come.
func (s *Service) revisitDue(ctx context.Context) error {
	var notes []noteTo
	err := s.inTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListDueDeferrals(ctx, s.nowMs())
		if err != nil {
			return fmt.Errorf("list the due deferrals: %w", err)
		}
		notes, err = s.revisit(ctx, q, rows, "%q is back from deferral: its time came.")
		return err
	})
	if err != nil || len(notes) == 0 {
		return err
	}
	s.signalTurns()
	return s.postNotes(ctx, notes)
}

// WithdrawItem takes back an open request: the lead of the team that asked
// for it does it, or the user. The requests that it chained to other teams
// are withdrawn with it.
func (s *Service) WithdrawItem(ctx context.Context, a Actor, itemID string) (Item, error) {
	it, team, g, err := s.loadRequest(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	if closed(it) {
		return Item{}, invalid("%q is %s already", it.Title, it.State)
	}
	asking, err := askingTeam(ctx, s.q, it, g)
	if err != nil {
		return Item{}, err
	}
	who := "the user"
	if a.Role != RoleUser {
		if err := s.leadOn(ctx, a, asking, g); err != nil {
			return Item{}, invalid("only the lead of %s, which asked for %q, or the user withdraws it", asking.Name, it.Title)
		}
		who = "team " + asking.Name
	}
	var notes []noteTo
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		notes, err = s.withdrawWith(ctx, q, team, it, "Withdrawn by "+who+".", fmt.Sprintf("%s withdrew %q.", upperFirst(who), it.Title))
		return err
	})
	if err != nil {
		return Item{}, err
	}
	notes = append(notes, noteTo{asking.ID, fmt.Sprintf("%s withdrew %q from team %s.", actorName(asking, a), it.Title, team.Name)})
	it.Intake, it.State, it.IntakeNote = IntakeWithdrawn, ItemDropped, "Withdrawn by "+who+"."
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return it, s.postNotes(ctx, notes)
}

func upperFirst(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// withdrawWith withdraws open request it of serving team with q: it is
// dropped with reason, its slot and turns are freed, and the requests that
// it chained are withdrawn too. servingText goes into the serving channel.
// It does nothing when the item moved meanwhile.
func (s *Service) withdrawWith(ctx context.Context, q *db.Queries, team Bot, it Item, reason, servingText string) ([]noteTo, error) {
	from, fromIntake := it.State, it.Intake
	it.Intake, it.State, it.IntakeNote, it.UpdatedAt = IntakeWithdrawn, ItemDropped, reason, s.nowMs()
	n, err := updateItem(ctx, q, it, from, fromIntake)
	if err != nil || n == 0 {
		return nil, err
	}
	if err := releaseSlot(ctx, q, &it); err != nil {
		return nil, err
	}
	if err := q.CancelQueuedItemTurns(ctx, db.CancelQueuedItemTurnsParams{FinishedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, ItemID: it.ID}); err != nil {
		return nil, fmt.Errorf("cancel the turns of item %s: %w", it.ID, err)
	}
	if err := s.grantSlots(ctx, q, team.ID); err != nil {
		return nil, err
	}
	more, err := s.withdrawChain(ctx, q, team, it)
	return append([]noteTo{{team.ID, servingText}}, more...), err
}

// withdrawChain withdraws, with q, the open requests that serving team
// chained for request it.
func (s *Service) withdrawChain(ctx context.Context, q *db.Queries, team Bot, it Item) ([]noteTo, error) {
	chained, err := q.ListChainedItems(ctx, it.ID)
	if err != nil {
		return nil, fmt.Errorf("list the requests chained to item %s: %w", it.ID, err)
	}
	var notes []noteTo
	for _, row := range chained {
		child, err := toItem(row)
		if err != nil {
			return nil, err
		}
		if closed(child) {
			continue
		}
		serving, err := teamWith(ctx, q, child.TeamID)
		if err != nil {
			return nil, err
		}
		more, err := s.withdrawWith(ctx, q, serving, child, fmt.Sprintf("Withdrawn: %q, which it served, was withdrawn.", it.Title),
			fmt.Sprintf("Team %s withdrew %q: %q, which it served, was withdrawn.", team.Name, child.Title, it.Title))
		if err != nil {
			return nil, err
		}
		notes = append(notes, more...)
	}
	return notes, nil
}

// withdrawGoal withdraws, with q, every open request of goal g on other
// teams when g ends, and notes each serving channel.
func (s *Service) withdrawGoal(ctx context.Context, q *db.Queries, g Goal, teamName string) ([]noteTo, error) {
	items, err := goalItems(ctx, q, g.ID)
	if err != nil {
		return nil, err
	}
	var notes []noteTo
	for _, it := range items {
		if it.TeamID == g.TeamID || closed(it) {
			continue
		}
		serving, err := teamWith(ctx, q, it.TeamID)
		if err != nil {
			return nil, err
		}
		more, err := s.withdrawWith(ctx, q, serving, it, fmt.Sprintf("Withdrawn: team %s ended its goal.", teamName),
			fmt.Sprintf("Team %s ended its goal; %q is withdrawn.", teamName, it.Title))
		if err != nil {
			return nil, err
		}
		notes = append(notes, more...)
	}
	return notes, nil
}

// intakeEffects acts on a change of a request's intake with q: a decline
// blocks the asking goal's items that wait on the request, and a decline or
// a deferral wakes the asking lead to re-plan. It returns the notes to post.
func (s *Service) intakeEffects(ctx context.Context, q *db.Queries, team Bot, g Goal, fromIntake string, it Item) ([]noteTo, error) {
	if it.Intake == fromIntake || (it.Intake != IntakeDeclined && it.Intake != IntakeDeferred) {
		return nil, nil
	}
	var notes []noteTo
	if it.Intake == IntakeDeclined {
		var err error
		if notes, err = s.blockDependents(ctx, q, g, it, fmt.Sprintf("Team %s declined %q: %s", team.Name, it.Title, it.IntakeNote)); err != nil {
			return nil, err
		}
	}
	asking, err := askingTeam(ctx, q, it, g)
	if err != nil {
		return nil, err
	}
	reason := fmt.Sprintf("Team %s %s %q: %s. Re-plan the items that wait on it: withdraw it, ask another team, or drop those items.",
		team.Name, it.Intake, it.Title, strings.TrimRight(it.IntakeNote, "."))
	return notes, s.wakeLead(ctx, q, asking, g, reason)
}

// blockDependents blocks, with q, the items of goal g that wait to start on
// it, with note.
func (s *Service) blockDependents(ctx context.Context, q *db.Queries, g Goal, it Item, note string) ([]noteTo, error) {
	items, err := goalItems(ctx, q, g.ID)
	if err != nil {
		return nil, err
	}
	var notes []noteTo
	for _, d := range items {
		if d.State != ItemTodo || !slices.Contains(d.DependsOn, it.ID) {
			continue
		}
		d.BlockedFrom, d.State, d.Note, d.UpdatedAt = ItemTodo, ItemBlocked, note, s.nowMs()
		n, err := updateItem(ctx, q, d, ItemTodo, d.Intake)
		if err != nil {
			return nil, err
		}
		if n == 1 {
			notes = append(notes, noteTo{d.TeamID, fmt.Sprintf("%q is blocked: %s", d.Title, note)})
		}
	}
	return notes, nil
}

// moveText describes a change of an item for the channel, with the change
// of its intake when it has one.
func moveText(team Bot, a Actor, from, fromIntake string, it Item) string {
	if it.Intake == fromIntake {
		return itemEvent(team, a, from, it)
	}
	who := actorName(team, a)
	if it.Intake == IntakeDeclined {
		return fmt.Sprintf("%s declined %q: %s", who, it.Title, it.IntakeNote)
	}
	if it.Intake == IntakeDeferred {
		return fmt.Sprintf("%s deferred %q until %s: %s", who, it.Title, deferWhen(it, team.Name), it.IntakeNote)
	}
	if it.JumpedAt > 0 {
		return fmt.Sprintf("%s took %q out of order for %s: %s", who, it.Title, memberName(team, it.OwnerID), it.IntakeNote)
	}
	return itemEvent(team, a, from, it)
}

// deferWhen says when a deferred request of a team comes back.
func deferWhen(it Item, teamName string) string {
	switch it.DeferRevisit {
	case RevisitGoalEnd:
		return "team " + teamName + "'s current goal ends"
	case RevisitDate:
		return clock(it.DeferUntil)
	}
	return "someone brings it back"
}

// clock shows a time in ms for a note.
func clock(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 UTC")
}
