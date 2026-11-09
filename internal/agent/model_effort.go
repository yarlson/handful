package agent

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"yargpt/internal/core"
)

// envSubagentPrefix starts the variables that would give the executor
// another model or effort than the bot's (core.EnvSubagent and any later
// ones); every claude process loses them.
const envSubagentPrefix = "CLAUDE_CODE_SUBAGENT_"

// modelNotFound is the error of an assistant event when Claude Code cannot
// run the requested model; stderr then names unrecognizedModel.
const (
	modelNotFound     = "model_not_found"
	unrecognizedModel = "unrecognized_model"
)

// runFlags are the model and effort of a claude process: the values that
// YarGPT passes as --model and --effort ("" passes no flag), where the model
// comes from, and what its posts record.
type runFlags struct {
	model       string
	effort      string
	modelSource string
	post        core.PostMeta
}

// settings resolves the model and effort of bot's turns in room (the zero
// Bot outside a room; only a team has defaults) and checks the values that
// YarGPT passes again, since a stored value may break a newer rule.
func (r *Runner) settings(bot, room core.Bot) (runFlags, error) {
	e := core.Resolve(bot, room.Team, r.svc.Defaults())
	f := runFlags{model: passedValue(e.Model, e.ModelSource), effort: passedValue(e.Effort, e.EffortSource), modelSource: e.ModelSource, post: core.PostMeta{Model: e.Model, Effort: e.Effort}}
	if _, err := core.NormalizeModel(f.model); err != nil {
		return runFlags{}, fmt.Errorf("the model %q (%s) can't be used: %s Change it in Settings", f.model, sourcePhrase(e.ModelSource), reasonOf(err))
	}
	if _, err := core.NormalizeEffort(f.effort); err != nil {
		return runFlags{}, fmt.Errorf("the effort %q (%s) can't be used: %s Change it in Settings", f.effort, sourcePhrase(e.EffortSource), reasonOf(err))
	}
	return f, nil
}

// reasonOf is the message of a failed check without its "invalid input" prefix.
func reasonOf(err error) string {
	return strings.TrimPrefix(err.Error(), core.ErrInvalid.Error()+": ")
}

// passedValue is the value that YarGPT passes as --model or --effort for a
// resolved value and its source: only a value that YarGPT itself sets. A
// value from the environment or Claude Code's default passes no flag.
func passedValue(value, source string) string {
	switch source {
	case core.SourceBot, core.SourceTeam, core.SourceGlobal:
		return value
	}
	return ""
}

// childEnv is the environment of a claude process: base without the
// subagent model and effort variables, and without the effort variable when
// YarGPT passes --effort, which that variable would outrank. ANTHROPIC_MODEL
// stays, since --model outranks it.
func childEnv(base []string, effortFlag bool) []string {
	out := make([]string, 0, len(base))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, envSubagentPrefix) || (effortFlag && name == core.EnvEffort) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

var fullClaudeID = regexp.MustCompile(`^claude-[a-z0-9.-]+$`)

// comparableModel lowercases a model value and strips a trailing "[…]"
// suffix such as "[1m]".
func comparableModel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasSuffix(s, "]") {
		if i := strings.LastIndex(s, "["); i >= 0 {
			s = s[:i]
		}
	}
	return s
}

