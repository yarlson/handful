package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeModelChecksAndLowercasesAliases(t *testing.T) {
	long := strings.Repeat("a", 200)
	for in, want := range map[string]string{
		"":                       "",
		"  ":                     "",
		"opus":                   "opus",
		" Opus ":                 "opus",
		"HAIKU":                  "haiku",
		"fable":                  "fable",
		"Sonnet":                 "sonnet",
		"opusplan":               "opusplan",
		"OpusPlan":               "OpusPlan", // an alias YarGPT doesn't know stays as given
		"claude-opus-5-5":        "claude-opus-5-5",
		"claude-sonnet-5-5[1m]":  "claude-sonnet-5-5[1m]",
		"us.anthropic.claude:v1": "us.anthropic.claude:v1",
		"arn:aws:bedrock:us-east-1:123:inference-profile/x@1": "arn:aws:bedrock:us-east-1:123:inference-profile/x@1",
		"a_b": "a_b",
		long:  long,
	} {
		got, err := NormalizeModel(in)
		if err != nil || got != want {
			t.Errorf("NormalizeModel(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for in, msg := range map[string]string{
		"inherit":       "Choose Default instead.",
		"Inherit":       "Choose Default instead.",
		"claude opus":   "A model ID can't contain spaces or start with '-'.",
		"opus\tx":       "A model ID can't contain spaces or start with '-'.",
		"-opus":         "A model ID can't contain spaces or start with '-'.",
		"--dangerously": "A model ID can't contain spaces or start with '-'.",
		"opus;rm":       "A model ID can use only letters, digits and . _ : / @ [ ] -, up to 200 characters.",
		"opus$":         "A model ID can use only letters, digits and . _ : / @ [ ] -, up to 200 characters.",
		"ópus":          "A model ID can use only letters, digits and . _ : / @ [ ] -, up to 200 characters.",
		long + "a":      "A model ID can use only letters, digits and . _ : / @ [ ] -, up to 200 characters.",
	} {
		_, err := NormalizeModel(in)
		if !errors.Is(err, ErrInvalid) || !strings.HasSuffix(err.Error(), ": "+msg) {
			t.Errorf("NormalizeModel(%q) err = %v; want ErrInvalid %q", in, err, msg)
		}
	}
}

func TestNormalizeEffortAcceptsOnlyTheLevels(t *testing.T) {
	for in, want := range map[string]string{"": "", " ": "", "low": "low", "Medium": "medium", " HIGH ": "high", "xhigh": "xhigh", "max": "max"} {
		got, err := NormalizeEffort(in)
		if err != nil || got != want {
			t.Errorf("NormalizeEffort(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"none", "extra high", "maximum", "inherit", "lowest"} {
		_, err := NormalizeEffort(in)
		if !errors.Is(err, ErrInvalid) || !strings.HasSuffix(err.Error(), ": Effort must be one of low, medium, high, xhigh, max.") {
			t.Errorf("NormalizeEffort(%q) err = %v; want the levels message", in, err)
		}
	}
}

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestNewDefaultsFallsBackToTheEnvironmentAndListsOverrides(t *testing.T) {
	cases := []struct {
		name                         string
		model, modelSrc, eff, effSrc string
		env                          map[string]string
		want                         Defaults
	}{
		{name: "nothing", want: Defaults{ModelSource: SourceClaudeCode, EffortSource: SourceClaudeCode, EnvOverrides: []string{}}},
		{
			name: "flag and config", model: "opus", modelSrc: DefaultFromFlag, eff: "max", effSrc: DefaultFromConfig,
			env:  map[string]string{EnvModel: "haiku"},
			want: Defaults{Model: "opus", ModelSource: DefaultFromFlag, Effort: "max", EffortSource: DefaultFromConfig, EnvOverrides: []string{}},
		},
		{
			name: "environment", env: map[string]string{EnvModel: "haiku", EnvEffort: "low"},
			want: Defaults{Model: "haiku", ModelSource: SourceEnv, Effort: "low", EffortSource: SourceEnv, EnvOverrides: []string{EnvEffort}},
		},
		{
			name: "effort set and variable present", eff: "high", effSrc: DefaultFromConfig,
			env:  map[string]string{EnvEffort: "low", "CLAUDE_CODE_SUBAGENT_MODEL": "haiku", "CLAUDE_CODE_SUBAGENT_EFFORT": "low", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE": "1"},
			want: Defaults{ModelSource: SourceClaudeCode, Effort: "high", EffortSource: DefaultFromConfig, EnvOverrides: []string{EnvEffort, "CLAUDE_CODE_SUBAGENT_MODEL", "CLAUDE_CODE_SUBAGENT_EFFORT", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE"}},
		},
	}
	for _, c := range cases {
		got := NewDefaults(c.model, c.modelSrc, c.eff, c.effSrc, env(c.env))
		if got.Model != c.want.Model || got.ModelSource != c.want.ModelSource || got.Effort != c.want.Effort || got.EffortSource != c.want.EffortSource || !slices.Equal(got.EnvOverrides, c.want.EnvOverrides) || got.EnvOverrides == nil {
			t.Errorf("%s: NewDefaults = %+v; want %+v", c.name, got, c.want)
		}
	}
}

func TestResolveTakesBotThenTeamThenGlobalForModelAndEffortApart(t *testing.T) {
	global := NewDefaults("sonnet", DefaultFromConfig, "high", DefaultFromFlag, env(nil))
	envOnly := NewDefaults("", "", "", "", env(map[string]string{EnvModel: "haiku", EnvEffort: "low"}))
	none := NewDefaults("", "", "", "", env(nil))
	team := &Team{DefaultModel: "opus", DefaultEffort: "medium"}
	half := &Team{DefaultEffort: "xhigh"}
	cases := []struct {
		name string
		bot  Bot
		team *Team
		d    Defaults
		want Effective
	}{
		{"own values everywhere", Bot{Model: "haiku", Effort: "max"}, team, global, Effective{Model: "haiku", ModelSource: SourceBot, Effort: "max", EffortSource: SourceBot}},
		{"team fills both", Bot{}, team, global, Effective{Model: "opus", ModelSource: SourceTeam, Effort: "medium", EffortSource: SourceTeam}},
		{"own model, team effort", Bot{Model: "haiku"}, team, global, Effective{Model: "haiku", ModelSource: SourceBot, Effort: "medium", EffortSource: SourceTeam}},
		{"own effort, team model", Bot{Effort: "low"}, team, global, Effective{Model: "opus", ModelSource: SourceTeam, Effort: "low", EffortSource: SourceBot}},
		{"team with only an effort", Bot{}, half, global, Effective{Model: "sonnet", ModelSource: SourceGlobal, Effort: "xhigh", EffortSource: SourceTeam}},
		{"direct chat: no team", Bot{}, nil, global, Effective{Model: "sonnet", ModelSource: SourceGlobal, Effort: "high", EffortSource: SourceGlobal}},
		{"environment", Bot{}, nil, envOnly, Effective{Model: "haiku", ModelSource: SourceEnv, Effort: "low", EffortSource: SourceEnv}},
		{"own model over environment", Bot{Model: "opus"}, nil, envOnly, Effective{Model: "opus", ModelSource: SourceBot, Effort: "low", EffortSource: SourceEnv}},
		{"team over environment", Bot{}, team, envOnly, Effective{Model: "opus", ModelSource: SourceTeam, Effort: "medium", EffortSource: SourceTeam}},
		{"nothing anywhere", Bot{}, nil, none, Effective{ModelSource: SourceClaudeCode, EffortSource: SourceClaudeCode}},
		{"zero defaults", Bot{}, &Team{}, Defaults{}, Effective{ModelSource: SourceClaudeCode, EffortSource: SourceClaudeCode}},
	}
	for _, c := range cases {
		if got := Resolve(c.bot, c.team, c.d); got != c.want {
			t.Errorf("%s: Resolve = %+v; want %+v", c.name, got, c.want)
		}
	}
}

func TestServiceKeepsTheDefaultsItIsGiven(t *testing.T) {
	if d := newTestService(t).Defaults(); d.ModelSource != SourceClaudeCode || d.EffortSource != SourceClaudeCode || d.EnvOverrides == nil {
		t.Fatalf("default Defaults = %+v; want Claude Code's, with no overrides", d)
	}
	svc := newTestService(t)
	d := NewDefaults("opus", DefaultFromFlag, "", "", env(nil))
	WithDefaults(d)(svc)
	if got := svc.Defaults(); got.Model != "opus" || got.ModelSource != DefaultFromFlag {
		t.Fatalf("Defaults = %+v; want %+v", got, d)
	}
}

func TestBotsAndTeamsStoreModelAndEffortAndRejectBadValuesWithoutSaving(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	ana, err := svc.CreateBotWithModel(ctx, "Ana", "", " Opus ", "MAX")
	if err != nil || ana.Model != "opus" || ana.Effort != "max" {
		t.Fatalf("create = %+v, %v; want opus and max", ana, err)
	}
	if _, err := svc.CreateBotWithModel(ctx, "Bad", "", "-x", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("create with a bad model: err = %v", err)
	}
	if _, err := svc.CreateBotWithModel(ctx, "Bad", "", "", "huge"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("create with a bad effort: err = %v", err)
	}
	bots, _ := svc.ListBots(ctx)
	if len(bots) != 1 {
		t.Fatalf("bots = %d; want only Ana: a bad bot isn't saved", len(bots))
	}

	name, bad, good, empty := "Anna", "huge", "Sonnet", ""
	if _, err := svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Name: &name, Effort: &bad}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("update with a bad effort: err = %v", err)
	}
	if _, err := svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Name: &name, Model: &bad, Effort: &empty}); err != nil {
		t.Fatalf("a custom model: %v", err)
	}
	inherit := "inherit"
	if _, err := svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Model: &inherit}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("update with inherit: err = %v", err)
	}
	got, _ := svc.GetBot(ctx, ana.ID)
	if got.Name != "Anna" || got.Model != "huge" || got.Effort != "" {
		t.Fatalf("after updates = %+v", got)
	}
	got, err = svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Model: &good})
	if err != nil || got.Model != "sonnet" || got.Effort != "" || got.Name != "Anna" {
		t.Fatalf("set the model = %+v, %v; want sonnet and the rest kept", got, err)
	}
	if got, err = svc.UpdateBot(ctx, ana.ID, &name, nil); err != nil || got.Model != "sonnet" {
		t.Fatalf("UpdateBot = %+v, %v; want the model kept", got, err)
	}

	bo, _ := svc.CreateBot(ctx, "Bo", "")
	team, err := svc.CreateTeam(ctx, "Ship", "", []string{ana.ID, bo.ID}, TeamSettings{DefaultModel: "HAIKU", DefaultEffort: "Low"})
	if err != nil || team.Team.DefaultModel != "haiku" || team.Team.DefaultEffort != "low" {
		t.Fatalf("create team = %+v, %v", team.Team, err)
	}
	if got, _ := svc.GetBot(ctx, team.ID); got.Team.DefaultModel != "haiku" || got.Team.DefaultEffort != "low" || got.Members[0].Model != "sonnet" {
		t.Fatalf("stored team = %+v, members %+v", got.Team, got.Members)
	}
	if _, err := svc.CreateTeam(ctx, "Bad", "", []string{ana.ID}, TeamSettings{DefaultEffort: "huge"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("create team with a bad effort: err = %v", err)
	}
	agreement, effort := "New.", "max"
	if _, err := svc.UpdateTeam(ctx, team.ID, TeamPatch{Agreement: &agreement, DefaultModel: &bad, DefaultEffort: &inherit}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("update team with a bad effort: err = %v", err)
	}
	updated, err := svc.UpdateTeam(ctx, team.ID, TeamPatch{DefaultEffort: &effort})
	if err != nil || updated.Team.DefaultEffort != "max" || updated.Team.DefaultModel != "haiku" || updated.Team.Agreement != "" {
		t.Fatalf("update team = %+v, %v; want max, the model kept and no agreement", updated.Team, err)
	}
	cleared, err := svc.UpdateTeam(ctx, team.ID, TeamPatch{DefaultModel: &empty})
	if err != nil || cleared.Team.DefaultModel != "" || cleared.Team.DefaultEffort != "max" {
		t.Fatalf("clear the team model = %+v, %v", cleared.Team, err)
	}
	if bots, _ := svc.ListBots(ctx); !slices.ContainsFunc(bots, func(b Bot) bool {
		return b.ID == team.ID && b.Team.DefaultEffort == "max" && b.Members[0].Model == "sonnet"
	}) {
		t.Fatalf("ListBots = %+v; want the team's defaults and the members' models", bots)
	}
	for _, p := range []BotPatch{{Model: &good}, {Effort: &effort}} {
		if _, err := svc.UpdateBotSettings(ctx, team.ID, p); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "Rooms have no model") {
			t.Fatalf("model on a room: err = %v", err)
		}
	}
	if _, err := svc.UpdateBotSettings(ctx, team.ID, BotPatch{Name: &agreement}); err != nil {
		t.Fatalf("rename a team: %v", err)
	}
	if _, err := svc.UpdateBotSettings(ctx, "missing", BotPatch{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing bot: err = %v", err)
	}
	seats, _ := svc.RoomSeats(ctx, team.ID)
	if seats[0].Model != "sonnet" || seats[1].Model != "" {
		t.Fatalf("seats = %+v; want the members' own models", seats)
	}
}

