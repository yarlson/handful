package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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

// The plan windows route gives an empty list before Claude Code reports a window, then the latest
// state of each window.
func TestThePlanWindowsRouteGivesEachWindowsLatestState(t *testing.T) {
	h, svc := newTestHandler(t)
	if rec := do(h, "GET", "/api/plan-windows", "", nil); rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Fatalf("plan windows before a report = %d %s; want []", rec.Code, rec.Body)
	}
	used, threshold := 0.78, 0.75
	weekly := core.PlanWindow{Window: "seven_day", Status: "allowed_warning", Utilization: &used, ResetsAt: 1795136400, SurpassedThreshold: &threshold}
	if err := svc.RecordPlanWindows(context.Background(), []core.PlanWindow{weekly, {Window: "five_hour", Status: "allowed"}}); err != nil {
		t.Fatal(err)
	}
	rec := do(h, "GET", "/api/plan-windows", "", nil)
	var got []core.PlanWindow
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil || len(got) != 2 {
		t.Fatalf("plan windows = %d %s; want two", rec.Code, rec.Body)
	}
	for _, field := range []string{`"window":"seven_day"`, `"status":"allowed_warning"`, `"utilization":0.78`, `"resetsAt":1795136400`, `"surpassedThreshold":0.75`, `"receivedAt":`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("plan windows %s; want %s", rec.Body, field)
		}
	}
	if strings.Contains(rec.Body.String(), `"window":"five_hour","status":"allowed","utilization"`) {
		t.Errorf("plan windows %s; want no utilization for a window without one", rec.Body)
	}
}
