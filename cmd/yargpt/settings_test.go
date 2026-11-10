package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yargpt/internal/httpapi"
)

func TestUnderLaunchdNeedsTheAgentsServiceNameAndLaunchdAsParent(t *testing.T) {
	for _, c := range []struct {
		name string
		ppid int
		want bool
	}{
		{"dev.yarlson.yargpt.server", 1, true},
		// A child of the server, such as a bot's command, inherits the name.
		{"dev.yarlson.yargpt.server", 4242, false},
		{"0", 1, false},
		{"", 1, false},
		{"com.apple.Terminal", 1, false},
	} {
		getenv := func(k string) string {
			if k == "XPC_SERVICE_NAME" {
				return c.name
			}
			return ""
		}
		if got := underLaunchd(getenv, func() int { return c.ppid }); got != c.want {
			t.Errorf("XPC_SERVICE_NAME=%q, parent %d: underLaunchd = %v; want %v", c.name, c.ppid, got, c.want)
		}
	}
}

// writeConfig writes body as dir's config.json.
func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStartupCheckRunsTheStartupChecksFromTheFlags(t *testing.T) {
	dir := t.TempDir()
	lookPath := func(p string) (string, error) {
		if p == "/missing/claude" {
			return "", errors.New("not found")
		}
		return "/bin/" + p, nil
	}
	flags := settings{Claude: "claude", MaxParallel: 3}
	// What the server runs with now doesn't count: a restart starts from the flags.
	o := options{dataDir: dir, set: map[string]bool{}, flagSettings: flags, Claude: "/missing/claude", Effort: "ultra"}
	check := o.startupCheck(func(string) string { return "" }, lookPath)
	for _, c := range []struct{ file, key, err string }{
		{`{"effort": "max", "claude": "claude"}`, "", ""},
		{`{"maxParallel": 4,}`, "", "invalid character"},
		{`{"model": "bad model"}`, "model", `config.json: "model": A model ID can't contain spaces or start with '-'.`},
		{`{"effort": "ultra"}`, "effort", `config.json: "effort" must be one of low, medium, high, xhigh, max`},
		{`{"claude": "/missing/claude"}`, "claude", `find Claude Code CLI "/missing/claude": not found`},
	} {
		writeConfig(t, dir, c.file)
		key, err := check()
		if key != c.key || (c.err == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), c.err) {
			t.Errorf("startup check of %s = %q, %v; want %q, %q", c.file, key, err, c.key, c.err)
		}
	}
	// A flag wins at startup, so a bad file value under it doesn't stop the server.
	o.set = map[string]bool{"effort": true}
	o.flagSettings.Effort = "high"
	writeConfig(t, dir, `{"effort": "ultra"}`)
	if key, err := o.startupCheck(func(string) string { return "" }, lookPath)(); err != nil {
		t.Fatalf("startup check under -effort = %q, %v; want the flag to win", key, err)
	}
}

func TestSettingsOfTheServerNameTheFlagsAndTheirSources(t *testing.T) {
	o := options{
		addr: "127.0.0.1:7777", dataDir: "/data", loginEnv: true, logFile: "~/server.log", tray: true,
		Claude: "claude", Model: "opus", Effort: "high", MaxParallel: 5, Updates: updateSettings{AutoCheck: true, DrainMinutes: 45, UpdateCheck: false},
		set: map[string]bool{"effort": true, "max-parallel": true, "tray": true, "login-env": true, "log": true},
	}
	running := map[string]any{"model": "opus", "effort": "high", "maxParallel": 5, "claude": "claude", "updates.autoCheck": true, "updates.drainMinutes": 45, "updateCheck": false}
	if got := runningSettings(o); !reflect.DeepEqual(got, running) {
		t.Fatalf("running = %v; want %v", got, running)
	}
	if got, want := settingFlags(o), map[string]string{"effort": "-effort high", "maxParallel": "-max-parallel 5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("flags = %v; want %v", got, want)
	}
	app := []httpapi.RuntimeValue{
		{Key: "addr", Value: "127.0.0.1:7777", Source: "default"},
		{Key: "data", Value: "/data", Source: "default"},
		{Key: "log", Value: "~/server.log", Source: "app"},
		{Key: "login-env", Value: true, Source: "app"},
		{Key: "tray", Value: true, Source: "app"},
	}
	if got := runtimeValues(o, true); !reflect.DeepEqual(got, app) {
		t.Fatalf("runtime under launchd = %+v; want %+v", got, app)
	}
	if got := runtimeValues(o, false); got[2].Source != "flag" || got[0].Source != "default" {
		t.Fatalf("runtime from a terminal = %+v; want given flags from the command line", got)
	}
}

func TestSettingsOfAServerFromATerminalOfferNoRestart(t *testing.T) {
	o := options{dataDir: "/data", set: map[string]bool{}}
	log := slog.New(slog.DiscardHandler)
	r, s := o.settingsOf(func() {}, func(string) string { return "" }, func() int { return 1 }, log)
	if r != nil || s.Restart != nil || s.CancelRestart != nil || s.Path != "/data/config.json" || s.LookPath == nil || s.StartupCheck == nil || s.Build != httpapi.BuildSource {
		t.Fatalf("settings from a terminal = %v, %+v; want no restarter, and a startup check", r, s)
	}
	stopped := false
	r, s = o.settingsOf(func() { stopped = true }, func(string) string { return launchdLabel }, func() int { return 1 }, log)
	if r == nil || s.Restart == nil || s.CancelRestart == nil {
		t.Fatal("settings under launchd: want a restarter")
	}
	s.Restart(false)
	if !stopped || !errors.Is(r.result(), errRestart) {
		t.Fatalf("restart now: stopped %v, result %v; want the server to end to restart", stopped, r.result())
	}
}

