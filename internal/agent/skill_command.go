package agent

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"handful/internal/core"
)

// teamCommands are the commands of a team's chat, which the UI handles before it sends: a skill
// with one of these ids runs there only through the Skill tool, never as a command.
var teamCommands = map[string]bool{"clear": true, "compact": true, "goal": true, "task": true}

// skillCommand returns words, the user's own, trimmed, when they start with "/<skill-id>" for an
// existing skill that reserved doesn't hold. Claude Code runs a skill itself when the last text
// block of a message starts with it, and takes the rest of that block as the skill's arguments,
// as when the words are typed in its terminal. So such words go to it as the message's last
// block, after Handful's own text. Only a skill is matched: any other "/word" (Claude Code's own
// commands, such as /model) stays inside the message's text, where it is plain text.
func (r *Runner) skillCommand(words string, reserved map[string]bool) (string, bool) {
	words = strings.TrimSpace(words)
	rest, ok := strings.CutPrefix(words, "/")
	if !ok {
		return "", false
	}
	id, _, _ := strings.Cut(strings.Join(strings.Fields(rest), " "), " ")
	if id == "" || reserved[id] {
		return "", false
	}
	if _, err := r.svc.GetSkill(id); err != nil {
		return "", false
	}
	return words, true
}

// teamCommand returns the skill command of the user's post that woke turn t in a team: after
// any @mentions, the post starts with "/<skill-id>" for a skill that no team command hides.
// Posts of bots, and posts in plain rooms, have none.
func (r *Runner) teamCommand(u core.RoomUpdate, t core.Turn) (string, bool) {
	post, ok := lastUserPost(u.Messages)
	if !u.Room.IsTeam() || !t.FromUserPost() || !ok {
		return "", false
	}
	return r.skillCommand(withoutMentions(post, u.Room.Members), teamCommands)
}

// lastUserPost returns the text of the last post of the user in entries.
func lastUserPost(entries []core.Entry) (string, bool) {
	for _, e := range slices.Backward(entries) {
		if e.Kind == core.KindUser {
			var body struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(e.Body, &body)
			return body.Text, true
		}
	}
	return "", false
}

// withoutMentions returns text without the @mentions of members, or @here,
// that it starts with, as in "@Bo, @Cy /review the diff".
func withoutMentions(text string, members []core.Member) string {
	text = strings.TrimSpace(text)
	for strings.HasPrefix(text, "@") {
		rest, ok := afterMention(text[1:], members)
		if !ok {
			return text
		}
		text = strings.TrimLeft(rest, " \t\n,:")
	}
	return text
}

// afterMention returns the text after the member name or "here" that text
// starts with, ignoring case. The name must end where a word ends, and the
// longest name wins, so "Forge Dev" wins over "Forge".
func afterMention(text string, members []core.Member) (string, bool) {
	names := append([]string{"here"}, memberNames(members)...)
	slices.SortStableFunc(names, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	for _, name := range names {
		if len(text) >= len(name) && strings.EqualFold(text[:len(name)], name) && wordEnds(text[len(name):]) {
			return text[len(name):], true
		}
	}
	return "", false
}

// wordEnds reports whether rest, the text after a word, starts outside it.
func wordEnds(rest string) bool {
	r, _ := utf8.DecodeRuneInString(rest)
	return rest == "" || !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func memberNames(members []core.Member) []string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Name
	}
	return names
}
