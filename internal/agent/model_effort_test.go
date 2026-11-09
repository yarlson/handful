package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"yargpt/internal/core"
)

// recordedStream reads a stream-json file that Claude Code wrote (see
// testdata/claude-2.1.319/README.md).
func recordedStream(t *testing.T, name string) []json.RawMessage {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "claude-2.1.319", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []json.RawMessage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), maxStreamLine)
	for sc.Scan() {
		lines = append(lines, json.RawMessage(slices.Clone(sc.Bytes())))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// initModel returns the model that the stream's system/init event reports, as streamEvent reads it.
func initModel(t *testing.T, lines []json.RawMessage) string {
	t.Helper()
	for _, l := range lines {
		var ev streamEvent
		if err := json.Unmarshal(l, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type == "system" && ev.Subtype == "init" {
			return ev.Model
		}
	}
	t.Fatal("no init event")
	return ""
}

// Check 1 for Product: the init event names the model in "model", as a full ID.
func TestInitEventReportsTheFullModelIDInItsModelField(t *testing.T) {
	t.Parallel()
	if got := initModel(t, recordedStream(t, "haiku-effort-low.jsonl")); got != "claude-haiku-5-5" {
		t.Fatalf("init model for --model haiku = %q; want claude-haiku-5-5", got)
	}
}

// Check 2 for Product: no event of the stream reports the effort level that
// Claude Code applied, so the UI labels the effort as requested.
func TestStreamDoesNotReportTheAppliedEffort(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"haiku-effort-low.jsonl", "haiku-effort-max.jsonl"} {
		for _, l := range recordedStream(t, name) {
			var v any
			if err := json.Unmarshal(l, &v); err != nil {
				t.Fatal(err)
			}
			walkJSON(v, "", func(key string, val any) {
				if strings.Contains(strings.ToLower(key), "effort") && key != "per_turn_effort_active" {
					t.Errorf("%s: key %q may report the effort", name, key)
				}
				if s, ok := val.(string); ok && slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, s) {
					t.Errorf("%s: %q = %q looks like an effort level", name, key, s)
				}
			})
		}
	}
}

func walkJSON(v any, key string, visit func(key string, val any)) {
	visit(key, v)
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			walkJSON(c, k, visit)
		}
	case []any:
		for _, c := range x {
			walkJSON(c, key, visit)
		}
	}
}

// Check 3 for Product: with ANTHROPIC_MODEL=sonnet in the environment,
// --model haiku runs haiku, so YarGPT keeps the variable.
func TestModelFlagOutranksAnthropicModel(t *testing.T) {
	t.Parallel()
	if got := initModel(t, recordedStream(t, "anthropic-model-sonnet-flag-haiku.jsonl")); got != "claude-haiku-5-5" {
		t.Fatalf("init model = %q; want claude-haiku-5-5 from --model", got)
	}
	got := childEnv([]string{"ANTHROPIC_MODEL=sonnet", "HOME=/h"}, true)
	if !slices.Equal(got, []string{"ANTHROPIC_MODEL=sonnet", "HOME=/h"}) {
		t.Fatalf("childEnv = %q; want ANTHROPIC_MODEL kept", got)
	}
}

func TestArgsPassModelAndEffortOnlyWhenSet(t *testing.T) {
	t.Parallel()
	flagValue := func(args []string, flag string) (string, bool) {
		i := slices.Index(args, flag)
		if i < 0 || i+1 >= len(args) {
			return "", false
		}
		return args[i+1], true
	}
	cases := []struct {
		model, effort string
	}{{"", ""}, {"haiku", ""}, {"", "max"}, {"claude-opus-5-5", "low"}}
	for _, c := range cases {
		run, err := claudeRun{MCPConfig: "m.json", BotName: "B", SessionID: "s", Flags: runFlags{model: c.model, effort: c.effort}}.args()
		if err != nil {
			t.Fatal(err)
		}
		for name, args := range map[string][]string{"turn": run, "compact": compactArgs("s", c.model, c.effort)} {
			for flag, want := range map[string]string{"--model": c.model, "--effort": c.effort} {
				got, ok := flagValue(args, flag)
				if ok != (want != "") || got != want {
					t.Errorf("%s args for model %q effort %q: %s = %q, %v; want %q", name, c.model, c.effort, flag, got, ok, want)
				}
			}
		}
	}
}

