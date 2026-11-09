package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// Goal and item states, and the roles of board turns, as stored.
const (
	GoalQueued    = "queued" // waits in its team's queue; never open
	GoalActive    = "active"
	GoalDelivered = "delivered"
	GoalPaused    = "paused"
	GoalDone      = "done"
	GoalEnded     = "ended"

	ItemTodo    = "todo"
	ItemDoing   = "doing"
	ItemReview  = "review"
	ItemDone    = "done"
	ItemBlocked = "blocked"
	ItemDropped = "dropped"

	RoleLead   = "lead"   // the lead plans the goal and keeps the board moving
	RoleWork   = "work"   // the owner works on an item
	RoleReview = "review" // the reviewer checks an item
	RoleRetro  = "retro"  // the lead's turn after the user accepts a goal
)

const (
	maxGoalText     = 4000
	maxGoalItems    = 30
	maxItemTitle    = 120
	maxItemText     = 8000 // a brief or a result
	maxItemNote     = 2000 // acceptance criteria or a note
	maxReviewRounds = 3
)

// Goal is a request of the user to a team. The lead splits it into items.
type Goal struct {
	ID     string `json:"id"`
	TeamID string `json:"teamId"`
	Text   string `json:"text"`
	State  string `json:"state"`
	Budget int64  `json:"budget"`
	// Used is how many turns of the goal have started; at Budget, the goal pauses.
	Used int64 `json:"used"`
	// QueuePos is the goal's place in its team's queue, from 1; 0 unless queued.
	QueuePos  int64 `json:"queuePos"`
	CreatedAt int64 `json:"createdAt"`
}

// Open reports whether the goal can still change: it is active, delivered or paused.
func (g Goal) Open() bool {
	return g.State == GoalActive || g.State == GoalDelivered || g.State == GoalPaused
}

// Item is one piece of a goal's work.
type Item struct {
	ID         string `json:"id"`
	TeamID     string `json:"teamId"`
	GoalID     string `json:"goalId"`
	Title      string `json:"title"`
	Brief      string `json:"brief"`
	Acceptance string `json:"acceptance"`
	State      string `json:"state"`
	OwnerID    string `json:"ownerId"`
	ReviewerID string `json:"reviewerId"`
	// DependsOn lists items of the goal that must be done or dropped first.
	DependsOn []string `json:"dependsOn"`
	Result    string   `json:"result"`
	// Note is why the item is blocked, what a review asks to change, or the lead's answer.
	Note string `json:"note"`
	// BlockedFrom is the state of a blocked item before it was blocked.
	BlockedFrom  string `json:"blockedFrom,omitempty"`
	ReviewRounds int64  `json:"reviewRounds"`
	CreatedBy    string `json:"createdBy"` // LearnedByUser or a bot id
	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
	// Intake is how the serving team took a request: IntakeRequested and so
	// on. It is "" for a team's own item.
	Intake string `json:"intake,omitempty"`
	// IntakeNote is the note of a decline, a deferral, an out-of-order
	// accept or a withdrawal, which the asking team sees.
	IntakeNote   string `json:"intakeNote,omitempty"`
	DeferRevisit string `json:"deferRevisit,omitempty"` // RevisitGoalEnd, RevisitDate or RevisitManual
	DeferUntil   int64  `json:"deferUntil,omitempty"`   // ms, for RevisitDate
	// SlotAt is when the request got one of its team's request slots; 0
	// while it holds none. JumpedAt is when its lead took it out of order.
	SlotAt   int64 `json:"slotAt,omitempty"`
	JumpedAt int64 `json:"jumpedAt,omitempty"`
	// ForItemID is the request of another goal that a chained request serves.
	ForItemID string `json:"forItemId,omitempty"`
	// LatestChange is the change request that the owner works on: the latest
	// one, while no submission came after it. EventCount counts the item's
	// history, which starts at HistoryFrom (ms) for items older than it.
	LatestChange *ItemChange `json:"latestChange,omitempty"`
	EventCount   int64       `json:"eventCount"`
	HistoryFrom  int64       `json:"historyFrom,omitempty"`
}

// Board is the open goal of a team, or its latest goal, with the goal's
// items, and the requests of other teams' open goals to the team. Goal is
// nil when the team has had no goal. An item of the goal with another
// TeamID is a request that another team works on.
type Board struct {
	Goal     *Goal     `json:"goal"`
	Items    []Item    `json:"items"`
	Requests []Request `json:"requests"`
	// Queue is the team's queued goals, in order.
	Queue []Goal `json:"queue"`
	// Slots is how the team's request slots are used.
	Slots Slots `json:"slots"`
	// Asked is what the team asked of other teams, with the state of each.
	Asked []Request `json:"asked"`
	// Teams names the other teams that the board's items and requests involve.
	Teams map[string]string `json:"teams"`
}

// Request is an item that another team's goal asked this team to do.
type Request struct {
	Item
	FromTeamID string `json:"fromTeamId"`
	// Position and Total place a request in its group: the requests that
	// wait for triage, or the accepted ones that wait for a slot.
	Position int64  `json:"position,omitempty"`
	Total    int64  `json:"total,omitempty"`
	Reason   Reason `json:"reason"`
}

// Actor is who acts on a board: a member in a turn in the team, or the app
// (the zero Actor), for example when it blocks an item that nobody works on.
type Actor struct {
	BotID    string
	TurnID   string // the bot's turn, whose messages it sends
	TeamID   string // the team of the bot's turn
	Role     string // the board role of the bot's turn, or RoleUser for the user
	UserPost bool   // a post of the user woke the bot's turn
}

// RoleUser is the role of an Actor that is the user, acting in the app: a
// change by the user needs no turn of the lead of the item's team.
const RoleUser = "user"

// NewItem is an item that a member adds to the goal.
type NewItem struct {
	Title      string
	Brief      string
	Acceptance string
	OwnerID    string
	ReviewerID string
	DependsOn  []string
	// ForItemID names the request of another team's goal that a request is
	// for, when the lead's team serves several (RequestWork only).
	ForItemID string
	intake    string // IntakeRequested for a request
}

func toGoal(row db.Goal, used int64) Goal {
	return Goal{ID: row.ID, TeamID: row.TeamID, Text: row.Text, State: row.State, Budget: row.Budget, Used: used, QueuePos: row.QueuePos, CreatedAt: row.CreatedAt}
}

func toItem(row db.Item) (Item, error) {
	it := Item{
		ID: row.ID, TeamID: row.TeamID, GoalID: row.GoalID, Title: row.Title, Brief: row.Brief, Acceptance: row.Acceptance,
		State: row.State, OwnerID: row.OwnerID, ReviewerID: row.ReviewerID, Result: row.Result, Note: row.Note,
		BlockedFrom: row.BlockedFrom, ReviewRounds: row.ReviewRounds, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Intake: row.Intake, IntakeNote: row.IntakeNote, DeferRevisit: row.DeferRevisit, DeferUntil: row.DeferUntil,
		SlotAt: row.SlotAt, JumpedAt: row.JumpedAt, ForItemID: row.ForItemID,
	}
	if err := json.Unmarshal([]byte(row.DependsOn), &it.DependsOn); err != nil {
		return Item{}, fmt.Errorf("decode the dependencies of item %s: %w", row.ID, err)
	}
	return it, nil
}

func (s *Service) goal(ctx context.Context, q *db.Queries, id string) (Goal, error) {
	row, err := q.GetGoal(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Goal{}, notFound("goal", id)
	}
	if err != nil {
		return Goal{}, fmt.Errorf("get goal %s: %w", id, err)
	}
	used, err := q.CountGoalTurns(ctx, id)
	if err != nil {
		return Goal{}, fmt.Errorf("count the turns of goal %s: %w", id, err)
	}
	return toGoal(row, used), nil
}

