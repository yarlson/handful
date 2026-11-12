package move

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"handful/internal/db"
	"handful/internal/db/dbtest"
)

var movedAt = time.UnixMilli(1_800_000_000_000)

// home is a home folder with YarGPT's data: a database as YarGPT left it,
// a bot's and a team's workspace with files, and their Claude Code sessions.
type home struct {
	t     *testing.T
	dir   string
	paths Paths
}

func newHome(t *testing.T) *home {
	t.Helper()
	dir := t.TempDir()
	h := &home{t: t, dir: dir, paths: HomePaths(dir)}
	for path, text := range map[string]string{
		"bots/b1/workspace/notes.md":  "Bot notes.",
		"teams/t1/workspace/plan.md":  "Team plan.",
		"skills/review/SKILL.md":      "Review.",
		"media/m1.png":                "png",
		"config.json":                 `{"maxParallel": 4}`,
		"bots/b2/workspace/.keep":     "",
		"bots/b1/agent-memory.md":     "kept",
		"teams/t1/workspace/sub/a.md": "deep",
	} {
		h.write(filepath.Join(h.paths.Old, path), text)
	}
	h.yargptDB(filepath.Join(h.paths.Old, dbFile))
	for _, rel := range []string{"bots/b1/workspace", "teams/t1/workspace"} {
		key, _ := sessionKey(filepath.Join(h.paths.Old, rel))
		h.write(filepath.Join(h.paths.Projects, key, "s1.jsonl"), "session of "+rel)
		h.write(filepath.Join(h.paths.Projects, key, "s1", "subagents", "a.jsonl"), "subagent of "+rel)
		if err := os.Symlink("s1.jsonl", filepath.Join(h.paths.Projects, key, "latest.jsonl")); err != nil {
			t.Fatal(err)
		}
	}
	h.write(filepath.Join(h.paths.Projects, "-other-project", "x.jsonl"), "another folder's session")
	return h
}

