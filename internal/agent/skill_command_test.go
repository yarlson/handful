package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"handful/internal/core"
)

// A team post's skill command goes after the room context, as the message's last block, unchanged
// but for the leading @mentions; Handful adds no skill text of its own.
func TestATeamPostsSkillGoesAfterTheRoomContextAsItsOwnBlock(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, _, _, _ := h.newTeam(t)
	if _, err := h.svc.WriteSkill("make-proof", "Use when asked for a proof file.", "1. Write the word to proof.txt."); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, team.ID, "/make-proof yes", "n1"); err != nil {
		t.Fatal(err)
	}
	h.waitCalls(t, 1)
	if _, _, err := h.svc.SubmitUserMessage(ctx, team.ID, "@Bo /make-proof for October", "n2"); err != nil {
		t.Fatal(err)
	}
	calls := h.waitCalls(t, 2)
	for i, want := range []struct{ who, post, command string }{{"Ana", "User: /make-proof yes", "/make-proof yes"}, {"Bo", "User: @Bo /make-proof for October", "/make-proof for October"}} {
		context, command, blocks := strings.Cut(calls[i], "\n<<BLOCK>>\n")
		if !blocks || command != want.command {
			t.Errorf("%s's turn ends with block %q (two blocks: %v); want %q", want.who, command, blocks, want.command)
		}
		for _, part := range []string{"- you (" + want.who, `[team] Team channel "Ship"`, want.post} {
			if !strings.Contains(context, part) {
				t.Errorf("%s's room context lacks %q", want.who, part)
			}
		}
		if strings.Contains(calls[i], "invoked your skill") || strings.Contains(calls[i], "Write the word to proof.txt") {
			t.Errorf("%s's turn carries Handful's skill text: %s", want.who, calls[i])
		}
	}
}

func TestASlashSkillInAPlainRoomStaysText(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	ana := h.newNamedBot(t, "Ana")
	room, err := h.svc.CreateRoom(ctx, "Plan", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.WriteSkill("make-proof", "Use when asked for a proof file.", "1. Write the word to proof.txt."); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, room.ID, "/make-proof yes", "n1"); err != nil {
		t.Fatal(err)
	}
	calls := h.waitCalls(t, 1)
	if strings.Contains(calls[0], "<<BLOCK>>") || !strings.Contains(calls[0], "User: /make-proof yes") {
		t.Errorf("a /skill in a plain room should stay plain text: %s", calls[0])
	}
}

func roomEntry(kind, text string) core.Entry {
	body, _ := json.Marshal(map[string]string{"text": text})
	return core.Entry{Kind: kind, Body: body}
}

