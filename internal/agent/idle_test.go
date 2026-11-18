package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"handful/internal/core"
)

// idleClock is the runner's clock in an idle compaction test. The test sets the time, and each
// wait for a due compaction goes to waits, where the test ends it.
type idleClock struct {
	mu    sync.Mutex
	at    time.Time
	waits chan idleWait
	// ended closes when the runner's goroutines end; nil when no wait for them is going.
	// Only the test's goroutine uses it.
	ended chan struct{}
}

type idleWait struct {
	d    time.Duration // how long the runner asked to wait
	fire chan time.Time
}

func (c *idleClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *idleClock) after(d time.Duration) <-chan time.Time {
	w := idleWait{d: d, fire: make(chan time.Time, 1)}
	c.waits <- w
	return w.fire
}

// fire ends wait w at time at.
func (c *idleClock) fire(w *idleWait, at time.Time) {
	c.mu.Lock()
	c.at = at
	c.mu.Unlock()
	w.fire <- at
}

// idleStart is the time of every model call in the tests; the compaction is due 54 minutes later.
var idleStart = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)

func callUsage(input, write, read, output int64) string {
	return fmt.Sprintf(`{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}`, input, write, read, output)
}

// largeCall has a context of 100,000 tokens, the least that is compacted. Its input, cache
// write and output, with 2,000 tokens for the compaction's own text, are 10% of that: the most
// that still compacts.
var largeCall = callUsage(1_000, 6_500, 92_500, 500)

// newIdleHarness makes a harness whose claude processes follow mode and report call's tokens for
// each model call, with a clock that the test controls and a log of Info and above. The test
// runs turns with turn, without Run, unless it starts Run.
func newIdleHarness(t *testing.T, mode, call string) (*harness, *idleClock) {
	t.Helper()
	h := newHarness(t, mode)
	h.env = append(h.env, "FAKE_USAGE="+call)
	c := &idleClock{at: idleStart, waits: make(chan idleWait, 8)}
	h.runner.now, h.runner.after = c.now, c.after
	h.runner.log = slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	t.Cleanup(func() {
		h.runner.mu.Lock()
		defer h.runner.mu.Unlock()
		h.runner.cancelIdleLocked(func(sessionKey) bool { return true })
	})
	return h, c
}

// settle waits until the runner's goroutines end and returns nil, or until an idle compaction
// waits and returns its wait.
func (c *idleClock) settle(t *testing.T, h *harness) *idleWait {
	t.Helper()
	// A wait that an idle wait cut short goes on. The next settle waits for it again instead of
	// starting another: the next dispatch's wg.Add from 0 must come after that Wait returns.
	if c.ended == nil {
		ended := make(chan struct{})
		go func() { h.runner.wg.Wait(); close(ended) }()
		c.ended = ended
	}
	select {
	case <-c.ended:
		c.ended = nil
		return nil
	case w := <-c.waits:
		return &w
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the runner")
		return nil
	}
}

// turn sends text to the chat chatID and runs its turn. It returns the wait of the idle
// compaction that the turn's process planned when it exited, or nil when it planned none.
func (c *idleClock) turn(t *testing.T, h *harness, chatID, text string) *idleWait {
	t.Helper()
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), chatID, text, "n-"+text); err != nil {
		t.Fatal(err)
	}
	h.runner.dispatch(context.Background())
	w := c.settle(t, h)
	waitFor(t, "the turn to end", func() bool {
		h.runner.mu.Lock()
		defer h.runner.mu.Unlock()
		for bot, rt := range h.runner.running {
			if bot == chatID || rt.roomID == chatID {
				return false
			}
		}
		return true
	})
	if h.runner.Running(chatID) {
		t.Fatal("the chat counts as working while its idle compaction only waits")
	}
	return w
}

// plan returns the planned idle compaction of key; nil when there is none.
func (h *harness) plan(key sessionKey) *idlePlan {
	h.runner.mu.Lock()
	defer h.runner.mu.Unlock()
	return h.runner.idle[key]
}

