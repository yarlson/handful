package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeClaude writes an executable named claude and returns its path.
func fakeClaude(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return "127.0.0.1:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
}

func TestParseFlagsKeepsDefaultsAndRecordsGivenFlags(t *testing.T) {
	o, err := parseFlags([]string{"-model", "sonnet", "-effort", "max", "-max-parallel", "5"})
	if err != nil {
		t.Fatal(err)
	}
	if o.addr != "127.0.0.1:7777" || o.Claude != "claude" || o.Model != "sonnet" || o.Effort != "max" || o.MaxParallel != 5 || !strings.HasSuffix(o.dataDir, ".yargpt") {
		t.Fatalf("options = %+v; want the default address, data folder and claude, model sonnet and 5 parallel turns", o)
	}
	if o.Updates != (updateSettings{AutoCheck: true, DrainMinutes: 30, UpdateCheck: true}) {
		t.Fatalf("update settings = %+v; want the defaults", o.Updates)
	}
	if !o.set["model"] || !o.set["effort"] || !o.set["max-parallel"] || o.set["addr"] {
		t.Fatalf("set = %v; want model and max-parallel only", o.set)
	}
}

func TestPrepareFindsClaudeAndCreatesTheDataDirectory(t *testing.T) {
	claude := fakeClaude(t)
	o := options{addr: "127.0.0.1:7777", dataDir: filepath.Join(t.TempDir(), "data"), Claude: claude, MaxParallel: 3}
	if err := prepare(&o); err != nil {
		t.Fatal(err)
	}
	if o.claudePath != claude {
		t.Errorf("claudePath = %q; want %q", o.claudePath, claude)
	}
	if info, err := os.Stat(o.dataDir); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("data directory: %v, %v; want a directory with mode 0700", info, err)
	}
}

func TestPrepareRejectsBadSetups(t *testing.T) {
	claude := fakeClaude(t)
	cases := []struct {
		name, addr, claude, wantErr string
	}{
		{"network address", "0.0.0.0:7777", claude, "not a loopback address"},
		{"address without port", "127.0.0.1", claude, "parse -addr"},
		{"missing claude", "127.0.0.1:7777", filepath.Join(t.TempDir(), "no-claude"), "find Claude Code CLI"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := options{addr: tc.addr, dataDir: t.TempDir(), Claude: tc.claude, MaxParallel: 3}
			if err := prepare(&o); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("prepare(addr %q, claude %q) = %v; want an error with %q", tc.addr, tc.claude, err, tc.wantErr)
			}
		})
	}
}

func TestPrepareStopsOnAnInvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"maxParallel": 0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o := options{addr: "127.0.0.1:7777", dataDir: dir, Claude: fakeClaude(t), MaxParallel: 3}
	if err := prepare(&o); err == nil || !strings.Contains(err.Error(), "maxParallel") {
		t.Fatalf("prepare = %v; want the config file error", err)
	}
}