func TestPassedValueIsOnlyAValueThatYarGPTSets(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{core.SourceBot: "opus", core.SourceTeam: "opus", core.SourceGlobal: "opus", core.SourceEnv: "", core.SourceClaudeCode: ""} {
		if got := passedValue("opus", source); got != want {
			t.Errorf("passedValue(opus, %s) = %q; want %q", source, got, want)
		}
	}
}

// The executor subagent has no model or effort of its own, so it runs on
// the bot's.
func TestExecutorInheritsTheBotsModelAndEffort(t *testing.T) {
	t.Parallel()
	for name, def := range executorAgents("B") {
		d, _ := def.(map[string]any)
		for _, k := range []string{"model", "effort"} {
			if _, ok := d[k]; ok {
				t.Errorf("subagent %s sets %s", name, k)
			}
		}
	}
}

func TestChildEnvDropsSubagentVariablesAndTheEffortVariableUnderAnEffortFlag(t *testing.T) {
	t.Parallel()
	base := []string{"HOME=/h", "CLAUDE_CODE_EFFORT_LEVEL=low", "CLAUDE_CODE_SUBAGENT_MODEL=haiku", "CLAUDE_CODE_SUBAGENT_EFFORT=low", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1", "CLAUDE_CODE_SUBAGENTS=x", "XCLAUDE_CODE_EFFORT_LEVEL=y"}
	if got, want := childEnv(base, true), []string{"HOME=/h", "CLAUDE_CODE_SUBAGENTS=x", "XCLAUDE_CODE_EFFORT_LEVEL=y"}; !slices.Equal(got, want) {
		t.Errorf("with --effort: %q; want %q", got, want)
	}
	if got, want := childEnv(base, false), []string{"HOME=/h", "CLAUDE_CODE_EFFORT_LEVEL=low", "CLAUDE_CODE_SUBAGENTS=x", "XCLAUDE_CODE_EFFORT_LEVEL=y"}; !slices.Equal(got, want) {
		t.Errorf("without --effort: %q; want %q", got, want)
	}
}

func TestModelMismatchFollowsTheRulesOfTheSpec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		requested, reported string
		want                bool
	}{
		{"opus", "claude-sonnet-5-5", true},
		{"claude-opus-5-5", "claude-sonnet-5-5", true},
		{"haiku", "claude-haiku-5-5", false},
		{"Opus", "claude-opus-5-5", false},
		{"opus", "us.anthropic.claude-opus-5-5-v1:0", false},
		{"sonnet", "us.anthropic.claude-opus-5-5-v1:0", true},
		{"fable", "claude-fable-5-1", false},
		{"opus", "claude-opusx-5", true},
		{"opusplan", "claude-sonnet-5-5", false},
		{"opusplan", "claude-opus-5-5", false},
		{"opusplan", "claude-haiku-5-5", true},
		{"claude-sonnet-5-5[1m]", "claude-sonnet-5-5", false},
		{"claude-sonnet-5-5", "claude-sonnet-5-5[1m]", false},
		{"claude-sonnet-5-5", "CLAUDE-SONNET-5-5", false},
		{"arn:aws:bedrock:us-east-1:1:inference-profile/us.anthropic.claude-opus-5-5", "claude-sonnet-5-5", false},
		{"us.anthropic.claude-opus-5-5-v1:0", "claude-sonnet-5-5", false},
		{"sonnet[1m]", "claude-sonnet-5-5", false},
		{"best", "claude-sonnet-5-5", false},
		{"", "claude-sonnet-5-5", false},
		{"opus", "", false},
	}
	for _, c := range cases {
		if got := modelMismatch(c.requested, c.reported); got != c.want {
			t.Errorf("modelMismatch(%q, %q) = %v; want %v", c.requested, c.reported, got, c.want)
		}
	}
}

