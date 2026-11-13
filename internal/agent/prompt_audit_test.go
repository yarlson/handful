package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"handful/internal/core"
)

// wantText checks that text holds each of want and none of gone.
func wantText(t *testing.T, what, text string, want, gone []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q", what, w)
		}
	}
	for _, g := range gone {
		if strings.Contains(text, g) {
			t.Errorf("%s still says %q", what, g)
		}
	}
}

// Audit finding 3: a task has no turn limit; only the team's daily budget limits turns.
func TestBuilderSaysATeamHasNoTurnLimitUntilADailyBudget(t *testing.T) {
	t.Parallel()
	builder := systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Builder", Role: core.RoleBuilder}, Now: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)})
	wantText(t, "Builder's prompt", builder,
		[]string{"A team has no turn limit until the user sets a daily budget in the team's settings."},
		[]string{"30 turns", "budget of turns"})
}

// Audit finding 8: in a team turn send_to_agent only posts to a room, waking no one.
func TestSendToAgentInATeamTurnSaysItOnlyPostsToARoom(t *testing.T) {
	t.Parallel()
	team := sendToAgentDescription(true)
	wantText(t, "send_to_agent in a team turn", team,
		[]string{"In a team turn you cannot message a bot: to ask a teammate to act, post in the team's channel with send_to_user and write @Name."},
		[]string{"message them directly", "Message one of the user's other bots"})
	if outside := sendToAgentDescription(false); !strings.Contains(outside, "To ask a teammate to act, message them directly.") {
		t.Errorf("send_to_agent outside a team = %q; want it to say to message a teammate", outside)
	}
}

// Audit finding 5: a reviewer can't use Edit or Write in any turn in the team while its step is in
// review. The denial names the step, and the work and lead briefs say so.
func TestAReviewersEditBlockIsSaidInEveryTurnAndNamesTheStep(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, bo, _ := h.newTeam(t)
	items := h.newGoal(t, team, boardItem("Parse", "Parse it.", bo.ID, ana.ID))
	if err := h.svc.StartItemWork(ctx, items[0].ID, bo.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.SubmitItem(ctx, core.Actor{BotID: bo.ID, TeamID: team.ID, Role: core.RoleWork}, items[0].ID, "Parsed."); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	token, err := h.runner.tokens.issue(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t1", RoomID: team.ID})
	if err != nil {
		t.Fatal(err)
	}
	work, err := mcpSession(t, srv.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { work.Close() })
	text, isErr := callText(t, work, "approve", map[string]any{"tool_name": "Write", "input": map[string]any{"file_path": "a.go"}})
	want := `You review "Parse", in review in this team: Edit and Write are denied in every turn of yours in the team until it leaves review.`
	var answer struct{ Behavior, Message string }
	if err := json.Unmarshal([]byte(text), &answer); isErr || err != nil || answer.Behavior != "deny" || !strings.Contains(answer.Message, want) {
		t.Fatalf("approve Write by Parse's reviewer in another turn = %q (error %v); want a denial with %q", text, isErr, want)
	}

	board := core.Board{Items: []core.Item{
		{ID: "i1", Title: "Parse", State: core.ItemReview, OwnerID: "bo", ReviewerID: "ana"},
		{ID: "i2", Title: "Docs", State: core.ItemDoing, OwnerID: "ana"},
	}}
	wantText(t, "Ana's work brief", workBrief(board, "i2", nil),
		[]string{`You review "Parse", which is in review: Edit and Write are denied in every turn of yours in this team until it leaves review.`}, nil)
	wantText(t, "Bo's work brief", workBrief(core.Board{Items: []core.Item{{ID: "i3", Title: "Docs", State: core.ItemDoing, OwnerID: "bo"}}}, "i3", nil),
		nil, []string{"which is in review"})
	wantText(t, "the lead's brief", leadBrief,
		[]string{"A member who reviews a step that is in review can't use Edit or Write in any turn in this team until that step leaves review."}, nil)
}

// Audit findings 6, 7 and 11–16: the smaller statements match the code.
func TestTheSmallerStatementsMatchTheCode(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	ana := core.Bot{ID: "a", Name: "Ana"}
	team := core.Bot{ID: "t", Name: "Ship", Kind: core.KindRoom, Members: []core.Member{{ID: "a", Name: "Ana"}, {ID: "b", Name: "Bo"}}, Team: &core.Team{LeadID: "a"}}
	rooms := []core.Bot{team, {ID: "r", Name: "Reports", Kind: core.KindRoom}}
	solo := systemPrompt(promptContext{Bot: ana, Teammates: []core.Bot{{ID: "b", Name: "Bo"}}, Now: now})
	inTeam := systemPrompt(promptContext{Bot: ana, Team: &team, Rooms: rooms, Now: now})
	builder := systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Builder", Role: core.RoleBuilder}, Now: now})
	sent := "you are woken when that team asks you, pushes back or delivers; your board shows its state under \"Sent to other teams\" meanwhile"
	// 6.
	wantText(t, "the lead brief", leadBrief, []string{sent}, []string{"you hear back in this channel"})
	// 7.
	wantText(t, "the retro brief", retroBrief, []string{"This task was approved."}, []string{"The user accepted"})
	// 11.
	userStart := "except a task the user starts at once: it becomes the Now task, or the first of"
	wantText(t, "the gatekeeper text", leadGatekeeper, []string{userStart}, []string{"the user's too"})
	wantText(t, "update_board", updateBoardDescription, []string{userStart}, []string{"the user's too"})
	// 12.
	wantText(t, "a bot's prompt", solo, []string{"Plain assistant text is private, with one exception: when the user wrote to you in your own chat and you sent nothing, your final text is posted as your reply."}, []string{"private and never shown"})
	// 13.
	wantText(t, "a bot's prompt", solo, []string{"priority false is read at their next turn outside a team"}, nil)
	// 14.
	wantText(t, "Builder's prompt", builder, []string{"The user's post wakes the bots it @mentions, or every member when it mentions none; a bot's post wakes only the bots it @mentions."}, nil)
	// 15.
	wantText(t, "a team turn's prompt", inTeam, []string{`- Rooms you can post a report to with send_to_agent, with no @mention: "Reports" (id r).`}, []string{`"Ship" (id t), "Reports"`})
	if alone := systemPrompt(promptContext{Bot: ana, Team: &team, Rooms: rooms[:1], Now: now}); strings.Contains(alone, "Rooms you can post a report to") {
		t.Error("a team turn lists report rooms although the bot is in none but the team")
	}
	// 16.
	files := agentMessageFields(t)["files"]
	wantText(t, "send_to_agent's files", files, []string{"a relative path is in the team's shared folder when the room is a team, else in your own folder"}, nil)
	// 13, the tool's side.
	wantText(t, "send_to_agent's priority", agentMessageFields(t)["priority"], []string{"at their next turn outside a team"}, nil)
}

// agentMessageFields returns the jsonschema description of each field of send_to_agent's input.
func agentMessageFields(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	typ := reflect.TypeFor[agentMessageInput]()
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		out[name] = f.Tag.Get("jsonschema")
	}
	return out
}
