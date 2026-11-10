package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// sqlBody is a SELECT without its comment lines, with runs of white space as
// one space.
func sqlBody(text string) string {
	var lines []string
	for line := range strings.Lines(text) {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}

func TestTheOffTaskShareFileIsTheQueryThatTheCodeRuns(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "measures", "off-task-share.sql"))
	if err != nil {
		t.Fatal(err)
	}
	file := strings.NewReplacer(":from", "?1", ":to", "?2").Replace(sqlBody(string(raw)))
	file = strings.TrimSuffix(file, ";")
	if code := sqlBody(offTaskShare); code != file {
		t.Fatalf("docs/measures/off-task-share.sql differs from OffTaskShare in query.sql:\nfile: %s\ncode: %s", file, code)
	}
}

func TestTheOffTaskShareCountsTeamRoomTurnsOnNoTaskWithTheSpecsExclusions(t *testing.T) {
	ctx := context.Background()
	conn, err := Open(ctx, filepath.Join(t.TempDir(), "yargpt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	bot := func(id, kind string, position int) {
		exec(`INSERT INTO bots (id, name, kind, position, created_at, updated_at) VALUES (?, ?, ?, ?, 0, 0)`, id, id, kind, position)
	}
	for i, id := range []string{"ana", "bo", "cy", "dee", "eve"} {
		bot(id, "agent", i)
	}
	for _, id := range []string{"ship", "docs", "chat"} {
		bot(id, "room", 0)
	}
	// Ship's chosen lead is Bo. Docs chose Zed, who left: its lead is its
	// first member, Dee. Chat is a plain room.
	exec(`INSERT INTO teams (team_id, lead_bot_id) VALUES ('ship', 'bo'), ('docs', 'zed')`)
	for _, m := range [][2]string{{"ship", "ana"}, {"ship", "bo"}, {"ship", "cy"}, {"docs", "eve"}, {"docs", "dee"}, {"chat", "ana"}} {
		exec(`INSERT INTO room_members (room_id, bot_id) VALUES (?, ?)`, m[0], m[1])
	}
	exec(`UPDATE bots SET position = 0 WHERE id = 'dee'`)
	exec(`UPDATE bots SET position = 9 WHERE id = 'eve'`)
	turn := func(id, bot, room, source string, hops int, goal, role string, decision int, started int64) {
		exec(`INSERT INTO turns (id, bot_id, source, prompt, status, room_id, hops, goal_id, board_role, decision, created_at, started_at)
			VALUES (?, ?, ?, '', 'done', ?, ?, ?, ?, ?, ?, ?)`, id, bot, source, room, hops, goal, role, decision, started, started)
	}
	const from, to = 1000, 2000
	// Ship: 5 counted, 3 of them on no task.
	turn("mention", "ana", "ship", "room", 1, "", "", 0, 1100)         // counted, off task
	turn("report", "cy", "ship", "task", 0, "", "", 0, 1200)           // counted, off task
	turn("user-to-ana", "ana", "ship", "room", 0, "", "", 0, 1300)     // counted, off task: Ana isn't the lead
	turn("inherited", "cy", "ship", "room", 1, "g1", "", 0, 1400)      // counted, on a task
	turn("work", "ana", "ship", "room", 0, "g1", "work", 0, 1500)      // counted, on a task
	turn("decision", "bo", "ship", "system", 0, "g2", "lead", 1, 1600) // left out: a decision
	turn("user-to-lead", "bo", "ship", "room", 0, "", "", 0, 1700)     // left out: the user's talk with the lead
	turn("before", "ana", "ship", "room", 1, "", "", 0, 999)           // left out: before from
	turn("at-to", "ana", "ship", "room", 1, "", "", 0, 2000)           // left out: at to
	// Docs: the user's post to its first member, the lead, is left out.
	turn("docs-user-to-lead", "dee", "docs", "room", 0, "", "", 0, 1100) // left out
	turn("docs-user-to-eve", "eve", "docs", "room", 0, "", "", 0, 1200)  // counted, off task
	// Direct chats and plain rooms are left out.
	turn("direct", "ana", "", "user", 0, "", "", 0, 1100)
	turn("plain", "ana", "chat", "room", 1, "", "", 0, 1100)

	got, err := New(conn).OffTaskShare(ctx, OffTaskShareParams{FromMs: sql.NullInt64{Int64: from, Valid: true}, ToMs: sql.NullInt64{Int64: to, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	want := []OffTaskShareRow{{TeamID: "docs", OffTask: 1, Total: 1}, {TeamID: "ship", OffTask: 3, Total: 5}}
	if !slices.Equal(got, want) {
		t.Fatalf("off-task share = %+v; want %+v", got, want)
	}
}
