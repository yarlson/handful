// Model and effort: how the UI names a bot's values and where they come from.
// The server resolves them (bot → team default → global → environment →
// Claude Code); this file only words them.

export type ModelSource = "bot" | "team" | "global" | "env" | "claude-code"
export type GlobalSource = "flag" | "config" | "env" | "claude-code"

// Effective is what a bot runs with in one chat.
export type Effective = {
  model: string
  modelSource: ModelSource
  modelId?: string
  modelMismatch?: boolean
  effort: string
  effortSource: ModelSource
}

// Defaults are the global values, read-only in the UI, and the choices.
export type Defaults = {
  model: string
  modelSource: GlobalSource
  effort: string
  effortSource: GlobalSource
  envOverrides: string[]
  modelAliases: string[]
  effortLevels: string[]
}

export const CUSTOM = "custom"

const LONG_ID = 24

// isAlias tells whether a model value is one of the aliases offered in the select.
export function isAlias(model: string, aliases: string[]): boolean {
  return aliases.includes(model)
}

// shortId shortens a long model ID in the middle, so the start and the end stay readable.
export function shortId(id: string): string {
  if (id.length <= LONG_ID) return id
  const half = (LONG_ID - 1) / 2
  return `${id.slice(0, Math.ceil(half))}…${id.slice(-Math.floor(half))}`
}

// modelName names a model value: an alias capitalized, a custom ID shortened.
export function modelName(model: string, aliases: string[]): string {
  if (isAlias(model, aliases)) return model.charAt(0).toUpperCase() + model.slice(1)
  return shortId(model)
}

// effortName names an effort level for a select.
export function effortName(level: string): string {
  if (level === "xhigh") return "Extra high (xhigh)"
  return level.charAt(0).toUpperCase() + level.slice(1)
}

// sourceName names where a value comes from, for the "Default — …" options.
export function sourceName(source: ModelSource | GlobalSource): string {
  if (source === "team") return "team"
  if (source === "global") return "global settings"
  if (source === "flag") return "flag"
  if (source === "config") return "config"
  if (source === "env") return "environment"
  if (source === "bot") return "this bot"
  return "Claude Code's default"
}

// Inherited is what a value would be without the bot's (or team's) own setting.
export type Inherited = { value: string; source: ModelSource | GlobalSource }

// defaultModelOption is the label of the select's first option, which inherits.
export function defaultModelOption(inherited: Inherited, aliases: string[]): string {
  if (!inherited.value) return "Default — Claude Code's default"
  return `Default — ${modelName(inherited.value, aliases)} (from ${sourceName(inherited.source)})`
}

// defaultEffortOption is the label of the effort select's first option. With no inherited
// level it never names one: the model's own default differs per model.
export function defaultEffortOption(inherited: Inherited): string {
  if (!inherited.value) return "Default (model's own)"
  return `Default — ${effortName(inherited.value)} (from ${sourceName(inherited.source)})`
}

// inheritedFromGlobal is what a bot inherits outside a team: the global values.
export function inheritedFromGlobal(d: Defaults): { model: Inherited; effort: Inherited } {
  return {
    model: { value: d.model, source: d.modelSource },
    effort: { value: d.effort, source: d.effortSource },
  }
}

// selectValue is what the model select shows for a stored value: "" for Default, the alias,
// or CUSTOM for any other ID.
export function selectValue(model: string, aliases: string[]): string {
  if (!model) return ""
  return isAlias(model, aliases) ? model : CUSTOM
}

// chipText is the short label of a bot's values: "Opus · max", or "default effort" when
// nothing sets an effort.
export function chipText(e: Effective, aliases: string[]): string {
  const model = e.model ? modelName(e.model, aliases) : "Default"
  return `${model} · ${e.effort || "default effort"}`
}

