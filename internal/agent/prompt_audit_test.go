package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"regexp"
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

// boardWord finds "goal" and "item" as words; the schema names goal_id and item_id are not words.
var boardWord = regexp.MustCompile(`(?i)\b(goals?|items?)\b`)

// Audit finding 9: the texts about the board say task and step, as the app does, and /task.
func TestTheBoardTextsSayTaskAndStep(t *testing.T) {
	t.Parallel()
	builder := systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Builder", Role: core.RoleBuilder}, Now: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)})
	setup := builder[strings.Index(builder, "## Setting up the user's bots"):]
	texts := map[string]string{
		"lead brief": leadBrief, "retro brief": retroBrief, "approve brief": approveBrief, "inbox brief": inboxBrief,
		"gatekeeper": leadGatekeeper, "member text": memberWork, "update_board": updateBoardDescription,
		"Builder's guide": strings.ReplaceAll(setup, "goal_budget", ""),
	}
	typ := reflect.TypeFor[boardInput]()
	for i := range typ.NumField() {
		texts["update_board field "+typ.Field(i).Name] = typ.Field(i).Tag.Get("jsonschema")
	}
	for what, text := range texts {
		if word := boardWord.FindString(strings.NewReplacer("goal_id", "", "item_id", "").Replace(text)); word != "" {
			t.Errorf("%s says %q: %s", what, word, text)
		}
	}
	wantText(t, "Builder's guide", setup, []string{"send a task with /task <what the team should do>"}, []string{"/goal"})
	solo := systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Now: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)})
	wantText(t, "a bot's prompt", solo, []string{"(a request, a task or a step)", "for example with the id of a task or a step"}, []string{"a board item", "work-item"})
	board := core.Board{Goal: &core.Goal{ID: "g1", Text: "Ship.", State: core.GoalActive}, Items: []core.Item{{ID: "i1", Title: "Parse", State: core.ItemDoing, OwnerID: "a"}}}
	wantText(t, "a work brief", workBrief(board, "i1", nil), []string{`Your step: "Parse" (id i1).`, "Do this step now, in this turn, and only this step."}, []string{"item now", "Your item"})
}

// Audit finding 10: bots read a task's state in the app's words, not as it is stored.
func TestBotsReadATasksStateInTheAppsWords(t *testing.T) {
	t.Parallel()
	for stored, want := range map[string]string{
		core.GoalActive: "in progress", core.GoalDelivered: "done, waiting for approval", core.GoalIncoming: "in the inbox",
		core.GoalQueued: "in the plan", core.GoalDeclined: "pushed back", core.GoalEnded: "withdrawn",
		core.GoalPaused: "paused", core.GoalDone: "done", core.GoalLater: "later", core.GoalStopped: "stopped",
	} {
		if got := taskState(stored); got != want {
			t.Errorf("taskState(%q) = %q; want %q", stored, got, want)
		}
	}
	team := core.Bot{ID: "t", Team: &core.Team{}}
	now := boardTable(team, core.Board{Goal: &core.Goal{ID: "g1", Text: "Ship.", State: core.GoalActive, Used: 2}}, time.Time{})
	wantText(t, "the board", now, []string{"Now task (id g1, in progress, 2 turns so far): Ship.\nSteps:\n"}, []string{"Goal (id", "Items:", "active"})
	over := boardTable(team, core.Board{Goal: &core.Goal{ID: "g1", Text: "Ship.", State: core.GoalDone}}, time.Time{})
	wantText(t, "a retro's board", over, []string{"Task (id g1, done, 0 turns so far): Ship."}, []string{"Now task"})
	sent := boardTable(team, core.Board{Sent: []core.Goal{{ID: "s", Text: "Guide.", TeamID: "d", State: core.GoalIncoming}}, Teams: map[string]string{"d": "Docs"}}, time.Time{})
	wantText(t, "the sent tasks", sent, []string{`to team "Docs" [in the inbox]`}, []string{"[incoming]"})
}

// Audit finding 18 (the user's option A): every prompt variant tells the bot to read a repository's
// CLAUDE.md and AGENTS.md, word for word, since claude doesn't load CLAUDE.md.
func TestEveryPromptVariantSaysToReadARepositorysClaudeMD(t *testing.T) {
	t.Parallel()
	const line = "Claude Code does not load a project's CLAUDE.md; when you work in a repository, read its CLAUDE.md and AGENTS.md first."
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	ana := core.Bot{ID: "a", Name: "Ana", Instructions: "Write code."}
	team := core.Bot{ID: "t", Name: "Ship", Kind: core.KindRoom, Members: []core.Member{{ID: "a", Name: "Ana"}, {ID: "b", Name: "Bo"}}, Team: &core.Team{LeadID: "a"}}
	member := core.Bot{ID: "b", Name: "Bo"}
	for what, pc := range map[string]promptContext{
		"a bot":              {Bot: ana, Now: now},
		"a bot with rules":   {Bot: ana, Global: "Use tabs.", Now: now},
		"Builder":            {Bot: core.Bot{ID: "bl", Name: "Builder", Role: core.RoleBuilder}, Now: now},
		"a team's lead":      {Bot: ana, Team: &team, Now: now},
		"a team's member":    {Bot: member, Team: &team, Global: "Use tabs.", Now: now},
		"a bot with friends": {Bot: ana, Teammates: []core.Bot{member}, Rooms: []core.Bot{{ID: "r", Name: "Plan", Kind: core.KindRoom}}, Now: now},
	} {
		if got := systemPrompt(pc); strings.Count(got, line) != 1 {
			t.Errorf("%s's prompt has the CLAUDE.md line %d times; want once", what, strings.Count(got, line))
		}
	}
}

// Audit finding 17: a real run shows the executor subagent gets Handful's MCP tools, send_to_user
// among them, so its prompt forbids messaging instead of saying it can't.
func TestTheExecutorIsToldNotToMessageTheUser(t *testing.T) {
	t.Parallel()
	prompt, _ := executorAgents("Ana")["executor"].(map[string]any)["prompt"].(string)
	wantText(t, "the executor's prompt", prompt,
		[]string{"Do not message the user or other bots, and do not ask questions: send_to_user and send_to_agent are the parent's to use."},
		[]string{"You cannot talk to the user"})
}
