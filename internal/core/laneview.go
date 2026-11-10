package core

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"yargpt/internal/db"
)

// laneViewer builds the views of requests for boards, with what it loaded
// once: the views of each serving team's lane, team names and goal items.
type laneViewer struct {
	s     *Service
	ctx   context.Context
	views map[string]map[string]Request // team id → item id → view
	names map[string]string
	items map[string][]Item
}

func (s *Service) newLaneViewer(ctx context.Context) *laneViewer {
	return &laneViewer{s: s, ctx: ctx, views: map[string]map[string]Request{}, names: map[string]string{}, items: map[string][]Item{}}
}

func (v *laneViewer) name(id string) (string, error) {
	if n, ok := v.names[id]; ok {
		return n, nil
	}
	b, err := v.s.GetBot(v.ctx, id)
	if err != nil {
		return "", err
	}
	v.names[id] = b.Name
	return b.Name, nil
}

func (v *laneViewer) goalItems(id string) ([]Item, error) {
	if items, ok := v.items[id]; ok {
		return items, nil
	}
	items, err := goalItems(v.ctx, v.s.q, id)
	v.items[id] = items
	return items, err
}

// fillLane fills in a team's requests with their reasons, its slots, its
// queue and what it asked of other teams.
func (s *Service) fillLane(ctx context.Context, teamID string, board *Board) error {
	v := s.newLaneViewer(ctx)
	l, err := laneWith(ctx, s.q, teamID)
	if err != nil {
		return err
	}
	board.Slots = l.slots
	if board.Requests, err = v.laneViews(l); err != nil {
		return err
	}
	if err := v.closedViews(teamID, board); err != nil {
		return err
	}
	if board.Queue, err = queuedGoals(ctx, s.q, teamID); err != nil {
		return err
	}
	board.Asked, err = v.asked(teamID, board, l)
	return err
}

// laneViews returns the views of a lane's requests in arrival order.
func (v *laneViewer) laneViews(l lane) ([]Request, error) {
	out := []Request{}
	byID := map[string]Request{}
	ctx := newReasonContext(l)
	for _, r := range l.reqs {
		view := Request{Item: r.Item, FromTeamID: r.from}
		view.Reason, view.Position, view.Total = ctx.reason(r)
		waits, err := v.waitsOn(l, r)
		if err != nil {
			return nil, err
		}
		view.Reason.WaitsOn = waits
		out = append(out, view)
		byID[r.ID] = view
	}
	v.views[l.team.ID] = byID
	return out, nil
}

// closedViews adds the requests to a team that were declined or withdrawn
// in the last day, of goals that are over.
func (v *laneViewer) closedViews(teamID string, board *Board) error {
	rows, err := v.s.q.ListClosedTeamRequests(v.ctx, db.ListClosedTeamRequestsParams{TeamID: teamID, Since: v.s.now().Add(-24 * time.Hour).UnixMilli()})
	if err != nil {
		return fmt.Errorf("list the closed requests to team %s: %w", teamID, err)
	}
	for _, r := range rows {
		it, err := toItem(r.Item)
		if err != nil {
			return err
		}
		reason, _, _ := reasonContext{}.reason(laneRequest{Item: it})
		board.Requests = append(board.Requests, Request{Item: it, FromTeamID: r.FromTeamID, Reason: reason})
	}
	return nil
}