func (h *home) write(path, text string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// yargptDB creates a database with Handful's tables at YarGPT's last
// version, as the bridge left it, with a bot and a message.
func (h *home) yargptDB(path string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		h.t.Fatal(err)
	}
	conn, err := dbtest.Open(h.t.Context(), path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer conn.Close()
	for _, stmt := range []string{
		`INSERT INTO bots (id, name, instructions, created_at, updated_at) VALUES ('b1', 'Dev', 'Use mcp__yargpt__send_to_user.', 1, 1)`,
		`INSERT INTO entries (bot_id, seq, rev, id, kind, body, created_at) VALUES ('b1', 1, 1, 'e1', 'user', '{"text":"hi"}', 1)`,
		fmt.Sprintf(`PRAGMA user_version = %d`, db.LastYarGPTSchema),
	} {
		if _, err := conn.ExecContext(h.t.Context(), stmt); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *home) mover() *Mover {
	return &Mover{Paths: h.paths, Running: func() string { return "" }, Now: func() time.Time { return movedAt }, Version: "0.14.0"}
}

// snapshot describes everything under the home folder: each file's text
// (for a database, its version and rows), each link's target and each
// folder, by path.
func (h *home) snapshot() map[string]string {
	h.t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(h.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(h.dir, path)
		homeKey, _ := sessionKey(h.dir)
		rel = strings.ReplaceAll(rel, homeKey, "<home>")
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			out[rel] = "-> " + strings.Replace(target, h.dir, "<home>", 1)
			return err
		case strings.HasSuffix(path, "-wal") || strings.HasSuffix(path, "-shm"):
			// Reading a database leaves these; its rows are what counts.
		case d.IsDir():
			out[rel] = "folder"
		case strings.HasSuffix(path, ".db"):
			out[rel] = h.dbRows(path)
		default:
			data, err := os.ReadFile(path)
			out[rel] = string(data)
			return err
		}
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

// dbRows describes a database by its version and its bots' instructions,
// read without changing it.
func (h *home) dbRows(path string) string {
	h.t.Helper()
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		h.t.Fatal(err)
	}
	defer conn.Close()
	var version, entries int
	var instructions string
	if err := conn.QueryRowContext(h.t.Context(), "SELECT (SELECT user_version FROM pragma_user_version), (SELECT count(*) FROM entries), (SELECT group_concat(instructions) FROM bots)").Scan(&version, &entries, &instructions); err != nil {
		h.t.Fatalf("read %s: %v", path, err)
	}
	return fmt.Sprintf("db version %d, %d entries, instructions %q", version, entries, instructions)
}

// movedHome is the state after a clean move: what every test of the move
// compares with.
func movedHome(t *testing.T) map[string]string {
	t.Helper()
	h := newHome(t)
	if _, err := h.mover().Data(t.Context()); err != nil {
		t.Fatal(err)
	}
	return h.snapshot()
}

// diff lists the paths whose state differs between got and want.
func diff(got, want map[string]string) string {
	var b strings.Builder
	for path, w := range want {
		if g, ok := got[path]; !ok || g != w {
			fmt.Fprintf(&b, "\n  %s: got %q, want %q", path, g, w)
		}
	}
	for path, g := range got {
		if _, ok := want[path]; !ok {
			fmt.Fprintf(&b, "\n  %s: got %q, want none", path, g)
		}
	}
	return b.String()
}

// The move renames the folder, links the old name to it, records the move,
// backs up the database as YarGPT left it, and copies the sessions to their
// new keys, keeping the originals (spec ad09e72c §4.3, AC3).
func TestTheMoveTakesYarGPTsFolderToHandfulsAndLinksTheOldName(t *testing.T) {
	h := newHome(t)
	before := h.snapshot()
	linked, err := h.mover().Data(t.Context())
	if err != nil || !linked {
		t.Fatalf("Data = %v, %v; want the link created", linked, err)
	}
	after := h.snapshot()
	if after[".yargpt"] != "-> <home>/.handful" || after[".handful"] != "folder" {
		t.Fatalf(".yargpt %q, .handful %q; want a link to the new folder", after[".yargpt"], after[".handful"])
	}
	for path, v := range before {
		if rest, ok := strings.CutPrefix(path, ".yargpt/"); ok {
			if got := after[".handful/"+rest]; got != v {
				t.Errorf("%s after the move = %q; want %q", rest, got, v)
			}
		}
	}
	yargptDB := `db version 19, 1 entries, instructions "Use mcp__yargpt__send_to_user."`
	if got := after[".handful/backup/yargpt-before-handful.db"]; got != yargptDB {
		t.Errorf("backup = %q; want %q", got, yargptDB)
	}
	mv, ok, err := ReadMoved(h.paths.New)
	if err != nil || !ok || mv != (Moved{MovedAt: movedAt.UnixMilli(), Version: "0.14.0", YarGPTSchema: db.LastYarGPTSchema}) {
		t.Errorf("moved record = %+v, %v, %v; want the time and the version", mv, ok, err)
	}
	for _, rel := range []string{"bots/b1/workspace", "teams/t1/workspace"} {
		homeKey, _ := sessionKey(h.dir)
		oldKey, _ := sessionKey(filepath.Join(h.paths.Old, rel))
		newKey, _ := sessionKey(filepath.Join(h.paths.New, rel))
		oldKey, newKey = strings.Replace(oldKey, homeKey, "<home>", 1), strings.Replace(newKey, homeKey, "<home>", 1)
		for _, file := range []string{"s1.jsonl", "s1/subagents/a.jsonl", "latest.jsonl"} {
			from, to := filepath.Join(".claude/projects", oldKey, file), filepath.Join(".claude/projects", newKey, file)
			if after[to] == "" || after[to] != before[from] || after[from] != before[from] {
				t.Errorf("session %s: copy %q, original %q; want both %q", file, after[to], after[from], before[from])
			}
		}
	}
	if after[".claude/projects/-other-project/x.jsonl"] != "another folder's session" {
		t.Error("another folder's sessions changed")
	}

	// Without the link the next start makes it again, and changes nothing else.
	if err := os.Remove(h.paths.Old); err != nil {
		t.Fatal(err)
	}
	if linked, err := h.mover().Data(t.Context()); err != nil || !linked {
		t.Fatalf("start without the link: %v, %v; want it linked again", linked, err)
	}
	if again := h.snapshot(); !maps.Equal(again, after) {
		t.Fatalf("after linking again:%s", diff(again, after))
	}
	if linked, err := h.mover().Data(t.Context()); err != nil || linked {
		t.Fatalf("a start after the move: %v, %v; want nothing to do", linked, err)
	}
}

// A move stopped after any of its steps, for example between the rename
// and the backup, ends as a clean move does on the next start (AC4).
func TestAMoveStoppedAfterAnyStepFinishesOnTheNextStart(t *testing.T) {
	want := movedHome(t)
	var steps int
	for k := 1; ; k++ {
		h := newHome(t)
		m := h.mover()
		m.stopAfter = k
		_, err := m.Data(t.Context())
		if err == nil {
			steps = k - 1
			break
		}
		if !errors.Is(err, errStopped) {
			t.Fatalf("stopped after step %d: %v", k, err)
		}
		if _, err := h.mover().Data(t.Context()); err != nil {
			t.Fatalf("the start after step %d: %v", k, err)
		}
		if got := h.snapshot(); !maps.Equal(got, want) {
			t.Fatalf("stopped after step %d, then started:%s", k, diff(got, want))
		}
	}
	// 2 session copies of 2 steps each, the record, the rename and the link.
	if steps != 7 {
		t.Fatalf("a move has %d steps; want 7", steps)
	}
}

// A backup that fails stops the start before any migration, with the
// spec's words.
func TestAMoveWhoseBackupFailsSaysSoAndStops(t *testing.T) {
	h := newHome(t)
	h.write(filepath.Join(h.paths.Old, "backup"), "a file where the folder goes")
	_, err := h.mover().Data(t.Context())
	var be *BackupError
	if !errors.As(err, &be) || !strings.HasPrefix(err.Error(), "Couldn't back up your data before moving it: ") || !strings.HasSuffix(err.Error(), ". Free some space and start again.") {
		t.Fatalf("Data = %v; want the backup error", err)
	}
}

// With both folders, or with a YarGPT server running, the move touches
// neither folder (AC5, AC6).
func TestTheMoveLeavesBothFoldersAloneWhenItCantTellOrYarGPTRuns(t *testing.T) {
	both := newHome(t)
	both.write(filepath.Join(both.paths.New, "notes.md"), "Handful's own")
	before := both.snapshot()
	if _, err := both.mover().Data(t.Context()); !errors.Is(err, ErrBothFolders) || err.Error() != "both ~/.yargpt and ~/.handful exist; keep one as ~/.handful and rename the other, then start again" {
		t.Fatalf("both folders: %v; want ErrBothFolders", err)
	}
	if after := both.snapshot(); !maps.Equal(after, before) {
		t.Fatalf("both folders changed:%s", diff(after, before))
	}

	running := newHome(t)
	before = running.snapshot()
	m := running.mover()
	m.Running = func() string { return "127.0.0.1:7777" }
	if _, err := m.Data(t.Context()); err == nil || err.Error() != "a YarGPT server is running on 127.0.0.1:7777; stop it, then start handful again" {
		t.Fatalf("YarGPT running: %v; want the S5 line", err)
	}
	if after := running.snapshot(); !maps.Equal(after, before) {
		t.Fatalf("YarGPT running, the folders changed:%s", diff(after, before))
	}
}

// A fresh install has nothing to move or link, before and after its first
// start makes the folder.
func TestAFreshInstallMovesAndLinksNothing(t *testing.T) {
	dir := t.TempDir()
	m := &Mover{Paths: HomePaths(dir), Running: func() string { return "127.0.0.1:7777" }, Now: time.Now}
	for range 2 {
		if linked, err := m.Data(t.Context()); err != nil || linked {
			t.Fatalf("Data = %v, %v; want nothing done", linked, err)
		}
		if err := os.MkdirAll(m.New, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(m.Old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("~/.yargpt: %v; want none", err)
	}
}

// Claude Code's key of a folder: every character that isn't a letter or a
// digit is "-", and a key over 200 characters isn't copied.
func TestTheSessionKeyFollowsClaudeCodesRule(t *testing.T) {
	if key, ok := sessionKey("/Users/ana b/.yargpt/bots/b_1/workspace"); !ok || key != "-Users-ana-b--yargpt-bots-b-1-workspace" {
		t.Fatalf("key = %q, %v", key, ok)
	}
	long := "/" + strings.Repeat("a", 199)
	if _, ok := sessionKey(long); !ok {
		t.Fatal("a 200-character key: want it kept")
	}
	if _, ok := sessionKey(long + "b"); ok {
		t.Fatal("a 201-character key: want it skipped")
	}
}

// A session that already has its new key keeps it.
func TestTheMoveKeepsASessionThatAlreadyHasItsNewKey(t *testing.T) {
	h := newHome(t)
	newKey, _ := sessionKey(filepath.Join(h.paths.New, "bots/b1/workspace"))
	h.write(filepath.Join(h.paths.Projects, newKey, "s1.jsonl"), "newer")
	if _, err := h.mover().Data(t.Context()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(h.paths.Projects, newKey, "s1.jsonl")); !bytes.Equal(data, []byte("newer")) {
		t.Fatalf("session = %q; want the newer one kept", data)
	}
}

func TestCloseNoticeRecordsItAndKeepsTheMove(t *testing.T) {
	h := newHome(t)
	if err := CloseNotice(h.paths.Old); err != nil {
		t.Fatalf("no move: %v; want nothing to do", err)
	}
	if _, err := h.mover().Data(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := CloseNotice(h.paths.New); err != nil {
		t.Fatal(err)
	}
	if mv, ok, err := ReadMoved(h.paths.New); err != nil || !ok || mv != (Moved{MovedAt: movedAt.UnixMilli(), Version: "0.14.0", YarGPTSchema: db.LastYarGPTSchema, NoticeDone: true}) {
		t.Fatalf("moved record = %+v, %v, %v; want the notice closed", mv, ok, err)
	}
}

// Skills that name an old tool name the renamed one; other files stay.
func TestRenameSkillToolsRewritesOnlyTheOldToolNamesInSkills(t *testing.T) {
	dir := t.TempDir()
	if err := RenameSkillTools(dir); err != nil {
		t.Fatalf("no skills folder: %v", err)
	}
	h := &home{t: t, dir: dir}
	files := map[string]string{
		"skills/review/SKILL.md":   "Post with mcp__yargpt__send_to_user, then mcp__yargpt__update_board.",
		"skills/review/notes.md":   "Plain.",
		"skills/review/script.sh":  "echo mcp__yargpt__x",
		"bots/b1/workspace/one.md": "mcp__yargpt__x",
	}
	for path, text := range files {
		h.write(filepath.Join(dir, path), text)
	}
	for range 2 {
		if err := RenameSkillTools(dir); err != nil {
			t.Fatal(err)
		}
	}
	files["skills/review/SKILL.md"] = "Post with mcp__handful__send_to_user, then mcp__handful__update_board."
	for path, want := range files {
		if got, _ := os.ReadFile(filepath.Join(dir, path)); string(got) != want {
			t.Errorf("%s = %q; want %q", path, got, want)
		}
	}
}

// ~/.yargpt as a link to a folder elsewhere isn't moved, and the start says
// so instead of starting empty.
func TestAStartRefusesAnOldFolderThatIsALinkElsewhere(t *testing.T) {
	h := newHome(t)
	elsewhere := filepath.Join(h.dir, "Dropbox", "yargpt")
	if err := os.MkdirAll(filepath.Dir(elsewhere), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(h.paths.Old, elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, h.paths.Old); err != nil {
		t.Fatal(err)
	}
	before := h.snapshot()
	if _, err := h.mover().Data(t.Context()); err == nil || !strings.HasPrefix(err.Error(), "~/.yargpt is a link to "+elsewhere+", and Handful moves only a real folder") {
		t.Fatalf("Data = %v; want the link refused", err)
	}
	if after := h.snapshot(); !maps.Equal(after, before) {
		t.Fatalf("the start changed:%s", diff(after, before))
	}
}

// Session copies stay as private as their originals.
func TestSessionCopiesKeepTheirModes(t *testing.T) {
	h := newHome(t)
	if _, err := h.mover().Data(t.Context()); err != nil {
		t.Fatal(err)
	}
	key, _ := sessionKey(filepath.Join(h.paths.New, "bots/b1/workspace"))
	for path, want := range map[string]os.FileMode{
		filepath.Join(h.paths.Projects, key):                               0o700,
		filepath.Join(h.paths.Projects, key, "s1.jsonl"):                   0o600,
		filepath.Join(h.paths.Projects, key, "s1", "subagents", "a.jsonl"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v, %v; want mode %o", path, info.Mode(), err, want)
		}
	}
}