func readEnvLog(t *testing.T, h *harness) string {
	t.Helper()
	env, err := os.ReadFile(filepath.Join(h.dir, "env.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(env)
}

func TestAnEffortFromYarGPTRemovesTheEffortVariableAndSubagentVariablesAlwaysGo(t *testing.T) {
	t.Parallel()
	for _, effort := range []string{"max", ""} {
		h := newHarnessWith(t, "reply", "", effort)
		h.env = []string{"CLAUDE_CODE_EFFORT_LEVEL=low", "CLAUDE_CODE_SUBAGENT_MODEL=haiku", "CLAUDE_CODE_SUBAGENT_EFFORT=low", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1", "ANTHROPIC_MODEL=sonnet"}
		bot := h.newBot(t)
		h.start(t)
		if _, _, err := h.svc.SubmitUserMessage(context.Background(), bot.ID, "hi", "n1"); err != nil {
			t.Fatal(err)
		}
		h.waitEntry(t, bot.ID, func(e core.Entry) bool { return e.Kind == core.KindBot })
		env, call := readEnvLog(t, h), h.argsLog(t)[0]
		if strings.Contains(env, "CLAUDE_CODE_SUBAGENT_") {
			t.Errorf("effort %q: env keeps subagent variables:\n%s", effort, env)
		}
		if !strings.Contains(env, "ANTHROPIC_MODEL=sonnet\n") {
			t.Errorf("effort %q: env lost ANTHROPIC_MODEL:\n%s", effort, env)
		}
		if keeps := strings.Contains(env, "CLAUDE_CODE_EFFORT_LEVEL=low\n"); keeps != (effort == "") {
			t.Errorf("effort %q: env keeps CLAUDE_CODE_EFFORT_LEVEL = %v:\n%s", effort, keeps, env)
		}
		if has := strings.Contains(call, "--effort max"); has != (effort != "") {
			t.Errorf("effort %q: args have --effort max = %v: %s", effort, has, call)
		}
	}
}

func TestCompactionRunsWithTheSessionsModelAndEffort(t *testing.T) {
	t.Parallel()
	h := newHarnessWith(t, "compact", "haiku", "high")
	h.env = []string{"CLAUDE_CODE_EFFORT_LEVEL=low"}
	ctx := context.Background()
	bot := h.newBot(t)
	if err := h.svc.SetSession(ctx, bot.ID, "sess-x"); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, err := h.svc.QueueCompaction(ctx, bot.ID); err != nil {
		t.Fatal(err)
	}
	h.waitEntry(t, bot.ID, func(e core.Entry) bool { return bodyField(e, "type") == "context-compacted" })
	if call := h.argsLog(t)[0]; !strings.Contains(call, "--model haiku --effort high") {
		t.Fatalf("compaction call = %s; want --model haiku --effort high", call)
	}
	if env := readEnvLog(t, h); strings.Contains(env, "CLAUDE_CODE_EFFORT_LEVEL") {
		t.Fatalf("compaction env keeps the effort variable:\n%s", env)
	}
}

func TestAMismatchedModelGetsOneNoticeAndIsReported(t *testing.T) {
	t.Parallel()
	h := newHarnessWith(t, "reply", "opus", "")
	h.env = []string{"FAKE_INIT_MODEL=claude-sonnet-5-5"}
	ctx := context.Background()
	bot := h.newBot(t)
	h.start(t)
	for i, nonce := range []string{"n1", "n2"} {
		if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "hi", nonce); err != nil {
			t.Fatal(err)
		}
		n := 0
		h.waitEntry(t, bot.ID, func(e core.Entry) bool {
			if e.Kind == core.KindBot {
				n++
			}
			return n == i+1
		})
	}
	entries, err := h.svc.Entries(ctx, bot.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var notices []string
	firstTurn := ""
	for _, e := range entries {
		if e.Kind == core.KindBot && firstTurn == "" {
			firstTurn = e.TurnID
		}
	}
	for _, e := range entries {
		if bodyField(e, "type") == "model-mismatch" {
			if e.TurnID == "" || e.TurnID != firstTurn {
				t.Errorf("notice turn = %q; want the turn that ran, %q", e.TurnID, firstTurn)
			}
			text, _ := bodyField(e, "text").(string)
			notices = append(notices, text)
		}
	}
	want := "Asked for `opus` (the global setting), Claude Code ran `claude-sonnet-5-5`. Check the model in Settings."
	if len(notices) != 1 || notices[0] != want {
		t.Fatalf("notices = %q; want one %q", notices, want)
	}
	if id, mismatch := h.runner.ModelReport(bot.ID, ""); id != "claude-sonnet-5-5" || !mismatch {
		t.Fatalf("ModelReport = %q, %v; want claude-sonnet-5-5, true", id, mismatch)
	}
}

func TestAMatchingModelIsReportedWithoutANotice(t *testing.T) {
	t.Parallel()
	h := newHarnessWith(t, "reply", "opus", "")
	h.env = []string{"FAKE_INIT_MODEL=claude-opus-5-5"}
	ctx := context.Background()
	bot := h.newBot(t)
	if id, _ := h.runner.ModelReport(bot.ID, ""); id != "" {
		t.Fatalf("ModelReport before a run = %q; want none", id)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "hi", "n1"); err != nil {
		t.Fatal(err)
	}
	h.waitEntry(t, bot.ID, func(e core.Entry) bool { return e.Kind == core.KindBot })
	if id, mismatch := h.runner.ModelReport(bot.ID, ""); id != "claude-opus-5-5" || mismatch {
		t.Fatalf("ModelReport = %q, %v; want claude-opus-5-5, false", id, mismatch)
	}
	entries, err := h.svc.Entries(ctx, bot.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if bodyField(e, "type") == "model-mismatch" {
			t.Fatalf("unexpected notice: %s", e.Body)
		}
	}
}

func TestSettingsResolveModelAndEffortSeparatelyByTurnPlace(t *testing.T) {
	t.Parallel()
	team := core.Bot{Team: &core.Team{DefaultModel: "sonnet", DefaultEffort: "medium"}}
	plainRoom := core.Bot{}
	type want struct{ model, effort, modelSource, effortSource string }
	cases := []struct {
		name           string
		global         [2]string
		bot            core.Bot
		room           core.Bot
		want           want
		postModel, eff string
	}{
		{"nothing set, direct chat", [2]string{}, core.Bot{}, core.Bot{}, want{"", "", core.SourceClaudeCode, core.SourceClaudeCode}, "", ""},
		{"global only", [2]string{"opus", "high"}, core.Bot{}, core.Bot{}, want{"opus", "high", core.SourceGlobal, core.SourceGlobal}, "opus", "high"},
		{"bot model, global effort", [2]string{"opus", "high"}, core.Bot{Model: "haiku"}, core.Bot{}, want{"haiku", "high", core.SourceBot, core.SourceGlobal}, "haiku", "high"},
		{"bot effort only", [2]string{}, core.Bot{Effort: "max"}, core.Bot{}, want{"", "max", core.SourceClaudeCode, core.SourceBot}, "", "max"},
		{"plain room has no default", [2]string{"opus", ""}, core.Bot{}, plainRoom, want{"opus", "", core.SourceGlobal, core.SourceClaudeCode}, "opus", ""},
		{"team fills both gaps", [2]string{"opus", "high"}, core.Bot{}, team, want{"sonnet", "medium", core.SourceTeam, core.SourceTeam}, "sonnet", "medium"},
		{"own model, team effort", [2]string{"opus", "high"}, core.Bot{Model: "haiku"}, team, want{"haiku", "medium", core.SourceBot, core.SourceTeam}, "haiku", "medium"},
		{"own both in team", [2]string{}, core.Bot{Model: "claude-opus-5-5", Effort: "low"}, team, want{"claude-opus-5-5", "low", core.SourceBot, core.SourceBot}, "claude-opus-5-5", "low"},
	}
	for _, c := range cases {
		h := newHarnessWith(t, "reply", c.global[0], c.global[1])
		f, err := h.runner.settings(c.bot, c.room)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := want{f.model, f.effort, f.modelSource, ""}
		e := core.Resolve(c.bot, c.room.Team, h.svc.Defaults())
		got.effortSource = e.EffortSource
		if got != c.want {
			t.Errorf("%s: flags %+v; want %+v", c.name, got, c.want)
		}
		if f.post.Model != c.postModel || f.post.Effort != c.eff {
			t.Errorf("%s: post meta %+v; want %q, %q", c.name, f.post, c.postModel, c.eff)
		}
	}
}

func TestEnvironmentValuesAreReportedButPassNoFlag(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	h.runner.svc = mustService(t, h, core.NewDefaults("", "", "", "", func(k string) string {
		return map[string]string{core.EnvModel: "sonnet", core.EnvEffort: "low"}[k]
	}))
	f, err := h.runner.settings(core.Bot{}, core.Bot{})
	if err != nil {
		t.Fatal(err)
	}
	if f.model != "" || f.effort != "" || f.modelSource != core.SourceEnv || f.post.Model != "sonnet" || f.post.Effort != "low" {
		t.Fatalf("flags = %+v; want no flags, source env, and the env values in the post", f)
	}
}

func mustService(t *testing.T, h *harness, d core.Defaults) *core.Service {
	t.Helper()
	svc, err := core.New(h.conn, h.dir, time.UTC, core.WithDefaults(d))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestBadStoredValuesStopTheRunAtStart(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ column, value, want string }{
		{"model", "-x", `the model "-x" (set on this bot) can't be used: A model ID can't contain spaces or start with '-'. Change it in Settings`},
		{"effort", "huge", `the effort "huge" (set on this bot) can't be used: Effort must be one of low, medium, high, xhigh, max. Change it in Settings`},
	} {
		h := newHarness(t, "reply")
		ctx := context.Background()
		bot := h.newBot(t)
		if _, err := h.conn.ExecContext(ctx, "UPDATE bots SET "+c.column+" = ? WHERE id = ?", c.value, bot.ID); err != nil {
			t.Fatal(err)
		}
		h.start(t)
		if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "hi", "n1"); err != nil {
			t.Fatal(err)
		}
		e := h.waitEntry(t, bot.ID, func(e core.Entry) bool { return bodyField(e, "type") == "turn-failed" })
		if text, _ := bodyField(e, "text").(string); text != c.want {
			t.Errorf("%s: failure = %q; want %q", c.column, text, c.want)
		}
		if _, err := os.Stat(filepath.Join(h.dir, "args.log")); !os.IsNotExist(err) {
			t.Errorf("%s: claude ran with a bad value", c.column)
		}
		if a := attentionOf(t, h, bot.ID); a != core.AttentionModel {
			t.Errorf("%s: attention = %q; want model", c.column, a)
		}
	}
}

func attentionOf(t *testing.T, h *harness, botID string) string {
	t.Helper()
	bots, err := h.svc.ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		if b.ID == botID {
			return b.Attention
		}
	}
	t.Fatalf("no bot %s", botID)
	return ""
}

func TestAModelThatClaudeCodeRejectsGetsOneErrorEntryAndNoRetry(t *testing.T) {
	t.Parallel()
	// Either sign of the rejection is enough: the assistant event's error, or the stderr line.
	for _, quiet := range []bool{false, true} {
		modelRejectedRun(t, quiet)
	}
}

func modelRejectedRun(t *testing.T, quiet bool) {
	h := newHarness(t, "model-error")
	if quiet {
		h.env = []string{"FAKE_QUIET=1"}
	}
	ctx := context.Background()
	bot := h.newBot(t)
	model := "claude-foo-9"
	if _, err := h.svc.UpdateBotSettings(ctx, bot.ID, core.BotPatch{Model: &model}); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "hi", "n1"); err != nil {
		t.Fatal(err)
	}
	e := h.waitEntry(t, bot.ID, func(e core.Entry) bool { return bodyField(e, "type") == "model-error" })
	want := "Claude Code couldn't run model `claude-foo-9` (set on this bot): There's an issue with the selected model (claude-foo-9). It may not exist or you may not have access to it. Run --model to pick a different model."
	if text, _ := bodyField(e, "text").(string); text != want || bodyField(e, "botId") != bot.ID {
		t.Fatalf("model error = %s; want %q for the bot", e.Body, want)
	}
	waitFor(t, "the turn to fail", func() bool {
		turns, _ := h.svc.QueuedTurns(ctx, 10)
		h.runner.mu.Lock()
		defer h.runner.mu.Unlock()
		return len(turns) == 0 && !h.runner.botBusyLocked(bot.ID)
	})
	if calls := h.argsLog(t); len(calls) != 1 {
		t.Fatalf("claude ran %d times; want once, with no retry", len(calls))
	}
	entries, _ := h.svc.Entries(ctx, bot.ID, 0, 0)
	for _, e := range entries {
		if bodyField(e, "type") == "turn-failed" {
			t.Fatalf("a model error also wrote %s", e.Body)
		}
	}
	if a := attentionOf(t, h, bot.ID); a != core.AttentionModel {
		t.Fatalf("attention = %q; want model", a)
	}
}

