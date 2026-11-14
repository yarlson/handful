package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"handful/internal/core"
)

const editStream = "edit-and-write.jsonl"

func editStreamPath(t *testing.T) string {
	t.Helper()
	stream, err := filepath.Abs(filepath.Join("testdata", "claude-2.1.325", editStream))
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

// recordedEvents are the events of the recorded Edit and Write stream, in order.
func recordedEvents(t *testing.T) []streamEvent {
	t.Helper()
	f, err := os.Open(editStreamPath(t))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var events []streamEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev streamEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("parse %q: %v", sc.Text(), err)
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

// activityRows maps each activity row's tool and summary to its stored body.
func activityRows(t *testing.T, svc *core.Service, botID string) map[string]string {
	t.Helper()
	entries, err := svc.Entries(context.Background(), botID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]string{}
	for _, e := range entries {
		if e.Kind == core.KindActivity {
			rows[bodyField(e, "tool").(string)+" "+bodyField(e, "summary").(string)] = string(e.Body)
		}
	}
	return rows
}

func storedDiff(t *testing.T, body string) string {
	t.Helper()
	var b struct {
		Diff json.RawMessage `json:"diff"`
	}
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatal(err)
	}
	return string(b.Diff)
}

// Claude Code 2.1.325's recorded stream has an Edit, a Write that creates a file and a Write that
// updates one. Each row stores the diff of its own call; the Read rows store none.
func TestEditAndWriteRowsStoreTheDiffOfTheirOwnCall(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "replay")
	h.env = append(h.env, "FAKE_STREAM="+editStreamPath(t))
	bot := h.newBot(t)
	h.start(t)
	h.say(t, bot.ID, "go")
	want := map[string]string{
		"Read /workspace/a.txt":  "",
		"Read /workspace/b.txt":  "",
		"Edit /workspace/a.txt":  `{"added":1,"removed":1,"hunks":[{"oldStart":1,"oldLines":3,"newStart":1,"newLines":3,"lines":[" one","-two","+TWO"," three"]}]}`,
		"Write /workspace/c.txt": `{"added":2,"removed":0,"hunks":[{"oldStart":0,"oldLines":0,"newStart":1,"newLines":2,"lines":["+alpha","+beta"]}]}`,
		"Write /workspace/b.txt": `{"added":1,"removed":1,"hunks":[{"oldStart":1,"oldLines":2,"newStart":1,"newLines":2,"lines":["-old first","+new first"," old second","\\ No newline at end of file"]}]}`,
	}
	rows := activityRows(t, h.svc, bot.ID)
	if len(rows) != len(want) {
		t.Fatalf("activity rows = %v; want the 5 calls %v", rows, want)
	}
	for row, diff := range want {
		if got := storedDiff(t, rows[row]); got != diff {
			t.Errorf("diff of %q = %s; want %s", row, got, diff)
		}
	}
}

// The result's originalFile is the whole file before the change. No stored row holds it: not the
// key, and not the old content of a.txt (Edit) or b.txt (Write).
func TestNoActivityRowStoresTheOriginalFile(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "replay")
	h.env = append(h.env, "FAKE_STREAM="+editStreamPath(t))
	bot := h.newBot(t)
	h.start(t)
	h.say(t, bot.ID, "go")
	for row, body := range activityRows(t, h.svc, bot.ID) {
		for _, original := range []string{"originalFile", `one\ntwo\nthree\n`, `old first\nold second`} {
			if strings.Contains(body, original) {
				t.Errorf("row %q stores %q: %s", row, original, body)
			}
		}
	}
}

