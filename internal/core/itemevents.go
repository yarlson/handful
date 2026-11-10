package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// Kinds of item events: every change of a board item leaves one, so that an
// item's review history survives its next move.
const (
	EventCreated    = "created"
	EventAssigned   = "assigned"
	EventSubmitted  = "submitted"
	EventChanges    = "changes"
	EventPassed     = "passed"
	EventBlocked    = "blocked"
	EventUnblocked  = "unblocked"
	EventDropped    = "dropped"
	EventDeferred   = "deferred"
	EventUndeferred = "undeferred"
	EventWithdrawn  = "withdrawn"
)

// maxResultInEvent is how much of a result a submitted event keeps; the item
// keeps the whole result.
const maxResultInEvent = 500

// ItemEvent is one change of a board item.
type ItemEvent struct {
	ID     string `json:"id"`
	ItemID string `json:"itemId"`
	// GoalID is set on a decision on a task, which belongs to no item.
	GoalID    string `json:"goalId,omitempty"`
	Kind      string `json:"kind"`
	ActorID   string `json:"actorId"`   // a bot id, or "" for the user and the app
	ActorName string `json:"actorName"` // the name at that time
	Round     int64  `json:"round"`     // the review round of a changes event
	Text      string `json:"text"`
	CreatedAt int64  `json:"createdAt"`
}

// ItemChange is the change request that an item's owner works on.
type ItemChange struct {
	Round int64  `json:"round"`
	By    string `json:"by"`
	At    int64  `json:"at"`
	Text  string `json:"text"`
}

// eventDraft is the event that a change of an item leaves; an empty Kind
// leaves none.
type eventDraft struct {
	Kind string
	Text string
}

func toItemEvent(r db.ItemEvent) ItemEvent {
	return ItemEvent{ID: r.ID, ItemID: r.ItemID, Kind: r.Kind, ActorID: r.ActorID, ActorName: r.ActorName, Round: r.Round, Text: r.Text, CreatedAt: r.CreatedAt}
}

// recordEvent stores, with q, the event that actor a's change d of item it
// leaves.
func (s *Service) recordEvent(ctx context.Context, q *db.Queries, a Actor, it Item, d eventDraft) error {
	if d.Kind == "" {
		return nil
	}
	name, err := eventActorName(ctx, q, a)
	if err != nil {
		return err
	}
	var round int64
	if d.Kind == EventChanges {
		round = it.ReviewRounds
	}
	err = q.InsertItemEvent(ctx, db.InsertItemEventParams{
		ID: uuid.NewString(), ItemID: it.ID, Kind: d.Kind, ActorID: a.BotID, ActorName: name,
		Round: round, Text: cut(d.Text, maxItemText), CreatedAt: s.nowMs(),
	})
	if err != nil {
		return fmt.Errorf("record the %s event of item %s: %w", d.Kind, it.ID, err)
	}
	return nil
}

// eventActorName names who acts in an event: a bot by its current name, the
// user, or the app.
func eventActorName(ctx context.Context, q *db.Queries, a Actor) (string, error) {
	if a.BotID == "" && a.Role == RoleUser {
		return "the user", nil
	}
	if a.BotID == "" {
		return "YarGPT", nil
	}
	row, err := q.GetBot(ctx, a.BotID)
	if err != nil {
		return "", fmt.Errorf("get bot %s: %w", a.BotID, err)
	}
	return row.Name, nil
}

// cut shortens text to at most n characters.
func cut(text string, n int) string {
	r := []rune(text)
	return string(r[:min(len(r), n)])
}

// ItemEvents returns the history of an item, oldest first.
func (s *Service) ItemEvents(ctx context.Context, itemID string) ([]ItemEvent, error) {
	if _, err := s.q.GetItem(ctx, itemID); errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("item", itemID)
	} else if err != nil {
		return nil, fmt.Errorf("get item %s: %w", itemID, err)
	}
	return s.itemEvents(ctx, itemID)
}

func (s *Service) itemEvents(ctx context.Context, itemID string) ([]ItemEvent, error) {
	rows, err := s.q.ListItemEvents(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("list the events of item %s: %w", itemID, err)
	}
	events := make([]ItemEvent, 0, len(rows))
	for _, r := range rows {
		events = append(events, toItemEvent(r))
	}
	return events, nil
}

// ChangeRequests returns every change request of an item, oldest first, for
// the prompts of its owner and its reviewer.
func (s *Service) ChangeRequests(ctx context.Context, itemID string) ([]ItemEvent, error) {
	events, err := s.ItemEvents(ctx, itemID)
	if err != nil {
		return nil, err
	}
	var changes []ItemEvent
	for _, e := range events {
		if e.Kind == EventChanges {
			changes = append(changes, e)
		}
	}
	return changes, nil
}

// withHistory fills in what the board shows of each item's history.
func (s *Service) withHistory(ctx context.Context, items []Item) error {
	for i := range items {
		events, err := s.itemEvents(ctx, items[i].ID)
		if err != nil {
			return err
		}
		items[i].EventCount, items[i].HistoryFrom = int64(len(events)), firstAt(events)
		items[i].LatestChange = latestChange(events)
	}
	return nil
}

// firstAt is when the first of events happened, or 0 when there is none.
func firstAt(events []ItemEvent) int64 {
	for _, e := range events {
		return e.CreatedAt
	}
	return 0
}

// latestChange returns the latest change request when no submission came
// after it, so the owner works on it; else nil.
func latestChange(events []ItemEvent) *ItemChange {
	var change *ItemChange
	for _, e := range events {
		if e.Kind == EventSubmitted {
			change = nil
		}
		if e.Kind == EventChanges {
			change = &ItemChange{Round: e.Round, By: e.ActorName, At: e.CreatedAt, Text: e.Text}
		}
	}
	return change
}