func TestTeamCommandIsOnlyASkillThatTheUsersPostInATeamStartsWith(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	team, _, _, _ := h.newTeam(t)
	for _, name := range []string{"make-proof", "clear", "compact", "goal", "task"} {
		if _, err := h.svc.WriteSkill(name, "A skill.", "Do "+name+"."); err != nil {
			t.Fatal(err)
		}
	}
	plain := team
	plain.Team = nil
	userTurn := core.Turn{Source: core.SourceRoom, RoomID: team.ID}
	cases := []struct {
		name     string
		room     core.Bot
		turn     core.Turn
		messages []core.Entry
		command  string
	}{
		{"the user's post", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof  yes please ")}, "/make-proof  yes please"},
		{"a bare skill", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof")}, "/make-proof"},
		{"after mentions", team, userTurn, []core.Entry{roomEntry(core.KindUser, "@bo, @Ana /make-proof x")}, "/make-proof x"},
		{"the last post of the user", team, userTurn, []core.Entry{roomEntry(core.KindUser, "hello"), roomEntry(core.KindUser, "/make-proof x"), roomEntry(core.KindBot, "on it")}, "/make-proof x"},
		{"an earlier post", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof x"), roomEntry(core.KindUser, "thanks")}, ""},
		{"a bot's post", team, userTurn, []core.Entry{roomEntry(core.KindBot, "/make-proof x")}, ""},
		{"no post", team, userTurn, nil, ""},
		{"a plain room", plain, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof x")}, ""},
		{"a turn a bot woke", team, core.Turn{Source: core.SourceRoom, RoomID: team.ID, Hops: 1}, []core.Entry{roomEntry(core.KindUser, "/make-proof x")}, ""},
		{"a board turn", team, core.Turn{Source: core.SourceRoom, RoomID: team.ID, BoardRole: core.RoleWork}, []core.Entry{roomEntry(core.KindUser, "/make-proof x")}, ""},
		{"an unknown skill", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/nope x")}, ""},
		{"text before the skill", team, userTurn, []core.Entry{roomEntry(core.KindUser, "run /make-proof x")}, ""},
		{"/clear", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/clear x")}, ""},
		{"/compact", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/compact")}, ""},
		{"/goal", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/goal ship it")}, ""},
		{"/task", team, userTurn, []core.Entry{roomEntry(core.KindUser, "@Bo /task write it")}, ""},
		{"a newline after the id", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof\nfor October")}, "/make-proof\nfor October"},
		{"a bare slash", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/ x")}, ""},
		{"a path", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof/x y")}, ""},
	}
	for _, c := range cases {
		command, ok := h.runner.teamCommand(core.RoomUpdate{Room: c.room, Messages: c.messages}, c.turn)
		if ok != (c.command != "") || command != c.command {
			t.Errorf("%s: teamCommand = %q, %v; want %q", c.name, command, ok, c.command)
		}
	}
}

func TestWithoutMentionsDropsTheLeadingMentionsOfMembers(t *testing.T) {
	t.Parallel()
	members := []core.Member{{Name: "Forge"}, {Name: "Forge Dev"}, {Name: "Bo"}}
	for _, c := range []struct{ text, want string }{
		{"/x go", "/x go"},
		{"  /x go ", "/x go"},
		{"@Forge Dev /x go", "/x go"},
		{"@forge /x go", "/x go"},
		{"@Bo, @here: /x", "/x"},
		{"@HERE\n/x", "/x"},
		{"@Bob /x", "@Bob /x"},
		{"@heresy /x", "@heresy /x"},
		{"@Cy /x", "@Cy /x"},
		{"@Bo", ""},
		{"@", "@"},
		{"@Bo hi /x", "hi /x"},
	} {
		if got := withoutMentions(c.text, members); got != c.want {
			t.Errorf("withoutMentions(%q) = %q; want %q", c.text, got, c.want)
		}
	}
}

// A bot chat's message that starts with a skill goes to Claude Code unchanged, as the message's
// last block, after Handful's own text; any other message, a "/word" that is no skill included,
// stays one text.
func TestABotChatsSkillMessageGoesUnchangedAsTheLastBlock(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	bot := h.newBot(t)
	if _, err := h.svc.WriteSkill("make-proof", "Use when asked for a proof file.", "1. Write the word to proof.txt."); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	sends := []string{"/make-proof for October\nand November", "Summarise, then use /make-proof.", "/model opus", "/no-such-skill hi"}
	for i, text := range sends {
		if _, _, err := h.svc.SubmitUserMessage(ctx, bot.ID, text, fmt.Sprintf("n%d", i)); err != nil {
			t.Fatal(err)
		}
		h.waitCalls(t, i+1)
		waitFor(t, "the turn ends", func() bool { return !h.runner.Running(bot.ID) })
	}
	calls := h.waitCalls(t, len(sends))
	_, msg, _ := strings.Cut(calls[0], "--session-id")
	first, command, blocks := strings.Cut(msg, "\n<<BLOCK>>\n")
	if !blocks || command != sends[0] || !strings.Contains(first, "<timestamp>") || strings.Contains(first, "make-proof") {
		t.Fatalf("the skill message = %q then %q (blocks %v); want Handful's text, then the message unchanged", first, command, blocks)
	}
	for i, text := range sends[1:] {
		if call := calls[i+1]; strings.Contains(call, "<<BLOCK>>") || !strings.Contains(call, "</timestamp>\n"+text) {
			t.Errorf("message %q went as %q; want one text that ends with it", text, call)
		}
	}
	if strings.Contains(strings.Join(calls, ""), "Write the word to proof.txt") {
		t.Error("a turn carries the skill's body; Claude Code loads it itself")
	}
}
