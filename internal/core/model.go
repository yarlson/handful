package core

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// ModelAliases are the Claude Code model aliases that the UI offers. Each
// always means the latest model of its family.
var ModelAliases = []string{"fable", "opus", "sonnet", "haiku"}

// EffortLevels are the reasoning efforts that Claude Code's --effort takes.
var EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// Where an effective model or effort comes from.
const (
	SourceBot        = "bot"         // the bot's own setting
	SourceTeam       = "team"        // the default of the team of the turn
	SourceGlobal     = "global"      // the -model / -effort flag or config.json
	SourceEnv        = "env"         // the environment of yargpt, with nothing set in YarGPT
	SourceClaudeCode = "claude-code" // nothing set: Claude Code's own default
)

// Where a global value comes from, in Defaults.
const (
	DefaultFromFlag   = "flag"
	DefaultFromConfig = "config"
)

// Environment variables of yargpt that change the model or the effort.
const (
	EnvModel  = "ANTHROPIC_MODEL"
	EnvEffort = "CLAUDE_CODE_EFFORT_LEVEL"
)

// EnvSubagent are the variables that would change the model or effort of the
// bots' helper subagents. The runner removes them from Claude Code's
// environment.
var EnvSubagent = []string{"CLAUDE_CODE_SUBAGENT_MODEL", "CLAUDE_CODE_SUBAGENT_EFFORT", "CLAUDE_CODE_SUBAGENT_MODEL_FORCE"}

const maxModelLen = 200

var modelChars = regexp.MustCompile(`^[A-Za-z0-9._:/@\[\]-]+$`)

// NormalizeModel checks a model setting and returns it as stored: "" for
// inherit, an alias in lower case, or a custom model ID as given.
func NormalizeModel(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	lower := strings.ToLower(s)
	if lower == "inherit" {
		return "", invalid("Choose Default instead.")
	}
	if strings.HasPrefix(s, "-") || strings.ContainsFunc(s, unicode.IsSpace) {
		return "", invalid("A model ID can't contain spaces or start with '-'.")
	}
	if len(s) > maxModelLen || !modelChars.MatchString(s) {
		return "", invalid("A model ID can use only letters, digits and . _ : / @ [ ] -, up to %d characters.", maxModelLen)
	}
	for _, a := range ModelAliases {
		if lower == a {
			return a, nil
		}
	}
	return s, nil
}

// NormalizeEffort checks an effort setting and returns it in lower case, or
// "" for inherit.
func NormalizeEffort(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", nil
	}
	for _, l := range EffortLevels {
		if s == l {
			return l, nil
		}
	}
	return "", invalid("Effort must be one of %s.", strings.Join(EffortLevels, ", "))
}

// Defaults are the global model and effort of every bot, from the flags, the
// config file and the environment of yargpt.
type Defaults struct {
	Model        string   `json:"model"`
	ModelSource  string   `json:"modelSource"` // flag | config | env | claude-code
	Effort       string   `json:"effort"`
	EffortSource string   `json:"effortSource"` // flag | config | env | claude-code
	EnvOverrides []string `json:"envOverrides"` // variables of the environment that YarGPT removes or that outrank it
}

// NewDefaults builds the Defaults of a model and effort that are set (from
// flag or config, by source; "" when not set) and the environment lookup.
func NewDefaults(model, modelSource, effort, effortSource string, getenv func(string) string) Defaults {
	d := Defaults{Model: model, ModelSource: modelSource, Effort: effort, EffortSource: effortSource, EnvOverrides: []string{}}
	if model == "" {
		d.Model, d.ModelSource = fromEnv(getenv(EnvModel))
	}
	if effort == "" {
		d.Effort, d.EffortSource = fromEnv(getenv(EnvEffort))
	}
	for _, name := range append([]string{EnvEffort}, EnvSubagent...) {
		if getenv(name) != "" {
			d.EnvOverrides = append(d.EnvOverrides, name)
		}
	}
	return d
}

func fromEnv(v string) (string, string) {
	if v == "" {
		return "", SourceClaudeCode
	}
	return v, SourceEnv
}

// Effective is the model and the effort that a bot's turn runs with, and
// where each comes from. ModelID and ModelMismatch come from the runner.
type Effective struct {
	Model         string `json:"model"`
	ModelSource   string `json:"modelSource"` // bot | team | global | env | claude-code
	ModelID       string `json:"modelId,omitempty"`
	ModelMismatch bool   `json:"modelMismatch,omitempty"`
	Effort        string `json:"effort"`
	EffortSource  string `json:"effortSource"` // bot | team | global | env | claude-code
}

// Resolve returns the model and the effort of a turn of bot, each on its own:
// the bot's setting, else the default of team (the team of a turn in a team;
// nil otherwise), else the global value, else the environment or Claude
// Code's default. Only values from the bot, the team or the global settings
// are passed to Claude Code as flags.
func Resolve(bot Bot, team *Team, d Defaults) Effective {
	var teamModel, teamEffort string
	if team != nil {
		teamModel, teamEffort = team.DefaultModel, team.DefaultEffort
	}
	var e Effective
	e.Model, e.ModelSource = pick(bot.Model, teamModel, d.Model, d.ModelSource)
	e.Effort, e.EffortSource = pick(bot.Effort, teamEffort, d.Effort, d.EffortSource)
	return e
}

// pick chooses the first set value of a bot, a team and the defaults.
func pick(own, team, global, globalSource string) (string, string) {
	if own != "" {
		return own, SourceBot
	}
	if team != "" {
		return team, SourceTeam
	}
	if globalSource == SourceEnv || globalSource == SourceClaudeCode {
		return global, globalSource
	}
	if global != "" {
		return global, SourceGlobal
	}
	return "", SourceClaudeCode
}

// PostMeta is what a post records about the turn that wrote it: the model and
// effort that YarGPT resolved for it. Empty fields are left out.
type PostMeta struct {
	Model  string
	Effort string
}

// modelFlags is the set of bots with a model problem; it lives in memory.
type modelFlags struct {
	mu  sync.Mutex
	ids map[string]bool
}

func newModelFlags() *modelFlags { return &modelFlags{ids: map[string]bool{}} }

// set marks or clears a bot and reports whether that changed the set.
func (f *modelFlags) set(id string, on bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ids[id] == on {
		return false
	}
	if on {
		f.ids[id] = true
	} else {
		delete(f.ids, id)
	}
	return true
}

func (f *modelFlags) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Collect(maps.Keys(f.ids))
}

// SetModelAttention marks a bot whose model Claude Code could not run, or
// replaced with another, so that it needs the user's attention; on false it
// clears the mark. A change of the bot's model or effort clears it too.
func (s *Service) SetModelAttention(botID string, on bool) {
	if s.modelAttention.set(botID, on) {
		s.hub.Publish(Event{Type: "bots"})
	}
}

// validModelEffort checks and normalizes a model and an effort setting.
func validModelEffort(model, effort string) (string, string, error) {
	model, err := NormalizeModel(model)
	if err != nil {
		return "", "", err
	}
	effort, err = NormalizeEffort(effort)
	if err != nil {
		return "", "", err
	}
	return model, effort, nil
}

// addTo puts the set fields of m into the body of a post.
func (m PostMeta) addTo(body map[string]any) {
	if m.Model != "" {
		body["model"] = m.Model
	}
	if m.Effort != "" {
		body["effort"] = m.Effort
	}
}