func goalItems(ctx context.Context, q *db.Queries, goalID string) ([]Item, error) {
	rows, err := q.ListGoalItems(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list the items of goal %s: %w", goalID, err)
	}
	items := make([]Item, 0, len(rows))
	for _, r := range rows {
		it, err := toItem(r)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, nil
}

// latestGoal returns the open goal of a team, or its latest goal; false when
// the team has had none.
func (s *Service) latestGoal(ctx context.Context, teamID string) (Goal, bool, error) {
	row, err := s.q.GetLatestGoal(ctx, teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return Goal{}, false, nil
	}
	if err != nil {
		return Goal{}, false, fmt.Errorf("get the goal of team %s: %w", teamID, err)
	}
	g, err := s.goal(ctx, s.q, row.ID)
	if err != nil {
		return Goal{}, false, err
	}
	return g, true, nil
}

// Board returns the open goal of a team, or its latest goal, the goal's
// items, and the open requests of other teams to it.
func (s *Service) Board(ctx context.Context, teamID string) (Board, error) {
	board := Board{Items: []Item{}, Requests: []Request{}, Queue: []Goal{}, Asked: []Request{}, Teams: map[string]string{}}
	g, ok, err := s.latestGoal(ctx, teamID)
	if err != nil {
		return Board{}, err
	}
	if ok {
		board.Goal = &g
		if board.Items, err = goalItems(ctx, s.q, g.ID); err != nil {
			return Board{}, err
		}
	}
	if err := s.fillLane(ctx, teamID, &board); err != nil {
		return Board{}, err
	}
	if err := s.boardHistory(ctx, &board); err != nil {
		return Board{}, err
	}
	return board, s.nameTeams(ctx, teamID, &board)
}

// boardHistory fills in the history of every item that a board shows.
func (s *Service) boardHistory(ctx context.Context, board *Board) error {
	if err := s.withHistory(ctx, board.Items); err != nil {
		return err
	}
	for _, list := range [][]Request{board.Requests, board.Asked} {
		for i := range list {
			items := []Item{list[i].Item}
			if err := s.withHistory(ctx, items); err != nil {
				return err
			}
			list[i].Item = items[0]
		}
	}
	return nil
}

// nameTeams fills in the names of the other teams that a board involves.
func (s *Service) nameTeams(ctx context.Context, teamID string, board *Board) error {
	ids := []string{}
	for _, it := range board.Items {
		ids = append(ids, it.TeamID)
	}
	for _, r := range board.Requests {
		ids = append(ids, r.FromTeamID)
	}
	for _, r := range board.Asked {
		ids = append(ids, r.TeamID, r.FromTeamID)
	}
	for _, id := range ids {
		if _, known := board.Teams[id]; id == teamID || known {
			continue
		}
		other, err := s.GetBot(ctx, id)
		if err != nil {
			return err
		}
		board.Teams[id] = other.Name
	}
	return nil
}

func (s *Service) team(ctx context.Context, id string) (Bot, error) {
	team, err := s.GetBot(ctx, id)
	if err != nil {
		return Bot{}, err
	}
	if !team.IsTeam() {
		return Bot{}, invalid("%s is not a team", team.Name)
	}
	return team, nil
}

// goalStart checks that team can start a goal with text and returns the team
// and the cleaned text.
func (s *Service) goalStart(ctx context.Context, teamID, text string) (Bot, string, error) {
	team, err := s.team(ctx, teamID)
	if err != nil {
		return Bot{}, "", err
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > maxGoalText {
		return Bot{}, "", invalid("a goal must be 1 to %d characters", maxGoalText)
	}
	if _, ok := team.Lead(); !ok {
		return Bot{}, "", invalid("%s has no members", team.Name)
	}
	return team, text, nil
}

// StartGoal starts a goal that the user gives a team, and wakes the lead to
// plan it. While the team has an open goal or a queue, the goal is queued.
func (s *Service) StartGoal(ctx context.Context, teamID, text string) (Goal, error) {
	team, text, err := s.goalStart(ctx, teamID, text)
	if err != nil {
		return Goal{}, err
	}
	var g Goal
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if g, err = s.placeGoal(ctx, q, team, text); err != nil || g.State == GoalQueued {
			return err
		}
		return s.wakeLead(ctx, q, team, g, "The user started a new goal: plan it.")
	})
	if err != nil {
		return Goal{}, err
	}
	s.signalTurns()
	return g, s.goalStarted(ctx, team, g)
}

// StartGoalAsLead starts a goal for the lead's team in a turn that a user
// post woke. The lead plans it in the same turn, unless it is queued.
func (s *Service) StartGoalAsLead(ctx context.Context, a Actor, text string) (Goal, error) {
	team, text, err := s.goalStart(ctx, a.TeamID, text)
	if err != nil {
		return Goal{}, err
	}
	if lead, _ := team.Lead(); lead.ID != a.BotID {
		return Goal{}, invalid("only the lead of %s starts a goal", team.Name)
	}
	if !a.UserPost {
		return Goal{}, invalid("start a goal only in a turn that a post of the user started")
	}
	var g Goal
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		g, err = s.placeGoal(ctx, q, team, text)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	return g, s.goalStarted(ctx, team, g)
}

// goalStarted shows a new goal, active or queued, in the team's channel.
func (s *Service) goalStarted(ctx context.Context, team Bot, g Goal) error {
	s.hub.Publish(Event{Type: "bots"})
	if g.State == GoalQueued {
		return s.boardNote(ctx, team.ID, fmt.Sprintf("Queued goal #%d: %s", g.QueuePos, g.Text))
	}
	return s.boardNote(ctx, team.ID, "New goal: "+g.Text)
}

// boardNote shows a change of the board in the team's channel.
func (s *Service) boardNote(ctx context.Context, teamID, text string) error {
	_, err := s.AppendEntry(ctx, teamID, KindEvent, "", map[string]string{"type": "board", "text": text})
	return err
}

// boardOf returns the team and the goal that an actor acts on. A bot must be
// a member that works in a turn in that team; the goal must be open.
func (s *Service) boardOf(ctx context.Context, a Actor, teamID, goalID string) (Bot, Goal, error) {
	team, err := s.team(ctx, teamID)
	if err != nil {
		return Bot{}, Goal{}, err
	}
	if a.BotID != "" && a.TeamID != teamID {
		return Bot{}, Goal{}, invalid("act on the board of %s only in a turn in that team", team.Name)
	}
	if _, err := roomMember(team, a.BotID); a.BotID != "" && err != nil {
		return Bot{}, Goal{}, err
	}
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return Bot{}, Goal{}, err
	}
	if !g.Open() {
		return Bot{}, Goal{}, invalid("the goal is over")
	}
	return team, g, nil
}

// leadActs reports whether the actor is the lead of team acting as the lead
// in a turn in the team, so that the change needs no turn of that lead.
func (a Actor) leadActs(team Bot) bool {
	lead, ok := team.Lead()
	return ok && a.BotID == lead.ID && a.TeamID == team.ID && (a.Role == "" || a.Role == RoleLead)
}

func (a Actor) mustLead(team Bot) error {
	if lead, ok := team.Lead(); !ok || a.BotID != lead.ID {
		return invalid("only the lead of %s does this", team.Name)
	}
	return nil
}

// textOf trims a text that a member gives and checks its length.
func textOf(field, text string, limit int, required bool) (string, error) {
	text = strings.TrimSpace(text)
	if required && text == "" {
		return "", invalid("%s is required", field)
	}
	if utf8.RuneCountInString(text) > limit {
		return "", invalid("%s is longer than %d characters", field, limit)
	}
	return text, nil
}

// checkAssignees checks that the owner and the reviewer are members of team
// and different bots. Either can be empty.
func checkAssignees(team Bot, ownerID, reviewerID string) error {
	for _, id := range []string{ownerID, reviewerID} {
		if _, err := roomMember(team, id); id != "" && err != nil {
			return err
		}
	}
	if reviewerID != "" && reviewerID == ownerID {
		return invalid("the reviewer must be another member than the owner")
	}
	if reviewerID != "" && ownerID == "" {
		return invalid("give the item an owner before a reviewer")
	}
	return nil
}

