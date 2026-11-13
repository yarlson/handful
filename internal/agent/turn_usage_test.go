package agent

import (
	"context"
	"path/filepath"
	"testing"

	"handful/internal/core"
)

// Each turn stores the tokens that Claude Code 2.1.323 reported, from its recorded stream: the
// result's totals, the number of main-thread calls and the last call's context. A new session's
// first call writes most of its context to the cache (cold); the resumed one reads it.
func TestATurnRecordsItsTokensAndWhetherItWasCold(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		stream string
		want   core.TurnUsage
	}{
		{"cold-first-turn.jsonl", core.TurnUsage{InputTokens: 4, CacheReadTokens: 9443, CacheWriteTokens: 3659, OutputTokens: 306, Calls: 2, ContextTokens: 6665, Cold: true}},
		{"warm-resumed-turn.jsonl", core.TurnUsage{InputTokens: 4, CacheReadTokens: 13556, CacheWriteTokens: 363, OutputTokens: 78, Calls: 2, ContextTokens: 7028, Cold: false}},
	} {
		t.Run(c.stream, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, "replay")
			stream, err := filepath.Abs(filepath.Join("testdata", "claude-2.1.323", c.stream))
			if err != nil {
				t.Fatal(err)
			}
			h.env = append(h.env, "FAKE_STREAM="+stream)
			bot := h.newBot(t)
			h.start(t)
			ctx := context.Background()
			if _, ok, _ := h.svc.LastTurnUsage(ctx, bot.ID); ok {
				t.Fatal("a bot with no turn has a recorded usage")
			}
			h.say(t, bot.ID, "go")
			got, ok, err := h.svc.LastTurnUsage(ctx, bot.ID)
			if err != nil || !ok || got != c.want {
				t.Fatalf("recorded usage = %+v, %v, %v; want %+v", got, ok, err, c.want)
			}
		})
	}
}

// A turn that made no model call, as one whose model Claude Code can't run, records nothing.
func TestATurnWithoutAModelCallRecordsNoTokens(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "replay")
	stream, err := filepath.Abs(filepath.Join("testdata", "claude-2.1.319", "unknown-model.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	h.env = append(h.env, "FAKE_STREAM="+stream)
	bot := h.newBot(t)
	h.start(t)
	h.say(t, bot.ID, "go")
	if got, ok, _ := h.svc.LastTurnUsage(context.Background(), bot.ID); ok {
		t.Fatalf("recorded usage = %+v; want none", got)
	}
}

// The cold rule and the call count, at their edges: half of the context written is not cold, a
// call's later events and a subagent's calls don't count.
func TestTurnUsageCountsMainCallsAndTheColdRule(t *testing.T) {
	t.Parallel()
	call := func(id string, parent *string, in, write, read int64) streamEvent {
		return streamEvent{Type: "assistant", ParentToolUseID: parent, Message: &streamMessage{ID: id, Usage: &tokenUsage{InputTokens: in, CacheCreationInputTokens: write, CacheReadInputTokens: read}}}
	}
	sub := "agent-call"
	result := streamEvent{Type: "result", Usage: &tokenUsage{InputTokens: 7, CacheCreationInputTokens: 8, CacheReadInputTokens: 9, OutputTokens: 10}}
	usage := func(calls, context int64, cold bool) core.TurnUsage {
		return core.TurnUsage{InputTokens: 7, CacheReadTokens: 9, CacheWriteTokens: 8, OutputTokens: 10, Calls: calls, ContextTokens: context, Cold: cold}
	}
	for _, c := range []struct {
		name  string
		calls []streamEvent
		want  core.TurnUsage
		ok    bool
	}{
		{"half written is warm", []streamEvent{call("a", nil, 0, 50, 50)}, usage(1, 100, false), true},
		{"one more written is cold", []streamEvent{call("a", nil, 0, 51, 50)}, usage(1, 101, true), true},
		{"only the first call decides", []streamEvent{call("a", nil, 1, 10, 90), call("a", nil, 1, 10, 90), call("s", &sub, 0, 900, 0), call("b", nil, 0, 400, 100)}, usage(2, 500, false), true},
		{"no call", []streamEvent{call("s", &sub, 0, 9, 0), {Type: "assistant", Message: &streamMessage{}}}, core.TurnUsage{}, false},
	} {
		var calls callTokens
		for _, ev := range c.calls {
			calls.note(ev)
		}
		if got, ok := turnUsage(result, calls); got != c.want || ok != c.ok {
			t.Errorf("%s: turnUsage = %+v, %v; want %+v, %v", c.name, got, ok, c.want, c.ok)
		}
	}
	var one callTokens
	one.note(call("a", nil, 0, 1, 1))
	if _, ok := turnUsage(streamEvent{Type: "result"}, one); ok {
		t.Error("turnUsage of a result without usage = ok; want none")
	}
}
