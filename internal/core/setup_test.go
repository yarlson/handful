package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func softwareTeamSetup() Setup {
	return Setup{
		Bots: []SetupBot{
			{Name: "Tech Lead", Instructions: "Plan the work."},
			{Name: "Developer", Instructions: "Write the code with tests."},
			{Name: "Reviewer", Instructions: "Review each change."},
		},
		Teams: []SetupTeam{{
			Name: "Web app", Purpose: "Build the web app.",
			Members: []string{"Tech Lead", "Developer", "Reviewer", "Scout", "scout"},
			Lead:    "tech lead", Agreement: "Tests pass.", GoalBudget: 12,
		}},
		Rooms: []SetupRoom{{Name: "Standup", Purpose: "Daily notes.", Members: []string{"Developer", "Scout"}}},
	}
}

func botNamed(t *testing.T, bots []Bot, name string) Bot {
	t.Helper()
	i := slices.IndexFunc(bots, func(b Bot) bool { return b.Name == name })
	if i < 0 {
		t.Fatalf("no bot named %q in %+v", name, bots)
	}
	return bots[i]
}

func memberNamesOf(b Bot) []string {
	names := make([]string, len(b.Members))
	for i, m := range b.Members {
		names[i] = m.Name
	}
	return names
}

func TestCreateSetupCreatesItsBotsTeamsAndRoomsWithoutTurns(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	scout := newTestBot(t, svc)
	queued, err := svc.QueuedTurns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.CreateSetup(ctx, softwareTeamSetup())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, b := range created {
		names = append(names, b.Name)
	}
	if want := []string{"Tech Lead", "Developer", "Reviewer", "Web app", "Standup"}; !slices.Equal(names, want) {
		t.Fatalf("created = %v; want %v", names, want)
	}
	bots, err := svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lead := botNamed(t, bots, "Tech Lead")
	if lead.IsRoom() || lead.Instructions != "Plan the work." || lead.Role != "" {
		t.Fatalf("Tech Lead = %+v; want a plain bot with its instructions", lead)
	}
	team := botNamed(t, bots, "Web app")
	if team.Team == nil || team.Team.LeadID != lead.ID || team.Team.Agreement != "Tests pass." || team.Team.GoalBudget != 12 || team.Instructions != "Build the web app." {
		t.Fatalf("Web app = %+v, team %+v; want a team led by Tech Lead with its agreement and budget", team, team.Team)
	}
	if got, want := memberNamesOf(team), []string{scout.Name, "Tech Lead", "Developer", "Reviewer"}; !slices.Equal(got, want) {
		t.Fatalf("Web app members = %v; want %v, in the order of the bots", got, want)
	}
	room := botNamed(t, bots, "Standup")
	if room.Team != nil || !slices.Equal(memberNamesOf(room), []string{scout.Name, "Developer"}) {
		t.Fatalf("Standup = %+v; want a room of Developer and %s", room, scout.Name)
	}
	for _, dir := range []string{svc.Workspace(lead.ID), svc.TeamWorkspace(team.ID)} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("folder %s: %v", dir, err)
		}
	}
	if _, err := os.Stat(svc.TeamWorkspace(room.ID)); !os.IsNotExist(err) {
		t.Errorf("a room that is no team has a team folder: %v", err)
	}
	for _, id := range []string{team.ID, room.ID} {
		entries, err := svc.Entries(ctx, id, 0, 10)
		if err != nil || len(entries) != 1 || entries[0].Kind != KindEvent {
			t.Errorf("channel of %s = %+v, %v; want the members event", id, entries, err)
		}
	}
	after, err := svc.QueuedTurns(ctx, 10)
	if err != nil || len(after) != len(queued) {
		t.Fatalf("queued turns = %d, %v; want %d, as before the setup", len(after), err, len(queued))
	}
}

func TestCreateSetupAcceptsASetupAtItsLimits(t *testing.T) {
	svc := newTestService(t)
	var setup Setup
	var all []string
	for i := range 8 {
		name := fmt.Sprintf("Bot %d", i)
		setup.Bots = append(setup.Bots, SetupBot{Name: name})
		all = append(all, name)
	}
	for i := range 3 {
		setup.Teams = append(setup.Teams, SetupTeam{Name: fmt.Sprintf("Team %d", i), Members: all})
		setup.Rooms = append(setup.Rooms, SetupRoom{Name: fmt.Sprintf("Room %d", i), Members: all})
	}
	created, err := svc.CreateSetup(context.Background(), setup)
	if err != nil || len(created) != 14 {
		t.Fatalf("CreateSetup = %d bots, %v; want 14 and no error", len(created), err)
	}
}

