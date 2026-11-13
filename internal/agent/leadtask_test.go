package agent

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

// Only the user's own message in the bot's own chat counts as the user asking for a task.
func TestOnlyTheUsersOwnMessageInItsChatIsAUserChatTurn(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]bool{
		core.SourceUser: true, core.SourceRoutine: false, core.SourceAgent: false, core.SourceSystem: false,
		core.SourceRoom: false, core.SourceTask: false, core.SourceCompact: false,
	} {
		if got := fromUserChat(core.Turn{Source: source}, ""); got != want {
			t.Errorf("a %s turn in the bot's own chat: %v; want %v", source, got, want)
		}
	}
	if fromUserChat(core.Turn{Source: core.SourceUser}, "room") {
		t.Error("a user turn in a room counts as the bot's own chat")
	}
}

// A user's message to a team's lead runs with UserChat set, so its update_board may add a task.
func TestAUsersMessageToALeadRunsAsAUserChatTurn(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "sleep")
	_, ana, _, _ := h.newTeam(t)
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(context.Background(), ana.ID, "add CSV export to the team's tasks", "n1"); err != nil {
		t.Fatal(err)
	}
	var rc runContext
	waitFor(t, "Ana's turn runs", func() bool {
		h.runner.mu.Lock()
		defer h.runner.mu.Unlock()
		for _, s := range h.runner.sessions {
			if s.key.botID == ana.ID {
				rc, _ = h.runner.tokens.get(s.token)
			}
		}
		return rc.TurnID != ""
	})
	if !rc.UserChat || rc.RoomID != "" {
		t.Fatalf("Ana's run context = %+v; want a user chat turn", rc)
	}
}