func (h *harness) planState(p *idlePlan) idleState {
	h.runner.mu.Lock()
	defer h.runner.mu.Unlock()
	return p.state
}

// compactionTurns returns the status of each compact turn of the bot.
func (h *harness) compactionTurns(t *testing.T, botID string) []string {
	t.Helper()
	rows, err := h.conn.QueryContext(context.Background(), `SELECT status FROM turns WHERE bot_id = ? AND source = 'compact' ORDER BY created_at`, botID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// rawCalls returns, for each claude process, its arguments with the text of its system prompt
// file, and its first message.
func (h *harness) rawCalls(t *testing.T) (heads, messages []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.dir, "args.log"))
	if err != nil {
		t.Fatal(err)
	}
	calls := strings.Split(string(raw), "<<END>>\n")
	for i, c := range calls[:len(calls)-1] {
		head, input, _ := strings.Cut(c, "<<STDIN>>")
		heads, messages = append(heads, head), append(messages, stdinText(t, i, input))
	}
	return heads, messages
}

var runFileArg = regexp.MustCompile(`(--mcp-config|--append-system-prompt-file) \S+`)

func compactedEntries(t *testing.T, h *harness, transcript string) []string {
	t.Helper()
	entries, err := h.svc.Entries(context.Background(), transcript, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, e := range entries {
		if e.Kind == core.KindEvent && bodyField(e, "type") == "context-compacted" {
			texts = append(texts, fmt.Sprint(bodyField(e, "text")))
		}
	}
	return texts
}

// B1, B3, B4: 54 minutes after its last model call, a session of 100,000 tokens is compacted by
// a process that resumes it with the arguments and system prompt of the turn whose process it
// was, so the compaction reads the cached prefix. The chat shows the notice.
func TestALargeIdleSessionIsCompactedWithItsTurnsArguments(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "idle", largeCall)
	bot := h.newBot(t)
	w := c.turn(t, h, bot.ID, "hello")
	if w == nil {
		t.Fatal("no idle compaction planned after a call with 100,000 tokens of context")
	}
	if w.d != 54*time.Minute {
		t.Fatalf("the compaction waits %v; want 54m after the last call", w.d)
	}

	c.fire(w, idleStart.Add(55*time.Minute))
	if again := c.settle(t, h); again != nil {
		t.Fatal("the compaction's own process planned another compaction")
	}
	heads, messages := h.rawCalls(t)
	if len(heads) != 2 {
		t.Fatalf("claude ran %d times; want the turn, then the compaction", len(heads))
	}
	got, err := h.svc.GetBot(context.Background(), bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	session := got.ClaudeSessionID
	want := strings.Replace(runFileArg.ReplaceAllString(heads[0], "$1 FILE"), "--session-id "+session, "--resume "+session, 1)
	if compaction := runFileArg.ReplaceAllString(heads[1], "$1 FILE"); compaction != want {
		t.Fatalf("compaction arguments and system prompt =\n%s\nwant the turn's, resuming session %s:\n%s", compaction, session, want)
	}
	if messages[1] != "/compact" {
		t.Fatalf("compaction message = %q; want /compact", messages[1])
	}
	if texts := compactedEntries(t, h, bot.ID); len(texts) != 1 || texts[0] != "Compacted while idle, before the prompt cache expired." {
		t.Fatalf("compaction notices = %q; want the idle notice once", texts)
	}
	if turns := h.compactionTurns(t, bot.ID); len(turns) != 1 || turns[0] != "done" {
		t.Fatalf("compact turns = %q; want one, done", turns)
	}
	if h.plan(sessionKey{botID: bot.ID}) != nil {
		t.Fatal("the done compaction is still the session's plan")
	}
}

// B4: in a room, the notice names the member whose session was compacted.
func TestARoomSessionsIdleCompactionNamesTheMember(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "idle", largeCall)
	ctx := context.Background()
	ana := h.newNamedBot(t, "Ana")
	room, err := h.svc.CreateRoom(ctx, "Plan", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	h.env = append(h.env, "FAKE_HOLD=1")
	w := c.turn(t, h, room.ID, "hello")
	if w == nil {
		t.Fatal("no idle compaction planned for the room session")
	}
	c.fire(w, idleStart.Add(54*time.Minute))
	waitFor(t, "the compaction turn to start", func() bool { return slices.Equal(h.compactionTurns(t, ana.ID), []string{"running"}) })
	if !h.runner.Running(room.ID) {
		t.Fatal("the room does not count as working while its member's session compacts")
	}
	if err := os.WriteFile(filepath.Join(h.dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c.settle(t, h)
	if texts := compactedEntries(t, h, room.ID); len(texts) != 1 || texts[0] != "Ana: compacted while idle, before the prompt cache expired." {
		t.Fatalf("room notices = %q; want Ana's idle notice once", texts)
	}
	seats, err := h.svc.RoomSeats(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if heads, _ := h.rawCalls(t); len(heads) != 2 || !strings.Contains(heads[1], "--resume "+seats[0].ClaudeSessionID) {
		t.Fatalf("claude calls = %d; want the compaction to resume Ana's room session %s", len(heads), seats[0].ClaudeSessionID)
	}
}

// B1: a session under 100,000 tokens of context is not compacted.
func TestASessionUnder100kTokensGetsNoIdleCompaction(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "idle", callUsage(1_000, 2_000, 96_999, 500))
	bot := h.newBot(t)
	if w := c.turn(t, h, bot.ID, "hello"); w != nil {
		t.Fatal("an idle compaction was planned for a context of 99,999 tokens")
	}
}

// B2, B6: at the due time, each check that fails skips the compaction and logs its reason.
func TestADueIdleCompactionIsSkippedWithItsReason(t *testing.T) {
	t.Parallel()
	due := idleStart.Add(54 * time.Minute)
	for _, c := range []struct {
		name   string
		call   string
		at     time.Time
		before func(t *testing.T, h *harness, bot core.Bot)
		reason string
	}{
		{"the setting is off", largeCall, due, func(_ *testing.T, h *harness, _ core.Bot) {
			h.runner.cfg.IdleCompaction = func() bool { return false }
		}, "idle_compaction_off"},
		{"the last call wrote more than 10% of its context", callUsage(1_000, 6_500, 92_500, 501), due, nil, "prefix_changed"},
		{"the due time passed more than a minute ago", largeCall, due.Add(time.Minute + time.Nanosecond), nil, "lapsed"},
		{"a turn of the session waits", largeCall, due, func(t *testing.T, h *harness, bot core.Bot) {
			if _, _, err := h.svc.SubmitUserMessage(context.Background(), bot.ID, "again", "n-again"); err != nil {
				t.Fatal(err)
			}
		}, "newer_request"},
		{"turns wait for a usage limit", largeCall, due, func(_ *testing.T, h *harness, _ core.Bot) {
			h.runner.mu.Lock()
			h.runner.pausedUntil = due.Add(time.Hour)
			h.runner.mu.Unlock()
		}, "near_limit"},
		{"the context was cleared", largeCall, due, func(t *testing.T, h *harness, bot core.Bot) {
			if err := h.svc.ClearContext(context.Background(), bot.ID); err != nil {
				t.Fatal(err)
			}
		}, "not_resumed"},
		{"the bot is being deleted", largeCall, due, func(t *testing.T, h *harness, bot core.Bot) {
			if _, err := h.runner.BeginDelete(context.Background(), bot.ID); err != nil {
				t.Fatal(err)
			}
		}, "not_resumed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			h, clock := newIdleHarness(t, "idle", c.call)
			bot := h.newBot(t)
			w := clock.turn(t, h, bot.ID, "hello")
			if w == nil {
				t.Fatal("no idle compaction planned")
			}
			if c.before != nil {
				c.before(t, h, bot)
			}
			clock.fire(w, c.at)
			clock.settle(t, h)
			if logs := h.logs.String(); !strings.Contains(logs, `msg="skip idle compaction" bot=`+bot.ID+` room="" reason=`+c.reason+"\n") {
				t.Fatalf("log =\n%s\nwant the compaction skipped for %s", logs, c.reason)
			}
			if heads, _ := h.rawCalls(t); len(heads) != 1 {
				t.Fatalf("claude ran %d times; want only the turn", len(heads))
			}
			if turns := h.compactionTurns(t, bot.ID); len(turns) != 0 {
				t.Fatalf("compact turns = %q; want none", turns)
			}
		})
	}
}

// A new turn of the session cancels its planned compaction; the new turn's process plans the
// next one.
func TestANewTurnCancelsThePlannedIdleCompaction(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "idle", largeCall)
	bot := h.newBot(t)
	key := sessionKey{botID: bot.ID}
	if c.turn(t, h, bot.ID, "one") == nil {
		t.Fatal("no idle compaction planned after the first turn")
	}
	first := h.plan(key)
	if c.turn(t, h, bot.ID, "two") == nil {
		t.Fatal("no idle compaction planned after the second turn")
	}
	if got := h.planState(first); got != idleCancelled {
		t.Fatalf("the first plan is %s after a new turn; want cancelled", got)
	}
	if next := h.plan(key); next == nil || next == first {
		t.Fatal("the second turn's plan is not the session's plan")
	}
}

// Clear context in a chat cancels the planned compaction of that chat only.
func TestClearContextCancelsThePlannedIdleCompactionOfItsChat(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "idle", largeCall)
	ctx := context.Background()
	ana := h.newNamedBot(t, "Ana")
	room, err := h.svc.CreateRoom(ctx, "Plan", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if c.turn(t, h, ana.ID, "chat") == nil || c.turn(t, h, room.ID, "room") == nil {
		t.Fatal("no idle compaction planned for the chat and the room")
	}
	chat, seat := h.plan(sessionKey{botID: ana.ID}), h.plan(sessionKey{botID: ana.ID, roomID: room.ID})

	h.runner.CancelIdleCompaction(room.ID)
	if got := h.planState(seat); got != idleCancelled {
		t.Fatalf("the room's plan is %s after its Clear context; want cancelled", got)
	}
	if got := h.planState(chat); got != idlePlanned {
		t.Fatalf("the chat's plan is %s after the room's Clear context; want planned", got)
	}
	h.runner.CancelIdleCompaction(ana.ID)
	if got := h.planState(chat); got != idleCancelled {
		t.Fatalf("the chat's plan is %s after its Clear context; want cancelled", got)
	}
}

// The server's stop cancels a planned compaction.
func TestTheServersStopCancelsThePlannedIdleCompaction(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "idle", largeCall)
	bot := h.newBot(t)
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), bot.ID, "hello", "n1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.waits:
	case <-time.After(10 * time.Second):
		t.Fatal("no idle compaction planned")
	}
	p := h.plan(sessionKey{botID: bot.ID})
	h.cancel()
	<-h.done
	if got := h.planState(p); got != idleCancelled {
		t.Fatalf("the plan is %s after the server stopped; want cancelled", got)
	}
	if heads, _ := h.rawCalls(t); len(heads) != 1 {
		t.Fatalf("claude ran %d times; want only the turn", len(heads))
	}
}

