package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"yargpt/internal/core"
)

// settingsHandler serves the settings of a fresh config.json (body; "" for
// no file) with the running values of a server that started without flags.
func settingsHandler(t *testing.T, body string, change func(*Settings)) (http.Handler, *core.Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := Settings{
		Path:    path,
		Running: map[string]any{"model": "", "effort": "", "maxParallel": 3, "claude": "claude", "updates.autoCheck": true, "updates.drainMinutes": 30, "updateCheck": true},
		Flags:   map[string]string{},
		Runtime: []RuntimeValue{{Key: "addr", Value: "127.0.0.1:7777", Source: SourceDefault}, {Key: "tray", Value: true, Source: SourceApp}},
		Build:   BuildSource,
	}
	if change != nil {
		change(&s)
	}
	h, svc := newModelHandler(t, core.Defaults{}, WithSettings(s))
	return h, svc, path
}

type settingsReply struct {
	File struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	} `json:"file"`
	Build      string                     `json:"build"`
	Restart    string                     `json:"restart"`
	StartError string                     `json:"startError"`
	Values     map[string]json.RawMessage `json:"values"`
	Runtime    []RuntimeValue             `json:"runtime"`
}

type valueReply struct {
	Value   any    `json:"value"`
	Default any    `json:"default"`
	Source  string `json:"source"`
	Flag    string `json:"flag"`
	File    any    `json:"file"`
	Running any    `json:"running"`
	Restart bool   `json:"restart"`
	Error   string `json:"error"`
}

func readSettings(t *testing.T, h http.Handler) settingsReply {
	t.Helper()
	rec := do(h, "GET", "/api/settings", "", nil)
	var out settingsReply
	if err := json.Unmarshal(rec.Body.Bytes(), &out); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("GET /api/settings = %d %s, %v", rec.Code, rec.Body, err)
	}
	return out
}

func (r settingsReply) value(t *testing.T, key string) valueReply {
	t.Helper()
	var v valueReply
	if err := json.Unmarshal(r.Values[key], &v); err != nil {
		t.Fatalf("value %s = %s: %v", key, r.Values[key], err)
	}
	return v
}

func TestSettingsShowEachValueWithItsDefaultSourceAndRunningValue(t *testing.T) {
	h, _, path := settingsHandler(t, `{"maxParallel": 4, "claude": "/missing/claude", "updates": {"autoCheck": false}}`, func(s *Settings) {
		s.LookPath = func(p string) (string, error) {
			if p == "/missing/claude" {
				return "", errors.New("not found")
			}
			return p, nil
		}
	})
	rec := do(h, "GET", "/api/settings", "", nil)
	keys := []string{`"model":`, `"effort":`, `"maxParallel":`, `"claude":`, `"updates.autoCheck":`, `"updates.drainMinutes":`, `"updateCheck":`}
	body, last := rec.Body.String(), -1
	for _, k := range keys {
		i := strings.Index(body, k)
		if i <= last {
			t.Fatalf("values = %s; want the keys in the page's order %v", body, keys)
		}
		last = i
	}
	got := readSettings(t, h)
	if got.File.Path != path || got.File.Error != "" || got.Build != BuildSource || got.Restart != "manual" || len(got.Runtime) != 2 || got.Runtime[1] != (RuntimeValue{Key: "tray", Value: true, Source: SourceApp}) {
		t.Fatalf("settings = %+v", got)
	}
	want := map[string]valueReply{
		// A key the file changed after the server started: pending, as value != running.
		"maxParallel":          {Value: 4.0, Default: 3.0, Source: "file", Running: 3.0, Restart: true},
		"claude":               {Value: "/missing/claude", Default: "claude", Source: "file", Running: "claude", Restart: true, Error: "YarGPT won't start with this program. Fix it before restarting."},
		"updates.autoCheck":    {Value: false, Default: true, Source: "file", Running: true},
		"updates.drainMinutes": {Value: 30.0, Default: 30.0, Source: "default", Running: 30.0},
		"model":                {Value: "", Default: "", Source: "default", Running: "", Restart: true},
		"updateCheck":          {Value: true, Default: true, Source: "default", Running: true, Restart: true},
	}
	for key, w := range want {
		if v := got.value(t, key); v != w {
			t.Errorf("%s = %+v; want %+v", key, v, w)
		}
	}
	h, _, _ = settingsHandler(t, `{"claude": "claude"}`, func(s *Settings) { s.LookPath = func(p string) (string, error) { return p, nil } })
	if v := readSettings(t, h).value(t, "claude"); v.Error != "" {
		t.Fatalf("claude on PATH = %+v; want no error", v)
	}
}

