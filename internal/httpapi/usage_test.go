package httpapi

import (
	"context"
	"net/http"
	"testing"

	"handful/internal/agent"
	"handful/internal/core"
)

// The usage route gives the window that --autocompact uses and the bot's last recorded turn, or
// null before one; an unknown bot is not found.
func TestTheUsageRouteGivesTheWindowAndTheLastTurn(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	bot, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(h, "GET", "/api/bots/"+bot.ID+"/usage", "", nil); rec.Code != http.StatusOK || rec.Body.String() != `{"windowTokens":200000,"last":null}`+"\n" {
		t.Fatalf("usage before a turn = %d %s", rec.Code, rec.Body)
	}
	if agent.AutocompactWindow != 200000 {
		t.Fatalf("AutocompactWindow = %d; this test's body assumes 200000", agent.AutocompactWindow)
	}
	turn, err := svc.StartTaskTurn(ctx, bot.ID, "", "[task]", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordTurnUsage(ctx, bot.ID, turn.ID, core.TurnUsage{InputTokens: 4, CacheReadTokens: 9443, CacheWriteTokens: 3659, OutputTokens: 306, Calls: 2, ContextTokens: 6665, Cold: true}); err != nil {
		t.Fatal(err)
	}
	want := `{"windowTokens":200000,"last":{"inputTokens":4,"cacheReadTokens":9443,"cacheWriteTokens":3659,"outputTokens":306,"calls":2,"contextTokens":6665,"cold":true}}` + "\n"
	if rec := do(h, "GET", "/api/bots/"+bot.ID+"/usage", "", nil); rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("usage after a turn = %d %s; want %s", rec.Code, rec.Body, want)
	}
	if rec := do(h, "GET", "/api/bots/nobody/usage", "", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("usage of an unknown bot = %d %s; want 404", rec.Code, rec.Body)
	}
}
