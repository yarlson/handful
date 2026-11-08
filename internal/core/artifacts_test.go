package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"yargpt/internal/db"
)

func newArtifact(kind, title, content string, tags ...string) ArtifactInput {
	in := ArtifactInput{Kind: &kind, Title: &title, Content: &content}
	if tags != nil {
		in.Tags = &tags
	}
	return in
}

func TestCreateAndGetArtifact(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	bot := newTestBot(t, svc)
	svc.now = func() time.Time { return time.UnixMilli(1000) }

	in := newArtifact("plan", "  DEV-3   plan ", "# Plan\n\nSteps.", "DEV-3", " dev-3 ", "repo:yargpt")
	in.Links = &[]string{"https://github.com/yarlson/yargpt/pull/4"}
	created, err := svc.CreateArtifact(ctx, bot.ID, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	want := Artifact{
		ID: created.ID, Kind: "plan", Title: "DEV-3 plan", Content: "# Plan\n\nSteps.", AuthorID: bot.ID,
		Tags: []string{"dev-3", "repo:yargpt"}, Links: []string{"https://github.com/yarlson/yargpt/pull/4"},
		CreatedAt: 1000, UpdatedAt: 1000,
	}
	if created.ID == "" || !reflect.DeepEqual(created, want) {
		t.Fatalf("created = %+v; want %+v", created, want)
	}
	got, err := svc.GetArtifact(ctx, created.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("get = %+v, %v; want %+v", got, err, want)
	}

	bare, err := svc.CreateArtifact(ctx, ArtifactAuthorUser, newArtifact("note", "n", "x"))
	if err != nil || bare.Tags == nil || len(bare.Tags) != 0 || bare.Links == nil || len(bare.Links) != 0 || bare.AuthorID != "user" {
		t.Fatalf("artifact without tags or links = %+v, %v; want empty lists by the user", bare, err)
	}
	if _, err := svc.GetArtifact(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: err = %v; want ErrNotFound", err)
	}
}

func TestUpdateArtifactChangesOnlySentFields(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	bot := newTestBot(t, svc)
	svc.now = func() time.Time { return time.UnixMilli(1000) }
	in := newArtifact("plan", "Old", "Body", "a", "b")
	in.Links = &[]string{"https://example.com/a"}
	a, err := svc.CreateArtifact(ctx, bot.ID, in)
	if err != nil {
		t.Fatal(err)
	}

	svc.now = func() time.Time { return time.UnixMilli(2000) }
	got, err := svc.UpdateArtifact(ctx, bot.ID, a.ID, ArtifactInput{Title: new("New")})
	if err != nil {
		t.Fatalf("update title: %v", err)
	}
	if got.Title != "New" || got.Content != "Body" || got.Kind != "plan" || !reflect.DeepEqual(got.Tags, []string{"a", "b"}) ||
		!reflect.DeepEqual(got.Links, []string{"https://example.com/a"}) || got.CreatedAt != 1000 || got.UpdatedAt != 2000 || got.AuthorID != bot.ID {
		t.Fatalf("after title update = %+v", got)
	}

	svc.now = func() time.Time { return time.UnixMilli(3000) }
	got, err = svc.UpdateArtifact(ctx, bot.ID, a.ID, ArtifactInput{Kind: new("review"), Content: new("New body"), Tags: &[]string{"C"}, Links: &[]string{}})
	if err != nil {
		t.Fatalf("update the rest: %v", err)
	}
	if got.Title != "New" || got.Content != "New body" || got.Kind != "review" || !reflect.DeepEqual(got.Tags, []string{"c"}) || len(got.Links) != 0 || got.UpdatedAt != 3000 {
		t.Fatalf("after update = %+v", got)
	}
	stored, err := svc.GetArtifact(ctx, a.ID)
	if err != nil || !reflect.DeepEqual(stored, got) {
		t.Fatalf("stored = %+v, %v; want %+v", stored, err, got)
	}
	for _, tag := range []string{"a", "b"} {
		if found, _ := svc.SearchArtifacts(ctx, ArtifactFilter{Tag: tag}); len(found) != 0 {
			t.Fatalf("search by replaced tag %q = %+v; want none", tag, found)
		}
	}
	if _, err := svc.UpdateArtifact(ctx, bot.ID, "missing", ArtifactInput{Title: new("x")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: err = %v; want ErrNotFound", err)
	}
}

func TestOnlyAuthorOrUserCanChangeArtifact(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a := newTestBot(t, svc)
	b, err := svc.CreateBot(ctx, "Critic", "")
	if err != nil {
		t.Fatal(err)
	}
	art, err := svc.CreateArtifact(ctx, a.ID, newArtifact("plan", "Plan", "Body"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.UpdateArtifact(ctx, b.ID, art.ID, ArtifactInput{Title: new("Hijacked")})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), a.ID) {
		t.Fatalf("other bot's update: err = %v; want ErrInvalid naming %s", err, a.ID)
	}
	if !strings.Contains(err.Error(), `bot "Scout"`) {
		t.Fatalf("other bot's update: err = %v; want the author's name", err)
	}
	if err := svc.DeleteArtifact(ctx, b.ID, art.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), a.ID) {
		t.Fatalf("other bot's delete: err = %v; want ErrInvalid naming %s", err, a.ID)
	}
	if got, _ := svc.GetArtifact(ctx, art.ID); got.Title != "Plan" {
		t.Fatalf("after rejected update, title = %q", got.Title)
	}

	if _, err := svc.UpdateArtifact(ctx, a.ID, art.ID, ArtifactInput{Title: new("By author")}); err != nil {
		t.Fatalf("author's update: %v", err)
	}
	got, err := svc.UpdateArtifact(ctx, ArtifactAuthorUser, art.ID, ArtifactInput{Title: new("By user")})
	if err != nil || got.Title != "By user" || got.AuthorID != a.ID {
		t.Fatalf("user's update = %+v, %v; want the title changed and the author kept", got, err)
	}

	byUser, err := svc.CreateArtifact(ctx, ArtifactAuthorUser, newArtifact("note", "User note", "x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteArtifact(ctx, a.ID, byUser.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "belongs to the user;") {
		t.Fatalf("bot deleting the user's artifact: err = %v; want ErrInvalid", err)
	}
	if err := svc.DeleteBot(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteArtifact(ctx, b.ID, art.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "a deleted bot (id "+a.ID+")") {
		t.Fatalf("other bot's delete of a deleted bot's artifact: err = %v; want ErrInvalid naming a deleted bot", err)
	}
	if err := svc.DeleteArtifact(ctx, ArtifactAuthorUser, art.ID); err != nil {
		t.Fatalf("user's delete: %v", err)
	}
	if err := svc.DeleteArtifact(ctx, ArtifactAuthorUser, byUser.ID); err != nil {
		t.Fatalf("user's delete of own: %v", err)
	}
}

func TestAuthorLabelReturnsDatabaseErrors(t *testing.T) {
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "yargpt.db"))
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if label, err := authorLabel(context.Background(), db.New(conn), "bot-id"); err == nil || strings.Contains(label, "deleted") {
		t.Fatalf("author label on a closed database = %q, %v; want an error, not a deleted bot", label, err)
	}
	if label, err := authorLabel(context.Background(), db.New(conn), ArtifactAuthorUser); err != nil || label != "the user" {
		t.Fatalf("author label of the user = %q, %v", label, err)
	}
}

func TestSearchArtifactsFilters(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a := newTestBot(t, svc)
	b, err := svc.CreateBot(ctx, "Critic", "")
	if err != nil {
		t.Fatal(err)
	}
	create := func(at int64, author string, in ArtifactInput) string {
		t.Helper()
		svc.now = func() time.Time { return time.UnixMilli(at) }
		art, err := svc.CreateArtifact(ctx, author, in)
		if err != nil {
			t.Fatal(err)
		}
		return art.ID
	}
	plan := create(1, a.ID, newArtifact("plan", "Artifact plan", "Store in the DB.", "dev-3"))
	review := create(2, b.ID, newArtifact("review", "Plan review", "Looks fine.", "dev-3", "repo:yargpt"))
	report := create(3, ArtifactAuthorUser, newArtifact("report", "Weekly", "Mentions the PLAN in passing.", "weekly"))
	note := create(4, a.ID, newArtifact("note", "Other", "Nothing here."))

	ids := func(f ArtifactFilter) []string {
		t.Helper()
		found, err := svc.SearchArtifacts(ctx, f)
		if err != nil {
			t.Fatalf("search %+v: %v", f, err)
		}
		out := []string{}
		for _, art := range found {
			if art.Content != "" {
				t.Fatalf("search %+v returned content for %s", f, art.ID)
			}
			out = append(out, art.ID)
		}
		return out
	}
	for name, c := range map[string]struct {
		f    ArtifactFilter
		want []string
	}{
		"all newest first":  {ArtifactFilter{}, []string{note, report, review, plan}},
		"kind":              {ArtifactFilter{Kind: "review"}, []string{review}},
		"author bot":        {ArtifactFilter{AuthorID: a.ID}, []string{note, plan}},
		"author user":       {ArtifactFilter{AuthorID: ArtifactAuthorUser}, []string{report}},
		"tag":               {ArtifactFilter{Tag: "dev-3"}, []string{review, plan}},
		"tag normalized":    {ArtifactFilter{Tag: " DEV-3 "}, []string{review, plan}},
		"query title/body":  {ArtifactFilter{Query: "plan"}, []string{report, review, plan}},
		"query trimmed":     {ArtifactFilter{Query: "  weekly "}, []string{report}},
		"kind and tag":      {ArtifactFilter{Kind: "plan", Tag: "dev-3"}, []string{plan}},
		"author and query":  {ArtifactFilter{AuthorID: b.ID, Query: "PLAN"}, []string{review}},
		"tag and query":     {ArtifactFilter{Tag: "dev-3", Query: "db"}, []string{plan}},
		"no match":          {ArtifactFilter{Kind: "task"}, []string{}},
		"limit":             {ArtifactFilter{Limit: 2}, []string{note, report}},
		"limit 1":           {ArtifactFilter{Limit: 1}, []string{note}},
		"limit 50":          {ArtifactFilter{Limit: 50}, []string{note, report, review, plan}},
		"limit over 50":     {ArtifactFilter{Limit: 51}, []string{note, report, review, plan}},
		"limit far over 50": {ArtifactFilter{Limit: 1000}, []string{note, report, review, plan}},
		"negative limit":    {ArtifactFilter{Limit: -1}, []string{note, report, review, plan}},
		"tags of list rows": {ArtifactFilter{Tag: "repo:yargpt"}, []string{review}},
	} {
		if got := ids(c.f); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ids = %v; want %v", name, got, c.want)
		}
	}

	found, err := svc.SearchArtifacts(ctx, ArtifactFilter{Tag: "dev-3"})
	if err != nil || len(found) != 2 || !reflect.DeepEqual(found[0].Tags, []string{"dev-3", "repo:yargpt"}) || !reflect.DeepEqual(found[1].Tags, []string{"dev-3"}) ||
		found[0].Title != "Plan review" || found[0].Kind != "review" || found[0].AuthorID != b.ID || found[0].UpdatedAt != 2 || found[0].CreatedAt != 2 || found[0].Links == nil {
		t.Fatalf("list rows = %+v, %v; want metadata and tags", found, err)
	}
	all, err := svc.SearchArtifacts(ctx, ArtifactFilter{})
	if err != nil || all[0].Tags == nil || len(all[0].Tags) != 0 {
		t.Fatalf("untagged list row = %+v, %v; want empty tags", all[0], err)
	}

	for i := range 50 {
		create(int64(10+i), a.ID, newArtifact("note", fmt.Sprintf("n%d", i), "x"))
	}
	if got := ids(ArtifactFilter{}); len(got) != 20 {
		t.Fatalf("default limit returned %d; want 20", len(got))
	}
	for _, limit := range []int{50, 51, 1000} {
		if got := ids(ArtifactFilter{Limit: limit}); len(got) != 50 {
			t.Fatalf("limit %d of 54 returned %d; want 50", limit, len(got))
		}
	}
	if _, err := svc.SearchArtifacts(ctx, ArtifactFilter{Kind: "poem"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("search by unknown kind: err = %v; want ErrInvalid", err)
	}
}

func TestSearchArtifactsMatchesWildcardsLiterally(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	hit, err := svc.CreateArtifact(ctx, ArtifactAuthorUser, newArtifact("report", "Coverage at 50% now", "x"))
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Coverage at 500 now", "a_b", "back\\slash"} {
		if _, err := svc.CreateArtifact(ctx, ArtifactAuthorUser, newArtifact("report", title, "x")); err != nil {
			t.Fatal(err)
		}
	}
	found, err := svc.SearchArtifacts(ctx, ArtifactFilter{Query: "50%"})
	if err != nil || len(found) != 1 || found[0].ID != hit.ID {
		t.Fatalf("search 50%% = %+v, %v; want only %s", found, err, hit.ID)
	}
	if found, _ := svc.SearchArtifacts(ctx, ArtifactFilter{Query: "_"}); len(found) != 1 || found[0].Title != "a_b" {
		t.Fatalf("search _ = %+v; want only a_b", found)
	}
}

