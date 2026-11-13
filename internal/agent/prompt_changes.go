package agent

import "strings"

// Claude Code records a session's system prompt once and resends that record on every later
// request and resume, until the session is compacted (--system-prompt-snapshot on), so the
// cached prefix of the whole conversation stays valid. A changed system prompt reaches the bot
// in the turn message instead, as the sections that differ from what the session knows.

// promptSections splits a system prompt at its "# " and "## " heading lines. It returns each
// section's text, its heading line included, by heading, and the headings in order. Sections
// with the same heading are joined.
func promptSections(prompt string) (map[string]string, []string) {
	sections := map[string]string{}
	var order []string
	heading := ""
	for line := range strings.SplitAfterSeq(prompt, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			heading = strings.TrimSuffix(line, "\n")
		}
		if _, seen := sections[heading]; !seen {
			order = append(order, heading)
		}
		sections[heading] += line
	}
	return sections, order
}

// promptChanges returns the turn-message block that brings a session that knows the system
// prompt known up to date with current: every section of current that is new or differs, in
// full, and the heading of every section that is gone. It returns "" when nothing differs. The
// sections hold text that bots and the user wrote, so "</" in them is written as "<\/" and
// cannot end the block.
func promptChanges(known, current string) string {
	before, beforeOrder := promptSections(known)
	after, afterOrder := promptSections(current)
	var b strings.Builder
	for _, h := range afterOrder {
		if before[h] != after[h] {
			b.WriteString(strings.ReplaceAll(strings.TrimRight(after[h], "\n"), "</", `<\/`) + "\n\n")
		}
	}
	for _, h := range beforeOrder {
		if _, kept := after[h]; !kept {
			b.WriteString(strings.ReplaceAll(h, "</", `<\/`) + "\n(removed)\n\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "<context-update>\nParts of your system prompt changed since this session began. Each section below replaces the section with the same heading in your system prompt.\n\n" + b.String() + "</context-update>\n"
}