// validNewItem checks a new item against the goal's items and cleans its texts.
func validNewItem(n NewItem, team Bot, items []Item) (NewItem, error) {
	var err error
	if n.Title, err = textOf("the title", n.Title, maxItemTitle, true); err != nil {
		return NewItem{}, err
	}
	if n.Brief, err = textOf("the brief", n.Brief, maxItemText, true); err != nil {
		return NewItem{}, err
	}
	if n.Acceptance, err = textOf("the acceptance criteria", n.Acceptance, maxItemNote, true); err != nil {
		return NewItem{}, err
	}
	open := slices.DeleteFunc(slices.Clone(items), func(it Item) bool { return it.State == ItemDropped })
	if len(open) >= maxGoalItems {
		return NewItem{}, invalid("a goal has at most %d items that are not dropped", maxGoalItems)
	}
	if slices.ContainsFunc(items, func(it Item) bool { return it.Title == n.Title }) {
		return NewItem{}, invalid("the goal already has an item %q", n.Title)
	}
	if err := checkAssignees(team, n.OwnerID, n.ReviewerID); err != nil {
		return NewItem{}, err
	}
	for _, dep := range n.DependsOn {
		if !slices.ContainsFunc(items, func(it Item) bool { return it.ID == dep }) {
			return NewItem{}, invalid("%s is not an item of this goal", dep)
		}
	}
	n.DependsOn = slices.Compact(slices.Sorted(slices.Values(n.DependsOn)))
	return n, nil
}

// CreateItem adds an item to the open goal of the actor's team. The lead can
// give it an owner and a reviewer; another member's item is a proposal that
// the lead assigns.
func (s *Service) CreateItem(ctx context.Context, a Actor, n NewItem) (Item, error) {
	g, ok, err := s.latestGoal(ctx, a.TeamID)
	if err != nil {
		return Item{}, err
	}
	if !ok {
		return Item{}, invalid("the team has no goal; the lead starts one with start_goal")
	}
	team, g, err := s.boardOf(ctx, a, a.TeamID, g.ID)
	if err != nil {
		return Item{}, err
	}
	if err := checkCreator(a, team, g, n); err != nil {
		return Item{}, err
	}
	items, err := goalItems(ctx, s.q, g.ID)
	if err != nil {
		return Item{}, err
	}
	if n, err = validNewItem(n, team, items); err != nil {
		return Item{}, err
	}
	var it Item
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		it, err = s.insertItem(ctx, q, team, g, a, n)
		return err
	})
	if err != nil {
		return Item{}, err
	}
	s.signalTurns()
	if g.State == GoalDelivered {
		// The user asked for changes to the delivered goal.
		if err := s.closeDelivery(ctx, team.ID, g.ID); err != nil {
			return Item{}, err
		}
	}
	return it, s.boardNote(ctx, team.ID, itemEvent(team, a, "", it))
}

// insertItem adds a checked item to goal g with q, and wakes whoever it
// needs. An item of a delivered goal opens the goal again: the user asked
// for changes.
func (s *Service) insertItem(ctx context.Context, q *db.Queries, team Bot, g Goal, a Actor, n NewItem) (Item, error) {
	if g.State == GoalDelivered {
		if err := s.setGoalState(ctx, q, g, GoalActive); err != nil {
			return Item{}, err
		}
	}
	deps, err := json.Marshal(append([]string{}, n.DependsOn...))
	if err != nil {
		return Item{}, fmt.Errorf("encode the dependencies: %w", err)
	}
	now := s.nowMs()
	row, err := q.InsertItem(ctx, db.InsertItemParams{
		ID: uuid.NewString(), TeamID: team.ID, GoalID: g.ID, Title: n.Title, Brief: n.Brief, Acceptance: n.Acceptance,
		OwnerID: n.OwnerID, ReviewerID: n.ReviewerID, DependsOn: string(deps), CreatedBy: a.BotID, CreatedAt: now, UpdatedAt: now,
		Intake: n.intake, ForItemID: n.ForItemID,
	})
	if err != nil {
		return Item{}, fmt.Errorf("add item %q: %w", n.Title, err)
	}
	it, err := toItem(row)
	if err != nil {
		return Item{}, err
	}
	if err := s.recordEvent(ctx, q, a, it, eventDraft{Kind: EventCreated}); err != nil {
		return Item{}, err
	}
	return it, s.wake(ctx, q, team, g, a, it)
}

// checkCreator checks that the actor may add item n to goal g of team: the
// lead adds any item, and opens a delivered goal again with it; another
// member only proposes an item without an owner, to an undelivered goal.
func checkCreator(a Actor, team Bot, g Goal, n NewItem) error {
	if a.mustLead(team) == nil {
		return nil
	}
	if n.OwnerID != "" || n.ReviewerID != "" {
		return invalid("only the lead assigns items; leave owner_id and reviewer_id empty to propose one")
	}
	if g.State == GoalDelivered {
		return invalid("the goal is delivered; the lead adds items when the user asks for changes")
	}
	return nil
}

// leaving makes a change of moveItem that leaves an event of kind, with the
// text that text gives for the changed item.
func leaving(kind string, text func(Item) string, change func(team Bot, it *Item) error) func(Bot, *Item) (eventDraft, error) {
	return func(team Bot, it *Item) (eventDraft, error) {
		if err := change(team, it); err != nil {
			return eventDraft{}, err
		}
		return eventDraft{Kind: kind, Text: text(*it)}, nil
	}
}

// fixed is the text of an event that is note, whatever the item.
func fixed(note string) func(Item) string {
	return func(Item) string { return strings.TrimSpace(note) }
}

// moveItem applies a board action to an item. change checks the actor and the
// item, and changes the item; then the new state wakes whoever must act next.
// The item changes only if no other action changed it meanwhile.
func (s *Service) moveItem(ctx context.Context, a Actor, itemID string, change func(team Bot, it *Item) (eventDraft, error)) (Item, error) {
	row, err := s.q.GetItem(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, notFound("item", itemID)
	}
	if err != nil {
		return Item{}, fmt.Errorf("get item %s: %w", itemID, err)
	}
	it, err := toItem(row)
	if err != nil {
		return Item{}, err
	}
	team, g, err := s.boardOf(ctx, a, it.TeamID, it.GoalID)
	if err != nil {
		return Item{}, err
	}
	from, fromIntake := it.State, it.Intake
	event, err := change(team, &it)
	if err != nil {
		return Item{}, err
	}
	it.UpdatedAt = s.nowMs()
	var notes []noteTo
	err = s.inTx(ctx, func(q *db.Queries) error {
		n, err := updateItem(ctx, q, it, from, fromIntake)
		if err != nil {
			return err
		}
		if n == 0 {
			return invalid("item %q changed meanwhile; list the board and try again", it.Title)
		}
		if err := s.recordEvent(ctx, q, a, it, event); err != nil {
			return err
		}
		if err := releaseSlot(ctx, q, &it); err != nil {
			return err
		}
		if err := s.wake(ctx, q, team, g, a, it); err != nil {
			return err
		}
		notes, err = s.intakeEffects(ctx, q, team, g, fromIntake, it)
		return err
	})
	if err != nil {
		return Item{}, err
	}
	s.signalTurns()
	text := moveText(team, a, from, fromIntake, it)
	if g.TeamID != team.ID {
		// The asking team sees its request move too.
		notes = append([]noteTo{{g.TeamID, "Team " + team.Name + ": " + text}}, notes...)
	}
	return it, s.postNotes(ctx, append([]noteTo{{team.ID, text}}, notes...))
}

// wake queues the turns that an item's new state needs, in the transaction
// of the change: the owner's work, the reviewer's check, the work of items
// that waited for it, and the turns of the leads that must act. team is the
// item's team, which can differ from the goal's team for a request.
//
// The lead of the item's team must act when the item is blocked or has no
// owner. The lead of the goal's team must act when the goal has no work left
// in flight. A lead's own change as the lead wakes no lead, and a change by
// the user wakes no lead of the item's team.
func (s *Service) wake(ctx context.Context, q *db.Queries, team Bot, g Goal, a Actor, it Item) error {
	if err := q.CancelQueuedItemTurns(ctx, db.CancelQueuedItemTurnsParams{FinishedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, ItemID: it.ID}); err != nil {
		return fmt.Errorf("cancel the turns of item %s: %w", it.ID, err)
	}
	items, err := goalItems(ctx, q, g.ID)
	if err != nil {
		return err
	}
	for _, t := range itemTurns(g, it, items) {
		if _, err := s.queueTurn(ctx, q, t); err != nil {
			return err
		}
	}
	if err := s.wakeLeads(ctx, q, team, g, a, it, items); err != nil {
		return err
	}
	return s.grantGoalSlots(ctx, q, g, items)
}