// asked returns what a team asked of other teams: the requests of its open
// goal, and the requests that it chained for the requests it serves.
func (v *laneViewer) asked(teamID string, board *Board, l lane) ([]Request, error) {
	var asks []Item
	if board.Goal != nil && board.Goal.Open() {
		for _, it := range board.Items {
			if it.TeamID != teamID {
				asks = append(asks, it)
			}
		}
	}
	for _, r := range l.reqs {
		rows, err := v.s.q.ListChainedItems(v.ctx, r.ID)
		if err != nil {
			return nil, fmt.Errorf("list the requests chained to item %s: %w", r.ID, err)
		}
		for _, row := range rows {
			it, err := toItem(row)
			if err != nil {
				return nil, err
			}
			asks = append(asks, it)
		}
	}
	out := []Request{}
	for _, it := range asks {
		view, err := v.viewOf(it)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// viewOf returns the view of a request in its serving team's lane.
func (v *laneViewer) viewOf(it Item) (Request, error) {
	byID, ok := v.views[it.TeamID]
	if !ok {
		l, err := laneWith(v.ctx, v.s.q, it.TeamID)
		if err != nil {
			return Request{}, err
		}
		if _, err := v.laneViews(l); err != nil {
			return Request{}, err
		}
		byID = v.views[it.TeamID]
	}
	return byID[it.ID], nil
}

// waitsOn says what a request's work waits on in other teams: requests of
// its goal to a third team that it depends on, and a request of the serving
// team's own goal to the asking team, which makes the two wait on each other.
func (v *laneViewer) waitsOn(l lane, r laneRequest) (string, error) {
	if closed(r.Item) {
		return "", nil
	}
	parts, err := v.thirdTeams(r)
	if err != nil {
		return "", err
	}
	mutual, err := v.mutual(l, r)
	return strings.Join(append(parts, mutual...), " "), err
}

// thirdTeams says which teams the open requests that r depends on wait on.
func (v *laneViewer) thirdTeams(r laneRequest) ([]string, error) {
	var parts []string
	items, err := v.goalItems(r.GoalID)
	if err != nil {
		return nil, err
	}
	for _, dep := range items {
		if !slices.Contains(r.DependsOn, dep.ID) || dep.Intake == "" || dep.TeamID == r.TeamID || satisfies(dep) {
			continue
		}
		name, err := v.name(dep.TeamID)
		if err != nil {
			return nil, err
		}
		parts = append(parts, fmt.Sprintf("… waiting on team %s.", name))
	}
	return parts, nil
}

// mutual says which open requests of the serving team's own goal the asking
// team of r works on, so that the two teams wait on each other.
func (v *laneViewer) mutual(l lane, r laneRequest) ([]string, error) {
	if l.ownGoal == "" {
		return nil, nil
	}
	own, err := v.goalItems(l.ownGoal)
	if err != nil {
		return nil, err
	}
	var parts []string
	for _, it := range own {
		if it.TeamID != r.from || closed(it) {
			continue
		}
		name, err := v.name(r.from)
		if err != nil {
			return nil, err
		}
		parts = append(parts, fmt.Sprintf("Team %s is also waiting on your request %q.", name, it.Title))
	}
	return parts, nil
}

// reasonContext is what the reason line of a lane's requests needs.
type reasonContext struct {
	l       lane
	triage  []laneRequest
	waiting []laneRequest
	holders []string
	lead    string
}

func newReasonContext(l lane) reasonContext {
	c := reasonContext{l: l, waiting: l.waiting(), lead: "the lead"}
	for _, r := range l.reqs {
		if r.Intake == IntakeRequested {
			c.triage = append(c.triage, r)
		}
		if holdsSlot(r.Item) {
			c.holders = append(c.holders, fmt.Sprintf("%q", r.Title))
		}
	}
	if lead, ok := l.team.Lead(); ok {
		c.lead = lead.Name
	}
	return c
}

// place returns the 1-based place of r in group and the group's size, or 0.
func place(group []laneRequest, r laneRequest) (int64, int64) {
	i := slices.IndexFunc(group, func(g laneRequest) bool { return g.ID == r.ID })
	return int64(i + 1), int64(len(group))
}

// reason returns the reason line of request r, and its place in its group.
func (c reasonContext) reason(r laneRequest) (Reason, int64, int64) {
	switch r.Intake {
	case IntakeRequested:
		n, m := place(c.triage, r)
		return Reason{Code: ReasonTriage, Text: fmt.Sprintf("Waiting for %s to accept, decline or defer it (position %d of %d).", c.lead, n, m)}, n, m
	case IntakeDeferred:
		return Reason{Code: ReasonDeferred, Text: fmt.Sprintf("Deferred until %s: %s", deferWhen(r.Item, c.l.team.Name), r.IntakeNote)}, 0, 0
	case IntakeDeclined:
		return Reason{Code: ReasonDeclined, Text: "Declined: " + r.IntakeNote}, 0, 0
	case IntakeWithdrawn:
		return Reason{Code: ReasonWithdrawn, Text: r.IntakeNote}, 0, 0
	}
	return c.accepted(r)
}

// accepted returns the reason line of an accepted request.
func (c reasonContext) accepted(r laneRequest) (Reason, int64, int64) {
	jumped := Reason{Code: ReasonJumped, Text: fmt.Sprintf("Taken out of order by %s: %s", c.lead, r.IntakeNote)}
	if closed(r.Item) {
		return Reason{Code: ReasonDone, Text: "Done."}, 0, 0
	}
	if holdsSlot(r.Item) && r.JumpedAt > 0 {
		return jumped, 0, 0
	}
	if holdsSlot(r.Item) {
		return Reason{Code: ReasonInSlot, Text: fmt.Sprintf("In progress (%s).", memberName(c.l.team, r.OwnerID))}, 0, 0
	}
	n, m := place(c.waiting, r)
	if r.goalState == GoalPaused {
		return Reason{Code: ReasonAskingPaused, Text: "On hold: the asking goal is paused."}, n, m
	}
	if r.JumpedAt > 0 {
		return jumped, n, m
	}
	if c.l.slots.Applies && c.l.slots.Used >= c.l.slots.Limit {
		return Reason{Code: ReasonOwnGoalFirst, Text: fmt.Sprintf("Accepted, waiting for a slot: %s's own goal comes first; %d of %d request slots in use by %s.",
			c.l.team.Name, c.l.slots.Used, c.l.slots.Limit, strings.Join(c.holders, ", "))}, n, m
	}
	text := fmt.Sprintf("Accepted, position %d of %d: arrived %s", n, m, clock(r.CreatedAt))
	if n > 1 {
		text += fmt.Sprintf(", after %q", c.waiting[n-2].Title)
	}
	return Reason{Code: ReasonBehind, Text: text + "."}, n, m
}
