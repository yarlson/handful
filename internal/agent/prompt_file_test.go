package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"yargpt/internal/core"
)

// The system prompt holds the bot's memory. In argv, ps would show it and
// pkill -f would match it, so a pattern kill of a phrase from memory would
// stop the bot. It goes in a private file instead.
func TestTheSystemPromptGoesInAPrivateFileNotInArgv(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	const phrase = "node mock-server-7f3a.mjs"
	if _, err := h.svc.WriteFact(ctx, bot.ID, "", "agent", "profile", "Start it with "+phrase+"."); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "hi", "n1"); err != nil {
		t.Fatal(err)
	}
	h.waitEntry(t, bot.ID, func(e core.Entry) bool { return e.Kind == core.KindBot })
	raw, err := os.ReadFile(filepath.Join(h.dir, "args.log"))
	if err != nil {
		t.Fatal(err)
	}
	args, rest, _ := strings.Cut(string(raw), "<<SYSTEM>>")
	system, _, _ := strings.Cut(rest, "<<STDIN>>")
	if strings.Contains(args, phrase) || strings.Contains(args, "--append-system-prompt ") {
		t.Fatalf("argv carries the system prompt: %s", args)
	}
	if !strings.Contains(args, "--append-system-prompt-file "+filepath.Join(h.dir, "run", "prompt-")) {
		t.Fatalf("argv lacks the prompt file in the run folder: %s", args)
	}
	if !strings.Contains(system, phrase) {
		t.Fatalf("the prompt file lacks the bot's memory:\n%s", system)
	}
	waitFor(t, "the prompt file is removed after the process exits", func() bool {
		left, _ := filepath.Glob(filepath.Join(h.dir, "run", "prompt-*.md"))
		return len(left) == 0
	})
}

func TestRunFilesAreReadableOnlyByTheUser(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	path, err := h.runner.writeRunFile("prompt-*.md", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v; want 0600", info.Mode().Perm())
	}
	if got, _ := os.ReadFile(path); string(got) != "secret" {
		t.Fatalf("file holds %q", got)
	}
	h.runner.removeRunFiles([]string{path, path})
	if fileExists(path) {
		t.Fatal("the file is still there")
	}
}

func TestKilledByNamesTheSignal(t *testing.T) {
	t.Parallel()
	run := func(script string) error {
		return exec.CommandContext(context.Background(), "/bin/sh", "-c", script).Run()
	}
	for _, c := range []struct {
		err  error
		name string
		ok   bool
	}{
		{run("kill -TERM $$"), "SIGTERM", true},
		{run("kill -KILL $$"), "SIGKILL", true},
		{run("exit 3"), "", false},
		{errors.New("other"), "", false},
		{nil, "", false},
	} {
		if name, ok := killedBy(c.err); name != c.name || ok != c.ok {
			t.Errorf("killedBy(%v) = %q, %v; want %q, %v", c.err, name, ok, c.name, c.ok)
		}
	}
}

func TestATurnStoppedFromOutsideNamesTheSignal(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "sleep")
	ctx := context.Background()
	bot := h.newBot(t)
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "hi", "n1"); err != nil {
		t.Fatal(err)
	}
	var pid int
	waitFor(t, "claude to start", func() bool {
		raw, err := os.ReadFile(filepath.Join(h.dir, "pid"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
		return err == nil && pid > 0
	})
	// As a pattern kill would: SIGTERM to the process group, from outside YarGPT.
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	e := h.waitEntry(t, bot.ID, func(e core.Entry) bool { return bodyField(e, "type") == "turn-failed" })
	if text, _ := bodyField(e, "text").(string); text != "stopped by SIGTERM from outside YarGPT" {
		t.Fatalf("failure = %q; want the signal in plain words", text)
	}
}

func TestExitReasonKeepsYarGPTsOwnStopsAndPlainExits(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	s := &session{key: sessionKey{"b", ""}}
	term := exec.CommandContext(context.Background(), "/bin/sh", "-c", "kill -TERM $$").Run()
	for _, c := range []struct {
		err            error
		stopped, quiet bool
		want, stderr   string
	}{
		{term, false, false, "stopped by SIGTERM from outside YarGPT", ""},
		{term, true, false, term.Error(), "err"},
		{term, false, true, "no output from Claude for 30 minutes", ""},
		{errors.New("exit status 1"), false, false, "exit status 1", "err"},
	} {
		stderr, got := h.runner.exitReason(s, c.err, "err", c.stopped, c.quiet)
		if got.Error() != c.want || stderr != c.stderr {
			t.Errorf("exitReason(%v, stopped %v, silent %v) = %v, %q; want %q, %q", c.err, c.stopped, c.quiet, got, stderr, c.want, c.stderr)
		}
	}
}
