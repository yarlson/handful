package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"handful/internal/core"
)

// membersTeam is team "Ship": Lea leads on opus at high effort, Rex reviews with no model or
// effort of his own, Mia sets only her effort, and Me has none. The team's default model is
// sonnet; it sets no effort.
func membersTeam() *core.Bot {
	return &core.Bot{ID: "ship-id", Name: "Ship", Kind: core.KindRoom, Members: []core.Member{
		{ID: "lea-id", Name: "Lea", Model: "opus", Effort: "high"},
		{ID: "rex-id", Name: "Rex"},
		{ID: "mia-id", Name: "Mia", Effort: "low"},
		{ID: "me-id", Name: "Me"},
	}, Team: &core.Team{LeadID: "lea-id", ReviewerID: "rex-id", DefaultModel: "sonnet"}}
}

// Each member line shows the member's role, and the model and effort of its turns in the team:
// its own, else the team's default, else the global value, else "default". The reader's own
// line names only the reader and its role. The lines are the same for the lead, the reviewer
// and a member; the own chat and a plain room show none of them.
func TestTeamMemberLinesShowRoleModelAndEffortToEveryMember(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	teammates := []core.Bot{{ID: "lea-id", Name: "Lea", Instructions: "Plans the work."}, {ID: "rex-id", Name: "Rex", Instructions: "Reviews changes."}, {ID: "mia-id", Name: "Mia"}, {ID: "me-id", Name: "Me"}}
	lines := map[string]string{
		"lea-id": "- Lea (id lea-id, the lead, opus · high): Plans the work.\n",
		"rex-id": "- Rex (id rex-id, the reviewer, sonnet · default): Reviews changes.\n",
		"mia-id": "- Mia (id mia-id, sonnet · low): no instructions yet\n",
		"me-id":  "- Me (id me-id, sonnet · default): no instructions yet\n",
	}
	own := map[string]string{"lea-id": "- you (Lea, the lead)\n", "rex-id": "- you (Rex, the reviewer)\n", "mia-id": "- you (Mia)\n", "me-id": "- you (Me)\n"}
	for _, reader := range []string{"lea-id", "rex-id", "mia-id", "me-id"} {
		got := systemPrompt(promptContext{Bot: core.Bot{ID: reader}, Team: membersTeam(), InRoom: true, Teammates: teammates, Now: now})
		for id, line := range lines {
			if id == reader {
				line = own[id]
			}
			if !strings.Contains(got, line) {
				t.Errorf("reader %s: the team section lacks %q", reader, line)
			}
		}
	}

	global := core.Defaults{Model: "haiku", ModelSource: core.SourceGlobal, Effort: "xhigh", EffortSource: core.SourceGlobal}
	got := systemPrompt(promptContext{Bot: core.Bot{ID: "lea-id"}, Team: membersTeam(), InRoom: true, Defaults: global, Now: now})
	if want := "- Rex (id rex-id, the reviewer, sonnet · xhigh): "; !strings.Contains(got, want) {
		t.Errorf("with a global effort the prompt lacks %q", want)
	}
	noDefault := membersTeam()
	noDefault.Team.DefaultModel = ""
	got = systemPrompt(promptContext{Bot: core.Bot{ID: "lea-id"}, Team: noDefault, InRoom: true, Defaults: global, Now: now})
	if want := "- Mia (id mia-id, haiku · low): "; !strings.Contains(got, want) {
		t.Errorf("with no team default the prompt lacks %q", want)
	}

	for name, pc := range map[string]promptContext{
		"own chat": {Bot: core.Bot{ID: "lea-id"}, Rooms: []core.Bot{*membersTeam()}, Teammates: teammates[1:], Now: now},
		"room":     {Bot: core.Bot{ID: "lea-id"}, InRoom: true, Rooms: []core.Bot{*membersTeam()}, Teammates: teammates[1:], Now: now},
	} {
		got := systemPrompt(pc)
		if !strings.Contains(got, "- Rex (id rex-id): Reviews changes.\n") || strings.Contains(got, " · ") {
			t.Errorf("%s: the teammates lines changed:\n%s", name, got)
		}
	}
}