func TestModelFailureNamesAnInheritedSourceAndOrganizationLimits(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	out := outcome{resultError: true, modelNotFound: true, resultText: "Your organization does not allow this model.\nmore"}
	h.runner.noteModelFailure(ctx, sessionKey{bot.ID, ""}, bot.Name, "t1", runFlags{model: "opus", modelSource: core.SourceTeam}, out)
	h.runner.noteModelFailure(ctx, sessionKey{bot.ID, ""}, bot.Name, "t2", runFlags{modelSource: core.SourceEnv}, outcome{exitErr: os.ErrClosed, stderr: "[claude-code:unrecognized_model] {}"})
	entries, _ := h.svc.Entries(ctx, bot.ID, 0, 0)
	var texts []string
	for _, e := range entries {
		if bodyField(e, "type") == "model-error" {
			s, _ := bodyField(e, "text").(string)
			texts = append(texts, s)
		}
	}
	want := []string{
		"Claude Code couldn't run model `opus` (the team's default): Your organization does not allow this model. Your organization's settings may limit which models you can use.",
		"Claude Code couldn't run its model (from the environment): [claude-code:unrecognized_model] {}",
	}
	if !slices.Equal(texts, want) {
		t.Fatalf("entries = %q; want %q", texts, want)
	}
	for _, o := range []struct {
		out  outcome
		want bool
	}{
		{outcome{resultError: true, modelNotFound: true}, true},
		{outcome{exitErr: os.ErrClosed, stderr: "x unrecognized_model y"}, true},
		{outcome{modelNotFound: true}, false},
		{outcome{resultError: true, stderr: "other"}, false},
	} {
		if got := o.out.modelRejected(); got != o.want {
			t.Errorf("modelRejected(%+v) = %v; want %v", o.out, got, o.want)
		}
	}
}

