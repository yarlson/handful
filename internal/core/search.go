package core

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"handful/internal/db"
)

// Chat search limits: a query has MinSearchQuery to MaxSearchQuery characters, and a search
// returns at most MaxSearchResults messages.
const (
	MinSearchQuery   = 2
	MaxSearchQuery   = 200
	MaxSearchResults = 50
)

// SearchResult is a chat search's messages, newest first. More says that older messages
// match too.
type SearchResult struct {
	Entries []Entry `json:"entries"`
	More    bool    `json:"more"`
}

// SearchEntries finds the user, bot and agent messages of chat botID whose text contains
// query, with no case. Tool calls, approvals and events are not searched.
func (s *Service) SearchEntries(ctx context.Context, botID, query string) (SearchResult, error) {
	query = strings.TrimSpace(query)
	if n := utf8.RuneCountInString(query); n < MinSearchQuery || n > MaxSearchQuery {
		return SearchResult{}, invalid("a search needs %d to %d characters", MinSearchQuery, MaxSearchQuery)
	}
	rows, err := s.q.SearchEntries(ctx, db.SearchEntriesParams{BotID: botID, Query: query, Limit: MaxSearchResults + 1})
	if err != nil {
		return SearchResult{}, fmt.Errorf("search the entries of bot %s: %w", botID, err)
	}
	result := SearchResult{Entries: make([]Entry, 0, min(len(rows), MaxSearchResults)), More: len(rows) > MaxSearchResults}
	for _, r := range rows[:min(len(rows), MaxSearchResults)] {
		result.Entries = append(result.Entries, toEntry(r))
	}
	return result, nil
}