// wakeLeads queues the turns of the leads that must act after it changed, as
// wake describes. items is the goal's items after the change.
func (s *Service) wakeLeads(ctx context.Context, q *db.Queries, team Bot, g Goal, a Actor, it Item, items []Item) error {
	if (it.State == ItemBlocked || (it.OwnerID == "" && it.State != ItemDropped)) && !a.leadActs(team) && a.Role != RoleUser {
		if err := s.wakeLead(ctx, q, team, g, "The board changed."); err != nil {
			return err
		}
	}
	if slices.ContainsFunc(items, inFlight) {
		return nil
	}
	goalTeam, err := teamWith(ctx, q, g.TeamID)
	if err != nil || a.leadActs(goalTeam) {
		return err
	}
	return s.wakeLead(ctx, q, goalTeam, g, "The board changed.")
}

// teamWith loads a team with q, as GetBot does, inside a transaction.
func teamWith(ctx context.Context, q *db.Queries, id string) (Bot, error) {
	row, err := q.GetBot(ctx, id)
	if err != nil {
		return Bot{}, fmt.Errorf("get team %s: %w", id, err)
	}
	rows, err := q.ListRoomMembers(ctx, id)
	if err != nil {
		return Bot{}, fmt.Errorf("list the members of team %s: %w", id, err)
	}
	team := toBot(row)
	for _, r := range rows {
		team.Members = append(team.Members, Member{ID: r.BotID, Name: r.Name})
	}
	settings, err := q.GetTeam(ctx, id)
	if err != nil {
		return Bot{}, fmt.Errorf("get team %s: %w", id, err)
	}
	team.Team = toTeam(settings, team.Members)
	return team, nil
}

// wakeLead queues a turn of the lead for goal g, unless one of the lead's
// turns in the team waits already that shows g: a lead turn of g, or a
// conversation turn.
func (s *Service) wakeLead(ctx context.Context, q *db.Queries, team Bot, g Goal, reason string) error {
	lead, ok := team.Lead()
	if !ok {
		return nil
	}
	n, err := q.CountQueuedLeadTurns(ctx, db.CountQueuedLeadTurnsParams{BotID: lead.ID, RoomID: team.ID, GoalID: g.ID})
	if err != nil {
		return fmt.Errorf("look for a queued turn of the lead: %w", err)
	}
	if n > 0 {
		return nil
	}
	_, err = s.queueTurn(ctx, q, Turn{BotID: lead.ID, Source: SourceRoom, RoomID: team.ID, GoalID: g.ID, BoardRole: RoleLead, Prompt: reason})
	return err
}

// ready reports whether every item that it depends on is done, or dropped as
// no longer needed. A request that its team declined or deferred, or that
// was withdrawn, is not done: the asking lead re-plans.
func ready(it Item, items []Item) bool {
	for _, dep := range items {
		if slices.Contains(it.DependsOn, dep.ID) && !satisfies(dep) {
			return false
		}
	}
	return true
}

// satisfies reports whether an item no longer holds up the items that depend on it.
func satisfies(dep Item) bool {
	return dep.State == ItemDone || (dep.State == ItemDropped && dep.Intake == "")
}

// itemTurn is the board turn in which member works on it in role, in the
// item's team; the turn counts against goal g.
func itemTurn(g Goal, it Item, member, role, prompt string) Turn {
	return Turn{BotID: member, Source: SourceRoom, RoomID: it.TeamID, GoalID: g.ID, ItemID: it.ID, BoardRole: role, Prompt: prompt}
}

// itemTurns returns the turns that the new state of it needs. items is the
// goal's items after the change.
func itemTurns(g Goal, it Item, items []Item) []Turn {
	if !mayRun(it) && !closed(it) {
		return nil // a request without a slot waits for one
	}
	return stateTurns(g, it, items)
}

// stateTurns returns the turns that the state of it needs, as itemTurns.
func stateTurns(g Goal, it Item, items []Item) []Turn {
	switch it.State {
	case ItemTodo:
		if it.OwnerID != "" && ready(it, items) {
			return []Turn{itemTurn(g, it, it.OwnerID, RoleWork, "Work on your item.")}
		}
	case ItemDoing:
		return []Turn{itemTurn(g, it, it.OwnerID, RoleWork, "Work on your item again.")}
	case ItemReview:
		return []Turn{itemTurn(g, it, it.ReviewerID, RoleReview, "Review an item.")}
	case ItemDone, ItemDropped:
		var turns []Turn
		for _, next := range items {
			if next.State == ItemTodo && next.OwnerID != "" && mayRun(next) && slices.Contains(next.DependsOn, it.ID) && ready(next, items) {
				turns = append(turns, itemTurn(g, next, next.OwnerID, RoleWork, "Work on your item."))
			}
		}
		return turns
	}
	return nil
}

// inFlight reports whether a member works on it or will. A deferred request
// waits for its revisit, not for a member.
func inFlight(it Item) bool {
	if it.Intake == IntakeDeferred {
		return false
	}
	return it.State == ItemDoing || it.State == ItemReview || (it.State == ItemTodo && it.OwnerID != "")
}

// memberName returns the name of a member of team, or "a former member".
func memberName(team Bot, id string) string {
	if m, err := roomMember(team, id); err == nil {
		return m.Name
	}
	return "a former member"
}

// itemEvent describes a change of an item for the team's channel. from is
// the state before the change, or "" for a new item.
func itemEvent(team Bot, a Actor, from string, it Item) string {
	who := "The app"
	if a.BotID != "" {
		who = memberName(team, a.BotID)
	}
	title := fmt.Sprintf("%q", it.Title)
	if from == "" {
		if it.OwnerID == "" {
			return fmt.Sprintf("%s added %s without an owner.", who, title)
		}
		return fmt.Sprintf("%s added %s for %s.", who, title, memberName(team, it.OwnerID))
	}
	if text, ok := changeEvent(who, title, from, it.State); ok {
		return text
	}
	if it.State == ItemReview {
		return fmt.Sprintf("%s submitted %s; %s reviews it.", who, title, memberName(team, it.ReviewerID))
	}
	if it.State == ItemBlocked {
		return fmt.Sprintf("%s is blocked: %s", title, it.Note)
	}
	if it.OwnerID == "" {
		return fmt.Sprintf("%s waits for an owner: %s", title, it.Note)
	}
	return fmt.Sprintf("%s assigned %s to %s.", who, title, memberName(team, it.OwnerID))
}

// changeEvent describes a change of an item from one state to another that
// needs no member's name but the actor's.
func changeEvent(who, title, from, to string) (string, bool) {
	verbs := map[[2]string]string{
		{ItemReview, ItemDone}:    "passed",
		{ItemDoing, ItemDone}:     "finished",
		{ItemReview, ItemDoing}:   "asked for changes to",
		{ItemBlocked, ItemDoing}:  "unblocked",
		{ItemBlocked, ItemReview}: "unblocked",
	}
	verb, ok := verbs[[2]string{from, to}]
	if to == ItemDropped {
		verb, ok = "dropped", true
	}
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s %s %s.", who, verb, title), true
}

// RequestWork asks another team for an item of a goal: the lead's own
// active goal, or the goal of a request that the lead's team serves, so that
// a team in a chain can pass work on. The other team's lead assigns or
// declines it; its work counts against that goal, and the goal's items can
// depend on it.
func (s *Service) RequestWork(ctx context.Context, a Actor, teamID string, n NewItem) (Item, error) {
	g, forItem, err := s.requestGoal(ctx, a, n.ForItemID)
	if err != nil {
		return Item{}, err
	}
	team, g, err := s.boardOf(ctx, a, a.TeamID, g.ID)
	if err != nil {
		return Item{}, err
	}
	if err := a.mustLead(team); err != nil {
		return Item{}, err
	}
	other, err := s.team(ctx, teamID)
	if err != nil {
		return Item{}, err
	}
	if _, ok := other.Lead(); !ok || other.ID == team.ID {
		return Item{}, invalid("ask a team other than yours that has members")
	}
	if other.ID == g.TeamID {
		return Item{}, invalid("%s's goal asked for your work; submit your result and its lead takes it on", other.Name)
	}
	items, err := goalItems(ctx, s.q, g.ID)
	if err != nil {
		return Item{}, err
	}
	n.OwnerID, n.ReviewerID = "", ""
	if n, err = validNewItem(n, other, items); err != nil {
		return Item{}, err
	}
	n.ForItemID, n.intake = forItem, IntakeRequested
	var it Item
	err = s.inTx(ctx, func(q *db.Queries) error {
		var err error
		it, err = s.insertItem(ctx, q, other, g, a, n)
		return err
	})
	if err != nil {
		return Item{}, err
	}
	s.signalTurns()
	return it, s.requestNotes(ctx, a, team, other, g, it)
}