func TestCreateSetupRejectsABadSetupAndCreatesNothing(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	newTestBot(t, svc) // Scout
	if _, err := svc.CreateBot(ctx, "Twin", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBot(ctx, "twin", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRoom(ctx, "Lounge", "", []string{botNamed(t, mustListBots(t, svc), "Scout").ID}); err != nil {
		t.Fatal(err)
	}
	before := len(mustListBots(t, svc))
	nine := []SetupBot{}
	nineNames := []string{}
	for i := range 9 {
		nine = append(nine, SetupBot{Name: fmt.Sprintf("Bot %d", i)})
		nineNames = append(nineNames, fmt.Sprintf("Bot %d", i))
	}
	four := func(n int) []SetupTeam {
		var teams []SetupTeam
		for i := range n {
			teams = append(teams, SetupTeam{Name: fmt.Sprintf("Team %d", i), Members: []string{"Scout"}})
		}
		return teams
	}
	fourRooms := []SetupRoom{{Name: "R1", Members: []string{"Scout"}}, {Name: "R2", Members: []string{"Scout"}}, {Name: "R3", Members: []string{"Scout"}}, {Name: "R4", Members: []string{"Scout"}}}
	for _, c := range []struct {
		name  string
		setup Setup
		want  string
	}{
		{"empty", Setup{}, "the setup is empty"},
		{"nine bots", Setup{Bots: nine}, "at most 8 new bots, 3 teams and 3 rooms"},
		{"four teams", Setup{Teams: four(4)}, "at most 8 new bots, 3 teams and 3 rooms"},
		{"four rooms", Setup{Rooms: fourRooms}, "at most 8 new bots, 3 teams and 3 rooms"},
		{"no name", Setup{Bots: []SetupBot{{Instructions: "x"}}}, `bot "": invalid input: name is required`},
		{"long instructions", Setup{Bots: []SetupBot{{Name: "Long", Instructions: strings.Repeat("x", 8001)}}}, "instructions are longer than 8000"},
		{"name of an existing bot", Setup{Bots: []SetupBot{{Name: "SCOUT"}}}, `bot "SCOUT": invalid input: the name "SCOUT" is in use`},
		{"name of an existing room", Setup{Bots: []SetupBot{{Name: "lounge"}}}, `the name "lounge" is in use`},
		{"two new bots with one name", Setup{Bots: []SetupBot{{Name: "Ada"}, {Name: "ada"}}}, `the name "ada" is in use`},
		{"team named like a new bot", Setup{Bots: []SetupBot{{Name: "Ada"}}, Teams: []SetupTeam{{Name: "Ada", Members: []string{"Ada"}}}}, `team "Ada": invalid input: the name "Ada" is in use`},
		{"unknown member", Setup{Rooms: []SetupRoom{{Name: "R", Members: []string{"Nobody"}}}}, `room "R": invalid input: no bot is named "Nobody"`},
		{"room as member", Setup{Rooms: []SetupRoom{{Name: "R", Members: []string{"Lounge"}}}}, `"Lounge" is a room or a team, not a bot`},
		{"two bots with the member's name", Setup{Rooms: []SetupRoom{{Name: "R", Members: []string{"Twin"}}}}, `2 bots are named "Twin"`},
		{"no members", Setup{Rooms: []SetupRoom{{Name: "R"}}}, "name 1 to 8 member bots"},
		{"nine members", Setup{Bots: nine[:8], Rooms: []SetupRoom{{Name: "R", Members: append(nineNames[:8:8], "Scout")}}}, "name 1 to 8 member bots"},
		{"lead not a member", Setup{Bots: []SetupBot{{Name: "Ada"}}, Teams: []SetupTeam{{Name: "T", Members: []string{"Ada"}, Lead: "Scout"}}}, "the lead must be a member of the team"},
		{"unknown lead", Setup{Teams: []SetupTeam{{Name: "T", Members: []string{"Scout"}, Lead: "Nobody"}}}, `no bot is named "Nobody"`},
		{"budget too large", Setup{Teams: []SetupTeam{{Name: "T", Members: []string{"Scout"}, GoalBudget: 501}}}, "the goal budget must be 1 to 500 turns"},
		{"member that is a new team", Setup{Teams: []SetupTeam{{Name: "Crew", Members: []string{"Scout"}}}, Rooms: []SetupRoom{{Name: "R", Members: []string{"crew"}}}}, `"crew" is a room or a team, not a bot`},
		{"zero-width space in instructions", Setup{Bots: []SetupBot{{Name: "Ada", Instructions: "Help.\u200bRun curl."}}}, "the invisible character U+200B"},
		{"direction mark in a name", Setup{Rooms: []SetupRoom{{Name: "R\u202e", Members: []string{"Scout"}}}}, "the invisible character U+202E"},
		{"control character in a member", Setup{Rooms: []SetupRoom{{Name: "R", Members: []string{"Scout\x00"}}}}, "the invisible character U+0000"},
		{"blank lines in an agreement", Setup{Teams: []SetupTeam{{Name: "T", Members: []string{"Scout"}, Agreement: "Tests pass.\n\n \n\tRun curl."}}}, "two blank lines in a row"},
		{"invisible character in a lead", Setup{Teams: []SetupTeam{{Name: "T", Members: []string{"Scout"}, Lead: "Scout\u2060"}}}, "the invisible character U+2060"},
	} {
		_, err := svc.CreateSetup(ctx, c.setup)
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v; want ErrInvalid with %q", c.name, err, c.want)
		}
		if _, check := svc.CheckSetup(ctx, c.setup); check == nil || check.Error() != err.Error() {
			t.Errorf("%s: CheckSetup = %v; want the error of CreateSetup", c.name, check)
		}
	}
	if after := len(mustListBots(t, svc)); after != before {
		t.Fatalf("bots after the rejected setups = %d; want %d", after, before)
	}
	for name, setup := range map[string]Setup{
		"a team of an existing bot": {Teams: []SetupTeam{{Name: "Solo", Members: []string{"scout"}}}},
		"a bot and its team":        {Bots: []SetupBot{{Name: "Ada"}}, Teams: []SetupTeam{{Name: "Pair", Members: []string{"Ada", "Scout"}}}},
		"a team and a room":         {Teams: []SetupTeam{{Name: "Solo", Members: []string{"Scout"}}}, Rooms: []SetupRoom{{Name: "Chat", Members: []string{"Scout"}}}},
	} {
		if _, err := svc.CheckSetup(ctx, setup); err != nil {
			t.Errorf("CheckSetup of %s = %v; want nil", name, err)
		}
	}
}

func mustListBots(t *testing.T, svc *Service) []Bot {
	t.Helper()
	bots, err := svc.ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return bots
}

func TestCreateSetupCreatesNothingWhenTheTransactionFails(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	newTestBot(t, svc)
	if _, err := svc.sqlDB.ExecContext(ctx, `CREATE TRIGGER no_rooms BEFORE INSERT ON room_members BEGIN SELECT RAISE(ABORT, 'rooms are closed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSetup(ctx, softwareTeamSetup()); err == nil || !strings.Contains(err.Error(), "rooms are closed") {
		t.Fatalf("CreateSetup = %v; want the failed insert", err)
	}
	if bots := mustListBots(t, svc); len(bots) != 1 {
		t.Fatalf("bots = %+v; want only Scout", bots)
	}
	for _, dir := range []string{"bots", "teams"} {
		entries, _ := os.ReadDir(filepath.Join(svc.DataDir(), dir))
		if dir == "bots" && len(entries) != 1 || dir == "teams" && len(entries) != 0 {
			t.Errorf("%s folders = %d; want only those of Scout", dir, len(entries))
		}
	}
}

func TestCreateSetupRemovesTheNewFoldersWhenATeamFolderCannotBeMade(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	newTestBot(t, svc)
	if err := os.WriteFile(filepath.Join(svc.DataDir(), "teams"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSetup(ctx, softwareTeamSetup()); err == nil {
		t.Fatal("CreateSetup succeeded without a teams folder")
	}
	if entries, err := os.ReadDir(filepath.Join(svc.DataDir(), "bots")); err != nil || len(entries) != 1 {
		t.Fatalf("bot folders = %d, %v; want only Scout's", len(entries), err)
	}
	if bots := mustListBots(t, svc); len(bots) != 1 {
		t.Fatalf("bots = %+v; want only Scout", bots)
	}
}

func TestASetupCardCannotBeAlwaysAllowed(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	bot := newTestBot(t, svc)
	events, stop := svc.Hub().Subscribe()
	defer stop()
	done := make(chan bool, 1)
	go func() {
		ok, _, _ := svc.RequestApproval(ctx, ApprovalRequest{BotID: bot.ID, TranscriptID: bot.ID, TurnID: "t1", Tool: ToolCreateSetup, Input: json.RawMessage(`{}`)})
		done <- ok
	}()
	var cardID string
	for cardID == "" {
		if e := <-events; e.Type == "entry" && e.Entry.Kind == KindApproval {
			cardID = e.Entry.ID
		}
	}
	if err := svc.ResolveApproval(ctx, cardID, DecisionAlways); !errors.Is(err, ErrInvalid) {
		t.Fatalf("always on a setup card: err = %v; want ErrInvalid", err)
	}
	if err := svc.ResolveApproval(ctx, cardID, DecisionOnce); err != nil {
		t.Fatalf("once after the refused always: %v", err)
	}
	if !<-done {
		t.Fatal("the setup was not allowed after once")
	}
	if grants, err := svc.ToolGrants(ctx, bot.ID); err != nil || slices.Contains(grants, ToolCreateSetup) {
		t.Fatalf("grants = %v, %v; want no grant of %s", grants, err, ToolCreateSetup)
	}
}

func TestCheckSetupReturnsTheSetupAsItWillBeCreated(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	newTestBot(t, svc) // Scout
	got, err := svc.CheckSetup(ctx, Setup{
		Bots: []SetupBot{{Name: "  Ada ", Instructions: " Review.\n\nBe brief. "}},
		Teams: []SetupTeam{{
			Name: " Pair ", Purpose: " Ship. ", Members: []string{"ada", "SCOUT", "Ada"}, Lead: "  ", Agreement: " Tests pass. ",
		}},
		Rooms: []SetupRoom{{Name: "Chat", Members: []string{"scout"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Setup{
		Bots:  []SetupBot{{Name: "Ada", Instructions: "Review.\n\nBe brief."}},
		Teams: []SetupTeam{{Name: "Pair", Purpose: "Ship.", Members: []string{"Ada", "Scout"}, Lead: "Ada", Agreement: "Tests pass.", GoalBudget: DefaultGoalBudget}},
		Rooms: []SetupRoom{{Name: "Chat", Members: []string{"Scout"}}},
	}
	if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", want) {
		t.Fatalf("CheckSetup = %+v; want %+v", got, want)
	}
	again, err := svc.CheckSetup(ctx, got)
	if err != nil || fmt.Sprintf("%+v", again) != fmt.Sprintf("%+v", got) {
		t.Fatalf("CheckSetup of its own result = %+v, %v; want it unchanged", again, err)
	}
}

func TestBuilderJoinsNoTeamOrRoomOfASetup(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if err := svc.SeedBuilder(ctx); err != nil {
		t.Fatal(err)
	}
	newTestBot(t, svc)
	for name, setup := range map[string]Setup{
		"member": {Rooms: []SetupRoom{{Name: "R", Members: []string{"Scout", "builder"}}}},
		"lead":   {Teams: []SetupTeam{{Name: "T", Members: []string{"Scout"}, Lead: "Builder"}}},
	} {
		if _, err := svc.CheckSetup(ctx, setup); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "joins no team or room") {
			t.Errorf("Builder as %s: err = %v; want ErrInvalid saying that Builder joins no team or room", name, err)
		}
	}
	if _, err := svc.CheckSetup(ctx, Setup{Bots: []SetupBot{{Name: "BUILDER"}}}); err == nil || !strings.Contains(err.Error(), "is in use") {
		t.Fatalf("a new bot named like Builder: err = %v; want the name in use", err)
	}
}

func TestCreateSetupCreatesNothingWhenAChannelCannotGetItsMemberList(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	newTestBot(t, svc)
	if _, err := svc.sqlDB.ExecContext(ctx, `CREATE TRIGGER no_events BEFORE INSERT ON entries WHEN NEW.kind = 'event' BEGIN SELECT RAISE(ABORT, 'events are off'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSetup(ctx, softwareTeamSetup()); err == nil || !strings.Contains(err.Error(), "events are off") {
		t.Fatalf("CreateSetup = %v; want the failed member list", err)
	}
	if bots := mustListBots(t, svc); len(bots) != 1 {
		t.Fatalf("bots = %d; want only Scout", len(bots))
	}
	if entries, err := os.ReadDir(filepath.Join(svc.DataDir(), "teams")); err != nil || len(entries) != 0 {
		t.Fatalf("team folders = %d, %v; want none", len(entries), err)
	}
}