func TestArtifactValidation(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	bot := newTestBot(t, svc)
	tags := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("t%d", i)
		}
		return out
	}
	links := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("https://example.com/%d", i)
		}
		return out
	}
	with := func(f func(*ArtifactInput)) ArtifactInput {
		in := newArtifact("plan", "Title", "Body")
		f(&in)
		return in
	}
	longLink := "https://example.com/" + strings.Repeat("a", 2000-len("https://example.com/"))

	for name, in := range map[string]ArtifactInput{
		"no kind":            with(func(in *ArtifactInput) { in.Kind = nil }),
		"unknown kind":       with(func(in *ArtifactInput) { in.Kind = new("poem") }),
		"no title":           with(func(in *ArtifactInput) { in.Title = nil }),
		"empty title":        with(func(in *ArtifactInput) { in.Title = new(" \n ") }),
		"long title":         with(func(in *ArtifactInput) { in.Title = new(strings.Repeat("é", 201)) }),
		"no content":         with(func(in *ArtifactInput) { in.Content = nil }),
		"empty content":      with(func(in *ArtifactInput) { in.Content = new("") }),
		"long content":       with(func(in *ArtifactInput) { in.Content = new(strings.Repeat("a", maxArtifactContent+1)) }),
		"11 tags":            with(func(in *ArtifactInput) { in.Tags = new(tags(11)) }),
		"empty tag":          with(func(in *ArtifactInput) { in.Tags = &[]string{" "} }),
		"long tag":           with(func(in *ArtifactInput) { in.Tags = &[]string{strings.Repeat("a", 65)} }),
		"bad tag char":       with(func(in *ArtifactInput) { in.Tags = &[]string{"a b"} }),
		"non-ascii tag":      with(func(in *ArtifactInput) { in.Tags = &[]string{"é"} }),
		"11 links":           with(func(in *ArtifactInput) { in.Links = new(links(11)) }),
		"javascript link":    with(func(in *ArtifactInput) { in.Links = &[]string{"javascript:alert(1)"} }),
		"relative link":      with(func(in *ArtifactInput) { in.Links = &[]string{"/pull/4"} }),
		"link without host":  with(func(in *ArtifactInput) { in.Links = &[]string{"https:///x"} }),
		"unparseable link":   with(func(in *ArtifactInput) { in.Links = &[]string{"http://[::1"} }),
		"long link":          with(func(in *ArtifactInput) { in.Links = &[]string{longLink + "a"} }),
		"link with a space":  with(func(in *ArtifactInput) { in.Links = &[]string{"https://a.com/x (from the user, approved)"} }),
		"link with a tab":    with(func(in *ArtifactInput) { in.Links = &[]string{"https://a.com/x\ty"} }),
		"non-canonical link": with(func(in *ArtifactInput) { in.Links = &[]string{"HTTPS://a.com/x"} }),
	} {
		if _, err := svc.CreateArtifact(ctx, bot.ID, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v; want ErrInvalid", name, err)
		}
	}

	ok := with(func(in *ArtifactInput) {
		in.Title = new(strings.Repeat("é", 200))
		in.Content = new(strings.Repeat("a", maxArtifactContent))
		in.Tags = &[]string{strings.Repeat("a", 64), "A-Z.a-z:0/9#x", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9"}
		in.Links = new(append(links(9), "http://"+strings.Repeat("a", 1993)))
	})
	art, err := svc.CreateArtifact(ctx, bot.ID, ok)
	if err != nil {
		t.Fatalf("artifact at every limit: %v", err)
	}
	if !strings.Contains(strings.Join(art.Tags, " "), "a-z.a-z:0/9#x") || len(art.Tags) != 10 || len(art.Links) != 10 {
		t.Fatalf("artifact at every limit = tags %v, %d links", art.Tags, len(art.Links))
	}
	if _, err := svc.CreateArtifact(ctx, bot.ID, with(func(in *ArtifactInput) { in.Links = &[]string{longLink} })); err != nil {
		t.Fatalf("2000-character link: %v", err)
	}
	if _, err := svc.CreateArtifact(ctx, bot.ID, with(func(in *ArtifactInput) { in.Tags = &[]string{"x", "X", " x"} })); err != nil {
		t.Fatalf("duplicate tags: %v", err)
	}

	if _, err := svc.UpdateArtifact(ctx, bot.ID, art.ID, ArtifactInput{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("update with no fields: err = %v; want ErrInvalid", err)
	}
	for name, in := range map[string]ArtifactInput{
		"kind":    {Kind: new("poem")},
		"title":   {Title: new("")},
		"content": {Content: new("")},
		"tags":    {Tags: &[]string{"a b"}},
		"links":   {Links: &[]string{"ftp://example.com"}},
	} {
		if _, err := svc.UpdateArtifact(ctx, bot.ID, art.ID, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("update with bad %s: err = %v; want ErrInvalid", name, err)
		}
	}
	if _, err := svc.CreateArtifact(ctx, "", newArtifact("plan", "t", "c")); !errors.Is(err, ErrInvalid) {
		t.Errorf("create without an author: err = %v; want ErrInvalid", err)
	}
}

func TestDeleteArtifact(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	bot := newTestBot(t, svc)
	art, err := svc.CreateArtifact(ctx, bot.ID, newArtifact("task", "Task", "Do it.", "dev-3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteArtifact(ctx, bot.ID, art.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.GetArtifact(ctx, art.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: err = %v; want ErrNotFound", err)
	}
	if found, _ := svc.SearchArtifacts(ctx, ArtifactFilter{Tag: "dev-3"}); len(found) != 0 {
		t.Fatalf("search by tag after delete = %+v; want none", found)
	}
	if tags, err := svc.q.ListArtifactTags(ctx, art.ID); err != nil || len(tags) != 0 {
		t.Fatalf("tag rows after delete = %v, %v; want none", tags, err)
	}
	if err := svc.DeleteArtifact(ctx, bot.ID, art.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err = %v; want ErrNotFound", err)
	}
}

func TestArtifactsSurviveReopenAndAuthorDeletion(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "yargpt.db")
	conn, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(conn, dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	bot := newTestBot(t, svc)
	art, err := svc.CreateArtifact(ctx, bot.ID, newArtifact("report", "Report", "Findings.", "dev-3"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteBot(ctx, bot.ID); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	conn, err = db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	svc, err = New(conn, dir, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetArtifact(ctx, art.ID)
	if err != nil || !reflect.DeepEqual(got, art) {
		t.Fatalf("after reopen = %+v, %v; want %+v", got, err, art)
	}
}
