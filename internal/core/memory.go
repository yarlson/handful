package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// MaxFactLen is the longest fact, in characters, that memory accepts.
const MaxFactLen = 500

const (
	ScopeAgent = "agent"
	ScopeUser  = "user"

	TierProfile = "profile"
	TierLog     = "log"
	TierNote    = "note"

	userScopeKey   = "user"
	noteLifetime   = 7 * 24 * time.Hour
	promptLogFacts = 20
)

// LearnedByUser is the LearnedBy of a fact that the user added in the app. A
// fact that a bot wrote has the bot's id, so a bot cannot pass its own
// fact off as the user's.
const LearnedByUser = "user"

type Memory struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	Tier      string `json:"tier"`
	Fact      string `json:"fact"`
	LearnedBy string `json:"learnedBy"`
	CreatedAt int64  `json:"createdAt"`
}

func toMemory(m db.Memory) Memory {
	scope := ScopeAgent
	if m.ScopeKey == userScopeKey {
		scope = ScopeUser
	}
	return Memory{ID: m.ID, Scope: scope, Tier: m.Tier, Fact: m.Fact, LearnedBy: m.LearnedBy, CreatedAt: m.CreatedAt}
}

func scopeKey(botID, scope string) (string, error) {
	switch scope {
	case "", ScopeAgent:
		return botID, nil
	case ScopeUser:
		return userScopeKey, nil
	}
	return "", invalid("scope must be %q or %q", ScopeAgent, ScopeUser)
}

func normalizeFact(fact string) (string, error) {
	fact = strings.Join(strings.Fields(fact), " ")
	if fact == "" {
		return "", invalid("fact is empty")
	}
	if utf8.RuneCountInString(fact) > MaxFactLen {
		return "", invalid("fact is longer than %d characters", MaxFactLen)
	}
	return fact, nil
}

// WriteFact saves a fact. It returns false when the same fact already exists in that scope.
func (s *Service) WriteFact(ctx context.Context, botID, scope, tier, fact string) (bool, error) {
	key, err := scopeKey(botID, scope)
	if err != nil {
		return false, err
	}
	if tier == "" {
		tier = TierLog
	}
	if tier != TierProfile && tier != TierLog && tier != TierNote {
		return false, invalid("tier must be profile, log or note")
	}
	fact, err = normalizeFact(fact)
	if err != nil {
		return false, err
	}
	n, err := s.q.InsertMemory(ctx, db.InsertMemoryParams{ID: uuid.NewString(), ScopeKey: key, Tier: tier, Fact: fact, LearnedBy: botID, CreatedAt: s.nowMs()})
	if err != nil {
		return false, fmt.Errorf("save fact for bot %s: %w", botID, err)
	}
	return n == 1, nil
}

