package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"yargpt/internal/db"
)

// Artifacts are documents, such as plans, reviews and reports, that bots or
// the user save in the database for all of the user's bots. Every bot can read
// every artifact; only its author or the user can change or delete it.

// ArtifactAuthorUser is the AuthorID of an artifact that the user saved in
// the app or through the HTTP API, and the actor id of the user.
const ArtifactAuthorUser = LearnedByUser

const (
	maxArtifactTitle   = 200
	maxArtifactContent = 256 << 10
	maxArtifactTags    = 10
	maxArtifactTagLen  = 64
	maxArtifactLinks   = 10
	maxArtifactLinkLen = 2000
	defaultArtifacts   = 20
	maxArtifactResults = 50
)

// ArtifactKinds are the kinds an artifact can have.
var ArtifactKinds = []string{"plan", "task", "issue", "review", "report", "note"}

var artifactTag = regexp.MustCompile(`^[a-z0-9._:/#-]+$`)

type Artifact struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	Content   string   `json:"content,omitempty"` // left out of search results
	AuthorID  string   `json:"authorId"`
	Tags      []string `json:"tags"`
	Links     []string `json:"links"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}

// ArtifactInput holds the fields to set. A nil field is left as it is.
type ArtifactInput struct {
	Kind    *string   `json:"kind"`
	Title   *string   `json:"title"`
	Content *string   `json:"content"`
	Tags    *[]string `json:"tags"`
	Links   *[]string `json:"links"`
}

// ArtifactFilter selects artifacts. Empty fields match every artifact; Query
// is a case-insensitive substring of the title or the content.
type ArtifactFilter struct {
	Kind     string
	AuthorID string
	Tag      string
	Query    string
	Limit    int
}

func (in ArtifactInput) empty() bool {
	return in.Kind == nil && in.Title == nil && in.Content == nil && in.Tags == nil && in.Links == nil
}

// apply validates the fields that in sets and copies them into a.
func (in ArtifactInput) apply(a *Artifact) error {
	if in.Kind != nil {
		if !slices.Contains(ArtifactKinds, *in.Kind) {
			return invalid("kind must be one of %s", strings.Join(ArtifactKinds, ", "))
		}
		a.Kind = *in.Kind
	}
	if in.Title != nil {
		title := strings.Join(strings.Fields(*in.Title), " ")
		if title == "" || utf8.RuneCountInString(title) > maxArtifactTitle {
			return invalid("title must have 1 to %d characters", maxArtifactTitle)
		}
		a.Title = title
	}
	if in.Content != nil {
		if *in.Content == "" || len(*in.Content) > maxArtifactContent {
			return invalid("content must have 1 to %d bytes", maxArtifactContent)
		}
		a.Content = *in.Content
	}
	return in.applyLists(a)
}

func (in ArtifactInput) applyLists(a *Artifact) error {
	if in.Tags != nil {
		tags, err := normalizeTags(*in.Tags)
		if err != nil {
			return err
		}
		a.Tags = tags
	}
	if in.Links != nil {
		links, err := validateLinks(*in.Links)
		if err != nil {
			return err
		}
		a.Links = links
	}
	return nil
}

func normalizeTag(tag string) string { return strings.ToLower(strings.TrimSpace(tag)) }

// normalizeTags trims, lowercases, dedupes and sorts tags, so that a filter
// by tag matches however the tag was written.
func normalizeTags(tags []string) ([]string, error) {
	out := []string{}
	for _, tag := range tags {
		tag = normalizeTag(tag)
		if len(tag) > maxArtifactTagLen || !artifactTag.MatchString(tag) {
			return nil, invalid("tag %q must have 1 to %d of the characters a-z, 0-9 and . _ : / # -", tag, maxArtifactTagLen)
		}
		out = append(out, tag)
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) > maxArtifactTags {
		return nil, invalid("an artifact has at most %d tags", maxArtifactTags)
	}
	return out, nil
}

func validateLinks(links []string) ([]string, error) {
	if len(links) > maxArtifactLinks {
		return nil, invalid("an artifact has at most %d links", maxArtifactLinks)
	}
	out := []string{}
	for _, link := range links {
		u, err := url.Parse(link)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(link) > maxArtifactLinkLen {
			return nil, invalid("link %q must be an absolute http or https URL of at most %d characters", link, maxArtifactLinkLen)
		}
		out = append(out, link)
	}
	return out, nil
}

func decodeLinks(raw string) []string {
	links := []string{}
	// The column holds only arrays that encodeLinks wrote.
	_ = json.Unmarshal([]byte(raw), &links)
	return links
}

func encodeLinks(links []string) string {
	raw, _ := json.Marshal(links)
	return string(raw)
}

func toArtifact(r db.Artifact, tags []string) Artifact {
	return Artifact{ID: r.ID, Kind: r.Kind, Title: r.Title, Content: r.Content, AuthorID: r.AuthorID, Tags: tags, Links: decodeLinks(r.Links), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// CreateArtifact saves a new artifact by authorID, a bot id or
// ArtifactAuthorUser. Kind, title and content are required.
func (s *Service) CreateArtifact(ctx context.Context, authorID string, in ArtifactInput) (Artifact, error) {
	if authorID == "" {
		return Artifact{}, invalid("an artifact needs an author")
	}
	if in.Kind == nil || in.Title == nil || in.Content == nil {
		return Artifact{}, invalid("an artifact needs a kind, a title and content")
	}
	now := s.nowMs()
	a := Artifact{ID: uuid.NewString(), AuthorID: authorID, Tags: []string{}, Links: []string{}, CreatedAt: now, UpdatedAt: now}
	if err := in.apply(&a); err != nil {
		return Artifact{}, err
	}
	err := s.inTx(ctx, func(q *db.Queries) error {
		if _, err := q.InsertArtifact(ctx, db.InsertArtifactParams{ID: a.ID, Kind: a.Kind, Title: a.Title, Content: a.Content, Links: encodeLinks(a.Links), AuthorID: a.AuthorID, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}); err != nil {
			return fmt.Errorf("save artifact: %w", err)
		}
		return insertArtifactTags(ctx, q, a.ID, a.Tags)
	})
	if err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// UpdateArtifact changes the fields that in sets. actorID is a bot id or
// ArtifactAuthorUser; a bot can change only its own artifacts.
func (s *Service) UpdateArtifact(ctx context.Context, actorID, id string, in ArtifactInput) (Artifact, error) {
	if in.empty() {
		return Artifact{}, invalid("nothing to update: set kind, title, content, tags or links")
	}
	var a Artifact
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if a, err = ownArtifact(ctx, q, actorID, id); err != nil {
			return err
		}
		if err := in.apply(&a); err != nil {
			return err
		}
		a.UpdatedAt = s.nowMs()
		if _, err := q.UpdateArtifact(ctx, db.UpdateArtifactParams{Kind: a.Kind, Title: a.Title, Content: a.Content, Links: encodeLinks(a.Links), UpdatedAt: a.UpdatedAt, ID: id}); err != nil {
			return fmt.Errorf("update artifact %s: %w", id, err)
		}
		if in.Tags == nil {
			return nil
		}
		if err := q.DeleteArtifactTags(ctx, id); err != nil {
			return fmt.Errorf("replace tags of artifact %s: %w", id, err)
		}
		return insertArtifactTags(ctx, q, id, a.Tags)
	})
	if err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// DeleteArtifact deletes an artifact and its tags. actorID is a bot id or
// ArtifactAuthorUser; a bot can delete only its own artifacts.
func (s *Service) DeleteArtifact(ctx context.Context, actorID, id string) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if _, err := ownArtifact(ctx, q, actorID, id); err != nil {
			return err
		}
		if _, err := q.DeleteArtifact(ctx, id); err != nil {
			return fmt.Errorf("delete artifact %s: %w", id, err)
		}
		return nil
	})
}

func (s *Service) GetArtifact(ctx context.Context, id string) (Artifact, error) {
	return getArtifact(ctx, s.q, id)
}

func getArtifact(ctx context.Context, q *db.Queries, id string) (Artifact, error) {
	row, err := q.GetArtifact(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, notFound("artifact", id)
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("get artifact %s: %w", id, err)
	}
	tags, err := q.ListArtifactTags(ctx, id)
	if err != nil {
		return Artifact{}, fmt.Errorf("list tags of artifact %s: %w", id, err)
	}
	return toArtifact(row, append([]string{}, tags...)), nil
}

// ownArtifact reads the artifact id and checks that actorID may change it.
func ownArtifact(ctx context.Context, q *db.Queries, actorID, id string) (Artifact, error) {
	a, err := getArtifact(ctx, q, id)
	if err != nil || actorID == ArtifactAuthorUser || actorID == a.AuthorID {
		return a, err
	}
	return Artifact{}, invalid("artifact %s belongs to %s; only its author or the user can change it", id, authorLabel(ctx, q, a.AuthorID))
}

// authorLabel names an author in an error: the user, or a bot by its quoted
// name and id, so that a bot's name cannot read as the user.
func authorLabel(ctx context.Context, q *db.Queries, authorID string) string {
	if authorID == ArtifactAuthorUser {
		return "the user"
	}
	bot, err := q.GetBot(ctx, authorID)
	if err != nil {
		return fmt.Sprintf("a deleted bot (id %s)", authorID)
	}
	return fmt.Sprintf("bot %q (id %s)", bot.Name, authorID)
}

func insertArtifactTags(ctx context.Context, q *db.Queries, id string, tags []string) error {
	for _, tag := range tags {
		if err := q.InsertArtifactTag(ctx, db.InsertArtifactTagParams{ArtifactID: id, Tag: tag}); err != nil {
			return fmt.Errorf("save tag %q of artifact %s: %w", tag, id, err)
		}
	}
	return nil
}

func (s *Service) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// nullable turns an empty filter into NULL, which matches every artifact.
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// SearchArtifacts returns the artifacts that match f, newest updated first,
// without their content.
func (s *Service) SearchArtifacts(ctx context.Context, f ArtifactFilter) ([]Artifact, error) {
	if f.Kind != "" && !slices.Contains(ArtifactKinds, f.Kind) {
		return nil, invalid("kind must be one of %s", strings.Join(ArtifactKinds, ", "))
	}
	if f.Limit <= 0 || f.Limit > maxArtifactResults {
		f.Limit = defaultArtifacts
	}
	rows, err := s.q.SearchArtifacts(ctx, db.SearchArtifactsParams{
		Kind: nullable(f.Kind), AuthorID: nullable(f.AuthorID), Tag: nullable(normalizeTag(f.Tag)),
		Query: nullable(strings.TrimSpace(f.Query)), MaxRows: int64(f.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search artifacts: %w", err)
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	tagRows, err := s.q.ListTagsForArtifacts(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list tags of artifacts: %w", err)
	}
	tags := map[string][]string{}
	for _, t := range tagRows {
		tags[t.ArtifactID] = append(tags[t.ArtifactID], t.Tag)
	}
	out := make([]Artifact, len(rows))
	for i, r := range rows {
		out[i] = Artifact{ID: r.ID, Kind: r.Kind, Title: r.Title, AuthorID: r.AuthorID, Tags: append([]string{}, tags[r.ID]...), Links: decodeLinks(r.Links), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}
