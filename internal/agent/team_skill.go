package agent

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"yargpt/internal/core"
)

// teamCommands are the commands of a team's chat. A skill with one of these
// ids cannot be invoked there: the command wins.
var teamCommands = map[string]bool{"clear": true, "compact": true, "goal": true}

// teamSkill reports whether the user's post that woke a turn in a team
// invokes a skill: after any @mentions, it starts with "/<skill-id>" for an
// existing skill that is not a team command. It returns the skill and the
// text after the id. Posts of bots and plain rooms invoke none.
func (r *Runner) teamSkill(u core.RoomUpdate, t core.Turn) (core.Skill, string, bool) {
	post, ok := lastUserPost(u.Messages)
	if !u.Room.IsTeam() || !t.FromUserPost() || !ok {
		return core.Skill{}, "", false
	}
	skill, args, ok := r.slashSkill(withoutMentions(post, u.Room.Members))
	if !ok || teamCommands[skill.ID] {
		return core.Skill{}, "", false
	}
	return skill, args, true
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
