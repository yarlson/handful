package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsTheFileStrictly(t *testing.T) {
	if f, err := Load(filepath.Join(t.TempDir(), "config.json")); err != nil || f != (File{}) {
		t.Fatalf("missing file = %+v, %v; want an empty file", f, err)
	}
	f, err := Load(writeFile(t, `{"model": "opus", "maxParallel": 1, "updates": {"drainMinutes": 4}}`))
	if err != nil || *f.Model != "opus" || *f.MaxParallel != 1 || *f.Updates.DrainMinutes != 4 || f.Effort != nil {
		t.Fatalf("Load = %+v, %v", f, err)
	}
	for name, c := range map[string]struct{ body, want string }{
		"unknown key":   {`{"modle": "opus"}`, `unknown field "modle"`}, //nolint:misspell // the misspelled key is the input under test
		"trailing data": {`{} {}`, "after the JSON object"},
		"not JSON":      {`{"model": }`, "invalid character"},
		"maxParallel 0": {`{"maxParallel": 0}`, `"maxParallel": Turns at once must be 1 or more.`},
		"too large":     {`{"claude": "` + strings.Repeat("x", maxFileBytes) + `"}`, "larger than"},
	} {
		path := writeFile(t, c.body)
		if _, err := Load(path); err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), path) {
			t.Errorf("%s: err = %v; want one naming %s and %q", name, err, path, c.want)
		}
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("Load of a folder = nil; want an error")
	}
}

func TestLoadAcceptsAFileAtItsSizeLimit(t *testing.T) {
	body := `{"claude": "` + strings.Repeat("x", maxFileBytes-len(`{"claude": ""}`)) + `"}`
	if _, err := Load(writeFile(t, body)); err != nil {
		t.Fatalf("a file of exactly %d bytes: %v", maxFileBytes, err)
	}
}

func TestSetStoresCheckedValuesAndNullRemovesThem(t *testing.T) {
	var f File
	for key, raw := range map[string]string{
		"model": `" OPUS "`, "effort": `"High"`, "maxParallel": `1`, "claude": `"/opt/claude code/claude"`,
		"updates.autoCheck": `false`, "updates.drainMinutes": `240`, "updateCheck": `false`,
	} {
		if err := f.Set(key, json.RawMessage(raw)); err != nil {
			t.Fatalf("Set(%s, %s) = %v", key, raw, err)
		}
	}
	want := map[string]any{"model": "opus", "effort": "high", "maxParallel": 1, "claude": "/opt/claude code/claude", "updates.autoCheck": false, "updates.drainMinutes": 240, "updateCheck": false}
	for _, key := range Keys {
		if got, ok := f.Get(key); !ok || got != want[key] {
			t.Errorf("Get(%s) = %v, %v; want %v", key, got, ok, want[key])
		}
	}
	for _, key := range Keys {
		if err := f.Set(key, json.RawMessage(" null ")); err != nil {
			t.Fatalf("Set(%s, null) = %v", key, err)
		}
		if got, ok := f.Get(key); ok {
			t.Errorf("Get(%s) after null = %v; want unset", key, got)
		}
	}
	if f != (File{}) {
		t.Fatalf("file after every null = %+v; want empty, with no empty updates object", f)
	}
	if got, ok := f.Get("addr"); ok || got != nil {
		t.Fatalf("Get(addr) = %v, %v; want no such setting", got, ok)
	}
}