// sourceLine names where a member's values come from, for the Members tab: "own", "team
// default", "team · own" (model from the team, effort its own), and so on.
export function sourceLine(e: Effective): string {
  const word = (s: ModelSource) =>
    s === "bot"
      ? "own"
      : s === "team"
        ? "team default"
        : s === "env"
          ? "environment"
          : s === "global"
            ? "global"
            : "Claude Code"
  const m = word(e.modelSource)
  const f = word(e.effortSource)
  return m === f ? m : `model ${m} · effort ${f}`
}

// chipLabel is the full description of a bot's values, for the chip's aria-label and
// tooltip. It names the full model ID when a run reported one, and a mismatch first.
export function chipLabel(e: Effective, aliases: string[]): string {
  const name = e.model ? modelName(e.model, aliases) : "Claude Code's default model"
  const id = e.modelId && e.modelId !== e.model ? ` (${e.modelId})` : ""
  const model = `Model ${name}${id}, ${originPhrase(e.modelSource)}.`
  const effort = e.effort
    ? `Requested effort ${e.effort}, ${originPhrase(e.effortSource)}.`
    : "Effort: Default (model's own)."
  const mismatch = e.modelMismatch && e.modelId ? `Asked for ${name}, ran ${e.modelId}. ` : ""
  return `${mismatch}${model} ${effort}`
}

function originPhrase(s: ModelSource): string {
  if (s === "bot") return "set on this bot"
  if (s === "team") return "the team's default"
  if (s === "global") return "from global settings"
  if (s === "env") return "from the environment"
  return "Claude Code's default"
}

// postTooltip describes the model and effort that wrote a post, or null for posts that
// don't record them.
export function postTooltip(
  body: { model?: string; modelId?: string; effort?: string },
  aliases: string[],
): string | null {
  if (!body.model && !body.modelId && !body.effort) return null
  const parts: string[] = []
  const shown = body.model || body.modelId
  if (shown) {
    const name = body.model ? modelName(body.model, aliases) : shown
    const id = body.modelId && body.modelId !== body.model && body.model ? ` (${body.modelId})` : ""
    parts.push(`Written with ${name}${id}`)
  } else {
    parts.push("Written with Claude Code's default model")
  }
  if (body.effort) parts.push(`effort ${body.effort}`)
  return parts.join(" · ")
}

// setupLine describes a Builder setup's model and effort for a bot, or null when it sets
// neither.
export function setupLine(model: string, effort: string, aliases: string[]): string | null {
  const parts: string[] = []
  if (model) parts.push(`Model: ${modelName(model, aliases)}`)
  if (effort) parts.push(`Effort: ${effort}`)
  return parts.length ? parts.join(" · ") : null
}

// teamSetupLine describes a Builder setup's member defaults for a team, or null.
export function teamSetupLine(model: string, effort: string, aliases: string[]): string | null {
  if (!model && !effort) return null
  const m = model ? modelName(model, aliases) : "the default model"
  return `Members default to ${m} · ${effort || "default effort"}`
}

// overrides counts the members of a team that set their own model, which the team's
// default then doesn't reach.
export function overrides(members: { effective?: Effective }[]): number {
  return members.filter((m) => m.effective?.modelSource === "bot").length
}

// globalSource maps where a global value comes from onto a bot's sources.
function globalSource(s: GlobalSource): ModelSource {
  if (s === "flag" || s === "config") return "global"
  return s
}

// teamEffective is what a team's members without their own values run with in the team.
export function teamEffective(team: { defaultModel?: string; defaultEffort?: string }, d: Defaults): Effective {
  return {
    model: team.defaultModel || d.model,
    modelSource: team.defaultModel ? "team" : globalSource(d.modelSource),
    effort: team.defaultEffort || d.effort,
    effortSource: team.defaultEffort ? "team" : globalSource(d.effortSource),
  }
}

// ignoredOverrides lists the environment variables that Handful removes from a bot's run.
export function ignoredOverrides(d: Defaults): string[] {
  return d.envOverrides.filter((v) => v.startsWith("CLAUDE_CODE_SUBAGENT_"))
}
