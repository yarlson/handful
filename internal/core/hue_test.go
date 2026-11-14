package core

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
)

func huesByName(t *testing.T, svc *Service) map[string]string {
	t.Helper()
	bots, err := svc.ListBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hues := map[string]string{}
	for _, b := range bots {
		hues[b.Name] = b.Hue
	}
	return hues
}

// Builder, a bot made by hand and the bots of a setup each get the hue the fewest bots have,
// in palette order on a tie; the setup's team and room get none.
func TestEveryNewBotGetsTheLeastUsedHueAndRoomsGetNone(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if err := svc.SeedBuilder(ctx); err != nil {
		t.Fatal(err)
	}
	scout := newTestBot(t, svc)
	if scout.Hue != "emerald" {
		t.Fatalf("CreateBot after Builder: hue = %q; want emerald", scout.Hue)
	}
	created, err := svc.CreateSetup(ctx, softwareTeamSetup())
	if err != nil {
		t.Fatal(err)
	}
	if lead := botNamed(t, created, "Tech Lead"); lead.Hue != "violet" {
		t.Fatalf("CreateSetup: Tech Lead hue = %q; want violet", lead.Hue)
	}
	fay, err := svc.CreateBot(ctx, "Fay", "")
	if err != nil {
		t.Fatal(err)
	}
	if fay.Hue != "sky" {
		t.Fatalf("sixth bot: hue = %q; want sky, the first hue of a five-way tie", fay.Hue)
	}
	want := map[string]string{
		"Builder": "sky", "Scout": "emerald", "Tech Lead": "violet", "Developer": "fuchsia", "Reviewer": "lime", "Fay": "sky",
		"Web app": "", "Standup": "",
	}
	if got := huesByName(t, svc); !maps.Equal(got, want) {
		t.Fatalf("ListBots hues = %v; want %v", got, want)
	}
	if got, err := svc.GetBot(ctx, scout.ID); err != nil || got.Hue != "emerald" {
		t.Fatalf("GetBot(Scout) = hue %q, %v; want emerald", got.Hue, err)
	}
}

// A bot's hue changes only to a palette hue, saves in one step with the other settings, and
// counts for the next new bot; a room or team has no hue to set.
func TestBotSettingsChangeTheHueToAPaletteHueOnly(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	team, ana, _, _ := newTestTeam(t, svc)
	lime := "lime"
	saved, err := svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Hue: &lime})
	if err != nil || saved.Hue != "lime" {
		t.Fatalf("set Ana's hue to lime = %q, %v; want lime", saved.Hue, err)
	}
	renamed := "Anna"
	if saved, err = svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Name: &renamed}); err != nil || saved.Hue != "lime" {
		t.Fatalf("rename Ana = hue %q, %v; want lime kept", saved.Hue, err)
	}
	for _, bad := range []string{"rose", ""} {
		other := "Ann"
		_, err := svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Name: &other, Hue: &bad})
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "Unknown colour") {
			t.Fatalf("hue %q: err = %v; want an unknown colour error", bad, err)
		}
	}
	got, err := svc.GetBot(ctx, ana.ID)
	if err != nil || got.Name != "Anna" || got.Hue != "lime" {
		t.Fatalf("after bad hues: GetBot = %q %q, %v; want Anna, lime (nothing saved)", got.Name, got.Hue, err)
	}
	if _, err := svc.UpdateBotSettings(ctx, team.ID, BotPatch{Hue: &lime}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "no colour") {
		t.Fatalf("hue on a team: err = %v; want rooms and teams have no colour", err)
	}
	// Ana moved from sky to lime: sky and fuchsia are unused, and sky comes first.
	dee, err := svc.CreateBot(ctx, "Dee", "")
	if err != nil || dee.Hue != "sky" {
		t.Fatalf("new bot after Ana chose lime = %q, %v; want sky", dee.Hue, err)
	}
	if hues := huesByName(t, svc); hues["Anna"] != "lime" || hues["Ship"] != "" {
		t.Fatalf("ListBots hues = %v; want Anna lime and no hue for the team", hues)
	}
}