func TestAHandEditedValueThatStartupRejectsShowsItsError(t *testing.T) {
	// The file loads, but startup rejects its model and its effort.
	h, _, _ := settingsHandler(t, `{"model": "bad model", "effort": "ultra", "maxParallel": 4}`, nil)
	got := readSettings(t, h)
	want := map[string]valueReply{
		"model":  {Value: "bad model", Default: "", Source: "file", Running: "", Restart: true, Error: "A model ID can't contain spaces or start with '-'. The server won't start with it after a restart."},
		"effort": {Value: "ultra", Default: "", Source: "file", Running: "", Restart: true, Error: "Effort must be one of low, medium, high, xhigh, max. The server won't start with it after a restart."},
		// A value startup accepts has no error, and a default never has one.
		"maxParallel": {Value: 4.0, Default: 3.0, Source: "file", Running: 3.0, Restart: true},
		"claude":      {Value: "claude", Default: "claude", Source: "default", Running: "claude", Restart: true},
	}
	if got.File.Error != "" {
		t.Fatalf("file error = %q; want the file to load", got.File.Error)
	}
	for key, w := range want {
		if v := got.value(t, key); v != w {
			t.Errorf("%s = %+v; want %+v", key, v, w)
		}
	}
}

func TestAPatchWithNoKeysLeavesTheFileAlone(t *testing.T) {
	const before = "{\"model\":\"opus\"}"
	h, _, path := settingsHandler(t, before, nil)
	if rec := do(h, "PATCH", "/api/settings", `{}`, jsonHeader); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"opus"`) {
		t.Fatalf("PATCH {} = %d %s; want 200 with the settings", rec.Code, rec.Body)
	}
	if after, _ := os.ReadFile(path); string(after) != before {
		t.Fatalf("file after PATCH {} = %q; want it byte for byte as before", after)
	}
}

func TestSettingsChangeTheFileAndAnswerWithTheNewValues(t *testing.T) {
	h, _, path := settingsHandler(t, `{"updates": {"autoCheck": false}}`, nil)
	rec := do(h, "PATCH", "/api/settings", `{"maxParallel": 1, "model": " Sonnet ", "updates.drainMinutes": 240}`, jsonHeader)
	var got settingsReply
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
	}
	if v := got.value(t, "model"); v.Value != "sonnet" || v.Source != "file" {
		t.Fatalf("model after PATCH = %+v; want the normalized value from the file", v)
	}
	file, _ := os.ReadFile(path)
	if want := "{\n  \"model\": \"sonnet\",\n  \"maxParallel\": 1,\n  \"updates\": {\n    \"autoCheck\": false,\n    \"drainMinutes\": 240\n  }\n}\n"; string(file) != want {
		t.Fatalf("file = %q; want %q", file, want)
	}
	// Reset to default removes the key (AC6).
	if rec := do(h, "PATCH", "/api/settings", `{"maxParallel": null, "updates.autoCheck": null}`, jsonHeader); rec.Code != http.StatusOK {
		t.Fatalf("reset = %d %s", rec.Code, rec.Body)
	}
	file, _ = os.ReadFile(path)
	if strings.Contains(string(file), "maxParallel") || strings.Contains(string(file), "autoCheck") {
		t.Fatalf("file after reset = %s; want the keys gone", file)
	}
	if v := readSettings(t, h).value(t, "maxParallel"); v.Value != 3.0 || v.Source != "default" {
		t.Fatalf("maxParallel after reset = %+v; want the default", v)
	}
}

func TestBadSettingsGet400AndLeaveTheFileAsItWas(t *testing.T) {
	const before = "{\n\"model\":   \"opus\"\n}\n"
	h, _, path := settingsHandler(t, before, nil)
	for _, c := range []struct{ body, key, message string }{
		{`{"maxParallel": 0}`, "maxParallel", "Turns at once must be 1 or more."},
		{`{"model": "a b"}`, "model", "A model ID can't contain spaces or start with '-'."},
		{`{"model": "-x"}`, "model", "A model ID can't contain spaces or start with '-'."},
		{`{"effort": "huge"}`, "effort", "Effort must be one of low, medium, high, xhigh, max."},
		{`{"updates.drainMinutes": 4}`, "updates.drainMinutes", "Wait for running bots must be from 5 to 240 minutes."},
		{`{"updates.drainMinutes": 241}`, "updates.drainMinutes", "Wait for running bots must be from 5 to 240 minutes."},
		// All or nothing: the good value of a body with a bad one is not saved either.
		{`{"effort": "max", "maxParallel": -1}`, "maxParallel", "Turns at once must be 1 or more."},
		{`{"modle": "opus"}`, "modle", "modle isn't a setting."}, //nolint:misspell // the misspelled key is the input under test
		{`[1]`, "", `The body must be an object of settings, such as {"maxParallel": 4}.`},
		{`null`, "", `The body must be an object of settings, such as {"maxParallel": 4}.`},
	} {
		rec := do(h, "PATCH", "/api/settings", c.body, jsonHeader)
		var p settingsProblem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); rec.Code != http.StatusBadRequest || err != nil || p.Key != c.key || p.Message != c.message || p.Error != c.message {
			t.Errorf("PATCH %s = %d %s; want 400 %s: %q", c.body, rec.Code, rec.Body, c.key, c.message)
		}
		if after, _ := os.ReadFile(path); string(after) != before {
			t.Fatalf("file after PATCH %s = %q; want it byte for byte as before", c.body, after)
		}
	}
}

func TestSettingsThatAFlagOrTheCommandLineDecideGet409(t *testing.T) {
	h, _, path := settingsHandler(t, `{"effort": "low"}`, func(s *Settings) {
		s.Flags = map[string]string{"effort": "-effort high"}
		s.Running["effort"] = "high"
	})
	got := readSettings(t, h)
	if v := got.value(t, "effort"); v.Source != "flag" || v.Flag != "-effort high" || v.Value != "high" || v.File != "low" || v.Error != "" {
		t.Fatalf("effort under -effort = %+v; want the flag's value and the file's", v)
	}
	if v := got.value(t, "model"); v.File != nil {
		t.Fatalf("model = %+v; want no file value for a key no flag sets", v)
	}
	for _, c := range []struct{ body, key, message string }{
		{`{"effort": "max"}`, "effort", "Set by the -effort flag for this run; remove the flag to change it here."},
		{`{"addr": "127.0.0.1:9999"}`, "addr", "addr is a runtime value; it can't be changed here."},
		{`{"tray": false}`, "tray", "tray is a runtime value; it can't be changed here."},
	} {
		rec := do(h, "PATCH", "/api/settings", c.body, jsonHeader)
		var p settingsProblem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); rec.Code != http.StatusConflict || err != nil || p.Key != c.key || p.Message != c.message {
			t.Errorf("PATCH %s = %d %s; want 409 %s: %q", c.body, rec.Code, rec.Body, c.key, c.message)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != `{"effort": "low"}` {
		t.Fatalf("file = %s; want it unchanged", after)
	}
}

func TestABrokenFileShowsItsErrorAndOnlyAResetWritesIt(t *testing.T) {
	h, _, path := settingsHandler(t, `{"maxParallel": 4,}`, nil)
	got := readSettings(t, h)
	if !strings.Contains(got.File.Error, "invalid character") || !strings.Contains(got.File.Error, path) {
		t.Fatalf("file error = %q; want the parse error", got.File.Error)
	}
	if v := got.value(t, "maxParallel"); v.Value != 3.0 {
		t.Fatalf("maxParallel of a broken file = %+v; want the running value", v)
	}
	rec := do(h, "PATCH", "/api/settings", `{"maxParallel": 5}`, jsonHeader)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "config.json has an error") {
		t.Fatalf("PATCH a broken file = %d %s; want 409", rec.Code, rec.Body)
	}
	if rec := do(h, "PUT", "/api/settings/file", `{"maxParallel": 5}`, jsonHeader); rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT with keys = %d %s; want 400: a reset writes {}", rec.Code, rec.Body)
	}
	rec = do(h, "PUT", "/api/settings/file", `{}`, jsonHeader)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("reset file = %d %s; want 200 and no error", rec.Code, rec.Body)
	}
	if after, _ := os.ReadFile(path); string(after) != "{}\n" {
		t.Fatalf("file after reset = %q; want {}", after)
	}
	if rec := do(h, "PATCH", "/api/settings", `{"maxParallel": 5}`, jsonHeader); rec.Code != http.StatusOK {
		t.Fatalf("PATCH after reset = %d %s; want 200", rec.Code, rec.Body)
	}
}

func TestConcurrentSettingChangesKeepEachOther(t *testing.T) {
	h, _, path := settingsHandler(t, "", nil)
	bodies := []string{`{"model": "opus"}`, `{"effort": "max"}`, `{"maxParallel": 7}`, `{"claude": "/opt/claude"}`, `{"updates.autoCheck": false}`, `{"updates.drainMinutes": 60}`, `{"updateCheck": false}`}
	var wg sync.WaitGroup
	codes := make(chan int, len(bodies))
	for _, b := range bodies {
		wg.Go(func() { codes <- do(h, "PATCH", "/api/settings", b, jsonHeader).Code })
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != http.StatusOK {
			t.Fatalf("a concurrent PATCH = %d; want 200", c)
		}
	}
	file, _ := os.ReadFile(path)
	for _, want := range []string{`"opus"`, `"max"`, `7`, `"/opt/claude"`, `"autoCheck": false`, `60`, `"updateCheck": false`} {
		if !strings.Contains(string(file), want) {
			t.Fatalf("file after concurrent changes = %s; want %s kept", file, want)
		}
	}
}

func TestRestartUnderLaunchdAnswersAndThenEndsTheServer(t *testing.T) {
	h, _, _ := settingsHandler(t, "", nil)
	if rec := do(h, "POST", "/api/restart", `{"when": "now"}`, jsonHeader); rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "doesn't run under launchd") {
		t.Fatalf("restart outside launchd = %d %s; want 501", rec.Code, rec.Body)
	}
	if got := readSettings(t, h); got.Restart != "manual" {
		t.Fatalf("restart mode = %q; want manual", got.Restart)
	}
	var asked []bool
	h, svc, _ := settingsHandler(t, "", func(s *Settings) { s.Restart = func(idle bool) { asked = append(asked, idle) } })
	if got := readSettings(t, h); got.Restart != "launchd" {
		t.Fatalf("restart mode = %q; want launchd", got.Restart)
	}
	for _, body := range []string{`{"when": "later"}`, `{}`, `x`} {
		if rec := do(h, "POST", "/api/restart", body, jsonHeader); rec.Code != http.StatusBadRequest {
			t.Errorf("restart %s = %d; want 400", body, rec.Code)
		}
	}
	rec := do(h, "POST", "/api/restart", `{"when": "now"}`, jsonHeader)
	if on, _ := svc.Draining(); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"running":[]`) || on || len(asked) != 1 || asked[0] {
		t.Fatalf("restart now = %d %s, draining %v, asked %v; want 202, no drain, one restart now", rec.Code, rec.Body, on, asked)
	}
	rec = do(h, "POST", "/api/restart", `{"when": "idle"}`, jsonHeader)
	if on, _ := svc.Draining(); rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"draining":true`) || !on || len(asked) != 2 || !asked[1] {
		t.Fatalf("restart idle = %d %s, draining %v, asked %v; want 202 with drain on and an idle restart", rec.Code, rec.Body, on, asked)
	}
}

func TestARestartIsRefusedWhenTheServerWouldNotStart(t *testing.T) {
	restarted := false
	h, svc, _ := settingsHandler(t, `{"effort": "ultra"}`, func(s *Settings) {
		s.Restart = func(bool) { restarted = true }
		s.StartupCheck = func() (string, error) {
			return "effort", errors.New(`config.json: "effort" must be one of low, medium, high, xhigh, max`)
		}
	})
	for _, when := range []string{"now", "idle"} {
		rec := do(h, "POST", "/api/restart", `{"when": "`+when+`"}`, jsonHeader)
		var p settingsProblem
		want := `YarGPT wouldn't start after a restart: config.json: "effort" must be one of low, medium, high, xhigh, max. Fix it, then restart.`
		if err := json.Unmarshal(rec.Body.Bytes(), &p); rec.Code != http.StatusConflict || err != nil || p.Key != "effort" || p.Message != want || p.Error != want {
			t.Fatalf("restart %s with a bad file = %d %s; want 409 on effort: %q", when, rec.Code, rec.Body, want)
		}
	}
	if on, _ := svc.Draining(); on || restarted {
		t.Fatalf("draining %v, restarted %v after refused restarts; want neither", on, restarted)
	}
	// The app restarts through its own bridge, so GET says the same thing first.
	want := `config.json: "effort" must be one of low, medium, high, xhigh, max`
	if got := readSettings(t, h); got.StartError != want {
		t.Fatalf("startError = %q; want %q", got.StartError, want)
	}
	// The views that PATCH and PUT return carry it too.
	for _, c := range []struct{ method, target, body string }{
		{"PATCH", "/api/settings", `{"maxParallel": 4}`},
		{"PUT", "/api/settings/file", `{}`},
	} {
		rec := do(h, c.method, c.target, c.body, jsonHeader)
		var got settingsReply
		if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != http.StatusOK || err != nil || got.StartError != want {
			t.Fatalf("%s %s = %d %s; want 200 with startError %q", c.method, c.target, rec.Code, rec.Body, want)
		}
	}
}