// requestNotes shows a new request in the channels of the asking team, the
// asked team and, for a chained request, the team whose goal it is.
func (s *Service) requestNotes(ctx context.Context, a Actor, team, other Bot, g Goal, it Item) error {
	asked := fmt.Sprintf("Team %s asked for %q.", team.Name, it.Title)
	asking := fmt.Sprintf("%s asked team %s for %q.", memberName(team, a.BotID), other.Name, it.Title)
	if g.TeamID != team.ID {
		owner, err := s.GetBot(ctx, g.TeamID)
		if err != nil {
			return err
		}
		asked = fmt.Sprintf("Team %s asked for %q, for team %s's goal.", team.Name, it.Title, owner.Name)
		asking = fmt.Sprintf("%s asked team %s for %q, for team %s's goal.", memberName(team, a.BotID), other.Name, it.Title, owner.Name)
		if err := s.boardNote(ctx, g.TeamID, fmt.Sprintf("Team %s asked team %s for %q, for this goal.", team.Name, other.Name, it.Title)); err != nil {
			return err
		}
	}
	if err := s.boardNote(ctx, other.ID, asked); err != nil {
		return err
	}
	return s.boardNote(ctx, team.ID, asking)
}

// requestGoal returns the goal that a request of the actor's team counts
// against. forItemID names a request that the team serves; without it, that
// is the team's own active goal, else the one goal whose requests it serves.
func (s *Service) requestGoal(ctx context.Context, a Actor, forItemID string) (Goal, string, error) {
	served, err := s.servedRequests(ctx, a.TeamID)
	if err != nil {
		return Goal{}, "", err
	}
	if forItemID != "" {
		for _, r := range served {
			if r.ID == forItemID {
				g, err := s.goal(ctx, s.q, r.GoalID)
				return g, r.ID, err
			}
		}
		return Goal{}, "", invalid("%s is not a request of another team's active goal that your team works on", forItemID)
	}
	g, ok, err := s.latestGoal(ctx, a.TeamID)
	if err != nil {
		return Goal{}, "", err
	}
	if ok && g.State == GoalActive {
		return g, "", nil
	}
	g, err = s.onlyServedGoal(ctx, served)
	if err != nil || len(served) > 1 {
		return g, "", err
	}
	return g, served[0].ID, nil
}

// onlyServedGoal returns the goal of the requests that a team without an
// active goal serves, when they all come from one goal.
func (s *Service) onlyServedGoal(ctx context.Context, served []Item) (Goal, error) {
	if len(served) == 0 {
		return Goal{}, invalid("your team has no active goal and works on no request of another team; " +
			"the user starts a goal in the team's channel, or the team whose goal needs the work asks for it")
	}
	ids := []string{}
	for _, r := range served {
		if r.GoalID != served[0].GoalID {
			ids = append(ids, fmt.Sprintf("%q (id %s)", r.Title, r.ID))
		}
	}
	if len(ids) > 0 {
		return Goal{}, invalid("your team works on requests of several goals; give item_id of the request this work is for: %q (id %s), %s",
			served[0].Title, served[0].ID, strings.Join(ids, ", "))
	}
	return s.goal(ctx, s.q, served[0].GoalID)
}

// servedRequests lists the requests that other teams' active goals ask of team
// and that are not dropped.
func (s *Service) servedRequests(ctx context.Context, teamID string) ([]Item, error) {
	rows, err := s.q.ListTeamRequests(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("list the requests to team %s: %w", teamID, err)
	}
	var served []Item
	for _, r := range rows {
		g, err := s.goal(ctx, s.q, r.Item.GoalID)
		if err != nil {
			return nil, err
		}
		if g.State == GoalActive && r.Item.State != ItemDropped {
			served = append(served, Item{ID: r.Item.ID, GoalID: r.Item.GoalID, Title: r.Item.Title})
		}
	}
	return served, nil
}

// AssignItem gives an item that waits to start, or a blocked one, an owner
// and a reviewer. A blocked item then starts again from the beginning.
//
// Assigning a request of another team accepts it. One that skips requests
// that arrived earlier and wait for triage needs a note on why it goes
// first; with a note, it gets the next slot.
func (s *Service) AssignItem(ctx context.Context, a Actor, itemID, ownerID, reviewerID, note string) (Item, error) {
	before, err := s.earlierRequests(ctx, itemID)
	if err != nil {
		return Item{}, err
	}
	now := s.nowMs()
	return s.moveItem(ctx, a, itemID, leaving(EventAssigned, fixed(note), func(team Bot, it *Item) error {
		if err := a.mustLead(team); err != nil {
			return err
		}
		if it.State != ItemTodo && it.State != ItemBlocked {
			return invalid("only an item that waits to start, or a blocked one, can be assigned; %q is %s", it.Title, it.State)
		}
		if it.Intake == IntakeDeferred {
			return invalid("%q is deferred; bring it back with undefer first", it.Title)
		}
		if it.Intake == IntakeRequested {
			if err := accept(it, before, note, now); err != nil {
				return err
			}
		}
		if ownerID == "" {
			return invalid("owner_id is required")
		}
		if err := checkAssignees(team, ownerID, reviewerID); err != nil {
			return err
		}
		it.OwnerID, it.ReviewerID, it.State, it.BlockedFrom = ownerID, reviewerID, ItemTodo, ""
		return nil
	}))
}

// SubmitItem hands the owner's result on: to the reviewer, or as done when
// the item has no reviewer.
func (s *Service) SubmitItem(ctx context.Context, a Actor, itemID, result string) (Item, error) {
	return s.moveItem(ctx, a, itemID, leaving(EventSubmitted, func(it Item) string { return cut(it.Result, maxResultInEvent) }, func(team Bot, it *Item) error {
		if a.BotID != it.OwnerID || it.State != ItemDoing {
			return invalid("only the owner submits an item that is being done")
		}
		text, err := textOf("the result", result, maxItemText, true)
		if err != nil {
			return err
		}
		it.Result, it.Note, it.State = text, "", ItemDone
		if it.ReviewerID != "" {
			it.State = ItemReview
		}
		return nil
	}))
}

// BlockItem stops an item until the lead answers note: the owner blocks the
// item it works on, the reviewer the item it reviews.
func (s *Service) BlockItem(ctx context.Context, a Actor, itemID, note string) (Item, error) {
	return s.moveItem(ctx, a, itemID, leaving(EventBlocked, fixed(note), func(team Bot, it *Item) error {
		owner := a.BotID == it.OwnerID && it.State == ItemDoing
		reviewer := a.BotID == it.ReviewerID && it.State == ItemReview
		if !owner && !reviewer {
			return invalid("only the owner of an item that is being done, or its reviewer during a review, blocks it")
		}
		return block(it, note)
	}))
}

// block stops an item with a note, and remembers its state to go back to.
func block(it *Item, note string) error {
	text, err := textOf("the note", note, maxItemNote, true)
	if err != nil {
		return err
	}
	it.BlockedFrom, it.State, it.Note = it.State, ItemBlocked, text
	return nil
}

// UnblockItem answers a blocked item with the lead's note: it goes back to
// the state it was blocked in, so that its owner or its reviewer gets the
// answer, or it waits for an owner.
func (s *Service) UnblockItem(ctx context.Context, a Actor, itemID, note string) (Item, error) {
	return s.moveItem(ctx, a, itemID, leaving(EventUnblocked, fixed(note), func(team Bot, it *Item) error {
		if err := a.mustLead(team); err != nil {
			return err
		}
		if it.State != ItemBlocked {
			return invalid("%q is not blocked", it.Title)
		}
		text, err := textOf("the note", note, maxItemNote, true)
		if err != nil {
			return err
		}
		it.Note, it.State, it.BlockedFrom = text, unblockedState(*it), ""
		return nil
	}))
}

// unblockedState is the state that a blocked item goes back to: the state it
// was blocked in, as far as its owner and reviewer still allow it.
func unblockedState(it Item) string {
	to := it.BlockedFrom
	if to == ItemReview && it.ReviewerID == "" {
		to = ItemDoing
	}
	if to != ItemDoing && to != ItemReview || it.OwnerID == "" {
		return ItemTodo
	}
	return to
}