// stripProvider drops a provider prefix such as "us.anthropic." before "claude-".
func stripProvider(id string) string {
	if i := strings.Index(id, ".claude-"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// modelMismatch reports whether Claude Code ran another model than the one
// requested. It checks a family alias by prefix, opusplan as opus or sonnet,
// and a full Claude ID exactly; it skips an empty request or report and any
// other value, such as an unknown alias, a Bedrock ID or an ARN.
func modelMismatch(requested, reported string) bool {
	req, rep := comparableModel(requested), stripProvider(comparableModel(reported))
	if req == "" || rep == "" {
		return false
	}
	inFamily := func(f string) bool { return strings.HasPrefix(rep, "claude-"+f+"-") }
	if slices.Contains(core.ModelAliases, req) {
		return !inFamily(req)
	}
	if req == "opusplan" {
		return !inFamily("opus") && !inFamily("sonnet")
	}
	return fullClaudeID.MatchString(req) && req != rep
}

// modelReport is what Claude Code reported for a session's process.
type modelReport struct {
	requested string // the --model value; "" for none
	id        string // the model ID of the init event
	mismatch  bool
}

// ModelReport returns the model ID that Claude Code last reported running
// for the bot's chat (roomID "") or its seat in a room since yargpt started,
// and whether it differs from the requested model. id is "" before a run.
func (r *Runner) ModelReport(botID, roomID string) (id string, mismatch bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rep := r.models[sessionKey{botID, roomID}]
	return rep.id, rep.mismatch
}

// postMeta is what a post of the run records: its resolved model and
// effort, and the model ID that Claude Code reported.
func (r *Runner) postMeta(rc runContext) core.PostMeta {
	m := rc.Post
	m.ModelID, _ = r.ModelReport(rc.BotID, rc.RoomID)
	return m
}

// noteInitModel records the model that a process's init event reports and
// marks or clears the bot's model attention. On a mismatch it writes a
// notice to the chat, once until the requested or the reported model
// changes. Claude Code goes on with the other model; YarGPT never picks one
// itself.
func (r *Runner) noteInitModel(ctx context.Context, s *session, turnID, reported string) {
	if reported == "" {
		return
	}
	rep := modelReport{requested: s.flags.model, id: reported, mismatch: modelMismatch(s.flags.model, reported)}
	r.mu.Lock()
	prev := r.models[s.key]
	r.models[s.key] = rep
	r.mu.Unlock()
	r.svc.SetModelAttention(s.key.botID, rep.mismatch)
	if !rep.mismatch || prev == rep {
		return
	}
	text := fmt.Sprintf("Asked for `%s` (%s), Claude Code ran `%s`. Check the model in Settings.", rep.requested, sourcePhrase(s.flags.modelSource), reported)
	r.modelEntry(ctx, s.key, s.bot.Name, turnID, "model-mismatch", text)
}

// modelEntry writes a notice about the bot's model to its chat, or to the
// room of a seat with the bot's name first. The UI links it to Settings.
func (r *Runner) modelEntry(ctx context.Context, key sessionKey, botName, turnID, kind, text string) {
	transcript := key.botID
	if key.roomID != "" {
		transcript, text = key.roomID, botName+": "+text
	}
	body := map[string]string{"type": kind, "text": text, "botId": key.botID}
	if _, err := r.svc.AppendEntry(ctx, transcript, core.KindEvent, turnID, body); err != nil {
		r.log.Error("record a model notice", "bot", key.botID, "err", err)
	}
}

// modelRejected reports whether the outcome failed because Claude Code could
// not run the requested model.
func (o outcome) modelRejected() bool {
	return o.failed() && (o.modelNotFound || strings.Contains(o.stderr, unrecognizedModel))
}

// noteModelFailure tells the user that Claude Code could not run the turn's
// model, names where the model comes from and marks the bot. The turn is
// not retried: the next one runs when the user writes again, after changing
// the setting.
func (r *Runner) noteModelFailure(ctx context.Context, key sessionKey, botName, turnID string, flags runFlags, out outcome) {
	reason := firstLine(out.errorText())
	text := fmt.Sprintf("Claude Code couldn't run model `%s` (%s): %s", flags.model, sourcePhrase(flags.modelSource), reason)
	if flags.model == "" {
		text = fmt.Sprintf("Claude Code couldn't run its model (%s): %s", sourcePhrase(flags.modelSource), reason)
	}
	if strings.Contains(strings.ToLower(out.resultText+out.stderr), "organization") {
		text += " Your organization's settings may limit which models you can use."
	}
	r.svc.SetModelAttention(key.botID, true)
	r.modelEntry(ctx, key, botName, turnID, "model-error", text)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// sourcePhrase names where a model or effort comes from, in a message.
func sourcePhrase(source string) string {
	switch source {
	case core.SourceBot:
		return "set on this bot"
	case core.SourceTeam:
		return "the team's default"
	case core.SourceGlobal:
		return "the global setting"
	case core.SourceEnv:
		return "from the environment"
	}
	return "Claude Code's default"
}