func TestModelAttentionShowsUntilClearedOrTheSettingChanges(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	ana := newAgents(t, svc, "Ana")["Ana"]
	events, cancel := svc.Hub().Subscribe()
	defer cancel()
	svc.SetModelAttention(ana.ID, true)
	if e := <-events; e.Type != "bots" {
		t.Fatalf("event = %+v; want bots", e)
	}
	svc.SetModelAttention(ana.ID, true) // no change, no event
	if got := attentionOf(t, svc, ana.ID); got != AttentionModel {
		t.Fatalf("attention = %q; want model", got)
	}
	if _, err := svc.AppendBotMessage(ctx, ana.ID, "t1", "Which one?", nil, false, true); err != nil {
		t.Fatal(err)
	}
	if got := attentionOf(t, svc, ana.ID); got != AttentionQuestion {
		t.Fatalf("attention with a question = %q; want the question first", got)
	}
	if _, _, err := svc.SubmitUserMessage(ctx, ana.ID, "That one.", "n1"); err != nil {
		t.Fatal(err)
	}
	name := "Ana"
	if _, err := svc.UpdateBot(ctx, ana.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	if got := attentionOf(t, svc, ana.ID); got != AttentionModel {
		t.Fatalf("attention after a rename = %q; want model still", got)
	}
	opus := "opus"
	if _, err := svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Model: &opus}); err != nil {
		t.Fatal(err)
	}
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention after a model change = %q; want none", got)
	}
	svc.SetModelAttention(ana.ID, true)
	max := "max"
	if _, err := svc.UpdateBotSettings(ctx, ana.ID, BotPatch{Effort: &max}); err != nil {
		t.Fatal(err)
	}
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention after an effort change = %q; want none", got)
	}
	svc.SetModelAttention(ana.ID, true)
	svc.SetModelAttention(ana.ID, false)
	if got := attentionOf(t, svc, ana.ID); got != "" {
		t.Fatalf("attention after clearing = %q; want none", got)
	}
}

