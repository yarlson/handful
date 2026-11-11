package move

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"handful/internal/db"
)

// usedHandful is a home that moved, then made 2 chat messages, a memory and
// a routine change in Handful after the move, and one message before it.
func usedHandful(t *testing.T) *home {
	t.Helper()
	h := newHome(t)
	if _, err := h.mover().Data(t.Context()); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.Context(), filepath.Join(h.paths.New, dbFile))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	after := movedAt.UnixMilli() + 1
	for _, stmt := range []string{
		fmt.Sprintf(`INSERT INTO entries (bot_id, seq, rev, id, kind, body, created_at) VALUES ('b1', 2, 2, 'e2', 'user', '{"text":"a"}', %d)`, after),
		fmt.Sprintf(`INSERT INTO entries (bot_id, seq, rev, id, kind, body, created_at) VALUES ('b1', 3, 3, 'e3', 'bot', '{"text":"b"}', %d)`, after),
		fmt.Sprintf(`INSERT INTO entries (bot_id, seq, rev, id, kind, body, created_at) VALUES ('b1', 4, 4, 'e4', 'event', '{"text":"c"}', %d)`, after),
		fmt.Sprintf(`INSERT INTO memories (id, scope_key, tier, fact, learned_by, created_at) VALUES ('m1', 'agent:b1', 'log', 'New.', 'b1', %d)`, after),
		fmt.Sprintf(`INSERT INTO routines (id, bot_id, name, prompt, schedule, enabled, created_at, updated_at) VALUES ('r1', 'b1', 'Daily', 'Sum up.', '@daily', 1, 1, %d)`, after),
	} {
		if _, err := conn.ExecContext(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func lossText(tail string) string {
	return "Going back to YarGPT restores your data as it was on " + movedAt.Format("2006-01-02 15:04") + ".\n" +
		"Lost on the way back: 2 chat messages, 1 memory, 1 routine change made in Handful since then.\n" + tail
}

// Without -yes, going back only says what is lost (spec ad09e72c §4.4, AC15).
func TestGoingBackWithoutYesOnlySaysWhatIsLost(t *testing.T) {
	h := usedHandful(t)
	before := h.snapshot()
	var out bytes.Buffer
	if err := h.mover().Back(t.Context(), false, &out); err != nil {
		t.Fatal(err)
	}
	if want := lossText("They stay in ~/.yargpt/handful-after-move.db. Run again with -yes to go back.\n"); out.String() != want {
		t.Fatalf("output = %q; want %q", out.String(), want)
	}
	if after := h.snapshot(); !maps.Equal(after, before) {
		t.Fatalf("going back without -yes changed:%s", diff(after, before))
	}
}

// With -yes, Handful's database stays as handful-after-move.db, the backup
// is yargpt.db again, and the folder is ~/.yargpt again, with no link; a
// YarGPT release opens that database (AC15).
func TestGoingBackRestoresYarGPTsDataAndKeepsHandfuls(t *testing.T) {
	h := usedHandful(t)
	handful := h.dbRows(filepath.Join(h.paths.New, dbFile))
	var out bytes.Buffer
	if err := h.mover().Back(t.Context(), true, &out); err != nil {
		t.Fatal(err)
	}
	if want := lossText("Done: your data is back in ~/.yargpt. Now open YarGPT v0.13.0.\n"); out.String() != want {
		t.Fatalf("output = %q; want %q", out.String(), want)
	}
	after := h.snapshot()
	if after[".yargpt"] != "folder" || after[".handful"] != "" {
		t.Fatalf(".yargpt %q, .handful %q; want the folder back and no ~/.handful", after[".yargpt"], after[".handful"])
	}
	yargpt := `db version 19, 1 entries, instructions "Use mcp__yargpt__send_to_user."`
	if got := after[".yargpt/yargpt.db"]; got != yargpt {
		t.Errorf("yargpt.db = %q; want YarGPT's data %q", got, yargpt)
	}
	if got := after[".yargpt/handful-after-move.db"]; got != handful {
		t.Errorf("handful-after-move.db = %q; want Handful's data %q", got, handful)
	}
	if got := after[".yargpt/bots/b1/workspace/notes.md"]; got != "Bot notes." {
		t.Errorf("a bot's file = %q; want it back in ~/.yargpt", got)
	}
	out.Reset()
	if err := h.mover().Back(t.Context(), true, &out); err != nil || out.String() != "Your data is already back in ~/.yargpt.\n" {
		t.Fatalf("again: %v, %q; want nothing to do", err, out.String())
	}
	if again := h.snapshot(); !maps.Equal(again, after) {
		t.Fatalf("going back again changed:%s", diff(again, after))
	}
}

// Going back stopped after any of its steps ends as a clean one does when
// it runs again.
func TestGoingBackStoppedAfterAnyStepFinishesWhenItRunsAgain(t *testing.T) {
	clean := usedHandful(t)
	if err := clean.mover().Back(t.Context(), true, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := clean.snapshot()
	var steps int
	for k := 1; ; k++ {
		h := usedHandful(t)
		m := h.mover()
		m.stopAfter = k
		err := m.Back(t.Context(), true, &bytes.Buffer{})
		if err == nil {
			steps = k - 1
			break
		}
		if !errors.Is(err, errStopped) {
			t.Fatalf("stopped after step %d: %v", k, err)
		}
		if err := h.mover().Back(t.Context(), true, &bytes.Buffer{}); err != nil {
			t.Fatalf("the run after step %d: %v", k, err)
		}
		if got := h.snapshot(); !maps.Equal(got, want) {
			t.Fatalf("stopped after step %d, then run again:%s", k, diff(got, want))
		}
	}
	// The database kept, the backup restored, the link removed, the folder renamed.
	if steps != 4 {
		t.Fatalf("going back has %d steps; want 4", steps)
	}
}

// Going back refuses while a server runs, and when there is no move or no
// backup to go back to; each time nothing changes.
func TestGoingBackRefusesWhatItCantDoSafely(t *testing.T) {
	running := usedHandful(t)
	m := running.mover()
	m.Running = func() bool { return true }
	before := running.snapshot()
	if err := m.Back(t.Context(), true, &bytes.Buffer{}); err == nil || err.Error() != "a server is running on 127.0.0.1:7777; quit Handful, then run -move-back again" {
		t.Fatalf("server running: %v; want a refusal", err)
	}
	if after := running.snapshot(); !maps.Equal(after, before) {
		t.Fatalf("server running, changed:%s", diff(after, before))
	}

	noBackup := usedHandful(t)
	if err := os.Remove(filepath.Join(noBackup.paths.New, BackupFile)); err != nil {
		t.Fatal(err)
	}
	if err := noBackup.mover().Back(t.Context(), true, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no copy of YarGPT's data") {
		t.Fatalf("no backup: %v; want a refusal", err)
	}

	fresh := &Mover{Paths: HomePaths(t.TempDir()), Running: func() bool { return false }, Now: time.Now}
	if err := os.MkdirAll(fresh.New, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Back(t.Context(), true, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no record of a move") {
		t.Fatalf("never moved: %v; want a refusal", err)
	}
}

// After going back, a later start of Handful moves the data again: it puts
// the old backup and Handful's old database aside and backs up the data as
// YarGPT left it this time.
func TestMovingAgainAfterGoingBackBacksUpTheDataAsItIsNow(t *testing.T) {
	h := usedHandful(t)
	if err := h.mover().Back(t.Context(), true, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	again := h.mover()
	later := movedAt.Add(time.Hour)
	again.Now = func() time.Time { return later }
	if _, err := again.Data(t.Context()); err != nil {
		t.Fatal(err)
	}
	state := h.snapshot()
	stamp := fmt.Sprint(later.UnixMilli())
	for _, path := range []string{".handful/backup/yargpt-before-handful-" + stamp + ".db", ".handful/handful-after-move-" + stamp + ".db", ".handful/backup/yargpt-before-handful.db"} {
		if state[path] == "" {
			t.Errorf("%s is missing; want it kept", path)
		}
	}
	if state[".handful/handful-after-move.db"] != "" {
		t.Error("handful-after-move.db is still there; want it put aside")
	}
}
