package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"handful/internal/db"
)

// Reactions are the emoji the user can put on a message, in display order.
// The UI keeps the same list in ui/src/lib/reactions.ts.
var Reactions = []string{"👍", "👎", "❤️", "😂", "👀", "✅"}

// SetReaction puts the user's emoji on a message (on) or takes it off. The
// message body keeps the reactions in the order of Reactions. Reactions are
// display-only: they never wake a bot or reach its prompt. Setting the
// current state again changes nothing and publishes nothing.
func (s *Service) SetReaction(ctx context.Context, entryID, emoji string, on bool) (Entry, error) {
	if on && !slices.Contains(Reactions, emoji) {
		return Entry{}, invalid("%q is not a reaction", emoji)
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	row, err := qtx.GetEntry(ctx, entryID)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, notFound("entry", entryID)
	}
	if err != nil {
		return Entry{}, fmt.Errorf("get entry %s: %w", entryID, err)
	}
	if !isMessage(row.Kind) {
		return Entry{}, invalid("a %s entry takes no reactions", row.Kind)
	}
	body, changed, err := withReaction(row.Body, emoji, on)
	if err != nil {
		return Entry{}, fmt.Errorf("entry %s: %w", entryID, err)
	}
	if !changed {
		return toEntry(row), nil
	}
	row, err = qtx.UpdateEntryBody(ctx, db.UpdateEntryBodyParams{Body: body, ID: entryID})
	if err != nil {
		return Entry{}, fmt.Errorf("update entry %s: %w", entryID, err)
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, fmt.Errorf("commit reaction on entry %s: %w", entryID, err)
	}
	e := toEntry(row)
	s.hub.Publish(Event{Type: "entry", BotID: e.BotID, Entry: &e})
	return e, nil
}

// isMessage reports whether entries of the kind are messages, which take reactions.
func isMessage(kind string) bool {
	switch kind {
	case KindUser, KindBot, KindAgent:
		return true
	}
	return false
}

// withReaction returns the body with the emoji on or off, and whether that
// changed it. The other fields keep their exact bytes.
func withReaction(raw, emoji string, on bool) (string, bool, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return "", false, fmt.Errorf("decode body: %w", err)
	}
	var current []string
	if r, ok := body["reactions"]; ok {
		if err := json.Unmarshal(r, &current); err != nil {
			return "", false, fmt.Errorf("decode reactions: %w", err)
		}
	}
	if slices.Contains(current, emoji) == on {
		return raw, false, nil
	}
	next := orderedReactions(current, emoji, on)
	if len(next) == 0 {
		delete(body, "reactions")
	} else {
		r, err := json.Marshal(next)
		if err != nil {
			return "", false, fmt.Errorf("encode reactions: %w", err)
		}
		body["reactions"] = r
	}
	out, err := json.Marshal(body)
	if err != nil {
		return "", false, fmt.Errorf("encode body: %w", err)
	}
	return string(out), true, nil
}

// orderedReactions returns current with the emoji on or off: the known
// reactions in the order of Reactions, then stored emoji outside the set.
func orderedReactions(current []string, emoji string, on bool) []string {
	has := func(e string) bool { return e == emoji && on || e != emoji && slices.Contains(current, e) }
	var next []string
	for _, e := range Reactions {
		if has(e) {
			next = append(next, e)
		}
	}
	for _, e := range current {
		if !slices.Contains(Reactions, e) && has(e) {
			next = append(next, e)
		}
	}
	return next
}