// DropItem takes an item that is no longer needed off the goal.
func (s *Service) DropItem(ctx context.Context, a Actor, itemID, note string) (Item, error) {
	return s.moveItem(ctx, a, itemID, leaving(EventDropped, fixed(note), func(team Bot, it *Item) error {
		if err := a.mustLead(team); err != nil {
			return err
		}
		if it.State == ItemDone || it.State == ItemDropped {
			return invalid("%q is %s already", it.Title, it.State)
		}
		text, err := textOf("the note", note, maxItemNote, it.Intake != "")
		if err != nil {
			return err
		}
		it.Note, it.State = text, ItemDropped
		if it.Intake != "" {
			it.Intake, it.IntakeNote = IntakeDeclined, text
		}
		return nil
	}))
}

// PassItem is the reviewer's verdict that an item is done.
func (s *Service) PassItem(ctx context.Context, a Actor, itemID, note string) (Item, error) {
	return s.moveItem(ctx, a, itemID, leaving(EventPassed, fixed(note), func(team Bot, it *Item) error {
		if a.BotID != it.ReviewerID || it.State != ItemReview {
			return invalid("only the reviewer passes an item in review")
		}
		text, err := textOf("the note", note, maxItemNote, false)
		if err != nil {
			return err
		}
		it.Note, it.State = text, ItemDone
		return nil
	}))
}

// RequestChanges sends an item in review back to its owner with what must
// change. Every maxReviewRounds rounds without a pass, the item is blocked
// for the lead; after the lead unblocks it, it gets that many rounds again.
func (s *Service) RequestChanges(ctx context.Context, a Actor, itemID, note string) (Item, error) {
	return s.moveItem(ctx, a, itemID, leaving(EventChanges, fixed(note), func(team Bot, it *Item) error {
		if a.BotID != it.ReviewerID || it.State != ItemReview {
			return invalid("only the reviewer asks for changes to an item in review")
		}
		text, err := textOf("the note", note, maxItemNote, true)
		if err != nil {
			return err
		}
		it.ReviewRounds++
		it.Note, it.State = text, ItemDoing
		if it.ReviewRounds%maxReviewRounds == 0 {
			return block(it, fmt.Sprintf("%d review rounds without a pass. The last review: %s", it.ReviewRounds, text))
		}
		return nil
	}))
}

// StartItemWork marks an item as being done when its owner's work turn
// starts. It fails when the item is no longer the bot's to work on.
func (s *Service) StartItemWork(ctx context.Context, itemID, botID string) error {
	n, err := s.q.StartItem(ctx, db.StartItemParams{UpdatedAt: s.nowMs(), ID: itemID, OwnerID: botID})
	if err != nil {
		return fmt.Errorf("start item %s: %w", itemID, err)
	}
	if n == 0 {
		return invalid("item %s is no longer yours to work on", itemID)
	}
	return nil
}

// GoalAllows reports whether a board turn may start: its goal is active and
// has turns left. At the budget, it pauses the goal. A turn of a goal that is
// over or gone is cancelled. A retro turn always may start.
func (s *Service) GoalAllows(ctx context.Context, t Turn) (bool, error) {
	if t.BoardRole == RoleRetro {
		return true, nil
	}
	g, err := s.goal(ctx, s.q, t.GoalID)
	if errors.Is(err, ErrNotFound) || (err == nil && !g.Open()) {
		return false, s.cancelTurn(ctx, t.ID, "the goal is over")
	}
	if err != nil || g.State != GoalActive {
		return false, err
	}
	if g.Used < g.Budget {
		return true, nil
	}
	return false, s.pauseAtBudget(ctx, g)
}

func (s *Service) cancelTurn(ctx context.Context, id, reason string) error {
	if err := s.q.CancelTurn(ctx, db.CancelTurnParams{Error: reason, FinishedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, ID: id}); err != nil {
		return fmt.Errorf("cancel turn %s: %w", id, err)
	}
	return nil
}

// pauseAtBudget pauses a goal that used its budget. When the budget grew
// meanwhile, the goal stays active.
func (s *Service) pauseAtBudget(ctx context.Context, g Goal) error {
	paused := false
	err := s.inTx(ctx, func(q *db.Queries) error {
		n, err := q.PauseGoalAtBudget(ctx, db.PauseGoalAtBudgetParams{ID: g.ID, Used: g.Used})
		if err != nil || n == 0 {
			return err
		}
		paused = true
		if err := s.cancelGoalTurns(ctx, q, g.ID); err != nil {
			return err
		}
		return s.goalChanged(ctx, q, g, GoalPaused)
	})
	if err != nil || !paused {
		return err
	}
	s.hub.Publish(Event{Type: "bots"})
	return s.boardNote(ctx, g.TeamID, fmt.Sprintf("The goal used its budget of %d turns and is paused. Add turns to go on, or end the goal.", g.Budget))
}

func (s *Service) cancelGoalTurns(ctx context.Context, q *db.Queries, goalID string) error {
	if err := q.CancelQueuedGoalTurns(ctx, db.CancelQueuedGoalTurnsParams{FinishedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, GoalID: goalID}); err != nil {
		return fmt.Errorf("cancel the turns of goal %s: %w", goalID, err)
	}
	return nil
}

// setGoalState moves a goal from its state in g to state, with q. A goal
// that stops being active drops its queued turns: resume queues them again.
// The caller publishes the change.
func (s *Service) setGoalState(ctx context.Context, q *db.Queries, g Goal, state string) error {
	n, err := q.SetGoalState(ctx, db.SetGoalStateParams{State: state, ID: g.ID, FromState: g.State})
	if err != nil {
		return fmt.Errorf("set goal %s %s: %w", g.ID, state, err)
	}
	if n == 0 {
		return invalid("the goal changed meanwhile; look at the board again")
	}
	if state != GoalActive {
		if err := s.cancelGoalTurns(ctx, q, g.ID); err != nil {
			return err
		}
	}
	return s.goalChanged(ctx, q, g, state)
}

// StopTeam stops a team's board work when the user presses Stop: the team's
// active goal pauses, and the requests of other teams that the team works on
// are blocked, without a turn of its lead. It runs before the runner stops
// the team's turns, so that no item counts as left behind meanwhile.
func (s *Service) StopTeam(ctx context.Context, teamID string) error {
	if err := s.pauseTeamGoal(ctx, teamID); err != nil {
		return err
	}
	return s.stopRequests(ctx, teamID)
}

func (s *Service) pauseTeamGoal(ctx context.Context, teamID string) error {
	g, ok, err := s.latestGoal(ctx, teamID)
	if err != nil || !ok || g.State != GoalActive {
		return err
	}
	err = s.inTx(ctx, func(q *db.Queries) error { return s.setGoalState(ctx, q, g, GoalPaused) })
	if errors.Is(err, ErrInvalid) {
		return nil // the goal left active meanwhile, which is all that Stop needs
	}
	if err != nil {
		return err
	}
	s.hub.Publish(Event{Type: "bots"})
	return s.boardNote(ctx, teamID, "The goal is paused.")
}

// stopRequests blocks the requests to a team that wait on one of its members.
func (s *Service) stopRequests(ctx context.Context, teamID string) error {
	board, err := s.Board(ctx, teamID)
	if err != nil || len(board.Requests) == 0 {
		return err
	}
	team, err := s.GetBot(ctx, teamID)
	if err != nil {
		return err
	}
	for _, r := range board.Requests {
		if member, _ := waitingOn(r.Item, nil); member == "" {
			continue
		}
		err := s.moveSettled(ctx, Actor{Role: RoleUser}, r.Item, "The user stopped team "+team.Name+".")
		if err != nil {
			return err
		}
	}
	return nil
}

// ExtendGoal adds turns to the budget of a goal and resumes it when it is
// paused: the members whose items wait on them work again, and the lead wakes.
func (s *Service) ExtendGoal(ctx context.Context, goalID string, turns int64) (Goal, error) {
	var g Goal
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if g, err = s.goal(ctx, q, goalID); err != nil {
			return err
		}
		if g.State != GoalActive && g.State != GoalPaused {
			return invalid("only an active or a paused goal takes more turns")
		}
		if turns < 0 || g.Budget+turns > maxGoalBudget {
			return invalid("a goal has at most %d turns", maxGoalBudget)
		}
		if err := q.SetGoalBudget(ctx, db.SetGoalBudgetParams{Budget: g.Budget + turns, ID: g.ID}); err != nil {
			return fmt.Errorf("set the budget of goal %s: %w", g.ID, err)
		}
		g.Budget += turns
		return s.resume(ctx, q, g)
	})
	if err != nil {
		return Goal{}, err
	}
	g.State = GoalActive
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	return g, s.boardNote(ctx, g.TeamID, fmt.Sprintf("The goal can use %d turns; %d are used.", g.Budget, g.Used))
}