func TestABotPostRecordsTheModelAndEffortOfItsTurn(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	ana := newAgents(t, svc, "Ana")["Ana"]
	for _, c := range []struct {
		meta PostMeta
		want map[string]any
	}{
		{PostMeta{Model: "opus", Effort: "max"}, map[string]any{"model": "opus", "effort": "max"}},
		{PostMeta{Model: "opus"}, map[string]any{"model": "opus"}},
		{PostMeta{Effort: "low"}, map[string]any{"effort": "low"}},
		{PostMeta{}, map[string]any{}},
	} {
		e, err := svc.AppendBotMessageWith(ctx, ana.ID, "t1", "Hi.", nil, false, false, c.meta)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(e.Body, &body); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"model", "effort"} {
			if body[k] != c.want[k] {
				t.Errorf("%+v: body[%s] = %v; want %v", c.meta, k, body[k], c.want[k])
			}
		}
	}
}

func TestASetupCarriesModelAndEffortAndChecksThem(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	setup := Setup{
		Bots:  []SetupBot{{Name: "Tri", Instructions: "Triage.", Model: "Haiku", Effort: "LOW"}, {Name: "Rev", Instructions: "Review."}},
		Teams: []SetupTeam{{Name: "Crew", Members: []string{"Tri", "Rev"}, DefaultModel: "Sonnet", DefaultEffort: "Medium"}},
	}
	checked, err := svc.CheckSetup(ctx, setup)
	if err != nil {
		t.Fatal(err)
	}
	if b := checked.Bots[0]; b.Model != "haiku" || b.Effort != "low" {
		t.Fatalf("checked bot = %+v", b)
	}
	if tm := checked.Teams[0]; tm.DefaultModel != "sonnet" || tm.DefaultEffort != "medium" {
		t.Fatalf("checked team = %+v", tm)
	}
	created, err := svc.CreateSetup(ctx, setup)
	if err != nil {
		t.Fatal(err)
	}
	tri, _ := svc.GetBot(ctx, created[0].ID)
	crew, _ := svc.GetBot(ctx, created[2].ID)
	if tri.Model != "haiku" || tri.Effort != "low" || crew.Team.DefaultModel != "sonnet" || crew.Team.DefaultEffort != "medium" || crew.Members[0].Model != "haiku" {
		t.Fatalf("created %+v and team %+v", tri, crew.Team)
	}
	for _, bad := range []Setup{
		{Bots: []SetupBot{{Name: "X", Model: "a b"}}},
		{Bots: []SetupBot{{Name: "X", Effort: "huge"}}},
		{Teams: []SetupTeam{{Name: "T", Members: []string{"Tri"}, DefaultModel: "inherit"}}},
		{Teams: []SetupTeam{{Name: "T", Members: []string{"Tri"}, DefaultEffort: "huge"}}},
	} {
		if _, err := svc.CheckSetup(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("CheckSetup(%+v) err = %v; want ErrInvalid", bad, err)
		}
	}
	if _, err := svc.CheckSetup(ctx, Setup{Bots: []SetupBot{{Name: "X", Model: "a b"}, {Name: "x"}}}); err == nil || !strings.Contains(err.Error(), "spaces") {
		t.Fatalf("a bad model: err = %v", err)
	}
}
