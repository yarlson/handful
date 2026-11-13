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

var webAppSetup = map[string]any{
	"bots": []map[string]any{
		{"name": "Developer", "instructions": "Write the code with tests."},
		{"name": "Checker", "instructions": "Review each change."},
	},
	// goal_budget is what an older Builder still sends: it is valid, and dropped.
	"teams": []map[string]any{{"name": "Web app", "purpose": "Build the web app.", "members": []string{"Developer", "Checker", "ana"}, "agreement": "Tests pass.", "goal_budget": 7}},
	"rooms": []map[string]any{{"name": "Standup", "members": []string{"Developer", "Ana"}}},
}

func builderOf(t *testing.T, svc *core.Service) core.Bot {
	t.Helper()
	if err := svc.SeedBuilder(context.Background()); err != nil {
		t.Fatal(err)
	}
	bots, err := svc.ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(bots, func(b core.Bot) bool { return b.Role == core.RoleBuilder })
	if i < 0 {
		t.Fatalf("no Builder among %+v", bots)
	}
	return bots[i]
}

func TestCreateSetupIsOnlyBuildersAndCreatesNothingWithoutTheUsersCreate(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	builder := builderOf(t, h.svc)
	ana := h.newNamedBot(t, "Ana")
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
	builderSession := session(runContext{BotID: builder.ID, BotName: builder.Name, TurnID: "t1"})
	for cs, want := range map[*mcp.ClientSession]bool{builderSession: true, session(runContext{BotID: ana.ID, BotName: "Ana", TurnID: "t2"}): false} {
		listed, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := slices.ContainsFunc(listed.Tools, func(tool *mcp.Tool) bool { return tool.Name == "create_setup" }); got != want {
			t.Fatalf("create_setup listed = %v; want %v (only for Builder)", got, want)
		}
	}
	countBots := func() int {
		bots, err := h.svc.ListBots(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(bots)
	}
	before := countBots()

	text, isErr := callText(t, builderSession, "create_setup", map[string]any{"rooms": []map[string]any{{"name": "R", "members": []string{"Nobody"}}}})
	if !isErr || !strings.Contains(text, `no bot is named "Nobody"`) {
		t.Fatalf("create_setup with an unknown member = %q (error %v); want the reason", text, isErr)
	}
	if entries, _ := h.svc.Entries(ctx, builder.ID, 0, 0); slices.ContainsFunc(entries, func(e core.Entry) bool { return e.Kind == core.KindApproval }) {
		t.Fatal("a setup that fails its check showed a card")
	}

	decide := func(decision string) string {
		t.Helper()
		got := make(chan string, 1)
		go func() {
			text, _ := callText(t, builderSession, "create_setup", webAppSetup)
			got <- text
		}()
		card := h.waitEntry(t, builder.ID, func(e core.Entry) bool {
			return e.Kind == core.KindApproval && bodyField(e, "status") == "pending"
		})
		var body struct {
			Tool  string     `json:"tool"`
			Input setupInput `json:"input"`
		}
		if err := json.Unmarshal(card.Body, &body); err != nil || body.Tool != "create_setup" || len(body.Input.Bots) != 2 || body.Input.Rooms[0].Name != "Standup" {
			t.Fatalf("card = %s, %v; want the whole setup", card.Body, err)
		}
		if team := body.Input.Teams[0]; team.Lead != "Developer" || team.GoalBudget != 0 || !slices.Equal(team.Members, []string{"Developer", "Checker", "Ana"}) {
			t.Fatalf("card team = %+v; want the checked team: Ana by her name and the first member as lead, with no turn budget", team)
		}
		if err := h.svc.ResolveApproval(ctx, card.ID, decision); err != nil {
			t.Fatal(err)
		}
		return <-got
	}
	if text := decide(core.DecisionDeny); text != "Created nothing: The user denied this action. Do not create the setup another way." {
		t.Fatalf("create_setup after Cancel = %q", text)
	}
	if n := countBots(); n != before {
		t.Fatalf("bots after Cancel = %d; want %d", n, before)
	}

	text = decide(core.DecisionOnce)
	bots, err := h.svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	idOf := func(name string) string {
		i := slices.IndexFunc(bots, func(b core.Bot) bool { return b.Name == name })
		if i < 0 {
			t.Fatalf("no %q after Create; bots %+v", name, bots)
		}
		return bots[i].ID
	}
	want := "Created:\n" +
		`- bot "Developer" (id ` + idOf("Developer") + ")\n" +
		`- bot "Checker" (id ` + idOf("Checker") + ")\n" +
		`- team "Web app" (id ` + idOf("Web app") + "), led by Developer, with Developer, Checker, Ana\n" +
		`- room "Standup" (id ` + idOf("Standup") + "), with Developer, Ana\n" +
		"The new bots have not started yet. Do not message them; tell the user where to start."
	if text != want {
		t.Fatalf("create_setup after Create = %q; want %q", text, want)
	}
	if team, err := h.svc.GetBot(ctx, idOf("Web app")); err != nil || team.Team.GoalBudget != core.DefaultGoalBudget {
		t.Fatalf("created team = %+v, %v; want goal_budget 7 dropped, for the default %d", team.Team, err, core.DefaultGoalBudget)
	}
	turns, err := h.svc.QueuedTurns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range turns {
		if tr.BotID == idOf("Developer") || tr.BotID == idOf("Checker") {
			t.Fatalf("a new bot has a queued turn %+v; want none", tr)
		}
	}

	type answer struct {
		text  string
		isErr bool
	}
	got := make(chan answer, 1)
	go func() {
		text, isErr := callText(t, builderSession, "create_setup", map[string]any{"bots": []map[string]any{{"name": "Late", "instructions": "Arrive late."}}})
		got <- answer{text, isErr}
	}()
	card := h.waitEntry(t, builder.ID, func(e core.Entry) bool {
		return e.Kind == core.KindApproval && bodyField(e, "status") == "pending"
	})
	if _, err := h.svc.CreateBot(ctx, "late", ""); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ResolveApproval(ctx, card.ID, core.DecisionOnce); err != nil {
		t.Fatal(err)
	}
	if a := <-got; !a.isErr || !strings.Contains(a.text, `the name "Late" is in use`) {
		t.Fatalf("create_setup whose name was taken while the card waited = %q (error %v); want the failed check", a.text, a.isErr)
	}
}

func TestOnlyBuildersPromptExplainsHowToSetUpBots(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	groups := []core.Bot{{Name: "Web app", Kind: core.KindRoom}, {Name: "Lounge", Kind: core.KindRoom}}
	builder := systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Builder", Role: core.RoleBuilder}, Groups: groups, Now: now})
	for _, want := range []string{"## Setting up the user's bots", "<welcome>\n" + core.BuilderWelcome + "\n</welcome>", `Names that the user's rooms and teams use: "Web app", "Lounge".`, "Never add yourself to a team or room", "never with commands or the Handful API"} {
		if !strings.Contains(builder, want) {
			t.Errorf("Builder's prompt lacks %q", want)
		}
	}
	if alone := systemPrompt(promptContext{Bot: core.Bot{ID: "b", Name: "Builder", Role: core.RoleBuilder}, Now: now}); strings.Contains(alone, "Names that the user's rooms") {
		t.Error("Builder's prompt names rooms and teams although the user has none")
	}
	if other := systemPrompt(promptContext{Bot: core.Bot{ID: "a", Name: "Ana"}, Now: now}); strings.Contains(other, "create_setup") {
		t.Error("another bot's prompt mentions create_setup")
	}
}

func TestCreatedTextOfASetupWithoutNewBotsOnlyPointsToTheStart(t *testing.T) {
	t.Parallel()
	room := core.Bot{ID: "r1", Kind: core.KindRoom, Name: "Chat", Members: []core.Member{{ID: "a", Name: "Ana"}}}
	want := "Created:\n- room \"Chat\" (id r1), with Ana\nTell the user where to start."
	if got := createdText([]core.Bot{room}); got != want {
		t.Fatalf("createdText = %q; want %q", got, want)
	}
}

func TestBuildersTurnNamesTheUsersRoomsAndTeams(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "reply")
	ctx := context.Background()
	builder := builderOf(t, h.svc)
	ana := h.newNamedBot(t, "Ana")
	if _, err := h.svc.CreateRoom(ctx, "Lounge", "", []string{ana.ID}); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	if _, _, err := h.svc.SubmitUserMessage(ctx, builder.ID, "A personal assistant, please.", "n1"); err != nil {
		t.Fatal(err)
	}
	h.waitEntry(t, builder.ID, func(e core.Entry) bool { return e.Kind == core.KindBot && e.TurnID != "" })
	var builderCall string
	for _, call := range h.argsLog(t) {
		if strings.Contains(call, "## Setting up the user's bots") {
			builderCall = call
		}
	}
	if !strings.Contains(builderCall, `Names that the user's rooms and teams use: "Lounge".`) {
		t.Fatalf("Builder's turn does not name the room Lounge; calls with the Builder section: %q", builderCall)
	}
}
