package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"handful/internal/agent"
	"handful/internal/core"
	"handful/internal/db/dbtest"
)

type fakeModels map[string]string // "botID/roomID" -> model ID; a "!" prefix marks a mismatch

func (f fakeModels) ModelReport(botID, roomID string) (string, bool) {
	id := f[botID+"/"+roomID]
	return strings.TrimPrefix(id, "!"), strings.HasPrefix(id, "!")
}

func newModelHandler(t *testing.T, d core.Defaults, opts ...Option) (http.Handler, *core.Service) {
	t.Helper()
	dir := t.TempDir()
	conn, err := dbtest.Open(context.Background(), filepath.Join(dir, "yargpt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	svc, err := core.New(conn, dir, time.UTC, core.WithDefaults(d))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := agent.NewRunner(svc, agent.Config{ClaudePath: "/nonexistent"}, log)
	return New(svc, runner, nil, log, append([]Option{WithKey(testKey, "/data/ui-key")}, opts...)...), svc
}

type modelBot struct {
	ID        string          `json:"id"`
	Model     string          `json:"model"`
	Effort    string          `json:"effort"`
	Effective *core.Effective `json:"effective"`
	Team      *core.Team      `json:"team"`
	Members   []struct {
		ID        string          `json:"id"`
		Effective *core.Effective `json:"effective"`
	} `json:"members"`
	Error string `json:"error"`
}

func decodeBot(t *testing.T, body []byte) modelBot {
	t.Helper()
	var b modelBot
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return b
}

func TestBotEndpointsSetModelAndEffortAndShowTheEffectiveValues(t *testing.T) {
	d := core.NewDefaults("sonnet", core.DefaultFromConfig, "", "", func(string) string { return "" })
	models := fakeModels{}
	h, svc := newModelHandler(t, d, WithModelReporter(models))

	rec := do(h, "POST", "/api/bots", `{"name":"Ana","model":"Opus","effort":"max"}`, jsonHeader)
	ana := decodeBot(t, rec.Body.Bytes())
	if rec.Code != http.StatusCreated || ana.Model != "opus" || ana.Effort != "max" {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	if e := ana.Effective; e == nil || *e != (core.Effective{Model: "opus", ModelSource: core.SourceBot, Effort: "max", EffortSource: core.SourceBot}) {
		t.Fatalf("effective = %+v", ana.Effective)
	}
	for _, body := range []string{`{"name":"Bad","model":"a b"}`, `{"name":"Bad","effort":"huge"}`} {
		if rec := do(h, "POST", "/api/bots", body, jsonHeader); rec.Code != http.StatusBadRequest {
			t.Fatalf("create %s = %d", body, rec.Code)
		}
	}

	rec = do(h, "PATCH", "/api/bots/"+ana.ID, `{"name":"Anna","effort":"extreme"}`, jsonHeader)
	if rec.Code != http.StatusBadRequest || !strings.Contains(decodeBot(t, rec.Body.Bytes()).Error, "Effort must be one of low, medium, high, xhigh, max.") {
		t.Fatalf("bad effort = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "PATCH", "/api/bots/"+ana.ID, `{"model":"inherit"}`, jsonHeader); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Choose Default instead.") {
		t.Fatalf("inherit = %d %s", rec.Code, rec.Body)
	}
	if b, _ := svc.GetBot(context.Background(), ana.ID); b.Name != "Ana" || b.Effort != "max" {
		t.Fatalf("after bad patches = %+v; want nothing saved", b)
	}

	models[ana.ID+"/"] = "!claude-sonnet-5-5"
	rec = do(h, "PATCH", "/api/bots/"+ana.ID, `{"model":""}`, jsonHeader)
	got := decodeBot(t, rec.Body.Bytes())
	want := core.Effective{Model: "sonnet", ModelSource: core.SourceGlobal, ModelID: "claude-sonnet-5-5", ModelMismatch: true, Effort: "max", EffortSource: core.SourceBot}
	if rec.Code != http.StatusOK || got.Model != "" || got.Effort != "max" || got.Effective == nil || *got.Effective != want {
		t.Fatalf("clear the model = %d %s; want effective %+v", rec.Code, rec.Body, want)
	}

	rec = do(h, "POST", "/api/bots", `{"name":"Bo"}`, jsonHeader)
	bo := decodeBot(t, rec.Body.Bytes())
	rec = do(h, "POST", "/api/teams", `{"name":"Ship","memberIds":["`+ana.ID+`","`+bo.ID+`"],"defaultModel":"Haiku","defaultEffort":"low"}`, jsonHeader)
	team := decodeBot(t, rec.Body.Bytes())
	if rec.Code != http.StatusCreated || team.Team.DefaultModel != "haiku" || team.Team.DefaultEffort != "low" || team.Effective != nil {
		t.Fatalf("create team = %d %s", rec.Code, rec.Body)
	}
	models[bo.ID+"/"+team.ID] = "claude-haiku-5-5"
	rec = do(h, "PATCH", "/api/teams/"+team.ID, `{"defaultEffort":"medium"}`, jsonHeader)
	team = decodeBot(t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || team.Team.DefaultModel != "haiku" || team.Team.DefaultEffort != "medium" || len(team.Members) != 2 {
		t.Fatalf("update team = %d %s", rec.Code, rec.Body)
	}
	wantMembers := map[string]core.Effective{
		ana.ID: {Model: "haiku", ModelSource: core.SourceTeam, Effort: "max", EffortSource: core.SourceBot},
		bo.ID:  {Model: "haiku", ModelSource: core.SourceTeam, ModelID: "claude-haiku-5-5", Effort: "medium", EffortSource: core.SourceTeam},
	}
	for _, m := range team.Members {
		if m.Effective == nil || *m.Effective != wantMembers[m.ID] {
			t.Errorf("member %s effective = %+v; want %+v", m.ID, m.Effective, wantMembers[m.ID])
		}
	}
	if rec := do(h, "PATCH", "/api/teams/"+team.ID, `{"defaultModel":"-x"}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad team model = %d", rec.Code)
	}
	if rec := do(h, "PATCH", "/api/bots/"+team.ID, `{"model":"opus"}`, jsonHeader); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Rooms have no model") {
		t.Fatalf("model on a team = %d %s", rec.Code, rec.Body)
	}

	rec = do(h, "POST", "/api/rooms", `{"name":"Chat","memberIds":["`+bo.ID+`"]}`, jsonHeader)
	room := decodeBot(t, rec.Body.Bytes())
	rec = do(h, "GET", "/api/bots/"+room.ID, "", nil)
	room = decodeBot(t, rec.Body.Bytes())
	if len(room.Members) != 1 || room.Members[0].Effective == nil || *room.Members[0].Effective != (core.Effective{Model: "sonnet", ModelSource: core.SourceGlobal, EffortSource: core.SourceClaudeCode}) {
		t.Fatalf("plain room members = %s; want direct-chat values", rec.Body)
	}

	rec = do(h, "GET", "/api/bots", "", nil)
	var list []modelBot
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(list, func(b modelBot) bool {
		return b.ID == bo.ID && b.Effective != nil && b.Effective.ModelSource == core.SourceGlobal
	}) {
		t.Fatalf("list = %s; want Bo's effective values", rec.Body)
	}
}

func TestEffectiveValuesWorkWithoutAModelReporter(t *testing.T) {
	h, _ := newModelHandler(t, core.Defaults{})
	rec := do(h, "POST", "/api/bots", `{"name":"Ana"}`, jsonHeader)
	b := decodeBot(t, rec.Body.Bytes())
	if b.Effective == nil || *b.Effective != (core.Effective{ModelSource: core.SourceClaudeCode, EffortSource: core.SourceClaudeCode}) {
		t.Fatalf("effective = %s", rec.Body)
	}
}

func TestDefaultsEndpointShowsTheGlobalValuesAndChoices(t *testing.T) {
	env := map[string]string{"CLAUDE_CODE_SUBAGENT_MODEL": "haiku"}
	d := core.NewDefaults("", "", "high", core.DefaultFromConfig, func(k string) string { return env[k] })
	h, _ := newModelHandler(t, d)
	rec := do(h, "GET", "/api/defaults", "", nil)
	var got struct {
		Model, ModelSource, Effort, EffortSource string
		EnvOverrides, ModelAliases, EffortLevels []string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || got.Model != "" || got.ModelSource != "claude-code" || got.Effort != "high" || got.EffortSource != "config" ||
		!slices.Equal(got.EnvOverrides, []string{"CLAUDE_CODE_SUBAGENT_MODEL"}) ||
		!slices.Equal(got.ModelAliases, []string{"fable", "opus", "sonnet", "haiku"}) ||
		!slices.Equal(got.EffortLevels, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Fatalf("defaults = %d %s", rec.Code, rec.Body)
	}
	for _, key := range []string{`"modelSource"`, `"effortSource"`, `"envOverrides"`, `"modelAliases"`, `"effortLevels"`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("body %s lacks %s", rec.Body, key)
		}
	}
}