func TestPrepareStopsOnABadEffortAndKeepsGoodDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"effort": "huge"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o := options{addr: "127.0.0.1:7777", dataDir: dir, Claude: fakeClaude(t), MaxParallel: 3, set: map[string]bool{}}
	if err := prepare(&o); err == nil || err.Error() != `config.json: "effort" must be one of low, medium, high, xhigh, max` {
		t.Fatalf("prepare = %v; want the effort error naming the key", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"effort": "High"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	o = options{addr: "127.0.0.1:7777", dataDir: dir, Claude: fakeClaude(t), MaxParallel: 3, set: map[string]bool{}}
	if err := prepare(&o); err != nil || o.Effort != "high" || o.defaults.Effort != "high" || o.defaults.EffortSource != "config" {
		t.Fatalf("prepare = %v, %+v; want the config effort", err, o.defaults)
	}
}

func TestServeAnswersUntilTheContextEnds(t *testing.T) {
	o := options{addr: freeAddr(t), dataDir: t.TempDir(), claudePath: fakeClaude(t), MaxParallel: 3, Updates: updateSettings{AutoCheck: true, DrainMinutes: 45}}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, o, slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	url := "http://" + o.addr + "/api/bots"
	deadline := time.Now().Add(10 * time.Second)
	var bots []struct{ Name, Role string }
	for {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			ok := res.StatusCode == http.StatusOK
			if ok {
				err = json.NewDecoder(res.Body).Decode(&bots)
			}
			res.Body.Close()
			if ok {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s did not answer 200 within 10 s (last error %v)", url, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(bots) != 1 || bots[0].Name != "Builder" || bots[0].Role != "builder" {
		t.Fatalf("bots of a new data directory = %+v; want only Builder", bots)
	}
	// Every setting's edge values, written through the page's API, leave a
	// file that the server starts with.
	for _, body := range []string{
		`{"model": "claude-opus-5-5[1m]", "effort": "max", "maxParallel": 1, "claude": "/opt/claude code/claude", "updates.autoCheck": false, "updates.drainMinutes": 5, "updateCheck": false}`,
		`{"model": "OPUS", "effort": "Low", "maxParallel": 1000, "updates.drainMinutes": 240}`,
		`{"model": null, "effort": null, "maxParallel": null, "claude": null, "updates.autoCheck": null, "updates.drainMinutes": null, "updateCheck": null}`,
	} {
		patchSettings(t, o.addr, body)
		got := settings{}
		if err := applyConfigFile(filepath.Join(o.dataDir, "config.json"), nil, &got); err != nil {
			t.Fatalf("the server cannot start with the file after PATCH %s: %v", body, err)
		}
		if _, err := globalDefaults(&got, nil, func(string) string { return "" }); err != nil {
			t.Fatalf("the model or effort after PATCH %s stops the server: %v", body, err)
		}
	}

	// GET reports the error a restart would stop with, from the same check
	// as POST /api/restart, so the app's bridge can refuse before it tries.
	claude := fakeClaude(t)
	for _, c := range []struct{ file, startError string }{
		{`{"claude": "` + claude + `"}`, ""},
		{`{"maxParallel": 4,}`, "invalid character"},
		{`{"claude": "` + claude + `", "model": "bad model"}`, `config.json: "model": A model ID can't contain spaces`},
		{`{"claude": "` + claude + `", "effort": "ultra"}`, `config.json: "effort" must be one of`},
		{`{"claude": "/missing/claude"}`, `find Claude Code CLI "/missing/claude"`},
	} {
		if err := os.WriteFile(filepath.Join(o.dataDir, "config.json"), []byte(c.file), 0o600); err != nil {
			t.Fatal(err)
		}
		got := getStartError(t, o.addr)
		if c.startError == "" && got != nil || c.startError != "" && (got == nil || !strings.Contains(*got, c.startError)) {
			t.Errorf("startError with %s = %v; want %q (absent when empty)", c.file, got, c.startError)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve after cancel = %v; want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not return within 15 s after the context ended")
	}
}

// getStartError returns the startError of GET /api/settings on addr, or nil
// when the reply has none.
func getStartError(t *testing.T, addr string) *string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/api/settings", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var reply struct {
		StartError *string `json:"startError"`
	}
	if err := json.NewDecoder(res.Body).Decode(&reply); res.StatusCode != http.StatusOK || err != nil {
		t.Fatalf("GET /api/settings = %d, %v; want 200", res.StatusCode, err)
	}
	return reply.StartError
}

// patchSettings sends body to PATCH /api/settings on addr and wants 200.
func patchSettings(t *testing.T, addr, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPatch, "http://"+addr+"/api/settings", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PATCH /api/settings %s = %d %s; want 200", body, res.StatusCode, reply)
	}
}

// A second server on the same address must not touch the database of the
// first: it would mark the running turns of the first as interrupted.
func TestServeFailsOnATakenAddressBeforeItOpensTheDatabase(t *testing.T) {
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	o := options{addr: ln.Addr().String(), dataDir: t.TempDir(), MaxParallel: 3, claudePath: fakeClaude(t)}
	if err := serve(t.Context(), o, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Fatalf("serve on a taken address = %v; want a listen error", err)
	}
	if _, err := os.Stat(filepath.Join(o.dataDir, "yargpt.db")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("database after serve on a taken address: stat = %v; want no database file", err)
	}
}

// fakeLoginShell writes a shell script that prints startup noise and then the
// given environment in the format that shellenv.Load reads.
func fakeLoginShell(t *testing.T, env ...string) string {
	t.Helper()
	body := `printf 'motd\n__HANDFUL_ENV__\0'`
	for _, kv := range env {
		body += `; printf '%s\0' '` + kv + `'`
	}
	path := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetUpFindsClaudeOnTheLoginShellPATH(t *testing.T) {
	claudeDir := filepath.Dir(fakeClaude(t))
	t.Setenv("SHELL", fakeLoginShell(t, "PATH="+claudeDir+":/usr/bin:/bin", "HANDFUL_LOGIN_ENV_TEST=yes"))
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HANDFUL_LOGIN_ENV_TEST", "")

	o, _, err := setUp([]string{"-login-env", "-data", t.TempDir(), "-claude", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(claudeDir, "claude"); o.claudePath != want {
		t.Errorf("claudePath = %q; want %q from the login shell's PATH", o.claudePath, want)
	}
	if got := os.Getenv("HANDFUL_LOGIN_ENV_TEST"); got != "yes" {
		t.Errorf("HANDFUL_LOGIN_ENV_TEST = %q; want the login shell's value yes", got)
	}
}

func TestSetUpKeepsTheEnvironmentWhenTheLoginShellFails(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	t.Setenv("PATH", "/usr/bin:/bin")
	claude := fakeClaude(t)

	o, _, err := setUp([]string{"-login-env", "-data", t.TempDir(), "-claude", claude})
	if err != nil {
		t.Fatalf("setUp with a failing login shell = %v; want it to go on", err)
	}
	if o.claudePath != claude || os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatalf("claudePath %q, PATH %q; want %q and the unchanged PATH", o.claudePath, os.Getenv("PATH"), claude)
	}
}