func TestSourcePhrasesNameEachSource(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{core.SourceBot: "set on this bot", core.SourceTeam: "the team's default", core.SourceGlobal: "the global setting", core.SourceEnv: "from the environment", core.SourceClaudeCode: "Claude Code's default"} {
		if got := sourcePhrase(source); got != want {
			t.Errorf("sourcePhrase(%s) = %q; want %q", source, got, want)
		}
	}
}

func TestATeamTurnRunsWithTheTeamDefaultsAndPostsRecordThem(t *testing.T) {
	t.Parallel()
	h := newHarnessWith(t, "reply", "opus", "high")
	h.env = []string{"FAKE_INIT_MODEL=claude-sonnet-5-5"}
	ctx := context.Background()
	ana, bo := h.newNamedBot(t, "Ana"), h.newNamedBot(t, "Bo")
	own := "haiku"
	if _, err := h.svc.UpdateBotSettings(ctx, bo.ID, core.BotPatch{Model: &own}); err != nil {
		t.Fatal(err)
	}
	team, err := h.svc.CreateTeam(ctx, "Ship", "Ship it.", []string{ana.ID, bo.ID}, core.TeamSettings{LeadID: ana.ID, DefaultModel: "sonnet", DefaultEffort: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, team.ID, "Hi @Ana and @Bo", "n1"); err != nil {
		t.Fatal(err)
	}
	h.waitCalls(t, 2)
	if _, _, err := h.svc.SubmitUserMessage(ctx, ana.ID, "direct", "n2"); err != nil {
		t.Fatal(err)
	}
	calls := h.waitCalls(t, 3)
	byWho := map[string]string{}
	for _, c := range calls {
		switch {
		case strings.HasSuffix(c, "direct"):
			byWho["ana-direct"] = c
		case strings.Contains(c, "# You are Bo\n"):
			byWho["bo-team"] = c
		default:
			byWho["ana-team"] = c
		}
	}
	for who, want := range map[string]string{"ana-team": "--model sonnet --effort medium", "bo-team": "--model haiku --effort medium", "ana-direct": "--model opus --effort high"} {
		if !strings.Contains(byWho[who], want) {
			t.Errorf("%s call lacks %q", who, want)
		}
	}
	// No bot sent anything, so the final text reaches Ana's chat with the values of her turn.
	e := h.waitEntry(t, ana.ID, func(e core.Entry) bool { return e.Kind == core.KindBot })
	if bodyField(e, "model") != "opus" || bodyField(e, "effort") != "high" || bodyField(e, "modelId") != "claude-sonnet-5-5" {
		t.Fatalf("direct post = %s; want model opus, effort high and the reported ID", e.Body)
	}
	if logs := h.logs.String(); strings.Contains(logs, "deliver final text") || strings.Contains(logs, "record a model notice") {
		t.Fatalf("runner logged errors:\n%s", logs)
	}
}

func TestRoomPostsThroughMCPRecordTheRunsModel(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, _, _ := h.newTeam(t)
	h.runner.models[sessionKey{ana.ID, team.ID}] = modelReport{id: "claude-opus-5-5"}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	token, err := h.runner.tokens.issue(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t1", RoomID: team.ID, Post: core.PostMeta{Model: "opus", Effort: "max"}})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcpSession(t, srv.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	if text, isErr := callText(t, cs, "send_to_user", map[string]any{"text": "Done."}); isErr {
		t.Fatal(text)
	}
	e := h.waitEntry(t, team.ID, func(e core.Entry) bool { return e.Kind == core.KindBot })
	if bodyField(e, "model") != "opus" || bodyField(e, "effort") != "max" || bodyField(e, "modelId") != "claude-opus-5-5" {
		t.Fatalf("room post = %s; want the run's model, effort and ID", e.Body)
	}
	// A bot can't change its own model or effort: update_profile has no such fields.
	listed, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "update_profile" {
			continue
		}
		schema, _ := json.Marshal(tool.InputSchema)
		if strings.Contains(string(schema), `"model"`) || strings.Contains(string(schema), `"effort"`) {
			t.Fatalf("update_profile schema = %s; want no model or effort", schema)
		}
	}
	callText(t, cs, "update_profile", map[string]any{"name": "Ana", "model": "haiku", "effort": "low"})
	got, err := h.svc.GetBot(ctx, ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "" || got.Effort != "" {
		t.Fatalf("update_profile changed the model or effort: %+v", got)
	}
}

func TestMismatchAttentionClearsOnAMatchingRun(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	s := &session{key: sessionKey{bot.ID, ""}, bot: bot, flags: runFlags{model: "opus", modelSource: core.SourceBot}}
	h.runner.noteInitModel(ctx, s, "t1", "claude-sonnet-5-5")
	if a := attentionOf(t, h, bot.ID); a != core.AttentionModel {
		t.Fatalf("attention after a mismatch = %q; want model", a)
	}
	h.runner.noteInitModel(ctx, s, "t2", "")
	if a := attentionOf(t, h, bot.ID); a != core.AttentionModel {
		t.Fatalf("an init without a model changed the attention to %q", a)
	}
	h.runner.noteInitModel(ctx, s, "t3", "claude-opus-5-5")
	if a := attentionOf(t, h, bot.ID); a != "" {
		t.Fatalf("attention after a matching run = %q; want none", a)
	}
	h.runner.noteInitModel(ctx, s, "t4", "claude-sonnet-5-5")
	entries, _ := h.svc.Entries(ctx, bot.ID, 0, 0)
	n := 0
	for _, e := range entries {
		if bodyField(e, "type") == "model-mismatch" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("mismatch notices = %d; want one per change of the reported model (2)", n)
	}
}

func TestCreateSetupChecksAndCarriesModelAndEffort(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	builder := builderOf(t, h.svc)
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	token, err := h.runner.tokens.issue(runContext{BotID: builder.ID, BotName: builder.Name, TurnID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcpSession(t, srv.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })

	bad := map[string]any{"bots": []map[string]any{{"name": "Triage", "instructions": "Sort issues.", "effort": "huge"}}}
	if text, isErr := callText(t, cs, "create_setup", bad); !isErr || !strings.Contains(text, "Effort must be one of low, medium, high, xhigh, max.") {
		t.Fatalf("create_setup with a bad effort = %q (error %v)", text, isErr)
	}
	setup := map[string]any{
		"bots":  []map[string]any{{"name": "Triage", "instructions": "Sort issues.", "model": "Haiku", "effort": "LOW"}},
		"teams": []map[string]any{{"name": "Desk", "members": []string{"Triage"}, "default_model": "sonnet", "default_effort": "medium"}},
	}
	got := make(chan string, 1)
	go func() {
		text, _ := callText(t, cs, "create_setup", setup)
		got <- text
	}()
	card := h.waitEntry(t, builder.ID, func(e core.Entry) bool {
		return e.Kind == core.KindApproval && bodyField(e, "status") == "pending"
	})
	var body struct {
		Input setupInput `json:"input"`
	}
	if err := json.Unmarshal(card.Body, &body); err != nil {
		t.Fatal(err)
	}
	if b, tm := body.Input.Bots[0], body.Input.Teams[0]; b.Model != "haiku" || b.Effort != "low" || tm.DefaultModel != "sonnet" || tm.DefaultEffort != "medium" {
		t.Fatalf("card = %+v; want the checked model and effort values", body.Input)
	}
	if err := h.svc.ResolveApproval(ctx, card.ID, core.DecisionOnce); err != nil {
		t.Fatal(err)
	}
	<-got
	bots, err := h.svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bots {
		switch b.Name {
		case "Triage":
			if b.Model != "haiku" || b.Effort != "low" {
				t.Errorf("Triage = %q, %q; want haiku, low", b.Model, b.Effort)
			}
		case "Desk":
			if b.Team.DefaultModel != "sonnet" || b.Team.DefaultEffort != "medium" {
				t.Errorf("Desk defaults = %+v", b.Team)
			}
		}
	}
}

func TestComparableModelAndStripProviderHandleEdges(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"[1m]": "", " Claude-Opus-5-5[1M] ": "claude-opus-5-5", "opus]": "opus]", "claude-x": "claude-x"} {
		if got := comparableModel(in); got != want {
			t.Errorf("comparableModel(%q) = %q; want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{".claude-x": "claude-x", "us.anthropic.claude-x": "claude-x", "claude-x": "claude-x", "arn:x": "arn:x"} {
		if got := stripProvider(in); got != want {
			t.Errorf("stripProvider(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestAModelNoticeThatCannotBeSavedIsLogged(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	bot := h.newBot(t)
	h.runner.modelEntry(context.Background(), sessionKey{bot.ID, ""}, bot.Name, "t1", "model-mismatch", "ok")
	if logs := h.logs.String(); logs != "" {
		t.Fatalf("a saved notice logged %q", logs)
	}
	h.runner.modelEntry(context.Background(), sessionKey{"no-such-bot", ""}, "X", "t1", "model-mismatch", "lost")
	if logs := h.logs.String(); !strings.Contains(logs, "record a model notice") {
		t.Fatalf("logs = %q; want the failed notice", logs)
	}
}

func TestCompactionOfAMissingRoomSessionDropsItAndSaysSo(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "missing-session")
	ctx := context.Background()
	team, ana, _, _ := h.newTeam(t)
	if err := h.svc.SetRoomSession(ctx, team.ID, ana.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, err := h.svc.QueueCompaction(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	e := h.waitEntry(t, team.ID, func(e core.Entry) bool { return bodyField(e, "type") == "context-compacted" })
	if text, _ := bodyField(e, "text").(string); !strings.HasPrefix(text, "Ana: nothing to compact; the stored session no longer exists") {
		t.Fatalf("note = %q", text)
	}
	waitFor(t, "the missing session to be dropped", func() bool {
		seats, _ := h.svc.RoomSeats(ctx, team.ID)
		for _, s := range seats {
			if s.ID == ana.ID {
				return s.ClaudeSessionID == ""
			}
		}
		return false
	})
}