// While its session compacts, the bot counts as working: its next turn waits, and then resumes
// the compacted session.
func TestABotWorksWhileItsSessionCompactsAndThenResumesIt(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "idle", largeCall)
	h.env = append(h.env, "FAKE_HOLD=1")
	ctx := context.Background()
	bot := h.newBot(t)
	w := c.turn(t, h, bot.ID, "one")
	if w == nil {
		t.Fatal("no idle compaction planned")
	}
	c.fire(w, idleStart.Add(54*time.Minute))
	waitFor(t, "the compaction turn to start", func() bool { return slices.Equal(h.compactionTurns(t, bot.ID), []string{"running"}) })
	if !h.runner.Running(bot.ID) {
		t.Fatal("the bot does not count as working while its session compacts")
	}
	next, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, "two", "n-two")
	if err != nil {
		t.Fatal(err)
	}
	h.runner.dispatch(ctx)
	var status string
	if err := h.conn.QueryRowContext(ctx, `SELECT status FROM turns WHERE id = ?`, next).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("the next turn during the compaction is %q, %v; want queued", status, err)
	}

	if err := os.WriteFile(filepath.Join(h.dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if c.settle(t, h) != nil {
		t.Fatal("the compaction planned another compaction")
	}
	h.runner.dispatch(ctx)
	c.settle(t, h)
	got, err := h.svc.GetBot(ctx, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if heads, messages := h.rawCalls(t); len(heads) != 3 || messages[1] != "/compact" || !strings.Contains(heads[2], "--resume "+got.ClaudeSessionID) {
		t.Fatalf("claude ran %d times; want the turn, the compaction, then the next turn resuming %s", len(heads), got.ClaudeSessionID)
	}
}

// A compaction that fails ends its turn failed and logs why.
func TestAFailedIdleCompactionEndsItsTurnFailed(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "idle", largeCall)
	bot := h.newBot(t)
	w := c.turn(t, h, bot.ID, "hello")
	if w == nil {
		t.Fatal("no idle compaction planned")
	}
	h.setMode(t, "error")
	c.fire(w, idleStart.Add(54*time.Minute))
	c.settle(t, h)
	if turns := h.compactionTurns(t, bot.ID); len(turns) != 1 || turns[0] != "failed" {
		t.Fatalf("compact turns = %q; want one, failed", turns)
	}
	if logs := h.logs.String(); !strings.Contains(logs, `msg="idle compaction failed"`) || !strings.Contains(logs, "err=boom") {
		t.Fatalf("log =\n%s\nwant the failed compaction with its error", logs)
	}
	if texts := compactedEntries(t, h, bot.ID); len(texts) != 0 {
		t.Fatalf("compaction notices = %q; want none", texts)
	}
}

// B5: the session knows again the system prompt its last process started with, so the next turn
// brings back the updates that messages delivered in that process.
func TestTheTurnAfterAnIdleCompactionBringsBackTheUpdates(t *testing.T) {
	t.Parallel()
	h, c := newIdleHarness(t, "background", largeCall)
	ctx := context.Background()
	bot := h.newBot(t)
	h.start(t)
	h.startBackgroundTask(t, bot)
	if _, err := h.svc.WriteFact(ctx, bot.ID, "", "agent", "profile", "The user deploys on Mondays."); err != nil {
		t.Fatal(err)
	}
	first := contextBlock(h.sendLive(t, bot.ID, "ping", 1))
	if !strings.Contains(first, "The user deploys on Mondays.") {
		t.Fatalf("the update in the live process = %q; want the Memory section", first)
	}
	h.sendLive(t, bot.ID, "finish", 2)
	var w idleWait
	select {
	case w = <-c.waits:
	case <-time.After(10 * time.Second):
		t.Fatal("no idle compaction planned after the process exited")
	}

	h.setMode(t, "compact")
	c.fire(&w, idleStart.Add(54*time.Minute))
	h.waitEntry(t, bot.ID, func(e core.Entry) bool {
		return e.Kind == core.KindEvent && bodyField(e, "type") == "context-compacted"
	})
	waitFor(t, "the compaction to end", func() bool { return !h.runner.Active(bot.ID) })
	h.setMode(t, "reply")
	msg := h.say(t, bot.ID, "after")
	if again := contextBlock(msg); again != first {
		t.Fatalf("the update after the idle compaction =\n%s\nwant the first one again\n%s", again, first)
	}
	got, err := h.svc.GetBot(ctx, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls := h.argsLog(t); !strings.Contains(calls[len(calls)-1], "--resume "+got.ClaudeSessionID) {
		t.Fatalf("the turn after the compaction does not resume session %s", got.ClaudeSessionID)
	}
}