// ReplaceFact swaps the fact with the exact text old for fact, in one
// transaction. It fails with ErrInvalid and changes nothing when old does
// not exist in that scope.
func (s *Service) ReplaceFact(ctx context.Context, botID, scope, tier, old, fact string) error {
	key, err := scopeKey(botID, scope)
	if err != nil {
		return err
	}
	if tier == "" {
		tier = TierLog
	}
	if tier != TierProfile && tier != TierLog && tier != TierNote {
		return invalid("tier must be profile, log or note")
	}
	if old, err = normalizeFact(old); err != nil {
		return err
	}
	if fact, err = normalizeFact(fact); err != nil {
		return err
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	n, err := qtx.DeleteMemoryByFact(ctx, db.DeleteMemoryByFactParams{ScopeKey: key, Fact: old})
	if err != nil {
		return fmt.Errorf("forget fact for bot %s: %w", botID, err)
	}
	if n == 0 {
		return invalid("no fact with that exact text in scope %q to replace; use recall_memory to find it", scope)
	}
	if _, err := qtx.InsertMemory(ctx, db.InsertMemoryParams{ID: uuid.NewString(), ScopeKey: key, Tier: tier, Fact: fact, LearnedBy: botID, CreatedAt: s.nowMs()}); err != nil {
		return fmt.Errorf("save fact for bot %s: %w", botID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit fact replacement: %w", err)
	}
	return nil
}

// AddUserFact saves a fact that the user added in the app for bot. A fact
// with the same text that a bot wrote becomes the user's.
func (s *Service) AddUserFact(ctx context.Context, botID, scope, tier, fact string) error {
	bot, err := s.GetBot(ctx, botID)
	if err != nil {
		return err
	}
	if bot.IsRoom() {
		return invalid("%s is a room; rooms have no memory", bot.Name)
	}
	key, err := scopeKey(botID, scope)
	if err != nil {
		return err
	}
	if tier != TierProfile && tier != TierLog && tier != TierNote {
		return invalid("tier must be profile, log or note")
	}
	if fact, err = normalizeFact(fact); err != nil {
		return err
	}
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	qtx := s.q.WithTx(tx)
	if _, err := qtx.DeleteMemoryByFact(ctx, db.DeleteMemoryByFactParams{ScopeKey: key, Fact: fact}); err != nil {
		return fmt.Errorf("replace fact for bot %s: %w", botID, err)
	}
	if _, err := qtx.InsertMemory(ctx, db.InsertMemoryParams{ID: uuid.NewString(), ScopeKey: key, Tier: tier, Fact: fact, LearnedBy: LearnedByUser, CreatedAt: s.nowMs()}); err != nil {
		return fmt.Errorf("save the user's fact for bot %s: %w", botID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit the user's fact: %w", err)
	}
	return nil
}

// ForgetFact deletes a fact by its exact text. It returns false when no such fact exists.
func (s *Service) ForgetFact(ctx context.Context, botID, scope, fact string) (bool, error) {
	key, err := scopeKey(botID, scope)
	if err != nil {
		return false, err
	}
	fact, err = normalizeFact(fact)
	if err != nil {
		return false, err
	}
	n, err := s.q.DeleteMemoryByFact(ctx, db.DeleteMemoryByFactParams{ScopeKey: key, Fact: fact})
	if err != nil {
		return false, fmt.Errorf("forget fact for bot %s: %w", botID, err)
	}
	return n > 0, nil
}

func (s *Service) DeleteMemory(ctx context.Context, id string) error {
	n, err := s.q.DeleteMemory(ctx, id)
	if err != nil {
		return fmt.Errorf("delete memory %s: %w", id, err)
	}
	if n == 0 {
		return notFound("memory", id)
	}
	return nil
}

// Memories returns the bot's own facts and the shared user facts, newest first.
// Notes older than their lifetime are left out.
func (s *Service) Memories(ctx context.Context, botID string) ([]Memory, error) {
	rows, err := s.q.ListMemoriesForBot(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("list memories of bot %s: %w", botID, err)
	}
	cutoff := s.now().Add(-noteLifetime).UnixMilli()
	out := make([]Memory, 0, len(rows))
	for _, r := range rows {
		if r.Tier == TierNote && r.CreatedAt < cutoff {
			continue
		}
		out = append(out, toMemory(r))
	}
	return out, nil
}

// PromptMemory returns the facts to show in every turn: all profile facts and
// the most recent log and note facts.
func (s *Service) PromptMemory(ctx context.Context, botID string) (profile, recent []Memory, err error) {
	all, err := s.Memories(ctx, botID)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range all {
		if m.Tier == TierProfile {
			profile = append(profile, m)
		} else if len(recent) < promptLogFacts {
			recent = append(recent, m)
		}
	}
	return profile, recent, nil
}

// Recall searches memory. Facts that share more words with the query rank
// first; when no fact shares a word, a case-insensitive substring match is used.
func (s *Service) Recall(ctx context.Context, botID, query, scope string, limit int) ([]Memory, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, invalid("query is empty")
	}
	if scope != "" && scope != "all" && scope != ScopeAgent && scope != ScopeUser {
		return nil, invalid("scope must be agent, user or all")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	all, err := s.Memories(ctx, botID)
	if err != nil {
		return nil, err
	}
	var candidates []Memory
	for _, m := range all {
		if scope == "" || scope == "all" || m.Scope == scope {
			candidates = append(candidates, m)
		}
	}

	type scored struct {
		m     Memory
		score int
	}
	queryWords := words(query)
	var hits []scored
	for _, m := range candidates {
		factWords := words(m.Fact)
		score := 0
		for w := range queryWords {
			if _, ok := factWords[w]; ok {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{m, score})
		}
	}
	if len(hits) == 0 {
		needle := strings.ToLower(query)
		for _, m := range candidates {
			if strings.Contains(strings.ToLower(m.Fact), needle) {
				hits = append(hits, scored{m, 1})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].m.CreatedAt > hits[j].m.CreatedAt
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Memory, len(hits))
	for i, h := range hits {
		out[i] = h.m
	}
	return out, nil
}

func words(text string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(w) >= 2 {
			set[w] = struct{}{}
		}
	}
	return set
}

// PruneNotes deletes notes older than their lifetime.
func (s *Service) PruneNotes(ctx context.Context) error {
	if _, err := s.q.PruneNotes(ctx, s.now().Add(-noteLifetime).UnixMilli()); err != nil {
		return fmt.Errorf("prune notes: %w", err)
	}
	return nil
}
