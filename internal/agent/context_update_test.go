package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"handful/internal/core"
)

// setMode makes the claude processes that start from now on follow mode.
func (h *harness) setMode(t *testing.T, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, "claude"), []byte("#!/bin/sh\nFAKE_MODE="+mode+"\n"+fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
}

// messages returns the turn message that each claude process got first, in order.
func (h *harness) messages(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.dir, "args.log"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	calls := strings.Split(string(raw), "<<END>>\n")
	calls = calls[:len(calls)-1]
	msgs := make([]string, len(calls))
	for i, c := range calls {
		_, input, _ := strings.Cut(c, "<<STDIN>>")
		msgs[i] = stdinText(t, i, input)
	}
	return msgs
}

// say sends text to the chat chatID (a bot or a team), waits until its turns and processes have
// ended, and returns the message of the claude process that the turn started.
func (h *harness) say(t *testing.T, chatID, text string) string {
	t.Helper()
	before := len(h.messages(t))
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), chatID, text, "n-"+text); err != nil {
		t.Fatal(err)
	}
	h.waitCalls(t, before+1)
	waitFor(t, "the turn to end", func() bool { return !h.runner.Active(chatID) })
	return h.messages(t)[before]
}

// contextBlock is the <context-update> block of a turn message; "" when it has none.
func contextBlock(msg string) string {
	start := strings.Index(msg, "<context-update>")
	if start < 0 {
		return ""
	}
	end := strings.Index(msg[start:], "</context-update>\n")
	if end < 0 {
		return msg[start:]
	}
	return msg[start : start+end+len("</context-update>\n")]
}

// A change to memory or to the bot's instructions reaches the resumed session once, as the
// sections that changed, right after the timestamp line; the system prompt of the run stays.
func TestATurnBringsItsSessionUpToDateWithWhatChanged(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	h.start(t)
	if block := contextBlock(h.say(t, bot.ID, "one")); block != "" {
		t.Fatalf("a new session's first message carries %q; want no update", block)
	}

	if _, err := h.svc.WriteFact(ctx, bot.ID, "", "agent", "profile", "The user drinks green tea."); err != nil {
		t.Fatal(err)
	}
	msg := h.say(t, bot.ID, "two")
	ts, rest, _ := strings.Cut(msg, "\n")
	if !strings.HasPrefix(ts, "<timestamp>") || !strings.HasPrefix(rest, updateIntro+"## Memory\n") {
		t.Fatalf("message after a memory change =\n%s\nwant the timestamp line, then the Memory section in a context update", msg)
	}
	block := contextBlock(msg)
	if !strings.Contains(block, "- [") || !strings.Contains(block, "The user drinks green tea.") || strings.Contains(block, "# You are") || strings.Contains(block, "## How a turn works") {
		t.Fatalf("update after a memory change =\n%s\nwant only the whole Memory section, with the fact", block)
	}

	instructions := "Answer in French."
	if _, err := h.svc.UpdateBot(ctx, bot.ID, nil, &instructions); err != nil {
		t.Fatal(err)
	}
	block = contextBlock(h.say(t, bot.ID, "three"))
	if !strings.HasPrefix(block, updateIntro+"# You are Tester\n") || !strings.Contains(block, "Your standing instructions (your profile description): Answer in French.") || strings.Contains(block, "## Memory") {
		t.Fatalf("update after an instructions change =\n%s\nwant only the section with the instructions", block)
	}

	if block := contextBlock(h.say(t, bot.ID, "four")); block != "" {
		t.Fatalf("message with nothing changed carries %q; want no update", block)
	}
}

// A bot's own chat and its seat in a team are two sessions; each gets a change once, and the
// team's working agreement reaches only the team session.
func TestEachSessionOfABotGetsAChangeOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, _, _ := h.newTeam(t)
	h.start(t)
	h.say(t, ana.ID, "hello")
	h.say(t, team.ID, "plan")

	if _, err := h.svc.WriteFact(ctx, ana.ID, "", "user", "profile", "The user ships on Fridays."); err != nil {
		t.Fatal(err)
	}
	agreement := "Every change has a test that fails without it."
	if _, err := h.svc.UpdateTeam(ctx, team.ID, core.TeamPatch{Agreement: &agreement}); err != nil {
		t.Fatal(err)
	}
	inTeam := contextBlock(h.say(t, team.ID, "next"))
	if !strings.Contains(inTeam, "The user ships on Fridays.") || !strings.Contains(inTeam, "## Your team\n") || !strings.Contains(inTeam, agreement) {
		t.Fatalf("the team turn's update =\n%s\nwant the Memory and Your team sections", inTeam)
	}
	own := contextBlock(h.say(t, ana.ID, "and you?"))
	if !strings.Contains(own, "The user ships on Fridays.") || strings.Contains(own, agreement) {
		t.Fatalf("the own chat's update =\n%s\nwant the Memory section, without the team's agreement", own)
	}
	for _, chat := range []string{team.ID, ana.ID} {
		if block := contextBlock(h.say(t, chat, "again")); block != "" {
			t.Fatalf("chat %s carries the update again:\n%s", chat, block)
		}
	}
}

