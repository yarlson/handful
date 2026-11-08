package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"yargpt/internal/core"
)

func TestASlashSkillInATeamChatPutsTheSkillIntoTheTurnsOfTheMembersItWakes(t *testing.T) {
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
	if _, _, err := h.svc.SubmitUserMessage(ctx, team.ID, "@Bo /make-proof check it", "n2"); err != nil {
		t.Fatal(err)
	}
	calls := h.waitCalls(t, 2)
	for i, want := range []struct{ who, request string }{{"Ana", "yes"}, {"Bo", "check it"}} {
		for _, part := range []string{
			"- you (" + want.who,
			`[team] Team channel "Ship"`,
			`The user invoked your skill "make-proof". Their request: ` + want.request + "\n",
			`<skill name="make-proof">`,
			"1. Write the word to proof.txt.",
		} {
			if !strings.Contains(calls[i], part) {
				t.Errorf("%s's team turn lacks %q", want.who, part)
			}
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
	if strings.Contains(calls[0], "<skill") || !strings.Contains(calls[0], "User: /make-proof yes") {
		t.Errorf("a /skill in a plain room should stay plain text: %s", calls[0])
	}
}

func roomEntry(kind, text string) core.Entry {
	body, _ := json.Marshal(map[string]string{"text": text})
	return core.Entry{Kind: kind, Body: body}
}

func TestTeamSkillIsOnlyASkillThatTheUsersPostInATeamInvokes(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	team, _, _, _ := h.newTeam(t)
	for _, name := range []string{"make-proof", "clear", "compact", "goal"} {
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
		skill    string
		args     string
	}{
		{"the user's post", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof  yes please ")}, "make-proof", "yes please"},
		{"a bare skill", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof")}, "make-proof", ""},
		{"after mentions", team, userTurn, []core.Entry{roomEntry(core.KindUser, "@bo, @Ana /make-proof x")}, "make-proof", "x"},
		{"the last post of the user", team, userTurn, []core.Entry{roomEntry(core.KindUser, "hello"), roomEntry(core.KindUser, "/make-proof x"), roomEntry(core.KindBot, "on it")}, "make-proof", "x"},
		{"an earlier post", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof x"), roomEntry(core.KindUser, "thanks")}, "", ""},
		{"a bot's post", team, userTurn, []core.Entry{roomEntry(core.KindBot, "/make-proof x")}, "", ""},
		{"no post", team, userTurn, nil, "", ""},
		{"a plain room", plain, userTurn, []core.Entry{roomEntry(core.KindUser, "/make-proof x")}, "", ""},
		{"a turn a bot woke", team, core.Turn{Source: core.SourceRoom, RoomID: team.ID, Hops: 1}, []core.Entry{roomEntry(core.KindUser, "/make-proof x")}, "", ""},
		{"a board turn", team, core.Turn{Source: core.SourceRoom, RoomID: team.ID, BoardRole: core.RoleWork}, []core.Entry{roomEntry(core.KindUser, "/make-proof x")}, "", ""},
		{"an unknown skill", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/nope x")}, "", ""},
		{"text before the skill", team, userTurn, []core.Entry{roomEntry(core.KindUser, "run /make-proof x")}, "", ""},
		{"/clear", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/clear x")}, "", ""},
		{"/compact", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/compact")}, "", ""},
		{"/goal", team, userTurn, []core.Entry{roomEntry(core.KindUser, "/goal ship it")}, "", ""},
	}
	for _, c := range cases {
		skill, args, ok := h.runner.teamSkill(core.RoomUpdate{Room: c.room, Messages: c.messages}, c.turn)
		if ok != (c.skill != "") || skill.ID != c.skill || args != c.args {
			t.Errorf("%s: teamSkill = %q, %q, %v; want %q, %q", c.name, skill.ID, args, ok, c.skill, c.args)
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
