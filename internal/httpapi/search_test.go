package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"handful/internal/core"
)

func TestSearchFindsAChatsMessagesAndRefusesABadQuery(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	bot, err := svc.CreateBot(ctx, "Scout", "")
	if err != nil {
		t.Fatal(err)
	}
	match, err := svc.AppendEntry(ctx, bot.ID, core.KindUser, "", map[string]string{"text": "50% of the build"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendEntry(ctx, bot.ID, core.KindUser, "", map[string]string{"text": "500 builds"}); err != nil {
		t.Fatal(err)
	}
	rec := do(h, "GET", "/api/bots/"+bot.ID+"/search?q="+url.QueryEscape("0% OF"), "", nil)
	var result struct {
		Entries []core.Entry `json:"entries"`
		More    *bool        `json:"more"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("search: %d %s", rec.Code, rec.Body)
	}
	if len(result.Entries) != 1 || result.Entries[0].ID != match.ID || result.More == nil || *result.More {
		t.Fatalf("search = %s; want only the 50%% message and more false", rec.Body)
	}
	for _, q := range []string{"", "a", url.QueryEscape(strings.Repeat("я", core.MaxSearchQuery+1))} {
		if rec := do(h, "GET", "/api/bots/"+bot.ID+"/search?q="+q, "", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("q of %d bytes: %d %s; want 400", len(q), rec.Code, rec.Body)
		}
	}
}

// A phone that may only view a chat may search it.
func TestAViewingPhoneMaySearch(t *testing.T) {
	bothModes(t, func(t *testing.T, f *phoneFixture) {
		bot, err := f.svc.CreateBot(context.Background(), "Scout", "")
		if err != nil {
			t.Fatal(err)
		}
		token, _ := f.pair(core.ScopeView)
		if rec := f.do(req{path: "/api/bots/" + bot.ID + "/search?q=ab", session: token}); rec.Code != http.StatusOK {
			t.Fatalf("search with a view session: %d %s; want 200", rec.Code, rec.Body)
		}
	})
}