// An update reaches the session only with a turn that succeeds: after a failed turn and an
// interrupted one, the next turn carries the same update again.
func TestContextUpdateGoesAgainUntilATurnDeliversIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	h.start(t)
	h.say(t, bot.ID, "one")
	if _, err := h.svc.WriteFact(ctx, bot.ID, "", "agent", "log", "The build moved to CI."); err != nil {
		t.Fatal(err)
	}

	h.setMode(t, "error")
	failed := contextBlock(h.say(t, bot.ID, "two"))
	if !strings.Contains(failed, "The build moved to CI.") {
		t.Fatalf("the failed turn's update = %q; want the Memory section", failed)
	}

	h.setMode(t, "sleep")
	before := len(h.messages(t))
	if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "three", "n3"); err != nil {
		t.Fatal(err)
	}
	h.waitEntry(t, bot.ID, func(e core.Entry) bool { return e.Kind == core.KindActivity && bodyField(e, "status") == "running" })
	if _, err := h.runner.Interrupt(ctx, bot.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the stop", func() bool { return !h.runner.Active(bot.ID) })
	if stopped := contextBlock(h.messages(t)[before]); stopped != failed {
		t.Fatalf("the interrupted turn's update =\n%s\nwant the failed turn's\n%s", stopped, failed)
	}

	h.setMode(t, "reply")
	if delivered := contextBlock(h.say(t, bot.ID, "four")); delivered != failed {
		t.Fatalf("the next turn's update =\n%s\nwant the failed turn's again\n%s", delivered, failed)
	}
	if block := contextBlock(h.say(t, bot.ID, "five")); block != "" {
		t.Fatalf("after a turn that delivered it, the update goes again:\n%s", block)
	}
}

// restart stops h's runner and starts a new one, with a new service, on the same database.
func (h *harness) restart(t *testing.T) {
	t.Helper()
	h.cancel()
	<-h.done
	svc, err := core.New(h.conn, h.dir, h.svc.Location())
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	h.runner = NewRunner(svc, Config{ClaudePath: filepath.Join(h.dir, "claude"), MCPURL: "http://127.0.0.1:1/mcp"}, slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelError})))
	h.runner.environ = func() []string { return append(testEnviron(), h.env...) }
	h.done = make(chan struct{})
	h.start(t)
}

// What a session knows is stored: after a restart, a turn with nothing changed carries no
// update. A session that Handful stored nothing for, as before this check, gets every section
// once.
func TestTheKnownPromptSurvivesARestart(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	stored, unknown := h.newNamedBot(t, "Ana"), h.newNamedBot(t, "Bo")
	if err := h.svc.SetSession(ctx, unknown.ID, "sess-before"); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	h.say(t, stored.ID, "one")
	h.restart(t)

	if block := contextBlock(h.say(t, stored.ID, "two")); block != "" {
		t.Fatalf("after a restart, a session with nothing changed gets\n%s\nwant no update", block)
	}
	all := contextBlock(h.say(t, unknown.ID, "one"))
	for _, heading := range []string{updateIntro + "# You are Bo\n", "\n## How a turn works\n", "\n## Memory\n", "\n## Time\n"} {
		if strings.Count(all, heading) != 1 {
			t.Fatalf("a session Handful knows nothing of gets\n%s\nwant %q once", all, heading)
		}
	}
	if block := contextBlock(h.say(t, unknown.ID, "two")); block != "" {
		t.Fatalf("the next turn gets\n%s\nwant no update", block)
	}
}

