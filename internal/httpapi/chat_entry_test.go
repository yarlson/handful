package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"handful/internal/core"
)

func TestAPostIsReadOnlyFromItsOwnChat(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	bot, err := svc.CreateBot(ctx, "Scout", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	post, err := svc.AppendEntry(ctx, bot.ID, core.KindBot, "", map[string]string{"text": "the release is out"})
	if err != nil {
		t.Fatal(err)
	}
	rec := do(h, "GET", "/api/bots/"+bot.ID+"/entries/"+post.ID, "", nil)
	var got core.Entry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil || got.ID != post.ID {
		t.Fatalf("own chat: %d %s; want 200 with %s", rec.Code, rec.Body, post.ID)
	}
	for _, path := range []string{"/api/bots/" + other.ID + "/entries/" + post.ID, "/api/bots/" + bot.ID + "/entries/missing"} {
		if rec := do(h, "GET", path, "", nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d %s; want 404", path, rec.Code, rec.Body)
		}
	}
}

func TestAViewingPhoneMayReadAPost(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		ctx := context.Background()
		bot, err := f.svc.CreateBot(ctx, "Scout", "")
		if err != nil {
			t.Fatal(err)
		}
		post, err := f.svc.AppendEntry(ctx, bot.ID, core.KindUser, "", map[string]string{"text": "hi"})
		if err != nil {
			t.Fatal(err)
		}
		token, _ := f.pair(core.ScopeView)
		if rec := f.do(req{path: "/api/bots/" + bot.ID + "/entries/" + post.ID, session: token}); rec.Code != http.StatusOK {
			t.Fatalf("read a post with a view session: %d %s; want 200", rec.Code, rec.Body)
		}
	})
}
