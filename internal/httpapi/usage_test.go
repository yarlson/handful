package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"handful/internal/agent"
	"handful/internal/core"
	"handful/internal/db/dbtest"
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

// The spend route gives each bot's cost and helpers in the last 24h or 7d, the largest cost
// first; another period is a bad request.
func TestTheSpendRouteGivesEachBotsSpendInThePeriod(t *testing.T) {
	h, svc := newTestHandler(t)
	ctx := context.Background()
	if rec := do(h, "GET", "/api/spend?period=24h", "", nil); rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Fatalf("spend before a turn = %d %s; want []", rec.Code, rec.Body)
	}
	ana, err := svc.CreateBot(ctx, "Ana", "")
	if err != nil {
		t.Fatal(err)
	}
	record := func(cost float64, helpers int64) string {
		t.Helper()
		turn, err := svc.StartTaskTurn(ctx, ana.ID, "", "[task]", 0, "")
		if err != nil {
			t.Fatal(err)
		}
		sp := core.TurnSpend{Models: map[string]core.ModelSpend{"claude-haiku-5-5": {CostUSD: cost}}, Helpers: helpers}
		if err := svc.RecordTurnSpend(ctx, ana.ID, turn.ID, sp); err != nil {
			t.Fatal(err)
		}
		return turn.ID
	}
	record(0.25, 1)
	older := record(2, 3)
	conn, err := dbtest.Open(ctx, filepath.Join(svc.DataDir(), "yargpt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	twoDaysAgo := time.Now().Add(-48 * time.Hour).UnixMilli()
	if _, err := conn.ExecContext(ctx, `UPDATE turn_spend SET recorded_at = ? WHERE turn_id = ?`, twoDaysAgo, older); err != nil {
		t.Fatal(err)
	}
	for period, want := range map[string]string{
		"24h": `[{"botId":"` + ana.ID + `","costUsd":0.25,"helpers":1}]` + "\n",
		"7d":  `[{"botId":"` + ana.ID + `","costUsd":2.25,"helpers":4}]` + "\n",
	} {
		if rec := do(h, "GET", "/api/spend?period="+period, "", nil); rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("spend of %s = %d %s; want %s", period, rec.Code, rec.Body, want)
		}
	}
	for _, query := range []string{"", "?period=30d", "?period=24H"} {
		if rec := do(h, "GET", "/api/spend"+query, "", nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "use 24h or 7d") {
			t.Errorf("spend%s = %d %s; want 400 that names 24h and 7d", query, rec.Code, rec.Body)
		}
	}
}