// A member's new effort, and a new reviewer, change the team's member lines: the next turn of
// each member whose line changed carries the "## Your team" section once, and the turn after
// carries none.
func TestAMembersEffortOrTheReviewerReachesTheTeamOnceAsAContextUpdate(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, _, bo, _ := h.newTeam(t)
	h.start(t)
	h.say(t, team.ID, "plan")
	h.say(t, team.ID, "@Bo check the tests")

	high := "high"
	if _, err := h.svc.UpdateBotSettings(ctx, bo.ID, core.BotPatch{Effort: &high}); err != nil {
		t.Fatal(err)
	}
	effort := contextBlock(h.say(t, team.ID, "next"))
	if !strings.Contains(effort, "## Your team\n") || !strings.Contains(effort, "- Bo (id "+bo.ID+", default · high): ") {
		t.Fatalf("the lead's update after Bo's new effort =\n%s\nwant the Your team section with Bo on high", effort)
	}
	if block := contextBlock(h.say(t, team.ID, "again")); block != "" {
		t.Fatalf("the lead's next turn carries the update again:\n%s", block)
	}

	if _, err := h.svc.UpdateTeam(ctx, team.ID, core.TeamPatch{ReviewerID: &bo.ID}); err != nil {
		t.Fatal(err)
	}
	lead := contextBlock(h.say(t, team.ID, "who reviews?"))
	if !strings.Contains(lead, "- Bo (id "+bo.ID+", the reviewer, default · high): ") {
		t.Fatalf("the lead's update after the new reviewer =\n%s\nwant Bo as the reviewer", lead)
	}
	member := contextBlock(h.say(t, team.ID, "@Bo you review now"))
	if !strings.Contains(member, "## Your team\n") || !strings.Contains(member, "- you (Bo, the reviewer)\n") {
		t.Fatalf("Bo's update after the new reviewer =\n%s\nwant the Your team section with Bo as the reviewer", member)
	}
	for _, text := range []string{"lead again", "@Bo and again"} {
		if block := contextBlock(h.say(t, team.ID, text)); block != "" {
			t.Fatalf("turn %q carries the update again:\n%s", text, block)
		}
	}
}

// list_members shows the reviewer, then each member in the team's order with its role, model and
// effort, its steps on the Now task that are not done or dropped, and its full description,
// indented.
func TestListMembersShowsRolesModelsNowStepsAndFullDescriptions(t *testing.T) {
	t.Parallel()
	board := core.Board{Goal: &core.Goal{ID: "g1", State: core.GoalActive}, Items: []core.Item{
		{ID: "i1", Title: "Write the article", State: core.ItemDoing, OwnerID: "mia-id", ReviewerID: "rex-id"},
		{ID: "i2", Title: "Check the code", State: core.ItemTodo, OwnerID: "rex-id"},
		{ID: "i3", Title: "Plan the outline", State: core.ItemDone, OwnerID: "me-id", ReviewerID: "rex-id"},
		{ID: "i4", Title: "Old draft", State: core.ItemDropped, OwnerID: "me-id"},
	}}
	instructions := map[string]string{"lea-id": "Plans the work.\nSplits each task into steps.\n", "rex-id": "Reviews changes."}
	want := `Team "Ship" (id ship-id). Reviewer: Rex.
- Lea (id lea-id): lead · opus · high. Now: no step.
  Plans the work.
  Splits each task into steps.
- Rex (id rex-id): reviewer · sonnet · default. Now: reviews "Write the article" (doing); owns "Check the code" (todo).
  Reviews changes.
- Mia (id mia-id): member · sonnet · low. Now: owns "Write the article" (doing).
  no instructions yet
- Me (id me-id): member · sonnet · default. Now: no step.
  no instructions yet
`
	if got := membersList(*membersTeam(), board, instructions, core.Defaults{}); got != want {
		t.Errorf("membersList =\n%s\nwant\n%s", got, want)
	}

	noReviewer := membersTeam()
	noReviewer.Team.ReviewerID = ""
	got := membersList(*noReviewer, board, instructions, core.Defaults{})
	for _, line := range []string{`Team "Ship" (id ship-id). Reviewer: none.` + "\n", "- Rex (id rex-id): member · sonnet · default. Now: reviews \"Write the article\" (doing); owns \"Check the code\" (todo).\n"} {
		if !strings.Contains(got, line) {
			t.Errorf("with no reviewer the list lacks %q:\n%s", line, got)
		}
	}

	for name, goal := range map[string]*core.Goal{"no task": nil, "a delivered task": {ID: "g1", State: core.GoalDelivered}} {
		got := membersList(*membersTeam(), core.Board{Goal: goal, Items: board.Items}, instructions, core.Defaults{})
		if n := strings.Count(got, "Now: no step."); n != 4 {
			t.Errorf("%s: %d members with no step; want all 4:\n%s", name, n, got)
		}
	}
}

