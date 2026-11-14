package agent

import (
	"encoding/json"
	"strings"
)

// maxDiffJSON caps the JSON of the diff stored on one activity entry. A larger diff keeps only
// its line counts.
const maxDiffJSON = 32 << 10

// activityDiff is the change that an Edit or Write call made to its file, as unified-diff hunks.
// A diff over maxDiffJSON has no hunks and is Truncated.
type activityDiff struct {
	Added     int        `json:"added"`
	Removed   int        `json:"removed"`
	Truncated bool       `json:"truncated,omitempty"`
	Hunks     []diffHunk `json:"hunks,omitempty"`
}

// diffHunk is one hunk of Claude Code's structuredPatch. Each line starts with " " (context),
// "-" (removed), "+" (added) or "\" (the "\ No newline at end of file" note).
type diffHunk struct {
	OldStart int      `json:"oldStart"`
	OldLines int      `json:"oldLines"`
	NewStart int      `json:"newStart"`
	NewLines int      `json:"newLines"`
	Lines    []string `json:"lines"`
}

// fileChange is the part of an Edit's or a Write's tool_use_result that the diff needs. It has
// no field for originalFile, the whole file before the change, so that is never stored.
type fileChange struct {
	Type            string     `json:"type"` // Write: create or update
	Content         string     `json:"content"`
	StructuredPatch []diffHunk `json:"structuredPatch"`
}

// editsFile reports whether the tool's results carry a diff.
func editsFile(tool string) bool { return tool == "Edit" || tool == "Write" }

// changeDiff returns the diff in an Edit's or a Write's tool_use_result: its structuredPatch,
// or for a created file its content as added lines. It returns nil when the result has neither,
// as a failed call's text result or the result of a Claude Code that sends no patch.
func changeDiff(raw json.RawMessage) *activityDiff {
	var c fileChange
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil
	}
	hunks := c.StructuredPatch
	if c.Type == "create" {
		hunks = createdHunks(c.Content)
	}
	if len(hunks) == 0 {
		return nil
	}
	d := &activityDiff{Hunks: hunks}
	for _, h := range hunks {
		for _, l := range h.Lines {
			switch {
			case strings.HasPrefix(l, "+"):
				d.Added++
			case strings.HasPrefix(l, "-"):
				d.Removed++
			}
		}
	}
	if b, err := json.Marshal(d); err != nil || len(b) > maxDiffJSON {
		d.Hunks, d.Truncated = nil, true
	}
	return d
}

// createdHunks is a new file's content as one hunk of added lines.
func createdHunks(content string) []diffHunk {
	if content == "" {
		return nil
	}
	text, ends := strings.CutSuffix(content, "\n")
	lines := strings.Split(text, "\n")
	h := diffHunk{NewStart: 1, NewLines: len(lines)}
	for _, l := range lines {
		h.Lines = append(h.Lines, "+"+l)
	}
	if !ends {
		h.Lines = append(h.Lines, `\ No newline at end of file`)
	}
	return []diffHunk{h}
}

// onlyResult returns the tool_use id of the event's tool_result when the event has exactly one.
// The event's tool_use_result belongs to the event, not to one of its blocks, so with two
// results it cannot be matched to a call.
func onlyResult(ev streamEvent) string {
	id, n := "", 0
	for _, b := range ev.Message.Content {
		if b.Type == "tool_result" {
			id, n = b.ToolUseID, n+1
		}
	}
	if n != 1 {
		return ""
	}
	return id
}