// replayed feeds events to a fresh recorder and returns the stored rows.
func replayed(t *testing.T, events ...streamEvent) map[string]string {
	t.Helper()
	h := newHarness(t, "replay")
	bot := h.newBot(t)
	rec := newActivityRecorder(h.svc, bot.ID, "turn-1", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, ev := range events {
		switch ev.Type {
		case "assistant":
			rec.toolUses(context.Background(), ev)
		case "user":
			rec.toolResults(context.Background(), ev)
		}
	}
	return activityRows(t, h.svc, bot.ID)
}

// The recorded stream's events: 8 is the Edit's call, 9 its result, 10 the created file's call
// and 11 its result.
const editCall, editResult, createCall, createResult = 8, 9, 10, 11

// tool_use_result belongs to the user event, not to one of its tool_result blocks. When one
// event holds two results, the diff cannot be matched to one call, and neither row gets it.
func TestAnEventWithTwoResultsStoresNoDiff(t *testing.T) {
	t.Parallel()
	ev := recordedEvents(t)
	calls := ev[editCall]
	msg := *calls.Message
	msg.Content = append(append([]contentBlock{}, ev[editCall].Message.Content...), ev[createCall].Message.Content...)
	calls.Message = &msg
	results := ev[editResult]
	both := *results.Message
	both.Content = append(append([]contentBlock{}, ev[editResult].Message.Content...), ev[createResult].Message.Content...)
	results.Message = &both
	for row, body := range replayed(t, calls, results) {
		if diff := storedDiff(t, body); diff != "" || bodyField(core.Entry{Body: json.RawMessage(body)}, "status") != "done" {
			t.Errorf("row %q = %s; want done with no diff", row, body)
		}
	}
}

// A result with no tool_use_result, as from a Claude Code that sends none, stores the row
// without a diff. So does a NotebookEdit result, even one shaped like a patch.
func TestAResultWithoutAPatchOrFromAnotherToolStoresNoDiff(t *testing.T) {
	t.Parallel()
	ev := recordedEvents(t)
	bare := ev[editResult]
	bare.ToolUseResult = nil
	notebook := ev[editCall]
	msg := *notebook.Message
	msg.Content = []contentBlock{msg.Content[0]}
	msg.Content[0].Name = "NotebookEdit"
	notebook.Message = &msg
	for name, events := range map[string][]streamEvent{
		"no tool_use_result": {ev[editCall], bare},
		"NotebookEdit":       {notebook, ev[editResult]},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rows := replayed(t, events...)
			if len(rows) != 1 {
				t.Fatalf("rows = %v; want 1", rows)
			}
			for row, body := range rows {
				if strings.Contains(body, `"diff"`) {
					t.Errorf("row %q = %s; want no diff", row, body)
				}
			}
		})
	}
}

func TestChangeDiff(t *testing.T) {
	t.Parallel()
	// A created file of one line whose stored JSON is exactly the cap, 32768 bytes.
	head, tail := `{"added":1,"removed":0,"hunks":[{"oldStart":0,"oldLines":0,"newStart":1,"newLines":1,"lines":["+`, `"]}]}`
	atCap := strings.Repeat("a", 32768-len(head)-len(tail))
	created := func(content string) string {
		raw, err := json.Marshal(map[string]any{"type": "create", "content": content, "structuredPatch": []any{}, "originalFile": nil})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	for _, c := range []struct {
		name, result, want string
	}{
		{"absent", ``, `null`},
		{"a failed call's text", `"Error: String to replace not found in file."`, `null`},
		{"an update with no change", `{"type":"update","content":"x\n","structuredPatch":[]}`, `null`},
		{"an empty created file", created(""), `null`},
		{"a created file without a last newline", created("a\n\nb"), `{"added":3,"removed":0,"hunks":[{"oldStart":0,"oldLines":0,"newStart":1,"newLines":3,"lines":["+a","+","+b","\\ No newline at end of file"]}]}`},
		{"a created file at the cap", created(atCap + "\n"), head + atCap + tail},
		{"a created file over the cap", created(atCap + "a\n"), `{"added":1,"removed":0,"truncated":true}`},
		{"a patch over the cap", `{"structuredPatch":[{"oldStart":1,"oldLines":2,"newStart":1,"newLines":2,"lines":["-` + atCap + `","+b"," c"]}]}`, `{"added":1,"removed":1,"truncated":true}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(changeDiff(json.RawMessage(c.result)))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("changeDiff(%.200s) = %.300s; want %.300s", c.result, got, c.want)
			}
		})
	}
}
