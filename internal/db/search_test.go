package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestContainsFoldIgnoresTheCaseOfEveryScript(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text, query string
		want        bool
	}{
		{"Deploy the Server", "SERVER", true},
		{"Deploy the Server", "the s", true},
		{"Привет, Мир", "мир", true},
		{"Straße nach Köln", "KÖLN", true},
		{"ΣΟΦΙΑ", "σοφια", true},
		{"Deploy the Server", "servers", false},
		{"Deploy", "", true},
		{"", "a", false},
		{"AZ", "az", true},
		{"@[", "`{", false},
		// A byte that is not UTF-8 reads as U+FFFD, as in a Go range loop.
		{"a\x80b", "a\uFFFDb", true},
	}
	for _, c := range cases {
		if got := containsFold(c.text, c.query); got != c.want {
			t.Errorf("containsFold(%q, %q) = %v, want %v", c.text, c.query, got, c.want)
		}
	}
}

func openSearchDB(t testing.TB) (*sql.DB, *Queries) {
	t.Helper()
	conn, err := Open(context.Background(), filepath.Join(t.TempDir(), "handful.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.ExecContext(context.Background(), `INSERT INTO bots (id, name, instructions, claude_session_id, kind, created_at, updated_at)
		VALUES ('ana', 'Ana', '', '', 'agent', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	return conn, New(conn)
}

func TestSearchEntriesFindsNoEntryWithoutText(t *testing.T) {
	t.Parallel()
	_, q := openSearchDB(t)
	ctx := context.Background()
	for i, body := range []string{`{}`, `{"text": null}`, `{"text": 7}`, `{"text": "Seven"}`} {
		if _, err := q.AppendEntry(ctx, AppendEntryParams{BotID: "ana", ID: fmt.Sprint("e", i), Kind: "user", Body: body, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := q.SearchEntries(ctx, SearchEntriesParams{BotID: "ana", Query: "seven", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "e3" {
		t.Fatalf("search found %+v, want only e3", rows)
	}
}

// The search reads the chat's messages through entries_messages, so that it
// does not read the tool calls and events between them.
func TestSearchEntriesReadsOnlyTheMessageIndex(t *testing.T) {
	t.Parallel()
	conn, _ := openSearchDB(t)
	rows, err := conn.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+searchEntries, "ana", "x", 51)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(plan, "; "); !strings.Contains(got, "USING INDEX entries_messages") || strings.Contains(got, "TEMP B-TREE") {
		t.Fatalf("plan %q, want a read of entries_messages in seq order", got)
	}
}

func TestTheMessageIndexMigrationRunsAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "handful.db")
	openAt(t, path, 26).Close()
	conn, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	script, err := fs.ReadFile(migrations, "migrations/027_message_index.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(script)); err != nil {
		t.Fatalf("run the migration again: %v", err)
	}
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE type = 'index' AND name = 'entries_messages'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("entries_messages indexes = %d, %v; want 1", n, err)
	}
}

// BenchmarkSearchEntries searches a chat of 100,000 entries, of which 45%
// are messages, for a word that no entry has, so that it reads every message.
func BenchmarkSearchEntries(b *testing.B) {
	conn, q := openSearchDB(b)
	ctx := context.Background()
	words := strings.Fields("deploy server review branch merge test build release chat window bot team room query index page mark result older newer привет мир сервер ветка")
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO entries (bot_id, seq, rev, id, kind, turn_id, body, created_at) VALUES ('ana', ?, ?, ?, ?, '', ?, 1)`)
	if err != nil {
		b.Fatal(err)
	}
	defer insert.Close()
	for i := range 100_000 {
		kind := [...]string{"user", "user", "user", "bot", "bot", "bot", "bot", "bot", "agent", "activity", "activity", "activity", "activity", "activity", "activity", "activity", "activity", "activity", "event", "event"}[i%20]
		var text strings.Builder
		for w := range 12 + i%40 {
			text.WriteString(words[(i*7+w*13)%len(words)])
			text.WriteByte(' ')
		}
		body, err := json.Marshal(map[string]string{"text": text.String()})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := insert.ExecContext(ctx, i+1, i+1, fmt.Sprint("e", i), kind, string(body)); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	for _, query := range []string{"absent", "нет такого"} {
		b.Run(query, func(b *testing.B) {
			for b.Loop() {
				rows, err := q.SearchEntries(ctx, SearchEntriesParams{BotID: "ana", Query: query, Limit: 51})
				if err != nil || len(rows) != 0 {
					b.Fatalf("search = %d rows, %v; want none", len(rows), err)
				}
			}
		})
	}
}