// resume makes a paused goal active with q, and queues the turns of the
// members that its items wait on, and a turn of the lead. An active goal
// stays as it is.
func (s *Service) resume(ctx context.Context, q *db.Queries, g Goal) error {
	if g.State != GoalPaused {
		return nil
	}
	if err := s.setGoalState(ctx, q, g, GoalActive); err != nil {
		return err
	}
	items, err := goalItems(ctx, q, g.ID)
	if err != nil {
		return err
	}
	for _, it := range items {
		if err := s.requeue(ctx, q, g, it, items); err != nil {
			return err
		}
	}
	team, err := teamWith(ctx, q, g.TeamID)
	if err != nil {
		return err
	}
	return s.wakeLead(ctx, q, team, g, "The user resumed the goal.")
}

// requeue queues the turn of the member that it waits on, unless one is
// queued or running already.
func (s *Service) requeue(ctx context.Context, q *db.Queries, g Goal, it Item, items []Item) error {
	member, role := waitingOn(it, items)
	if member == "" {
		return nil
	}
	n, err := q.CountOpenItemTurns(ctx, db.CountOpenItemTurnsParams{ItemID: it.ID, BotID: member})
	if err != nil || n > 0 {
		return err
	}
	_, err = s.queueTurn(ctx, q, itemTurn(g, it, member, role, "Work on the board again."))
	return err
}

// waitingOn returns the member that an item waits on and the role of its
// turn: the owner of an item that is being done or is ready to start, the
// reviewer of an item in review. It returns "" when the item waits on nobody.
func waitingOn(it Item, items []Item) (string, string) {
	if !mayRun(it) {
		return "", ""
	}
	return waitsOn(it, items)
}

// waitsOn is waitingOn without the slot of a request.
func waitsOn(it Item, items []Item) (string, string) {
	if it.State == ItemReview {
		return it.ReviewerID, RoleReview
	}
	if it.State == ItemDoing || (it.State == ItemTodo && it.OwnerID != "" && ready(it, items)) {
		return it.OwnerID, RoleWork
	}
	return "", ""
}

// DeliverGoal hands the result of the actor's goal to the user when every
// item is closed: the lead's summary goes into the channel as a question,
// and the user accepts the goal or asks for changes.
func (s *Service) DeliverGoal(ctx context.Context, a Actor, summary string) (Goal, error) {
	team, g, summary, err := s.deliverable(ctx, a, summary)
	if err != nil {
		return Goal{}, err
	}
	lead, _ := team.Lead()
	body, err := json.Marshal(map[string]any{
		"text": summary, "fallback": false, "needsReply": true, "speakerId": lead.ID, "speakerName": lead.Name,
		"questionState": QuestionOpen, "goalId": g.ID,
	})
	if err != nil {
		return Goal{}, fmt.Errorf("encode the delivery: %w", err)
	}
	var e Entry
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.setGoalState(ctx, q, g, GoalDelivered); err != nil {
			return err
		}
		row, err := q.AppendEntry(ctx, db.AppendEntryParams{BotID: team.ID, ID: uuid.NewString(), Kind: KindBot, TurnID: a.TurnID, Body: string(body), CreatedAt: s.nowMs()})
		e = toEntry(row)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	g.State = GoalDelivered
	s.hub.Publish(Event{Type: "entry", BotID: team.ID, Entry: &e})
	s.hub.Publish(Event{Type: "bots"})
	return g, nil
}

// deliverable checks that the actor, the lead, can deliver its team's active
// goal with summary, and returns the team, the goal and the cleaned summary.
func (s *Service) deliverable(ctx context.Context, a Actor, summary string) (Bot, Goal, string, error) {
	g, ok, err := s.latestGoal(ctx, a.TeamID)
	if err != nil {
		return Bot{}, Goal{}, "", err
	}
	if !ok || g.State != GoalActive {
		return Bot{}, Goal{}, "", invalid("only an active goal is delivered")
	}
	team, g, err := s.boardOf(ctx, a, a.TeamID, g.ID)
	if err != nil {
		return Bot{}, Goal{}, "", err
	}
	if err := a.mustLead(team); err != nil {
		return Bot{}, Goal{}, "", err
	}
	if summary, err = textOf("the result", summary, maxItemText, true); err != nil {
		return Bot{}, Goal{}, "", err
	}
	items, err := goalItems(ctx, s.q, g.ID)
	if err != nil {
		return Bot{}, Goal{}, "", err
	}
	return team, g, summary, checkDelivery(items)
}

// checkDelivery checks that every item is done or dropped, and one is done.
func checkDelivery(items []Item) error {
	done := false
	for _, it := range items {
		if it.State != ItemDone && it.State != ItemDropped {
			return invalid("%q is %s; deliver when every item is done or dropped", it.Title, it.State)
		}
		done = done || it.State == ItemDone
	}
	if !done {
		return invalid("no item is done; drop the goal's work with the user, or add items")
	}
	return nil
}

// AcceptGoal is the user's acceptance of a delivered goal. The goal is done,
// and the lead looks back on it in one retro turn. The acceptance shows as
// the user's message, which answers the lead's delivery. With startNext, the
// head of the team's queue starts after the retro; else the queue holds.
func (s *Service) AcceptGoal(ctx context.Context, goalID string, startNext bool) (Goal, error) {
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return Goal{}, err
	}
	if g.State != GoalDelivered {
		return Goal{}, invalid("accept a goal after its lead delivers it; to stop it now, end it")
	}
	team, err := s.team(ctx, g.TeamID)
	if err != nil {
		return Goal{}, err
	}
	var after goalAfter
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.setGoalState(ctx, q, g, GoalDone); err != nil {
			return err
		}
		if lead, ok := team.Lead(); ok {
			// The retro turn comes first, so the lead looks back before it plans the next goal.
			if _, err := s.queueTurn(ctx, q, Turn{BotID: lead.ID, Source: SourceRoom, RoomID: team.ID, GoalID: g.ID, BoardRole: RoleRetro, Prompt: "The user accepted the goal."}); err != nil {
				return err
			}
		}
		var err error
		after, err = s.afterGoal(ctx, q, g.TeamID, startNext)
		return err
	})
	if err != nil {
		return Goal{}, err
	}
	g.State = GoalDone
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	if err := s.closeDelivery(ctx, g.TeamID, g.ID); err != nil {
		return Goal{}, err
	}
	if _, err = s.AppendEntry(ctx, team.ID, KindUser, "", userBody{Text: "Accepted the goal."}); err != nil {
		return Goal{}, err
	}
	return g, s.goalAfterNotes(ctx, g.TeamID, after)
}

// goalAfter is what followed the end of a team's goal: the requests back
// from deferral, and the next goal from the queue, if one started.
type goalAfter struct {
	notes   []noteTo
	next    Goal
	started bool
	held    bool // the queue has goals but holds
}

// afterGoal follows the end of a team's own goal with q: the requests that
// waited for it come back, and with startNext the head of the queue starts.
func (s *Service) afterGoal(ctx context.Context, q *db.Queries, teamID string, startNext bool) (goalAfter, error) {
	var after goalAfter
	var err error
	if after.notes, err = s.revisitGoalEnd(ctx, q, teamID); err != nil {
		return goalAfter{}, err
	}
	if !startNext {
		queue, err := queuedGoals(ctx, q, teamID)
		after.held = len(queue) > 0
		return after, err
	}
	after.next, after.started, err = s.nextGoal(ctx, q, teamID)
	return after, err
}

// goalAfterNotes posts what followed the end of a team's goal.
func (s *Service) goalAfterNotes(ctx context.Context, teamID string, after goalAfter) error {
	if err := s.postNotes(ctx, after.notes); err != nil {
		return err
	}
	if after.held {
		return s.boardNote(ctx, teamID, "The queue is on hold: start the next goal when you are ready.")
	}
	return s.nextGoalNote(ctx, after.next, after.started)
}