func TestBuildKindTellsTheSignedAppFromALocalOneAndASourceBuild(t *testing.T) {
	signed := func(bundle string) string {
		if bundle != "/Applications/YarGPT.app" {
			t.Fatalf("codesign of %q; want the bundle", bundle)
		}
		return "Authority=Developer ID Application: Someone (TEAM)\nAuthority=Apple Root CA\n"
	}
	adhoc := func(string) string { return "Signature=adhoc\n" }
	exe := "/Applications/YarGPT.app/Contents/Resources/yargpt"
	if got := buildKind(exe, signed); got != httpapi.BuildApp {
		t.Errorf("signed app = %q; want app", got)
	}
	if got := buildKind(exe, adhoc); got != httpapi.BuildAdhoc {
		t.Errorf("ad-hoc app = %q; want adhoc", got)
	}
	if got := buildKind("/src/yargpt/bin/yargpt", func(string) string { t.Fatal("codesign ran for a source build"); return "" }); got != httpapi.BuildSource {
		t.Errorf("source build = %q; want source", got)
	}
	if got := codesignInfo("/no/such/bundle.app"); got != "" {
		t.Errorf("codesign of a missing bundle = %q; want empty", got)
	}
}

// restartWatch runs waitIdle with a tick that send drives. A send returns
// once waitIdle took the tick, so the tick before it is fully handled.
func restartWatch(t *testing.T, draining *atomic.Bool, busy *atomic.Int32) (r *restarter, send func(), stopped, done chan struct{}) {
	t.Helper()
	return restartWatchWith(t, draining, busy, func() (string, error) { return "", nil })
}

// restartWatchWith is restartWatch with a startup check; leaving drain mode
// stores false in draining.
func restartWatchWith(t *testing.T, draining *atomic.Bool, busy *atomic.Int32, check func() (string, error)) (r *restarter, send func(), stopped, done chan struct{}) {
	t.Helper()
	stopped = make(chan struct{}, 1)
	r = newRestarter(func() { stopped <- struct{}{} }, check, slog.New(slog.DiscardHandler))
	tick := make(chan time.Time)
	done = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() { cancel(); <-done })
	go func() {
		defer close(done)
		r.waitIdle(ctx, tick, draining.Load, func() int { return int(busy.Load()) }, func() { draining.Store(false) })
	}()
	send = func() {
		select {
		case tick <- time.Time{}:
		case <-done:
		}
	}
	return r, send, stopped, done
}

func TestAnIdleRestartWaitsUntilNoWorkRuns(t *testing.T) {
	var draining atomic.Bool
	var busy atomic.Int32
	draining.Store(true)
	busy.Store(2)
	r, send, stopped, done := restartWatch(t, &draining, &busy)
	busy.Store(0)
	send() // nothing asked yet: no restart, even with no work
	send()
	if r.result() != nil {
		t.Fatal("restarted with no restart asked")
	}
	busy.Store(2)
	r.restart(true)
	r.restart(true) // asking twice is one request
	send()
	send()
	if r.result() != nil {
		t.Fatal("restarted while work runs")
	}
	busy.Store(0)
	send()
	<-stopped
	<-done
	if !errors.Is(r.result(), errRestart) {
		t.Fatalf("result = %v; want errRestart", r.result())
	}
}

func TestLeavingDrainModeCancelsAnIdleRestart(t *testing.T) {
	var draining atomic.Bool
	var busy atomic.Int32
	draining.Store(true)
	busy.Store(1)
	r, send, _, _ := restartWatch(t, &draining, &busy)
	r.restart(true)
	send()
	draining.Store(false)
	send()
	send()
	draining.Store(true)
	busy.Store(0)
	send()
	send()
	if r.result() != nil {
		t.Fatal("restarted after the drain ended; want the request cancelled")
	}
}

func TestAnIdleRestartGivesUpWhenTheFileWentBadMeanwhile(t *testing.T) {
	var draining atomic.Bool
	var busy atomic.Int32
	var bad atomic.Bool
	draining.Store(true)
	busy.Store(1)
	r, send, stopped, _ := restartWatchWith(t, &draining, &busy, func() (string, error) {
		if bad.Load() {
			return "effort", errors.New("bad effort")
		}
		return "", nil
	})
	r.restart(true)
	send()
	bad.Store(true) // a hand edit while the work runs
	busy.Store(0)
	send()
	send()
	select {
	case <-stopped:
		t.Fatal("restarted into a file the server would not start with")
	default:
	}
	if r.result() != nil || r.pending.Load() || draining.Load() {
		t.Fatalf("result %v, pending %v, draining %v; want the restart given up and drain mode left", r.result(), r.pending.Load(), draining.Load())
	}
}

func TestCancelDropsAWaitingRestart(t *testing.T) {
	var draining atomic.Bool
	var busy atomic.Int32
	draining.Store(true)
	busy.Store(1)
	r, send, _, _ := restartWatch(t, &draining, &busy)
	r.restart(true)
	r.cancel()
	busy.Store(0)
	send()
	send()
	if r.result() != nil {
		t.Fatal("restarted after cancel; want the request dropped")
	}
}

func TestANilRestarterWatchesNothing(t *testing.T) {
	var r *restarter
	r.watch(t.Context(), nil, nil)()
	if r.result() != nil {
		t.Fatal("a nil restarter asked for a restart")
	}
}