// list_members is offered to a bot in a team, in its team turns and its own chat, and lists the
// team of a team turn, or from the own chat the bot's only team or the team it names. It refuses
// another team in a team turn, a missing team_id in several teams, and a plain room.
func TestListMembersListsTheTeamOfTheTurnOrOfTheOwnChat(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	team, ana, bo, cy := h.newTeam(t)
	instructions, reviewer := "Writes the code.\nRuns the tests.", bo.ID
	if _, err := h.svc.UpdateBot(ctx, bo.ID, nil, &instructions); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.UpdateTeam(ctx, team.ID, core.TeamPatch{ReviewerID: &reviewer}); err != nil {
		t.Fatal(err)
	}
	h.newGoal(t, team, core.NewItem{Title: "Parse", Brief: "Parse it.", Acceptance: "Tests pass.", OwnerID: ana.ID, ReviewerID: bo.ID})
	room, err := h.svc.CreateRoom(ctx, "Chat", "", []string{ana.ID, bo.ID})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.runner.MCPHandler())
	t.Cleanup(srv.Close)
	session := func(rc runContext) *mcp.ClientSession {
		t.Helper()
		token, err := h.runner.tokens.issue(rc)
		if err != nil {
			t.Fatal(err)
		}
		cs, err := mcpSession(t, srv.URL, token)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs
	}
	ship := `Team "Ship" (id ` + team.ID + `). Reviewer: Bo.
- Ana (id ` + ana.ID + `): lead · default · default. Now: owns "Parse" (todo).
  no instructions yet
- Bo (id ` + bo.ID + `): reviewer · default · default. Now: reviews "Parse" (todo).
  Writes the code.
  Runs the tests.
`
	leadTurn := session(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t1", RoomID: team.ID, UserPost: true})
	memberTurn := session(runContext{BotID: bo.ID, BotName: "Bo", TurnID: "t2", RoomID: team.ID, BoardRole: core.RoleWork})
	routine := session(runContext{BotID: bo.ID, BotName: "Bo", TurnID: "t3"})
	roomTurn := session(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t4", RoomID: room.ID, UserPost: true})
	outside := session(runContext{BotID: cy.ID, BotName: "Cy", TurnID: "t5"})

	for name, cs := range map[string]*mcp.ClientSession{"lead's team turn": leadTurn, "member's team turn": memberTurn, "own chat": routine, "outside": outside} {
		listed, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(listed.Tools, func(tool *mcp.Tool) bool { return tool.Name == "list_members" })
		if want := name != "outside"; (i >= 0) != want {
			t.Fatalf("%s: list_members listed = %v; want %v (only for a bot in a team)", name, i >= 0, want)
		}
		if i < 0 {
			continue
		}
		schema, _ := json.Marshal(listed.Tools[i].InputSchema)
		if listed.Tools[i].Description != listMembersDescription || string(schema) != `{"additionalProperties":false,"properties":{"team_id":{"description":"the team's id; needed from your own chat when you are in several teams","type":"string"}},"type":"object"}` {
			t.Errorf("%s: list_members = %q, schema %s", name, listed.Tools[i].Description, schema)
		}
	}

	docs, err := h.svc.CreateTeam(ctx, "Docs", "Write docs.", []string{ana.ID, cy.ID}, core.TeamSettings{LeadID: cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name    string
		cs      *mcp.ClientSession
		args    map[string]any
		want    string
		wantErr bool
	}{
		{"lead's team turn", leadTurn, nil, ship, false},
		{"member's team turn", memberTurn, map[string]any{"team_id": team.ID}, ship, false},
		{"another team in a team turn", leadTurn, map[string]any{"team_id": docs.ID}, "in a turn in team Ship, list_members lists only that team; list another team you are in from your own chat", true},
		{"routine in the only team", routine, nil, ship, false},
		{"own chat, team not joined", routine, map[string]any{"team_id": docs.ID}, "invalid input: you are in no team with id " + docs.ID, true},
		{"plain room", roomTurn, nil, "list_members works in a team turn or in your own chat", true},
	} {
		got, isErr := callText(t, c.cs, "list_members", c.args)
		if got != c.want || isErr != c.wantErr {
			t.Errorf("%s: list_members = %q (error %v); want %q (error %v)", c.name, got, isErr, c.want, c.wantErr)
		}
	}

	ownChat := session(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t6", UserChat: true})
	want := "invalid input: you are in several teams; name one with team_id: Ship (id " + team.ID + "), Docs (id " + docs.ID + ")"
	if got, isErr := callText(t, ownChat, "list_members", nil); got != want || !isErr {
		t.Errorf("several teams without team_id = %q (error %v); want %q", got, isErr, want)
	}
	if got, isErr := callText(t, ownChat, "list_members", map[string]any{"team_id": docs.ID}); isErr || !strings.HasPrefix(got, `Team "Docs" (id `+docs.ID+`). Reviewer: none.`) {
		t.Errorf("several teams with team_id = %q (error %v); want team Docs", got, isErr)
	}
}
