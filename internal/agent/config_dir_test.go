package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"handful/internal/core"
)

// envLog returns the CLAUDE_* and model variables of the latest claude process.
func (h *harness) envLog(t *testing.T) string {
	t.Helper()
	env, err := os.ReadFile(filepath.Join(h.dir, "env.log"))
	if err != nil {
		t.Fatal(err)
	}
	return string(env)
}

func TestEachBotRunsClaudeOnItsOwnClaudeCodeFolderWithTheUsersLogin(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ana, bo := h.newNamedBot(t, "Ana"), h.newNamedBot(t, "Bo")
	h.start(t)
	for _, bot := range []core.Bot{ana, bo} {
		h.say(t, bot.ID, "hi "+bot.Name)
		want := "CLAUDE_CONFIG_DIR=" + filepath.Join(h.dir, "bots", bot.ID, "claude") + "\nCLAUDE_SECURESTORAGE_CONFIG_DIR=\n"
		if env := h.envLog(t); !strings.Contains(env, want) {
			t.Errorf("%s's claude environment:\n%s\nwant:\n%s", bot.Name, env, want)
		}
		if info, err := os.Stat(filepath.Join(h.dir, "bots", bot.ID, "claude")); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Errorf("%s's Claude Code folder: %v, %v; want a folder only the user can open", bot.Name, info, err)
		}
	}
}

func TestABotKeepsTheLoginOfTheUsersOwnClaudeCodeFolder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, env, want string
	}{
		{"a folder set for claude", "CLAUDE_CONFIG_DIR=/home/ana/claude-work", "CLAUDE_SECURESTORAGE_CONFIG_DIR=/home/ana/claude-work\n"},
		{"a login folder set for claude", "CLAUDE_SECURESTORAGE_CONFIG_DIR=/home/ana/login", "CLAUDE_SECURESTORAGE_CONFIG_DIR=/home/ana/login\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, "reply")
			bot := h.newBot(t)
			h.env = []string{tc.env}
			h.start(t)
			h.say(t, bot.ID, "hi")
			env := h.envLog(t)
			if !strings.Contains(env, tc.want) || !strings.Contains(env, "CLAUDE_CONFIG_DIR="+h.svc.BotConfigDir(bot.ID)+"\n") {
				t.Fatalf("claude environment with %s:\n%s\nwant the bot's folder and %s", tc.env, env, tc.want)
			}
		})
	}
}

func TestAResumedSessionFindsItsTranscriptInTheBotsClaudeCodeFolder(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	bot := h.newBot(t)
	ctx := context.Background()
	const session = "0b6a3c1e-7f43-4c55-9a43-3e2f8c1d0a77"
	if err := h.svc.SetSession(ctx, bot.ID, session); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(h.svc.Workspace(bot.ID))
	if err != nil {
		t.Fatal(err)
	}
	// Claude Code's key of a working folder: each character that is not a letter or a digit becomes "-".
	key := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(real, "-")
	home := filepath.Join(h.dir, "home")
	source := filepath.Join(home, ".claude", "projects", key)
	if err := os.MkdirAll(filepath.Join(source, session, "subagents"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{session + ".jsonl": `{"type":"user"}` + "\n", filepath.Join(session, "subagents", "agent-1.jsonl"): `{"type":"assistant"}` + "\n"}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.env = []string{"HOME=" + home}
	h.start(t)

	h.say(t, bot.ID, "hi")
	for name, text := range files {
		got, err := os.ReadFile(filepath.Join(h.svc.BotConfigDir(bot.ID), "projects", key, name))
		if err != nil || string(got) != text {
			t.Errorf("copy of %s = %q, %v; want %q", name, got, err, text)
		}
		if orig, err := os.ReadFile(filepath.Join(source, name)); err != nil || string(orig) != text {
			t.Errorf("original %s = %q, %v; want it unchanged", name, orig, err)
		}
	}
}

func TestATurnWithoutALoginSaysHowToLogIn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "not-logged-in")
	bot := h.newBot(t)
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), bot.ID, "hi", "n1"); err != nil {
		t.Fatal(err)
	}
	failure := h.waitEntry(t, bot.ID, func(e core.Entry) bool { return e.Kind == core.KindEvent && bodyField(e, "type") == "turn-failed" })
	want := "This bot is not logged in to Claude Code. Run claude in a terminal, log in with /login, then try again. Claude Code said: Not logged in · Please run /login"
	if got := bodyField(failure, "text"); got != want {
		t.Fatalf("failure = %q; want %q", got, want)
	}
}

func TestAnotherFailureShowsClaudeCodesTextAlone(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "error")
	bot := h.newBot(t)
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), bot.ID, "hi", "n1"); err != nil {
		t.Fatal(err)
	}
	failure := h.waitEntry(t, bot.ID, func(e core.Entry) bool { return e.Kind == core.KindEvent && bodyField(e, "type") == "turn-failed" })
	if got := bodyField(failure, "text"); got != "boom" {
		t.Fatalf("failure = %q; want boom", got)
	}
}