// From its own chat a lead lists its team's board and adds the user's task with update_board; a
// member is refused with its lead's name; a turn the user didn't start is refused; step and task
// actions stay in team turns.
func TestUpdateBoardInALeadsOwnChat(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, bo, _ := h.newTeam(t)
	room, err := h.svc.CreateRoom(ctx, "Plan", "", []string{ana.ID, bo.ID})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	session := func(rc runContext) func(map[string]any) (string, bool) {
		token, err := h.runner.tokens.issue(rc)
		if err != nil {
			t.Fatal(err)
		}
		cs, err := mcpSession(t, srv.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })
		return func(args map[string]any) (string, bool) { return callText(t, cs, "update_board", args) }
	}
	lead := session(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t1", UserChat: true})
	if text, isErr := lead(map[string]any{"action": "list"}); isErr || text != "The team has no task yet." {
		t.Fatalf("list from Ana's chat = %q (error %v); want Ship's empty board", text, isErr)
	}
	text, isErr := lead(map[string]any{"action": "user_task", "text": "Add CSV export.", "brief": "On the reports page."})
	board, err := h.svc.Board(ctx, team.ID)
	if err != nil || len(board.Inbox) != 1 {
		t.Fatalf("Ship's board after user_task = %+v, %v; want one task in the inbox", board, err)
	}
	g := board.Inbox[0]
	if want := `Added "Add CSV export." (task id ` + g.ID + `) to team Ship's board as the user's task; it is in the inbox. You decide on it in your next turn in the team.`; isErr || text != want {
		t.Fatalf("user_task = %q (error %v); want %q", text, isErr, want)
	}
	if g.Origin != core.OriginUser || g.FromBotID != "" || g.Brief != "On the reports page." {
		t.Fatalf("the task = %+v; want the user's own task", g)
	}
	if text, isErr := lead(map[string]any{"action": "user_task", "text": "Fix login.", "start_now": true}); isErr || !strings.Contains(text, "it is in progress.") {
		t.Fatalf("user_task with start_now = %q (error %v); want it in progress", text, isErr)
	}
	if text, isErr := lead(map[string]any{"action": "list"}); isErr || !strings.Contains(text, "Now task (id ") || !strings.Contains(text, "Fix login.") {
		t.Fatalf("list after start_now = %q (error %v); want Fix login as the Now task", text, isErr)
	}

	for what, c := range map[string]struct {
		call func(map[string]any) (string, bool)
		args map[string]any
		want string
	}{
		"a member's user_task":  {session(runContext{BotID: bo.ID, BotName: "Bo", TurnID: "t2", UserChat: true}), map[string]any{"action": "user_task", "text": "X."}, "you lead no team; Ana leads Ship and adds its tasks"},
		"a member's list":       {session(runContext{BotID: bo.ID, BotName: "Bo", TurnID: "t3", UserChat: true}), map[string]any{"action": "list", "team_id": team.ID}, "only Ana, the lead of Ship, uses its board from its own chat"},
		"a routine's user_task": {session(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t4"}), map[string]any{"action": "user_task", "text": "X."}, "works only in your own chat, in a turn that the user's own message to you started"},
		"a step from the chat":  {lead, map[string]any{"action": "create", "title": "Parse", "brief": "b", "acceptance": "a"}, "from your own chat, update_board does only list and user_task; create works in a turn in your team"},
		"a task from the chat":  {lead, map[string]any{"action": "propose", "text": "X."}, "propose works in a turn in your team"},
		"user_task in a room":   {session(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t6", RoomID: room.ID, UserPost: true}), map[string]any{"action": "user_task", "text": "X."}, "works only in your own chat, in a turn that the user's own message to you started"},
		"user_task in the team": {session(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t5", RoomID: team.ID, BoardRole: core.RoleLead}), map[string]any{"action": "user_task", "text": "X."}, "user_task works only in your own chat with the user"},
	} {
		if text, isErr := c.call(c.args); !isErr || !strings.Contains(text, c.want) {
			t.Errorf("%s = %q (error %v); want an error with %q", what, text, isErr, c.want)
		}
	}
	if board, _ := h.svc.Board(ctx, team.ID); len(board.Inbox) != 1 || len(board.Items) != 0 {
		t.Fatalf("Ship's board after the refusals = %+v; want only the two tasks the lead added", board)
	}
}

// The tool and a lead's own prompt say what a lead may do from its own chat.
func TestALeadIsToldItAddsTheUsersTaskFromItsChat(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	team := core.Bot{ID: "t", Name: "Ship", Kind: core.KindRoom, Members: []core.Member{{ID: "a", Name: "Ana"}, {ID: "b", Name: "Bo"}}, Team: &core.Team{LeadID: "a"}}
	lead := systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Rooms: []core.Bot{team}, Now: now})
	line := `- You lead team "Ship" (id t). When the user asks you here to give work to your team, add it to the team's board yourself with update_board user_task`
	if !strings.Contains(lead, line) {
		t.Errorf("Ana's own prompt lacks %q", line)
	}
	if member := systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Bo"}, Rooms: []core.Bot{team}, Now: now}); strings.Contains(member, "user_task") {
		t.Error("a member's prompt tells it to add tasks")
	}
	if inTeam := systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Team: &team, InRoom: true, Rooms: []core.Bot{team}, Now: now}); strings.Contains(inTeam, "You lead team \"Ship\" (id t). When the user asks you here") {
		t.Error("a team turn carries the own-chat line")
	}
	plain := core.Bot{ID: "r", Name: "Plan", Kind: core.KindRoom, Members: team.Members}
	if inRoom := systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, InRoom: true, Rooms: []core.Bot{team, plain}, Now: now}); strings.Contains(inRoom, "user_task") {
		t.Error("a plain room turn tells the lead to add tasks with user_task, which is refused there")
	}
	if want := "From your own chat with the user, a team's lead can only list the board and add a task the user asked for with user_task"; !strings.Contains(updateBoardDescription, want) {
		t.Errorf("update_board lacks %q", want)
	}
}

// A lead's turn in a plain room carries no own-chat line: user_task is refused there.
func TestALeadsRoomTurnHasNoOwnChatLine(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	_, ana, bo, _ := h.newTeam(t)
	room, err := h.svc.CreateRoom(ctx, "Plan", "", []string{ana.ID, bo.ID})
	if err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, ana.ID, "hi", "n1"); err != nil {
		t.Fatal(err)
	}
	h.waitCalls(t, 1)
	if _, _, err := h.svc.SubmitUserMessage(ctx, room.ID, "@Ana what next?", "n2"); err != nil {
		t.Fatal(err)
	}
	calls := h.waitCalls(t, 2)
	const line = "When the user asks you here to give work to your team"
	if !strings.Contains(calls[0], line) {
		t.Errorf("Ana's own chat lacks the own-chat line")
	}
	if strings.Contains(calls[1], line) {
		t.Errorf("Ana's room turn carries the own-chat line")
	}
}