func TestSetKeepsTheOtherUpdateSettingWhenOneIsRemoved(t *testing.T) {
	var f File
	for key, raw := range map[string]string{"updates.autoCheck": `true`, "updates.drainMinutes": `30`} {
		if err := f.Set(key, json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Set("updates.autoCheck", json.RawMessage(`null`)); err != nil || f.Updates == nil || f.Updates.AutoCheck != nil || *f.Updates.DrainMinutes != 30 {
		t.Fatalf("updates after removing autoCheck = %+v, %v; want only drainMinutes", f.Updates, err)
	}
	if err := f.Set("updates.drainMinutes", json.RawMessage(`4`)); err == nil || f.Updates == nil || *f.Updates.DrainMinutes != 30 {
		t.Fatalf("a rejected drainMinutes = %v, %+v; want the old value kept", err, f.Updates)
	}
}

func TestSetRejectsWhatTheServerWouldNotStartWith(t *testing.T) {
	cases := []struct{ key, raw, want string }{
		{"maxParallel", `0`, "Turns at once must be 1 or more."},
		{"maxParallel", `2.5`, "maxParallel must be a whole number."},
		{"maxParallel", `"3"`, "maxParallel must be a whole number."},
		{"model", `"a b"`, "A model ID can't contain spaces or start with '-'."},
		{"model", `"-x"`, "A model ID can't contain spaces or start with '-'."},
		{"model", `"inherit"`, "Choose Default instead."},
		{"model", `3`, "model must be text."},
		{"effort", `"huge"`, "Effort must be one of low, medium, high, xhigh, max."},
		{"updates.drainMinutes", `4`, "Wait for running bots must be from 5 to 240 minutes."},
		{"updates.drainMinutes", `241`, "Wait for running bots must be from 5 to 240 minutes."},
		{"claude", `""`, "The Claude Code program must be 1 to 1024 characters, with no NUL character."},
		{"claude", `"` + strings.Repeat("é", maxClaudeLen+1) + `"`, "The Claude Code program must be 1 to 1024 characters, with no NUL character."},
		{"claude", `"cla\u0000ude"`, "The Claude Code program must be 1 to 1024 characters, with no NUL character."},
		{"updates.autoCheck", `"yes"`, "updates.autoCheck must be true or false."},
		{"updateCheck", `1`, "updateCheck must be true or false."},
		{"addr", `"0.0.0.0:80"`, "addr isn't a setting."},
	}
	for _, c := range cases {
		var f File
		err := f.Set(c.key, json.RawMessage(c.raw))
		var ke *KeyError
		if !errors.As(err, &ke) || ke.Key != c.key || ke.Message != c.want {
			t.Errorf("Set(%s, %s) = %v; want %s: %q", c.key, c.raw, err, c.key, c.want)
		}
		if got, ok := f.Get(c.key); ok {
			t.Errorf("Set(%s, %s) stored %v; want nothing", c.key, c.raw, got)
		}
	}
	var f File
	if err := f.Set("claude", json.RawMessage(`"`+strings.Repeat("é", maxClaudeLen)+`"`)); err != nil {
		t.Errorf("a claude of %d characters: %v", maxClaudeLen, err)
	}
	if err := f.Set("updates.drainMinutes", json.RawMessage(`5`)); err != nil {
		t.Errorf("drainMinutes 5: %v", err)
	}
}

func TestWriteKeepsThePageOrderAndReplacesTheFileAtOnce(t *testing.T) {
	path := writeFile(t, `{"updateCheck": true, "claude": "old"}`)
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, raw := range map[string]string{"updates.drainMinutes": `45`, "model": `"sonnet"`, "maxParallel": `4`} {
		if err := f.Set(key, json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if err := Write(path, f); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"model\": \"sonnet\",\n  \"maxParallel\": 4,\n  \"claude\": \"old\",\n  \"updates\": {\n    \"drainMinutes\": 45\n  },\n  \"updateCheck\": true\n}\n"
	if string(got) != want {
		t.Fatalf("file = %q; want %q", got, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v; want 0600", info, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("folder = %v, %v; want only config.json, no temporary file", entries, err)
	}
	if err := Write(path, File{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "{}\n" {
		t.Fatalf("empty file = %q; want {}", got)
	}
}

func TestWriteFailsWithoutAFolderAndLeavesNothing(t *testing.T) {
	if err := Write(filepath.Join(t.TempDir(), "missing", "config.json"), File{}); err == nil {
		t.Fatal("Write into a missing folder = nil; want an error")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "config.json")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, File{}); err == nil || !strings.Contains(err.Error(), "replace config file") {
		t.Fatalf("Write over a folder = %v; want a replace error", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("folder after a failed write = %v; want the temporary file removed", entries)
	}
}

func TestKeysHaveDefaultsFlagsAndRestartRules(t *testing.T) {
	defaults := map[string]any{"model": "", "effort": "", "maxParallel": 3, "claude": "claude", "updates.autoCheck": true, "updates.drainMinutes": 30, "updateCheck": true}
	restarts := map[string]bool{"model": true, "effort": true, "maxParallel": true, "claude": true, "updateCheck": true}
	flagNames := map[string]string{"model": "model", "effort": "effort", "maxParallel": "max-parallel", "claude": "claude"}
	for _, key := range Keys {
		if Default(key) != defaults[key] || NeedsRestart(key) != restarts[key] || Flag(key) != flagNames[key] {
			t.Errorf("%s: default %v, restart %v, flag %q; want %v, %v, %q", key, Default(key), NeedsRestart(key), Flag(key), defaults[key], restarts[key], flagNames[key])
		}
	}
	if (&KeyError{Key: "effort", Message: "Bad."}).Error() != `"effort": Bad.` {
		t.Fatal("KeyError text")
	}
}

func TestCheckTextUsesTheRulesOfSet(t *testing.T) {
	for _, c := range []struct{ key, value, message string }{
		{"model", "opus", ""},
		{"model", "bad model", "A model ID can't contain spaces or start with '-'."},
		{"effort", "max", ""},
		{"effort", "ultra", "Effort must be one of low, medium, high, xhigh, max."},
		{"claude", "", "The Claude Code program must be 1 to 1024 characters, with no NUL character."},
		{"maxParallel", "3", "maxParallel isn't a text setting."},
	} {
		err := CheckText(c.key, c.value)
		ke, _ := errors.AsType[*KeyError](err)
		if c.message == "" && err != nil || c.message != "" && (ke == nil || ke.Key != c.key || ke.Message != c.message) {
			t.Errorf("CheckText(%q, %q) = %v; want %q", c.key, c.value, err, c.message)
		}
	}
}
