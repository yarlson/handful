package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"yargpt/internal/db"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(context.Background(), filepath.Join(dir, "yargpt.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	svc, err := New(conn, dir, time.UTC)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return svc
}

func newTestBot(t *testing.T, svc *Service) Bot {
	t.Helper()
	bot, err := svc.CreateBot(context.Background(), "Scout", "Research things.")
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}
	return bot
}

func TestCreateBotPreparesWorkspaceAndQueuesFirstRun(t *testing.T) {
	svc := newTestService(t)
	bot := newTestBot(t, svc)

	linkPath := filepath.Join(svc.Workspace(bot.ID), ".claude", "skills")
	link, err := os.Readlink(linkPath)
	if err != nil || filepath.IsAbs(link) || filepath.Join(filepath.Dir(linkPath), link) != svc.SkillsDir() {
		t.Fatalf("skills link = %q, %v; want a relative link to %q", link, err, svc.SkillsDir())
	}
	turns, err := svc.QueuedTurns(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Source != SourceSystem || turns[0].BotID != bot.ID {
		t.Fatalf("queued turns = %+v; want one system turn for bot %s", turns, bot.ID)
	}
}

func TestSubmitUserMessageIsIdempotentByNonce(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)

	first, dup, err := svc.SubmitUserMessage(ctx, bot.ID, "hello", "nonce-1")
	if err != nil || dup {
		t.Fatalf("first submit: turn=%q dup=%v err=%v", first, dup, err)
	}
	second, dup, err := svc.SubmitUserMessage(ctx, bot.ID, "hello", "nonce-1")
	if err != nil || !dup || second != first {
		t.Fatalf("second submit: turn=%q dup=%v err=%v; want turn %q dup=true", second, dup, err, first)
	}
	entries, err := svc.Entries(ctx, bot.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	users := 0
	for _, e := range entries {
		if e.Kind == KindUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user entries = %d; want 1", users)
	}
}

func TestEntryRevIncreasesOnUpdateSoClientsCatchUp(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)

	a, err := svc.AppendEntry(ctx, bot.ID, KindEvent, "", map[string]string{"n": "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AppendEntry(ctx, bot.ID, KindEvent, "", map[string]string{"n": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Seq != a.Seq+1 || b.Rev != a.Rev+1 {
		t.Fatalf("a=(seq %d, rev %d) b=(seq %d, rev %d); want consecutive", a.Seq, a.Rev, b.Seq, b.Rev)
	}
	updated, err := svc.UpdateEntry(ctx, a.ID, map[string]string{"n": "a2"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Seq != a.Seq || updated.Rev != b.Rev+1 {
		t.Fatalf("updated = (seq %d, rev %d); want seq %d rev %d", updated.Seq, updated.Rev, a.Seq, b.Rev+1)
	}
	changed, err := svc.Entries(ctx, bot.ID, b.Rev, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 || changed[0].ID != a.ID || string(changed[0].Body) != `{"n":"a2"}` {
		t.Fatalf("entries after rev %d = %+v; want only the updated entry", b.Rev, changed)
	}
}

func TestReplaceFactSwapsTheOldFactForTheNewOne(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	facts := func() []string {
		t.Helper()
		ms, err := svc.Memories(ctx, bot.ID, "")
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range ms {
			out = append(out, m.Tier+": "+m.Fact)
		}
		sort.Strings(out)
		return out
	}
	if _, err := svc.WriteFact(ctx, bot.ID, "", ScopeAgent, TierProfile, "Post GIFs as links."); err != nil {
		t.Fatal(err)
	}

	if err := svc.ReplaceFact(ctx, bot.ID, "", ScopeAgent, TierProfile, "Post GIFs  as links.", "Post GIFs as files, never as links."); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got, want := strings.Join(facts(), " | "), "profile: Post GIFs as files, never as links."; got != want {
		t.Fatalf("facts after replace = %q; want %q", got, want)
	}

	err := svc.ReplaceFact(ctx, bot.ID, "", ScopeAgent, TierProfile, "No such rule.", "A new rule.")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("replace of a missing fact: err = %v; want ErrInvalid", err)
	}
	if got, want := strings.Join(facts(), " | "), "profile: Post GIFs as files, never as links."; got != want {
		t.Fatalf("facts after a failed replace = %q; want %q unchanged", got, want)
	}
}

func TestFactsRecordWhetherTheUserOrABotWroteThem(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	if _, err := svc.WriteFact(ctx, bot.ID, "", ScopeUser, TierProfile, "Post GIFs in Random."); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddUserFact(ctx, bot.ID, ScopeAgent, TierProfile, "Ask before you merge."); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddUserFact(ctx, bot.ID, ScopeUser, TierProfile, "Post GIFs  in Random."); err != nil {
		t.Fatalf("user confirms a bot's fact: %v", err)
	}
	ms, err := svc.Memories(ctx, bot.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range ms {
		got[m.Fact] = m.LearnedBy
	}
	want := map[string]string{"Ask before you merge.": LearnedByUser, "Post GIFs in Random.": LearnedByUser}
	if len(got) != len(want) || got["Ask before you merge."] != want["Ask before you merge."] || got["Post GIFs in Random."] != want["Post GIFs in Random."] {
		t.Fatalf("facts by author = %v; want %v (a fact the user adds is the user's, also when a bot wrote it first)", got, want)
	}
	if _, err := svc.WriteFact(ctx, bot.ID, "", ScopeAgent, TierLog, "Bot note."); err != nil {
		t.Fatal(err)
	}
	ms, _ = svc.Memories(ctx, bot.ID, "")
	for _, m := range ms {
		if m.Fact == "Bot note." && m.LearnedBy != bot.ID {
			t.Fatalf("bot fact learned by %q; want the bot id %s", m.LearnedBy, bot.ID)
		}
	}
	if err := svc.AddUserFact(ctx, "no-such-bot", ScopeAgent, TierProfile, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("user fact for a missing bot: err = %v; want ErrNotFound", err)
	}
}

func TestMemoryWriteDedupesRecallRanksAndForget(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)

	for _, f := range []struct{ scope, tier, fact string }{
		{ScopeUser, TierProfile, "The user lives in Lisbon."},
		{ScopeAgent, TierLog, "The user asked for a flight to Lisbon in March."},
		{ScopeAgent, TierLog, "Ticket ABC-123 is blocked."},
	} {
		if created, err := svc.WriteFact(ctx, bot.ID, "", f.scope, f.tier, f.fact); err != nil || !created {
			t.Fatalf("write %q: created=%v err=%v", f.fact, created, err)
		}
	}
	if created, err := svc.WriteFact(ctx, bot.ID, "", ScopeUser, TierProfile, "The  user lives in Lisbon."); err != nil || created {
		t.Fatalf("duplicate write: created=%v err=%v; want created=false", created, err)
	}

	got, err := svc.Recall(ctx, bot.ID, "", "flight Lisbon", "all", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Fact != "The user asked for a flight to Lisbon in March." {
		t.Fatalf("recall flight Lisbon = %+v; want the flight fact first, then the home fact", got)
	}
	got, err = svc.Recall(ctx, bot.ID, "", "BC-12", "agent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Fact != "Ticket ABC-123 is blocked." {
		t.Fatalf("substring recall = %+v; want the ticket fact", got)
	}
	got, err = svc.Recall(ctx, bot.ID, "", "Lisbon", "user", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Scope != ScopeUser {
		t.Fatalf("user-scope recall = %+v; want only the shared fact", got)
	}

	if ok, err := svc.ForgetFact(ctx, bot.ID, "", ScopeAgent, "Ticket ABC-123 is blocked."); err != nil || !ok {
		t.Fatalf("forget: ok=%v err=%v", ok, err)
	}
	if got, _ := svc.Recall(ctx, bot.ID, "", "ticket", "all", 10); len(got) != 0 {
		t.Fatalf("recall after forget = %+v; want none", got)
	}
}

func TestMemorySharedUserScopeIsVisibleToOtherBots(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	a := newTestBot(t, svc)
	b := newTestBot(t, svc)
	if _, err := svc.WriteFact(ctx, a.ID, "", ScopeUser, TierProfile, "The user's name is Ana."); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteFact(ctx, a.ID, "", ScopeAgent, TierLog, "Only bot A knows this."); err != nil {
		t.Fatal(err)
	}
	profile, recent, err := svc.PromptMemory(ctx, b.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(profile) != 1 || profile[0].Fact != "The user's name is Ana." || len(recent) != 0 {
		t.Fatalf("bot B memory: profile=%+v recent=%+v; want only the shared profile fact", profile, recent)
	}
}

func TestNotesFadeAfterTheirLifetime(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	if _, err := svc.WriteFact(ctx, bot.ID, "", ScopeAgent, TierNote, "Short-lived context."); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return base.Add(8 * 24 * time.Hour) }
	if got, err := svc.Memories(ctx, bot.ID, ""); err != nil || len(got) != 0 {
		t.Fatalf("memories after 8 days = %+v, %v; want none", got, err)
	}
}

func TestRoutineScheduleValidation(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	for _, bad := range []string{"* * * * *", "*/2 * * * *", "@every 1m", "not a cron"} {
		if _, err := svc.CreateRoutine(ctx, bot.ID, "x", "do it", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("schedule %q: err = %v; want ErrInvalid", bad, err)
		}
	}
	for _, good := range []string{"*/5 * * * *", "17 8 * * 1-5", "@daily", "@every 2h"} {
		if _, err := svc.CreateRoutine(ctx, bot.ID, "x", "do it", good); err != nil {
			t.Errorf("schedule %q: %v; want valid", good, err)
		}
	}
}

func TestRunRoutineNowNamesWhoRanItAndRecordsTheRun(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	at := time.Date(2026, 11, 6, 10, 15, 0, 0, time.UTC)
	svc.now = func() time.Time { return at }
	bot := newTestBot(t, svc)
	r, err := svc.CreateRoutine(ctx, bot.ID, "Joke", "Tell a joke.", "0 8 * * *")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ by, want string }{
		{"Scout", "was run on demand by Scout"},
		{"", "was run on demand by the user"},
	} {
		turnID, err := svc.RunRoutineNow(ctx, r.ID, tc.by)
		if err != nil {
			t.Fatal(err)
		}
		turns, err := svc.QueuedTurns(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		prompt := ""
		for _, tr := range turns {
			if tr.ID == turnID {
				prompt = tr.Prompt
			}
		}
		if !strings.Contains(prompt, tc.want) {
			t.Errorf("run by %q: cue = %q; want %q", tc.by, prompt, tc.want)
		}
	}
	got, err := svc.GetRoutine(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastRunAt == nil || *got.LastRunAt != at.UnixMilli() {
		t.Errorf("last run after run-now = %v; want %d", got.LastRunAt, at.UnixMilli())
	}
	if got.NextRunAt == nil || *r.NextRunAt != *got.NextRunAt {
		t.Errorf("next run after run-now = %v; want it unchanged at %v", got.NextRunAt, r.NextRunAt)
	}
}

func TestUserEditsOfARoutineNoticeItsBotButTheBotsOwnEditsDoNot(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	r, err := svc.CreateRoutine(ctx, bot.ID, "Joke", "Tell a joke.", "0 8 * * *")
	if err != nil {
		t.Fatal(err)
	}
	notices := func() []string {
		t.Helper()
		entries, err := svc.Entries(ctx, bot.ID, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, e := range entries {
			var body struct{ Type, Text string }
			_ = json.Unmarshal(e.Body, &body)
			if body.Type == "routine-updated" || body.Type == "routine-deleted" {
				out = append(out, body.Text)
			}
		}
		return out
	}

	prompt, schedule := "Tell a joke and post a GIF.", "0 9 * * *"
	if _, err := svc.UpdateRoutine(ctx, r.ID, RoutinePatch{Prompt: &prompt, Schedule: &schedule}, bot.Name); err != nil {
		t.Fatal(err)
	}
	if got := notices(); len(got) != 0 {
		t.Fatalf("notices after the bot's own edit = %q; want none", got)
	}

	prompt = "Tell a short joke."
	if _, err := svc.UpdateRoutine(ctx, r.ID, RoutinePatch{Prompt: &prompt, Schedule: &schedule}, ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteRoutine(ctx, r.ID, ""); err != nil {
		t.Fatal(err)
	}
	want := []string{`The user changed routine "Joke": prompt.`, `The user deleted routine "Joke".`}
	if got := notices(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chat notices = %q; want %q (only changed fields are named)", got, want)
	}
	inbox, err := svc.Inbox(ctx, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 2 || inbox[0].FromID != LearnedByUser || inbox[0].Text != want[0] || inbox[1].Text != want[1] {
		t.Fatalf("inbox = %+v; want both notices from the user, for the bot's next turn", inbox)
	}
}

func TestRoutineRecordsWhoRanItLast(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	start := time.Date(2026, 11, 6, 7, 59, 0, 0, time.UTC)
	svc.now = func() time.Time { return start }
	bot := newTestBot(t, svc)
	r, err := svc.CreateRoutine(ctx, bot.ID, "Brief", "Brief me.", "0 8 * * *")
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		run  func() error
		want string
	}{
		{func() error { _, err := svc.RunRoutineNow(ctx, r.ID, "Scout"); return err }, `bot "Scout"`},
		{func() error { _, err := svc.RunRoutineNow(ctx, r.ID, ""); return err }, "the user"},
		{func() error {
			svc.now = func() time.Time { return start.Add(2 * time.Minute) }
			_, err := svc.FireDueRoutines(ctx)
			return err
		}, "the schedule"},
	} {
		if err := step.run(); err != nil {
			t.Fatal(err)
		}
		got, err := svc.GetRoutine(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.LastRunBy != step.want {
			t.Fatalf("last run by %q; want %q", got.LastRunBy, step.want)
		}
	}
}

func TestRoutineKeepsItsNextRunAcrossASpringForwardChange(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	svc.loc = ny
	// Accepted in January: 01:58 and 03:00 are an hour apart on most days.
	svc.now = func() time.Time { return time.Date(2027, 1, 10, 12, 0, 0, 0, ny) }
	bot := newTestBot(t, svc)
	r, err := svc.CreateRoutine(ctx, bot.ID, "Night", "Check the backups.", "0,58 1,3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	// On March 14, 2027 clocks go from 02:00 EST to 03:00 EDT, so the run
	// after 01:58 comes 2 real minutes later. The 01:00 run fires first.
	at := time.Date(2027, 3, 14, 1, 0, 30, 0, ny)
	svc.now = func() time.Time { return at }
	if _, err := svc.sqlDB.ExecContext(ctx, `UPDATE routines SET next_run_at = ? WHERE id = ?`, at.Add(-30*time.Second).UnixMilli(), r.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.FireDueRoutines(ctx); err != nil || n != 1 {
		t.Fatalf("fire: n=%d err=%v; want 1", n, err)
	}
	got, err := svc.GetRoutine(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2027, 3, 14, 1, 58, 0, 0, ny).UnixMilli(); got.NextRunAt == nil || *got.NextRunAt != want {
		t.Fatalf("next run after the 01:00 run = %v; want 01:58 EST (%d)", got.NextRunAt, want)
	}
}

func TestFireDueRoutinesQueuesTurnAndAdvancesNextRun(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	start := time.Date(2026, 11, 6, 7, 59, 0, 0, time.UTC)
	svc.now = func() time.Time { return start }
	bot := newTestBot(t, svc)
	r, err := svc.CreateRoutine(ctx, bot.ID, "Morning brief", "Summarize the news.", "0 8 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC).UnixMilli(); r.NextRunAt == nil || *r.NextRunAt != want {
		t.Fatalf("next run = %v; want %d", r.NextRunAt, want)
	}

	svc.now = func() time.Time { return start.Add(2 * time.Minute) }
	n, err := svc.FireDueRoutines(ctx)
	if err != nil || n != 1 {
		t.Fatalf("fire: n=%d err=%v; want 1", n, err)
	}
	again, err := svc.FireDueRoutines(ctx)
	if err != nil || again != 0 {
		t.Fatalf("second fire: n=%d err=%v; want 0", again, err)
	}
	got, err := svc.GetRoutine(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC).UnixMilli(); got.NextRunAt == nil || *got.NextRunAt != want {
		t.Fatalf("next run after fire = %v; want %d", got.NextRunAt, want)
	}
	turns, err := svc.QueuedTurns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	routineTurns := 0
	for _, tr := range turns {
		if tr.Source == SourceRoutine {
			routineTurns++
		}
	}
	if routineTurns != 1 {
		t.Fatalf("routine turns = %d; want 1", routineTurns)
	}
}

func TestSkillsWriteListGetDelete(t *testing.T) {
	svc := newTestService(t)
	sk, err := svc.WriteSkill("Daily Standup!", `Use when the user asks for a "standup".`, "# Steps\n1. Collect updates.")
	if err != nil {
		t.Fatal(err)
	}
	if sk.ID != "daily-standup" {
		t.Fatalf("id = %q; want daily-standup", sk.ID)
	}
	got, err := svc.GetSkill("daily-standup")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != `Use when the user asks for a "standup".` || got.Body != "# Steps\n1. Collect updates.\n" {
		t.Fatalf("skill = %+v", got)
	}
	list, err := svc.ListSkills()
	if err != nil || len(list) != 1 || list[0].Body != "" {
		t.Fatalf("list = %+v, %v; want one skill without body", list, err)
	}
	if _, err := svc.GetSkill("../escape"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("path traversal id: err = %v; want ErrInvalid", err)
	}
	if err := svc.DeleteSkill("daily-standup"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetSkill("daily-standup"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: err = %v; want ErrNotFound", err)
	}
}

func TestApprovalAlwaysCreatesGrantThatSkipsTheNextCard(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	events, stop := svc.Hub().Subscribe()
	defer stop()

	type result struct {
		ok  bool
		err error
	}
	done := make(chan result, 1)
	go func() {
		ok, _, err := svc.RequestApproval(ctx, ApprovalRequest{BotID: bot.ID, TranscriptID: bot.ID, TurnID: "turn-1", Tool: "Bash", Input: json.RawMessage(`{"command":"ls"}`)})
		done <- result{ok, err}
	}()
	var cardID string
	for cardID == "" {
		e := <-events
		if e.Type == "entry" && e.Entry.Kind == KindApproval {
			cardID = e.Entry.ID
		}
	}
	if err := svc.ResolveApproval(ctx, cardID, DecisionAlways); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.err != nil || !r.ok {
		t.Fatalf("approval result = %+v; want allowed", r)
	}
	if err := svc.ResolveApproval(ctx, cardID, DecisionDeny); !errors.Is(err, ErrApprovalClosed) {
		t.Fatalf("second resolve: err = %v; want ErrApprovalClosed", err)
	}
	ok, _, err := svc.RequestApproval(ctx, ApprovalRequest{BotID: bot.ID, TranscriptID: bot.ID, TurnID: "turn-2", Tool: "Bash", Input: json.RawMessage(`{}`)})
	if err != nil || !ok {
		t.Fatalf("granted tool: ok=%v err=%v; want allowed without a card", ok, err)
	}
}

func TestApprovalDenyAndCancel(t *testing.T) {
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	events, stop := svc.Hub().Subscribe()
	defer stop()

	done := make(chan bool, 1)
	go func() {
		ok, _, _ := svc.RequestApproval(ctx, ApprovalRequest{BotID: bot.ID, TranscriptID: bot.ID, TurnID: "t", Tool: "Write", Input: json.RawMessage(`{}`)})
		done <- ok
	}()
	for e := range events {
		if e.Type == "entry" && e.Entry.Kind == KindApproval {
			break
		}
	}
	cancel()
	if <-done {
		t.Fatal("cancelled approval returned allowed; want denied")
	}
	entries, err := svc.Entries(context.Background(), bot.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	last := entries[len(entries)-1]
	var body approvalBody
	if err := json.Unmarshal(last.Body, &body); err != nil || body.Status != "expired" {
		t.Fatalf("approval card = %s; want status expired", last.Body)
	}
}

func TestExpireTurnApprovalsEndsPendingCardsOfThatTurnOnly(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	events, stop := svc.Hub().Subscribe()
	defer stop()

	results := make(chan string, 2)
	for _, turn := range []string{"stopped-turn", "live-turn"} {
		go func(turn string) {
			ok, _, err := svc.RequestApproval(ctx, ApprovalRequest{BotID: bot.ID, TranscriptID: bot.ID, TurnID: turn, Tool: "Bash", Input: json.RawMessage(`{}`)})
			if err != nil {
				results <- "error"
				return
			}
			if ok {
				results <- turn + ":allowed"
			} else {
				results <- turn + ":denied"
			}
		}(turn)
	}
	cards := map[string]string{} // turn id -> card id
	for len(cards) < 2 {
		select {
		case e := <-events:
			if e.Type == "entry" && e.Entry.Kind == KindApproval {
				cards[e.Entry.TurnID] = e.Entry.ID
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("got approval cards %v after 5s; want 2", cards)
		}
	}
	svc.ExpireTurnApprovals("stopped-turn")
	if got := receive(t, results); got != "stopped-turn:denied" {
		t.Fatalf("first result = %q; want the stopped turn denied", got)
	}
	if err := svc.ResolveApproval(ctx, cards["live-turn"], DecisionOnce); err != nil {
		t.Fatalf("live turn's card should still be pending: %v", err)
	}
	if got := receive(t, results); got != "live-turn:allowed" {
		t.Fatalf("second result = %q; want the live turn allowed", got)
	}
	card, err := svc.GetEntry(ctx, cards["stopped-turn"])
	if err != nil {
		t.Fatal(err)
	}
	var body approvalBody
	if err := json.Unmarshal(card.Body, &body); err != nil || body.Status != "expired" {
		t.Fatalf("stopped turn's card = %s; want status expired", card.Body)
	}
}

func receive(t *testing.T, results <-chan string) string {
	t.Helper()
	select {
	case got := <-results:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no approval result after 5s")
		return ""
	}
}

func TestExpiringATurnWhileItsApprovalCardIsAddedEndsTheRequest(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	// The card's timestamp is read while the card is being added, before
	// anyone can see it: the turn stops at that moment.
	var expireOnce sync.Once
	svc.now = func() time.Time {
		expireOnce.Do(func() { svc.ExpireTurnApprovals("t1") })
		return time.Now()
	}

	done := make(chan bool, 1)
	go func() {
		ok, _, err := svc.RequestApproval(ctx, ApprovalRequest{BotID: bot.ID, TranscriptID: bot.ID, TurnID: "t1", Tool: "Bash", Input: json.RawMessage(`{}`)})
		done <- err == nil && !ok
	}()
	select {
	case denied := <-done:
		if !denied {
			t.Fatal("RequestApproval of the stopped turn did not return a denial")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RequestApproval still waits 5s after its turn was expired while the card was added")
	}
}

func TestRecoverAfterRestartClosesOpenCardsAndToolCalls(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	bot := newTestBot(t, svc)
	card, err := svc.AppendEntry(ctx, bot.ID, KindApproval, "t1", approvalBody{Tool: "Bash", Input: json.RawMessage(`{}`), Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	call, err := svc.AppendEntry(ctx, bot.ID, KindActivity, "t1", map[string]any{"tool": "Bash", "status": "running"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := svc.AppendEntry(ctx, bot.ID, KindActivity, "t1", map[string]any{"tool": "Read", "status": "done"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecoverAfterRestart(ctx); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{card.ID: "expired", call.ID: "error", done.ID: "done"} {
		e, err := svc.GetEntry(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		_ = json.Unmarshal(e.Body, &body)
		if body["status"] != want {
			t.Errorf("entry %s status = %v; want %s", e.Kind, body["status"], want)
		}
	}
}

func TestWriteAndReplaceFactRejectBadTiersAndEmptyFacts(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	bot := newTestBot(t, svc)
	if _, err := svc.WriteFact(ctx, bot.ID, "", ScopeAgent, "", "A rule."); err != nil {
		t.Fatal(err)
	}
	if ms, _ := svc.Memories(ctx, bot.ID, ""); len(ms) != 1 || ms[0].Tier != TierLog {
		t.Fatalf("facts = %+v; want one fact in the default tier %q", ms, TierLog)
	}
	if _, err := svc.WriteFact(ctx, bot.ID, "", ScopeAgent, "forever", "Another rule."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("WriteFact with tier forever: err = %v; want ErrInvalid", err)
	}
	for name, args := range map[string][3]string{
		"an unknown tier": {"forever", "A rule.", "A new rule."},
		"an empty old":    {"", " ", "A new rule."},
		"an empty fact":   {"", "A rule.", " "},
		"a missing old":   {"", "No such rule.", "A new rule."},
	} {
		if err := svc.ReplaceFact(ctx, bot.ID, "", ScopeAgent, args[0], args[1], args[2]); !errors.Is(err, ErrInvalid) {
			t.Errorf("ReplaceFact with %s: err = %v; want ErrInvalid", name, err)
		}
	}
	if err := svc.ReplaceFact(ctx, bot.ID, "", ScopeAgent, "", "A rule.", "A new rule."); err != nil {
		t.Fatal(err)
	}
	if ms, _ := svc.Memories(ctx, bot.ID, ""); len(ms) != 1 || ms[0].Fact != "A new rule." || ms[0].Tier != TierLog {
		t.Fatalf("facts after the replace = %+v; want only the new fact in tier %q", ms, TierLog)
	}
}
