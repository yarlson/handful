package shellenv

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeShell writes an executable sh script with the given body.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseReadsThePairsAfterTheMarker(t *testing.T) {
	out := []byte("Welcome back!\nnvm loaded" + marker + "\x00PATH=/a:/b\x00OPTS=x=1\x00NOTE=two\nlines\x00=no-name\x00NOEQUALS\x00")
	got, err := parse(out)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"PATH": "/a:/b", "OPTS": "x=1", "NOTE": "two\nlines"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse = %q; want %q", got, want)
	}
}

func TestParseFailsWithoutTheMarker(t *testing.T) {
	if _, err := parse([]byte("PATH=/a\x00")); err == nil {
		t.Fatal("parse without the marker succeeded; want an error")
	}
}

func TestLoadReturnsTheShellEnvironment(t *testing.T) {
	shell := fakeShell(t, `printf 'motd\n%s\0PATH=/opt/tools/bin\0HOME=/home/u\0' `+marker)
	got, err := Load(t.Context(), shell)
	if err != nil {
		t.Fatal(err)
	}
	if got["PATH"] != "/opt/tools/bin" || got["HOME"] != "/home/u" || len(got) != 2 {
		t.Fatalf("Load = %q; want PATH and HOME from the shell", got)
	}
}

func TestLoadFailsWhenTheShellFails(t *testing.T) {
	_, err := Load(t.Context(), fakeShell(t, "exit 3"))
	if err == nil || !strings.Contains(err.Error(), "run login shell") {
		t.Fatalf("Load with a failing shell = %v; want a run error", err)
	}
}

func TestLoadStopsAHangingShellAtTheDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Load(ctx, fakeShell(t, "sleep 30")); err == nil {
		t.Fatal("Load with a hanging shell succeeded; want an error")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("Load took %v after a 200 ms deadline; want it to stop within 5 s", took)
	}
}

func TestLoadRunsTheEnvironmentCommandInALoginShell(t *testing.T) {
	t.Setenv("YARGPT_SHELLENV_TEST", "a=b\nc")
	// The fake shell checks the flags, then runs the command with a plain sh,
	// which reads no startup files.
	shell := fakeShell(t, `[ "$1" = -ilc ] || exit 9
exec /bin/sh -c "$2"`)
	got, err := Load(t.Context(), shell)
	if err != nil {
		t.Fatal(err)
	}
	if got["YARGPT_SHELLENV_TEST"] != "a=b\nc" {
		t.Fatalf("YARGPT_SHELLENV_TEST = %q; want %q from the environment of the shell", got["YARGPT_SHELLENV_TEST"], "a=b\nc")
	}
}

func TestLoadKeepsTheEnvironmentWhenAStartupFileLeavesAProcess(t *testing.T) {
	// The background sleep keeps stdout open after the shell exits.
	shell := fakeShell(t, `sleep 2 &
printf '%s\0PATH=/opt/tools/bin\0' `+marker)
	got, err := Load(t.Context(), shell)
	if err != nil {
		t.Fatalf("Load = %v; want the environment that the shell printed", err)
	}
	if got["PATH"] != "/opt/tools/bin" {
		t.Fatalf("PATH = %q; want /opt/tools/bin", got["PATH"])
	}
}