// Handful's /compact runs in a process without Handful's prompt, and the next launch records its
// own prompt, so the next turn carries no update, even for a change made before it.
func TestAfterCompactInItsOwnProcessTheNextTurnCarriesNoUpdate(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	h.start(t)
	h.say(t, bot.ID, "one")
	if _, err := h.svc.WriteFact(ctx, bot.ID, "", "agent", "profile", "The user likes short answers."); err != nil {
		t.Fatal(err)
	}
	h.setMode(t, "compact")
	if _, err := h.svc.QueueCompaction(ctx, bot.ID); err != nil {
		t.Fatal(err)
	}
	h.waitEntry(t, bot.ID, func(e core.Entry) bool {
		return e.Kind == core.KindEvent && bodyField(e, "type") == "context-compacted"
	})
	waitFor(t, "the compaction to end", func() bool { return !h.runner.Active(bot.ID) })

	h.setMode(t, "reply")
	if block := contextBlock(h.say(t, bot.ID, "two")); block != "" {
		t.Fatalf("the turn after /compact gets\n%s\nwant no update", block)
	}
	if calls := h.argsLog(t); !strings.Contains(calls[len(calls)-1], "The user likes short answers.") {
		t.Fatal("the run after /compact lacks the fact in its system prompt")
	}
	if block := contextBlock(h.say(t, bot.ID, "three")); block != "" {
		t.Fatalf("the next turn gets\n%s\nwant no update", block)
	}
}

// When Claude Code compacts a session in a turn, it records the prompt its process started with
// again. An update delivered since then goes again, and sections that did not change do not.
func TestACompactionBringsBackTheUpdatesSinceTheProcessStarted(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "background")
	ctx := context.Background()
	bot := h.newBot(t)
	h.start(t)
	h.startBackgroundTask(t, bot)
	if _, err := h.svc.WriteFact(ctx, bot.ID, "", "agent", "profile", "The user deploys on Mondays."); err != nil {
		t.Fatal(err)
	}
	stdin := func() []string {
		raw, _ := os.ReadFile(filepath.Join(h.dir, "stdin.log"))
		lines := strings.SplitAfter(string(raw), "\n")
		lines = lines[:len(lines)-1]
		texts := make([]string, len(lines))
		for i, l := range lines {
			texts[i] = stdinText(t, i, l)
		}
		return texts
	}
	send := func(text string, n int) string {
		t.Helper()
		if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, text, "n-"+text); err != nil {
			t.Fatal(err)
		}
		waitFor(t, "message "+strconv.Itoa(n), func() bool { return len(stdin()) >= n && !h.runner.Running(bot.ID) })
		return stdin()[n-1]
	}
	first := contextBlock(send("ping", 1))
	if !strings.Contains(first, "The user deploys on Mondays.") {
		t.Fatalf("the update in the live process = %q; want the Memory section", first)
	}
	if block := contextBlock(send("ping again", 2)); block != "" {
		t.Fatalf("a delivered update goes again before any compaction:\n%s", block)
	}

	if _, err := h.svc.QueueCompaction(ctx, bot.ID); err != nil {
		t.Fatal(err)
	}
	h.waitEntry(t, bot.ID, func(e core.Entry) bool {
		return e.Kind == core.KindEvent && bodyField(e, "type") == "context-compacted"
	})
	waitFor(t, "the compaction to end", func() bool { return !h.runner.Running(bot.ID) })
	if again := contextBlock(send("ping after", 4)); again != first {
		t.Fatalf("the update after /compact =\n%s\nwant the first one again\n%s", again, first)
	}
	if block := contextBlock(send("auto-compact on the way", 5)); block != "" {
		t.Fatalf("a delivered update goes again before the compaction:\n%s", block)
	}
	if again := contextBlock(send("ping last", 6)); again != first {
		t.Fatalf("the update after a compaction in a turn =\n%s\nwant the first one again\n%s", again, first)
	}
	if calls := h.argsLog(t); len(calls) != 1 {
		t.Fatalf("claude started %d times; want one process for every message", len(calls))
	}
}

// A turn whose stored session is gone runs again on a fresh one, and what that session knows is
// stored for it, so the next turn carries no update.
func TestAFreshSessionAfterAMissingOneKnowsItsPrompt(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "missing-session")
	ctx := context.Background()
	bot := h.newBot(t)
	if err := h.svc.SetSession(ctx, bot.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	h.say(t, bot.ID, "one")
	h.setMode(t, "reply")
	if block := contextBlock(h.say(t, bot.ID, "two")); block != "" {
		t.Fatalf("the turn after a fresh session gets\n%s\nwant no update", block)
	}
}