func TestSettingsHaveNoStartErrorWhenTheServerWouldStart(t *testing.T) {
	h, _, _ := settingsHandler(t, `{"effort": "high"}`, func(s *Settings) {
		s.StartupCheck = func() (string, error) { return "", nil }
	})
	rec := do(h, "GET", "/api/settings", "", nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "startError") {
		t.Fatalf("settings = %d %s; want no startError", rec.Code, rec.Body)
	}
}

func TestLeavingDrainModeDropsAWaitingRestartAtOnce(t *testing.T) {
	cancelled := 0
	h, _, _ := settingsHandler(t, "", func(s *Settings) {
		s.Restart = func(bool) {}
		s.CancelRestart = func() { cancelled++ }
	})
	if rec := do(h, "POST", "/api/drain", `{}`, jsonHeader); rec.Code != http.StatusOK || cancelled != 0 {
		t.Fatalf("drain on = %d, cancelled %d; want 200 and nothing cancelled", rec.Code, cancelled)
	}
	if rec := do(h, "DELETE", "/api/drain", "", nil); rec.Code != http.StatusOK || cancelled != 1 {
		t.Fatalf("drain off = %d, cancelled %d; want 200 and the waiting restart dropped", rec.Code, cancelled)
	}
}

func TestSettingsWritesNeedTheAppsOwnOrigin(t *testing.T) {
	h, _, path := settingsHandler(t, `{"model": "opus"}`, func(s *Settings) { s.Restart = func(bool) { t.Fatal("a refused request restarted the server") } })
	for _, c := range []struct{ method, target, body string }{
		{"PATCH", "/api/settings", `{"model": "haiku"}`},
		{"PUT", "/api/settings/file", `{}`},
		{"POST", "/api/restart", `{"when": "now"}`},
	} {
		foreign := do(h, c.method, c.target, c.body, map[string]string{"Content-Type": "application/json", "Origin": "http://evil.example"})
		req := httptest.NewRequestWithContext(t.Context(), c.method, "http://evil.example:7777"+c.target, strings.NewReader(c.body))
		req.Header.Set("Content-Type", "application/json")
		rebind := httptest.NewRecorder()
		h.ServeHTTP(rebind, req)
		if foreign.Code != http.StatusForbidden || rebind.Code != http.StatusForbidden {
			t.Errorf("%s %s: foreign origin %d, foreign host %d; want 403 for both", c.method, c.target, foreign.Code, rebind.Code)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != `{"model": "opus"}` {
		t.Fatalf("file = %s; want it unchanged", after)
	}
}

func TestAServerWithoutASettingsFileHasNoSettings(t *testing.T) {
	h, _ := newModelHandler(t, core.Defaults{})
	for _, c := range []struct{ method, target, body string }{
		{"GET", "/api/settings", ""}, {"PATCH", "/api/settings", `{}`}, {"PUT", "/api/settings/file", `{}`},
	} {
		if rec := do(h, c.method, c.target, c.body, jsonHeader); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d; want 404", c.method, c.target, rec.Code)
		}
	}
}
