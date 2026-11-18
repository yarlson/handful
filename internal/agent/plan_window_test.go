package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"handful/internal/core"
)

// A turn stores the plan window of the rate_limit_event that Claude Code 2.1.325 sent while a
// helper ran: the weekly window at 78% with a warning at 75%.
func TestATurnStoresThePlanWindowOfItsRateLimitEvent(t *testing.T) {
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
	got, err := h.svc.PlanWindows(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("plan windows = %+v, %v; want the weekly window", got, err)
	}
	got[0].ReceivedAt = 0
	want := core.PlanWindow{Window: "seven_day", Status: "allowed_warning", Utilization: new(0.78), ResetsAt: 1795136400, SurpassedThreshold: new(0.75)}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("plan window = %+v; want %+v", got[0], want)
	}
}

// The windows of a rate_limit_event come from unifiedWindows, by name; without it, from the
// top-level window. A name of another form and the windows over the bound are left out.
func TestARateLimitEventGivesItsPlanWindows(t *testing.T) {
	t.Parallel()
	var many []string
	for i := range maxPlanWindows + 1 {
		many = append(many, fmt.Sprintf(`"w%02d":{"status":"allowed"}`, i))
	}
	for _, c := range []struct {
		name, info string
		want       []core.PlanWindow
	}{
		{
			"unified windows win over the top level",
			`{"status":"allowed_warning","rateLimitType":"seven_day","utilization":0.9,"unifiedWindows":{
				"seven_day":{"status":"allowed","utilization":0.5,"resetsAt":20},
				"five_hour":{"status":"rejected","resetsAt":10,"surpassedThreshold":1}}}`,
			[]core.PlanWindow{
				{Window: "five_hour", Status: "rejected", ResetsAt: 10, SurpassedThreshold: new(1.0)},
				{Window: "seven_day", Status: "allowed", Utilization: new(0.5), ResetsAt: 20},
			},
		},
		{
			"the top level without unified windows",
			`{"status":"allowed_warning","resetsAt":30,"rateLimitType":"seven_day_opus","surpassedThreshold":0.75}`,
			[]core.PlanWindow{{Window: "seven_day_opus", Status: "allowed_warning", ResetsAt: 30, SurpassedThreshold: new(0.75)}},
		},
		{
			"empty unified windows",
			`{"status":"allowed","rateLimitType":"five_hour","utilization":0,"unifiedWindows":{}}`,
			[]core.PlanWindow{{Window: "five_hour", Status: "allowed", Utilization: new(0.0)}},
		},
		{"no window name", `{"status":"allowed"}`, nil},
		{
			"names of another form",
			`{"unifiedWindows":{"Seven Day":{"status":"allowed"},"` + strings.Repeat("a", 65) + `":{"status":"allowed"},"` + strings.Repeat("b", 64) + `":{"status":"allowed"}}}`,
			[]core.PlanWindow{{Window: strings.Repeat("b", 64), Status: "allowed"}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var ev streamEvent
			if err := json.Unmarshal([]byte(`{"type":"rate_limit_event","rate_limit_info":`+c.info+`}`), &ev); err != nil {
				t.Fatal(err)
			}
			if got := ev.RateLimitInfo.planWindows(); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("windows of %s = %+v; want %+v", c.info, got, c.want)
			}
		})
	}
	var ev streamEvent
	if err := json.Unmarshal([]byte(`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{`+strings.Join(many, ",")+`}}}`), &ev); err != nil {
		t.Fatal(err)
	}
	got := ev.RateLimitInfo.planWindows()
	if len(got) != maxPlanWindows || got[0].Window != "w00" || got[maxPlanWindows-1].Window != fmt.Sprintf("w%02d", maxPlanWindows-1) {
		t.Fatalf("windows of %d = %+v; want the first %d by name", maxPlanWindows+1, got, maxPlanWindows)
	}
}
