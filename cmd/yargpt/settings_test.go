package main

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"yargpt/internal/httpapi"
)

func TestUnderLaunchdNeedsTheAgentsServiceName(t *testing.T) {
	for value, want := range map[string]bool{"dev.yarlson.yargpt.server": true, "0": false, "": false, "com.apple.Terminal": false} {
		if got := underLaunchd(func(k string) string {
			if k == "XPC_SERVICE_NAME" {
				return value
			}
			return ""
		}); got != want {
			t.Errorf("XPC_SERVICE_NAME=%q: underLaunchd = %v; want %v", value, got, want)
		}
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
	r, s := o.settingsOf(func() {}, func(string) string { return "" })
	if r != nil || s.Restart != nil || s.Path != "/data/config.json" || s.LookPath == nil || s.Build != httpapi.BuildSource {
		t.Fatalf("settings from a terminal = %v, %+v; want no restarter", r, s)
	}
	stopped := false
	r, s = o.settingsOf(func() { stopped = true }, func(string) string { return launchdLabel })
	if r == nil || s.Restart == nil {
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
	stopped = make(chan struct{}, 1)
	r = newRestarter(func() { stopped <- struct{}{} })
	tick := make(chan time.Time)
	done = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() { cancel(); <-done })
	go func() {
		defer close(done)
		r.waitIdle(ctx, tick, draining.Load, func() int { return int(busy.Load()) })
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

func TestANilRestarterWatchesNothing(t *testing.T) {
	var r *restarter
	r.watch(t.Context(), nil, nil)()
	if r.result() != nil {
		t.Fatal("a nil restarter asked for a restart")
	}
}
