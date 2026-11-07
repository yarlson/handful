package main

import (
	"context"
	"io"
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
	o, err := parseFlags([]string{"-model", "sonnet", "-max-parallel", "5"})
	if err != nil {
		t.Fatal(err)
	}
	if o.addr != "127.0.0.1:7777" || o.Claude != "claude" || o.Model != "sonnet" || o.MaxParallel != 5 || !strings.HasSuffix(o.dataDir, ".yargpt") {
		t.Fatalf("options = %+v; want the default address, data folder and claude, model sonnet and 5 parallel turns", o)
	}
	if !o.set["model"] || !o.set["max-parallel"] || o.set["addr"] {
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

func TestServeAnswersUntilTheContextEnds(t *testing.T) {
	o := options{addr: freeAddr(t), dataDir: t.TempDir(), MaxParallel: 3, claudePath: fakeClaude(t)}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, o, slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	url := "http://" + o.addr + "/api/bots"
	deadline := time.Now().Add(10 * time.Second)
	for {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s did not answer 200 within 10 s (last error %v)", url, err)
		}
		time.Sleep(50 * time.Millisecond)
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

func TestServeFailsWhenTheAddressIsTaken(t *testing.T) {
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	o := options{addr: ln.Addr().String(), dataDir: t.TempDir(), MaxParallel: 3, claudePath: fakeClaude(t)}
	if err := serve(t.Context(), o, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Fatalf("serve on a taken address = %v; want a listen error", err)
	}
}

// fakeLoginShell writes a shell script that prints startup noise and then the
// given environment in the format that shellenv.Load reads.
func fakeLoginShell(t *testing.T, env ...string) string {
	t.Helper()
	body := `printf 'motd\n__YARGPT_ENV__\0'`
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
	t.Setenv("SHELL", fakeLoginShell(t, "PATH="+claudeDir+":/usr/bin:/bin", "YARGPT_LOGIN_ENV_TEST=yes"))
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("YARGPT_LOGIN_ENV_TEST", "")

	o, _, err := setUp([]string{"-login-env", "-data", t.TempDir(), "-claude", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(claudeDir, "claude"); o.claudePath != want {
		t.Errorf("claudePath = %q; want %q from the login shell's PATH", o.claudePath, want)
	}
	if got := os.Getenv("YARGPT_LOGIN_ENV_TEST"); got != "yes" {
		t.Errorf("YARGPT_LOGIN_ENV_TEST = %q; want the login shell's value yes", got)
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
