package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// newTestTeam creates bots Ana, Bo and Cy, and team "Ship" of Ana and Bo
// with Bo as the lead. It drops the bots' first-run turns.
func newTestTeam(t *testing.T, svc *Service) (team Bot, ana, bo, cy Bot) {
	t.Helper()
	ctx := context.Background()
	bots := map[string]Bot{}
	for _, name := range []string{"Ana", "Bo", "Cy"} {
		b, err := svc.CreateBot(ctx, name, name+" helps.")
		if err != nil {
			t.Fatal(err)
		}
		bots[name] = b
	}
	finishQueued(t, svc)
	team, err := svc.CreateTeam(ctx, "Ship", "Ship small changes.", []string{bots["Ana"].ID, bots["Bo"].ID}, TeamSettings{LeadID: bots["Bo"].ID, Agreement: "Tests pass before a change is done."})
	if err != nil {
		t.Fatal(err)
	}
	return team, bots["Ana"], bots["Bo"], bots["Cy"]
}

func finishQueued(t *testing.T, svc *Service) {
	t.Helper()
	ctx := context.Background()
	turns, err := svc.QueuedTurns(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range turns {
		if err := svc.FinishTurn(ctx, tr.ID, "done", ""); err != nil {
			t.Fatal(err)
		}
	}
}

func lastEventText(t *testing.T, svc *Service, id string) string {
	t.Helper()
	entries, err := svc.Entries(context.Background(), id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, e := range entries {
		var body struct {
			Text string `json:"text"`
		}
		if e.Kind == KindEvent && json.Unmarshal(e.Body, &body) == nil {
			text = body.Text
		}
	}
	return text
}

func TestCreateTeamMakesARoomWithALeadAnAgreementAndASharedFolder(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, _ := newTestTeam(t, svc)

	want := Team{LeadID: bo.ID, Agreement: "Tests pass before a change is done.", GoalBudget: DefaultGoalBudget, RequestSlots: 1, InboxMax: DefaultInboxMax, InboxPerSender: DefaultInboxPerSender}
	if !team.IsRoom() || team.Team == nil || *team.Team != want {
		t.Fatalf("created team = %+v (team %+v); want a room with %+v", team, team.Team, want)
	}
	got, err := svc.GetBot(ctx, team.ID)
	if err != nil || got.Team == nil || *got.Team != want || len(got.Members) != 2 {
		t.Fatalf("GetBot(team) = %+v (team %+v), %v; want its members and %+v", got, got.Team, err, want)
	}
	link, err := os.Readlink(filepath.Join(svc.TeamWorkspace(team.ID), ".claude", "skills"))
	if err != nil || filepath.Join(svc.TeamWorkspace(team.ID), ".claude", link) != svc.SkillsDir() {
		t.Fatalf("team folder skills link = %q, %v; want a link to %s", link, err, svc.SkillsDir())
	}
	if text := lastEventText(t, svc, team.ID); text != "Members: Ana, Bo. Lead: Bo" {
		t.Fatalf("members event = %q; want the members and the lead", text)
	}
	all, err := svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range all {
		if (b.ID == team.ID) != (b.Team != nil) {
			t.Fatalf("ListBots: %s has team %+v; want team settings only on the team", b.Name, b.Team)
		}
	}
	rooms, err := svc.RoomsOf(ctx, ana.ID)
	if err != nil || len(rooms) != 1 || rooms[0].Team == nil || rooms[0].Team.LeadID != bo.ID {
		t.Fatalf("RoomsOf(Ana) = %+v, %v; want the team with lead Bo", rooms, err)
	}
	if turns, _ := svc.QueuedTurns(ctx, 10); len(turns) != 0 {
		t.Fatalf("queued turns after creating a team = %+v; want none", turns)
	}
}

func TestCreateTeamFillsDefaultsAndRejectsBadSettings(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, ana, bo, cy := newTestTeam(t, svc)

	team, err := svc.CreateTeam(ctx, "Docs", "", []string{ana.ID, bo.ID}, TeamSettings{})
	if err != nil || team.Team.LeadID != ana.ID || team.Team.GoalBudget != DefaultGoalBudget || team.Team.Agreement != "" {
		t.Fatalf("team without settings = %+v, %v; want the first member as lead and %d turns", team.Team, err, DefaultGoalBudget)
	}
	if team, err := svc.CreateTeam(ctx, "Big", "", []string{ana.ID}, TeamSettings{GoalBudget: 500, Agreement: "  Ask first.  "}); err != nil || team.Team.GoalBudget != 500 || team.Team.Agreement != "Ask first." {
		t.Fatalf("team with 500 turns = %+v, %v; want 500 turns and the trimmed agreement", team.Team, err)
	}
	if team, err := svc.CreateTeam(ctx, "Tiny", "", []string{ana.ID}, TeamSettings{GoalBudget: 1}); err != nil || team.Team.GoalBudget != 1 {
		t.Fatalf("team with 1 turn = %+v, %v; want 1 turn", team.Team, err)
	}
	if _, err := svc.CreateTeam(ctx, "Long", "", []string{ana.ID}, TeamSettings{Agreement: strings.Repeat("a", maxAgreementLen)}); err != nil {
		t.Fatalf("team with an agreement of %d characters: %v; want it accepted", maxAgreementLen, err)
	}
	before, _ := os.ReadDir(filepath.Join(svc.DataDir(), "teams"))
	bad := map[string]TeamSettings{
		"a lead that is not a member": {LeadID: cy.ID},
		"a negative budget":           {GoalBudget: -1},
		"a budget over the limit":     {GoalBudget: 501},
		"a long agreement":            {Agreement: strings.Repeat("a", maxAgreementLen+1)},
	}
	for name, set := range bad {
		if _, err := svc.CreateTeam(ctx, "Bad", "", []string{ana.ID, bo.ID}, set); !errors.Is(err, ErrInvalid) {
			t.Errorf("CreateTeam with %s: err = %v; want ErrInvalid", name, err)
		}
	}
	if _, err := svc.CreateTeam(ctx, "Empty", "", nil, TeamSettings{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("CreateTeam without members: err = %v; want ErrInvalid", err)
	}
	after, _ := os.ReadDir(filepath.Join(svc.DataDir(), "teams"))
	if len(after) != len(before) {
		t.Fatalf("team folders: %d before the failed creates, %d after; want no folder left behind", len(before), len(after))
	}
}

func TestUserPostInATeamWakesTheLeadUnlessItMentionsMembers(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"What should we ship next?", []string{"Bo"}},
		{"@Ana please draft it", []string{"Ana"}},
		{"@here status?", []string{"Ana", "Bo"}},
	}
	for _, c := range cases {
		t.Run(c.text, func(t *testing.T) {
			svc := newTestService(t)
			ctx := context.Background()
			team, ana, bo, _ := newTestTeam(t, svc)
			names := map[string]string{ana.ID: "Ana", bo.ID: "Bo"}
			if _, _, err := svc.SubmitUserMessage(ctx, team.ID, c.text, "n1"); err != nil {
				t.Fatal(err)
			}
			turns, err := svc.QueuedTurns(ctx, 10)
			if err != nil {
				t.Fatal(err)
			}
			var woken []string
			for _, tr := range turns {
				if tr.RoomID != team.ID || tr.Source != SourceRoom {
					t.Fatalf("queued turn = %+v; want a room turn in the team", tr)
				}
				woken = append(woken, names[tr.BotID])
			}
			if strings.Join(woken, ",") != strings.Join(c.want, ",") {
				t.Fatalf("post %q woke %v; want %v", c.text, woken, c.want)
			}
		})
	}
}

func TestTheFirstMemberLeadsWhenTheLeadLeavesTheTeam(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, cy := newTestTeam(t, svc)

	team, err := svc.SetRoomMembers(ctx, team.ID, []string{ana.ID, cy.ID})
	if err != nil || team.Team.LeadID != ana.ID {
		t.Fatalf("team after the lead left = %+v, %v; want Ana, the first member, as lead", team.Team, err)
	}
	if text := lastEventText(t, svc, team.ID); text != "Members: Ana, Cy. Lead: Ana" {
		t.Fatalf("members event = %q; want the new members and lead", text)
	}
	team, err = svc.SetRoomMembers(ctx, team.ID, []string{ana.ID, bo.ID, cy.ID})
	if err != nil || team.Team.LeadID != bo.ID {
		t.Fatalf("team after Bo came back = %+v, %v; want Bo, the lead the user chose, to lead again", team.Team, err)
	}
}

func TestUpdateTeamChangesItsSettings(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, _, cy := newTestTeam(t, svc)
	before := lastEventText(t, svc, team.ID)

	agreement, budget := "Ask before a merge.", int64(12)
	got, err := svc.UpdateTeam(ctx, team.ID, TeamPatch{Agreement: &agreement, GoalBudget: &budget})
	if err != nil || got.Team.Agreement != agreement || got.Team.GoalBudget != 12 || got.Team.LeadID != team.Team.LeadID {
		t.Fatalf("UpdateTeam(agreement, budget) = %+v, %v; want the new agreement and budget and the same lead", got.Team, err)
	}
	if text := lastEventText(t, svc, team.ID); text != before {
		t.Fatalf("event after a change without a new lead = %q; want no new event", text)
	}
	got, err = svc.UpdateTeam(ctx, team.ID, TeamPatch{LeadID: &ana.ID})
	if err != nil || got.Team.LeadID != ana.ID || got.Team.Agreement != agreement {
		t.Fatalf("UpdateTeam(lead) = %+v, %v; want Ana as lead and the agreement kept", got.Team, err)
	}
	if stored, _ := svc.GetBot(ctx, team.ID); stored.Team.LeadID != ana.ID || stored.Team.GoalBudget != 12 {
		t.Fatalf("stored team = %+v; want lead Ana and 12 turns", stored.Team)
	}
	if text := lastEventText(t, svc, team.ID); text != "Members: Ana, Bo. Lead: Ana" {
		t.Fatalf("event after a new lead = %q; want the members and the new lead", text)
	}
	if _, err := svc.UpdateTeam(ctx, team.ID, TeamPatch{LeadID: &cy.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("UpdateTeam(lead not a member): err = %v; want ErrInvalid", err)
	}
	room, err := svc.CreateRoom(ctx, "Chat", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTeam(ctx, room.ID, TeamPatch{Agreement: &agreement}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("UpdateTeam(plain room): err = %v; want ErrInvalid", err)
	}
	if _, err := svc.UpdateTeam(ctx, "missing", TeamPatch{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateTeam(missing): err = %v; want ErrNotFound", err)
	}
}

func TestTeamMemoryIsSharedByTheTeamInItsTurnsOnly(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, _ := newTestTeam(t, svc)

	if _, err := svc.WriteFact(ctx, ana.ID, team.ID, ScopeTeam, TierProfile, "Releases go out on Fridays."); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteFact(ctx, ana.ID, "", ScopeTeam, TierLog, "Outside the team."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("WriteFact(scope team, no team): err = %v; want ErrInvalid", err)
	}
	if _, err := svc.WriteFact(ctx, ana.ID, team.ID, "everyone", TierLog, "A fact."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("WriteFact(unknown scope): err = %v; want ErrInvalid", err)
	}
	profile, _, err := svc.PromptMemory(ctx, bo.ID, team.ID)
	if err != nil || len(profile) != 1 || profile[0].Scope != ScopeTeam || profile[0].LearnedBy != ana.ID {
		t.Fatalf("Bo's prompt memory in the team = %+v, %v; want Ana's team fact", profile, err)
	}
	if own, _ := svc.Memories(ctx, bo.ID, ""); len(own) != 0 {
		t.Fatalf("Bo's memory outside the team = %+v; want no team fact", own)
	}
	if got, err := svc.Recall(ctx, bo.ID, team.ID, "Fridays", ScopeTeam, 10); err != nil || len(got) != 1 {
		t.Fatalf("Recall(scope team) = %+v, %v; want the team fact", got, err)
	}
	if _, err := svc.Recall(ctx, bo.ID, "", "Fridays", ScopeTeam, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Recall(scope team, no team): err = %v; want ErrInvalid", err)
	}
	if err := svc.ReplaceFact(ctx, bo.ID, team.ID, ScopeTeam, TierProfile, "Releases go out on Fridays.", "Releases go out on Thursdays."); err != nil {
		t.Fatal(err)
	}
	if found, err := svc.ForgetFact(ctx, ana.ID, team.ID, ScopeTeam, "Releases go out on Thursdays."); err != nil || !found {
		t.Fatalf("ForgetFact(team fact) = %v, %v; want it forgotten", found, err)
	}

	if err := svc.AddUserFact(ctx, team.ID, ScopeTeam, TierProfile, "Ship only green builds."); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddUserFact(ctx, team.ID, ScopeUser, TierProfile, "The user is Yar."); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddUserFact(ctx, bo.ID, ScopeAgent, TierLog, "Bo's own note."); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"", ScopeAgent} {
		if err := svc.AddUserFact(ctx, team.ID, scope, TierLog, "A team has no agent memory."); !errors.Is(err, ErrInvalid) {
			t.Fatalf("AddUserFact(team, scope %q): err = %v; want ErrInvalid", scope, err)
		}
	}
	room, err := svc.CreateRoom(ctx, "Chat", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AddUserFact(ctx, room.ID, ScopeUser, TierLog, "A room has no memory."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("AddUserFact(plain room): err = %v; want ErrInvalid", err)
	}
	panel, err := svc.PanelMemories(ctx, team.ID)
	if got, want := factScopes(panel), "Ship only green builds.=team/user,The user is Yar.=user/user"; err != nil || got != want {
		t.Fatalf("PanelMemories(team) = %s, %v; want %s", got, err, want)
	}
	panel, err = svc.PanelMemories(ctx, bo.ID)
	if got, want := factScopes(panel), "Bo's own note.=agent/user,The user is Yar.=user/user"; err != nil || got != want {
		t.Fatalf("PanelMemories(Bo) = %s, %v; want %s", got, err, want)
	}
}

func TestDeleteRemovesTheMemoryAndTheFolderOfABotOrATeam(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, _ := newTestTeam(t, svc)
	if _, err := svc.WriteFact(ctx, ana.ID, team.ID, ScopeTeam, TierLog, "A team fact."); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteFact(ctx, ana.ID, "", ScopeAgent, TierLog, "Ana's fact."); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteFact(ctx, ana.ID, "", ScopeUser, TierLog, "A shared fact."); err != nil {
		t.Fatal(err)
	}

	if err := svc.DeleteBot(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(svc.DataDir(), "teams", team.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("team folder after delete: %v; want it removed", err)
	}
	if got, _ := svc.Memories(ctx, bo.ID, team.ID); len(got) != 1 || got[0].Fact != "A shared fact." {
		t.Fatalf("memory after deleting the team = %+v; want only the shared fact", got)
	}
	if err := svc.DeleteBot(ctx, ana.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(svc.DataDir(), "bots", ana.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bot folder after delete: %v; want it removed", err)
	}
	if got, _ := svc.Memories(ctx, ana.ID, ""); len(got) != 1 || got[0].Fact != "A shared fact." {
		t.Fatalf("memory after deleting Ana = %+v; want only the shared fact", got)
	}
	if err := svc.DeleteBot(ctx, ana.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete a deleted bot: err = %v; want ErrNotFound", err)
	}
}

func TestATurnInATeamWorksInTheTeamFolderAndTalksOnlyToTheTeam(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, _ := newTestTeam(t, svc)
	room, err := svc.CreateRoom(ctx, "Chat", "", []string{ana.ID, bo.ID})
	if err != nil {
		t.Fatal(err)
	}

	workspaces := []struct{ roomID, want string }{
		{"", svc.Workspace(ana.ID)},
		{room.ID, svc.Workspace(ana.ID)},
		{team.ID, svc.TeamWorkspace(team.ID)},
	}
	for _, w := range workspaces {
		if got, err := svc.TurnWorkspace(ctx, ana.ID, w.roomID); err != nil || got != w.want {
			t.Errorf("TurnWorkspace(Ana, %q) = %q, %v; want %q", w.roomID, got, err, w.want)
		}
	}
	targets := []struct {
		roomID, targetID string
		ok               bool
	}{
		{"", bo.ID, true},
		{room.ID, bo.ID, true},
		{team.ID, team.ID, true},
		{team.ID, bo.ID, false},
	}
	for _, c := range targets {
		err := svc.CheckTeamTurnTarget(ctx, c.roomID, ana.ID, c.targetID, "Can you help?", 0)
		if c.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalid)) {
			t.Errorf("CheckTeamTurnTarget(%q, %q) = %v; want allowed %v", c.roomID, c.targetID, err, c.ok)
		}
	}
}

func TestATeamTurnPostsToItsOwnRoomOnlyWhenThePostWakesNoOne(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, cy := newTestTeam(t, svc)
	reports, err := svc.CreateRoom(ctx, "Reports", "", []string{ana.ID})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := svc.CreateRoom(ctx, "Shared", "", []string{ana.ID, bo.ID, cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	others, err := svc.CreateRoom(ctx, "Others", "", []string{bo.ID, cy.ID})
	if err != nil {
		t.Fatal(err)
	}
	finishQueued(t, svc)
	for name, c := range map[string]struct {
		target, text string
		hops         int64
		want         string // "" when allowed, else a part of the error
	}{
		"own room, alone":              {reports.ID, "PR #100 is merged.", 0, ""},
		"own room, no mention":         {shared.ID, "PR #100 is merged.", 0, ""},
		"own room, a mention":          {shared.ID, "@Bo PR #100 is merged.", 0, "this post would wake Bo in Shared"},
		"own room, @here":              {shared.ID, "@here PR #100 is merged.", 0, "would wake Bo, Cy in Shared"},
		"own room, past the hop limit": {shared.ID, "@Bo PR #100 is merged.", maxAgentHops, ""},
		"own room, at the hop limit":   {shared.ID, "@Bo PR #100 is merged.", maxAgentHops - 1, "this post would wake Bo in Shared"},
		"a room Ana is not in":         {others.ID, "PR #100 is merged.", 0, "you are not a member of Others"},
		"a bot outside the team":       {cy.ID, "PR #100 is merged.", 0, "talk to the team in its channel"},
		"a bot in the team":            {bo.ID, "PR #100 is merged.", 0, "talk to the team in its channel"},
	} {
		err := svc.CheckTeamTurnTarget(ctx, team.ID, ana.ID, c.target, c.text, c.hops)
		if c.want == "" && err != nil {
			t.Errorf("%s: err = %v; want allowed", name, err)
		}
		if c.want != "" && (!errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "send_to_user")) {
			t.Errorf("%s: err = %v; want ErrInvalid with %q and what to do instead", name, err, c.want)
		}
	}
	if err := svc.CheckTeamTurnTarget(ctx, team.ID, ana.ID, "missing", "Hi.", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("a missing target: err = %v; want ErrNotFound", err)
	}
	// Outside a team turn nothing changes: any target, mention or not.
	for _, target := range []string{others.ID, shared.ID, cy.ID} {
		if err := svc.CheckTeamTurnTarget(ctx, reports.ID, ana.ID, target, "@Bo hi.", 0); err != nil {
			t.Errorf("outside a team turn to %s: err = %v; want allowed", target, err)
		}
	}
}

func TestATeamPostCarriesFilesFromTheTeamFolderOnly(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, _, _ := newTestTeam(t, svc)
	// The data folder is in the system temp folder, which bots may attach from.
	svc.tempDirs = []string{t.TempDir()}
	png := testPNG(t)
	if err := os.WriteFile(filepath.Join(svc.TeamWorkspace(team.ID), "chart.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc.Workspace(ana.ID), "own.png"), png, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.PostToRoom(ctx, team.ID, ana.ID, "", "The chart.", []string{"chart.png"}, 0, false); err != nil {
		t.Fatalf("team post with a file of the team folder: %v", err)
	}
	if _, err := svc.PostToRoom(ctx, team.ID, ana.ID, "", "Mine.", []string{filepath.Join(svc.Workspace(ana.ID), "own.png")}, 0, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("team post with a file of Ana's own workspace: err = %v; want ErrInvalid", err)
	}
}

// factScopes lists facts as "fact=scope/author", sorted by fact.
func factScopes(ms []Memory) string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Fact + "=" + m.Scope + "/" + m.LearnedBy
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestTeamsAreTheirOwnSidebarSection(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, _ := newTestTeam(t, svc)
	other, err := svc.CreateTeam(ctx, "Docs", "", []string{ana.ID}, TeamSettings{})
	if err != nil {
		t.Fatal(err)
	}
	room, err := svc.CreateRoom(ctx, "Chat", "", []string{ana.ID, bo.ID})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ReorderBots(ctx, []string{room.ID}); err != nil {
		t.Fatalf("reordering the rooms without the teams: %v", err)
	}
	if err := svc.ReorderBots(ctx, []string{other.ID, team.ID}); err != nil {
		t.Fatalf("reordering the teams: %v", err)
	}
	for name, ids := range map[string][]string{
		"a team with the rooms": {room.ID, team.ID, other.ID},
		"one team of two":       {team.ID},
	} {
		if err := svc.ReorderBots(ctx, ids); !errors.Is(err, ErrInvalid) {
			t.Errorf("reorder %s: err = %v; want ErrInvalid", name, err)
		}
	}
	listed, err := svc.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var teams []string
	for _, b := range listed {
		if b.IsTeam() {
			teams = append(teams, b.Name)
		}
	}
	if strings.Join(teams, ",") != "Docs,Ship" {
		t.Fatalf("teams in sidebar order = %v; want Docs, Ship", teams)
	}
}

func TestATeamWithoutMembersHasNoLead(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, bo, _ := newTestTeam(t, svc)
	for _, b := range []Bot{ana, bo} {
		if err := svc.DeleteBot(ctx, b.ID); err != nil {
			t.Fatal(err)
		}
	}

	got, err := svc.GetBot(ctx, team.ID)
	if err != nil || got.Team == nil || got.Team.LeadID != "" || len(got.Members) != 0 {
		t.Fatalf("team without members = %+v (team %+v), %v; want no members and no lead", got, got.Team, err)
	}
	if _, ok := got.Lead(); ok {
		t.Fatal("Lead() of a team without members reports a lead")
	}
	agreement := "Ask first."
	if _, err := svc.UpdateTeam(ctx, team.ID, TeamPatch{Agreement: &agreement}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("UpdateTeam of a team without members: err = %v; want ErrInvalid, since it has no lead", err)
	}
}
