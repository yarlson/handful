package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

// storedSpend is the bot's stored spend of the last day and its turns' stored tokens per model.
func storedSpend(t *testing.T, h *harness, botID string) ([]core.BotSpend, map[string]core.ModelSpend) {
	t.Helper()
	ctx := context.Background()
	bots, err := h.svc.SpendSince(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := h.conn.QueryContext(ctx, `SELECT m.model, m.input_tokens, m.output_tokens, m.cache_read_tokens, m.cache_write_tokens, m.cost_usd
		FROM turn_model_spend m JOIN turn_spend s ON s.turn_id = m.turn_id WHERE s.bot_id = ?`, botID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	models := map[string]core.ModelSpend{}
	for rows.Next() {
		var model string
		var m core.ModelSpend
		if err := rows.Scan(&model, &m.InputTokens, &m.OutputTokens, &m.CacheReadTokens, &m.CacheWriteTokens, &m.CostUSD); err != nil {
			t.Fatal(err)
		}
		models[model] = m
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return bots, models
}

// A turn stores the spend of the result event that Claude Code 2.1.325 sent after a Haiku turn
// with one helper: its cost, the Haiku tokens of modelUsage and the helper.
func TestATurnStoresTheSpendOfItsResult(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "replay")
	stream, err := filepath.Abs(filepath.Join("testdata", "claude-2.1.325", "haiku-helper.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	h.env = append(h.env, "FAKE_STREAM="+stream)
	bot := h.newBot(t)
	h.start(t)
	h.say(t, bot.ID, "go")
	bots, models := storedSpend(t, h, bot.ID)
	wantBots := []core.BotSpend{{BotID: bot.ID, CostUSD: 0.00513185, Helpers: 1}}
	if !reflect.DeepEqual(bots, wantBots) {
		t.Errorf("spend of the day = %+v; want %+v", bots, wantBots)
	}
	wantModels := map[string]core.ModelSpend{"claude-haiku-5-5": {InputTokens: 8, OutputTokens: 410, CacheReadTokens: 26715, CacheWriteTokens: 28048, CostUSD: 0.00513185}}
	if !reflect.DeepEqual(models, wantModels) {
		t.Errorf("spend per model = %+v; want %+v", models, wantModels)
	}
}

// A turn with two result events, as when a background task wakes the model before the turn
// ends, counts each result's own spend once. total_cost_usd and modelUsage run on across the
// session, so the second result's 0.30 already holds the first's 0.10.
func TestATurnWithTwoResultsCountsEachCostOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "replay")
	stream := filepath.Join(t.TempDir(), "two-results.jsonl")
	lines := []string{
		`{"type":"command_lifecycle","command_uuid":"COMMAND_UUID","state":"started"}`,
		`{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":3,"cache_read_input_tokens":100}},"parent_tool_use_id":null}`,
		`{"type":"result","subtype":"success","result":"started","result_index":0,"total_cost_usd":0.1,"usage":{"input_tokens":3},` +
			`"modelUsage":{"claude-opus-5-5":{"inputTokens":3,"outputTokens":5,"cacheReadInputTokens":100,"cacheCreationInputTokens":10,"costUSD":0.1}},` +
			`"model_usage_since_last_result":{"claude-opus-5-5":{"inputTokens":3,"outputTokens":5,"cacheReadInputTokens":100,"cacheCreationInputTokens":10,"costUSD":0.1}},` +
			`"subagent_stats":{"spawned":1}}`,
		`{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":2,"cache_read_input_tokens":200}},"parent_tool_use_id":null}`,
		`{"type":"result","subtype":"success","result":"done","result_index":1,"total_cost_usd":0.3,"usage":{"input_tokens":2},` +
			`"modelUsage":{"claude-opus-5-5":{"inputTokens":5,"outputTokens":12,"cacheReadInputTokens":300,"cacheCreationInputTokens":30,"costUSD":0.25},"claude-sonnet-5-5":{"inputTokens":1,"outputTokens":1,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":0.05}},` +
			`"model_usage_since_last_result":{"claude-opus-5-5":{"inputTokens":2,"outputTokens":7,"cacheReadInputTokens":200,"cacheCreationInputTokens":20,"costUSD":0.15},"claude-sonnet-5-5":{"inputTokens":1,"outputTokens":1,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"costUSD":0.05}},` +
			`"subagent_stats":{"spawned":2}}`,
		`{"type":"command_lifecycle","command_uuid":"COMMAND_UUID","state":"completed"}`,
	}
	if err := os.WriteFile(stream, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.env = append(h.env, "FAKE_STREAM="+stream)
	bot := h.newBot(t)
	h.start(t)
	h.say(t, bot.ID, "go")
	bots, models := storedSpend(t, h, bot.ID)
	if len(bots) != 1 || bots[0].Helpers != 2 || bots[0].CostUSD < 0.2999999 || bots[0].CostUSD > 0.3000001 {
		t.Errorf("spend of the day = %+v; want cost 0.30 and 2 helpers", bots)
	}
	wantModels := map[string]core.ModelSpend{
		"claude-opus-5-5":   {InputTokens: 5, OutputTokens: 12, CacheReadTokens: 300, CacheWriteTokens: 30, CostUSD: 0.25},
		"claude-sonnet-5-5": {InputTokens: 1, OutputTokens: 1, CostUSD: 0.05},
	}
	if !reflect.DeepEqual(models, wantModels) {
		t.Errorf("spend per model = %+v; want %+v", models, wantModels)
	}
}

// Each result spends what model_usage_since_last_result holds, and the helpers that
// subagent_stats.spawned added since the process's previous result. The numbers are Claude Code
// 2.1.327's in one process of four results, then a resumed process whose first total_cost_usd
// held the earlier process's cost too.
func TestEachResultSpendsWhatItAddedInItsProcess(t *testing.T) {
	t.Parallel()
	result := func(since float64, spawned int64) streamEvent {
		return streamEvent{
			Type:                      "result",
			ModelUsageSinceLastResult: map[string]modelUsage{"claude-haiku-5-5": {InputTokens: 2, CostUSD: since}},
			SubagentStats:             &subagentStats{Spawned: spawned},
		}
	}
	var first spendMeter
	for i, c := range []struct {
		ev          streamEvent
		wantCost    float64
		wantHelpers int64
	}{
		{result(0.00023908, 0), 0.00023908, 0},
		{result(0.00055696, 1), 0.00055696, 1},
		{result(0.00028614, 1), 0.00028614, 0},
		{result(0.00039059, 1), 0.00039059, 0},
	} {
		sp := first.spendOf(c.ev)
		if sp.CostUSD() != c.wantCost || sp.Helpers != c.wantHelpers {
			t.Errorf("result %d: spend = %+v; want cost %v and %d helpers", i, sp, c.wantCost, c.wantHelpers)
		}
	}
	var resumed spendMeter
	if sp := resumed.spendOf(result(9.434e-05, 2)); sp.CostUSD() != 9.434e-05 || sp.Helpers != 2 {
		t.Errorf("a new process's first result: spend = %+v; want cost 9.434e-05 and 2 helpers", sp)
	}
	if sp := resumed.spendOf(streamEvent{Type: "result"}); len(sp.Models) != 0 || sp.Helpers != 0 {
		t.Errorf("a result without spend fields: spend = %+v; want none", sp)
	}
}