// EndGoal closes an open goal before it is done; its queued turns are
// dropped, and its open requests to other teams are withdrawn. With
// startNext, the head of the team's queue starts; else the queue holds. The
// caller stops the goal's running turns.
func (s *Service) EndGoal(ctx context.Context, goalID string, startNext bool) error {
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return err
	}
	if g.State == GoalQueued {
		return invalid("the goal has not started; cancel it instead")
	}
	if !g.Open() {
		return invalid("the goal is over already")
	}
	team, err := s.GetBot(ctx, g.TeamID)
	if err != nil {
		return err
	}
	var withdrawn []noteTo
	var after goalAfter
	err = s.inTx(ctx, func(q *db.Queries) error {
		if err := s.setGoalState(ctx, q, g, GoalEnded); err != nil {
			return err
		}
		var err error
		if withdrawn, err = s.withdrawGoal(ctx, q, g, team.Name); err != nil {
			return err
		}
		after, err = s.afterGoal(ctx, q, g.TeamID, startNext)
		return err
	})
	if err != nil {
		return err
	}
	s.signalTurns()
	s.hub.Publish(Event{Type: "bots"})
	if err := s.closeDelivery(ctx, g.TeamID, g.ID); err != nil {
		return err
	}
	if err := s.boardNote(ctx, g.TeamID, "The goal ended."); err != nil {
		return err
	}
	if err := s.postNotes(ctx, withdrawn); err != nil {
		return err
	}
	return s.goalAfterNotes(ctx, g.TeamID, after)
}

// SettleItems keeps active goals moving when nobody works on them. It blocks
// the items that wait on a member who neither has a turn for them nor a
// running process in the team, for example after a failed turn, a turn that
// ended without handing the item on, or a restart; and it wakes the lead of
// a goal that has nothing in flight after such a stop. live reports whether a
// member's process runs in a team.
func (s *Service) SettleItems(ctx context.Context, live func(botID, teamID string) bool) error {
	if err := s.revisitDue(ctx); err != nil {
		return err
	}
	goals, err := s.q.ListOpenGoals(ctx)
	if err != nil {
		return fmt.Errorf("list open goals: %w", err)
	}
	for _, row := range goals {
		if row.State != GoalActive {
			continue
		}
		items, err := goalItems(ctx, s.q, row.ID)
		if err != nil {
			return err
		}
		for _, it := range items {
			if err := s.settle(ctx, it, items, live); err != nil {
				return err
			}
		}
		if err := s.wakeStalledLead(ctx, toGoal(row, 0), items); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) settle(ctx context.Context, it Item, items []Item, live func(botID, teamID string) bool) error {
	member, _ := waitingOn(it, items)
	if member == "" || live(member, it.TeamID) {
		return nil
	}
	n, err := s.q.CountOpenItemTurns(ctx, db.CountOpenItemTurnsParams{ItemID: it.ID, BotID: member})
	if err != nil || n > 0 {
		return err
	}
	team, err := s.GetBot(ctx, it.TeamID)
	if err != nil {
		return err
	}
	return s.moveSettled(ctx, Actor{}, it, memberName(team, member)+" stopped before handing it on; see the channel for why.")
}

// errMoved stops a change of an item that moved since the board was read.
var errMoved = errors.New("the item moved since it was read")

// moveSettled blocks an item with note, but only if it is still as it was
// read: a member may have handed it on meanwhile.
func (s *Service) moveSettled(ctx context.Context, a Actor, read Item, note string) error {
	_, err := s.moveItem(ctx, a, read.ID, leaving(EventBlocked, fixed(note), func(_ Bot, it *Item) error {
		if it.State != read.State || it.OwnerID != read.OwnerID || it.ReviewerID != read.ReviewerID {
			return errMoved
		}
		return block(it, note)
	}))
	if errors.Is(err, errMoved) {
		return nil
	}
	return err
}

// wakeStalledLead wakes the lead of an active goal that nobody works on: no
// item is in flight, no turn of the goal is open, the lead has no turn in the
// team, and the goal's last turn did not end well, for example after a
// restart or a failed turn. A lead that ended its turn without acting is not
// woken again.
func (s *Service) wakeStalledLead(ctx context.Context, g Goal, items []Item) error {
	if slices.ContainsFunc(items, inFlight) {
		return nil
	}
	n, err := s.q.CountOpenGoalTurns(ctx, g.ID)
	if err != nil || n > 0 {
		return err
	}
	status, err := s.q.LastGoalTurnStatus(ctx, g.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read the last turn of goal %s: %w", g.ID, err)
	}
	if status == "done" {
		return nil
	}
	team, err := s.team(ctx, g.TeamID)
	if err != nil {
		return err
	}
	lead, ok := team.Lead()
	if !ok {
		return nil
	}
	busy, err := s.q.CountOpenTurnsOfBotInRoom(ctx, db.CountOpenTurnsOfBotInRoomParams{BotID: lead.ID, RoomID: team.ID})
	if err != nil || busy > 0 {
		return err
	}
	err = s.inTx(ctx, func(q *db.Queries) error { return s.wakeLead(ctx, q, team, g, "The goal has no work in flight.") })
	if err != nil {
		return err
	}
	s.signalTurns()
	return nil
}

// ReviewsItem reports whether the bot reviews an item in review in the team,
// so that it may not change the work there.
func (s *Service) ReviewsItem(ctx context.Context, teamID, botID string) (bool, error) {
	n, err := s.q.CountReviewsBy(ctx, db.CountReviewsByParams{TeamID: teamID, ReviewerID: botID})
	if err != nil {
		return false, fmt.Errorf("look for the reviews of bot %s: %w", botID, err)
	}
	return n > 0, nil
}

// GoalBoard returns a goal and its items, whatever its state.
func (s *Service) GoalBoard(ctx context.Context, goalID string) (Board, error) {
	g, err := s.goal(ctx, s.q, goalID)
	if err != nil {
		return Board{}, err
	}
	items, err := goalItems(ctx, s.q, g.ID)
	if err != nil {
		return Board{}, err
	}
	board := Board{Goal: &g, Items: items, Requests: []Request{}, Queue: []Goal{}, Asked: []Request{}, Teams: map[string]string{}}
	if err := s.withHistory(ctx, board.Items); err != nil {
		return Board{}, err
	}
	return board, s.nameTeams(ctx, g.TeamID, &board)
}

// releaseMember frees a team's open items from a bot that left the team or
// was deleted, so that no turn waits on it: items it owns wait for a new
// owner, items it reviews lose their reviewer, and a review it did not finish
// is blocked for the lead. Its queued turns in the team are dropped.
func (s *Service) releaseMember(ctx context.Context, teamID, botID, name string) error {
	if err := s.q.CancelQueuedTurnsOfBotInRoom(ctx, db.CancelQueuedTurnsOfBotInRoomParams{FinishedAt: sql.NullInt64{Int64: s.nowMs(), Valid: true}, BotID: botID, RoomID: teamID}); err != nil {
		return fmt.Errorf("cancel the turns of bot %s in team %s: %w", botID, teamID, err)
	}
	rows, err := s.q.ListMemberItems(ctx, db.ListMemberItemsParams{TeamID: teamID, BotID: botID})
	if err != nil {
		return fmt.Errorf("list the items of bot %s in team %s: %w", botID, teamID, err)
	}
	for _, row := range rows {
		_, err := s.moveItem(ctx, Actor{}, row.ID, func(_ Bot, it *Item) (eventDraft, error) { return release(it, botID, name) })
		if err != nil {
			return err
		}
	}
	return nil
}

// release frees one item from a bot that left its team. Only a review that
// it did not finish leaves an event: the item is blocked for the lead.
func release(it *Item, botID, name string) (eventDraft, error) {
	if it.OwnerID == botID {
		it.OwnerID, it.ReviewerID, it.State, it.BlockedFrom = "", "", ItemTodo, ""
		it.Note = name + " left the team; the item waits for a new owner."
		return eventDraft{}, nil
	}
	it.ReviewerID = ""
	if it.State != ItemReview {
		return eventDraft{}, nil
	}
	it.State = ItemDoing
	err := block(it, name+" left the team before reviewing it.")
	return eventDraft{Kind: EventBlocked, Text: it.Note}, err
}
