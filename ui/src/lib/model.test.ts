import { assert, test } from "vitest"
import {
  chipLabel,
  chipText,
  CUSTOM,
  defaultEffortOption,
  defaultModelOption,
  effortName,
  ignoredOverrides,
  inheritedFromGlobal,
  isAlias,
  modelName,
  overrides,
  postTooltip,
  selectValue,
  setupLine,
  shortId,
  sourceLine,
  sourceName,
  teamEffective,
  teamSetupLine,
  type Defaults,
  type Effective,
} from "./model.ts"

const aliases = ["fable", "opus", "sonnet", "haiku"]
const eff = (e: Partial<Effective>): Effective => ({
  model: "",
  modelSource: "claude-code",
  effort: "",
  effortSource: "claude-code",
  ...e,
})
const defaults = (d: Partial<Defaults>): Defaults => ({
  model: "",
  modelSource: "claude-code",
  effort: "",
  effortSource: "claude-code",
  envOverrides: [],
  modelAliases: aliases,
  effortLevels: ["low", "medium", "high", "xhigh", "max"],
  ...d,
})

test("aliases are capitalized and custom IDs are kept, shortened in the middle when long", () => {
  assert.isTrue(isAlias("opus", aliases))
  assert.isFalse(isAlias("opusplan", aliases))
  assert.equal(modelName("opus", aliases), "Opus")
  assert.equal(modelName("claude-opus-5-5", aliases), "claude-opus-5-5")
  const id = "us.anthropic.claude-sonnet-5-5-20261001-v1:0"
  const short = shortId(id)
  assert.equal(short.length, 24)
  assert.isTrue(short.startsWith("us.anthropic"))
  assert.isTrue(short.endsWith("1001-v1:0"))
  assert.include(short, "…")
  assert.equal(shortId("a".repeat(24)), "a".repeat(24))
  assert.equal(shortId("a".repeat(25)).length, 24)
})

test("effort levels read as words, and xhigh is spelled out", () => {
  assert.equal(effortName("low"), "Low")
  assert.equal(effortName("max"), "Max")
  assert.equal(effortName("xhigh"), "Extra high (xhigh)")
})

test("each source has a name", () => {
  assert.deepEqual((["team", "global", "flag", "config", "env", "bot", "claude-code"] as const).map(sourceName), [
    "team",
    "global settings",
    "flag",
    "config",
    "environment",
    "this bot",
    "Claude Code's default",
  ])
})

test("the Default options name what they inherit, and never an effort level nothing sets", () => {
  assert.equal(defaultModelOption({ value: "sonnet", source: "config" }, aliases), "Default — Sonnet (from config)")
  assert.equal(defaultModelOption({ value: "", source: "claude-code" }, aliases), "Default — Claude Code's default")
  assert.equal(defaultEffortOption({ value: "high", source: "team" }), "Default — High (from team)")
  assert.equal(defaultEffortOption({ value: "", source: "claude-code" }), "Default (model's own)")
})

test("a bot outside a team inherits the global values", () => {
  assert.deepEqual(
    inheritedFromGlobal(defaults({ model: "opus", modelSource: "flag", effort: "low", effortSource: "env" })),
    {
      model: { value: "opus", source: "flag" },
      effort: { value: "low", source: "env" },
    },
  )
})

test("the model select shows Default, an alias or the custom field", () => {
  assert.equal(selectValue("", aliases), "")
  assert.equal(selectValue("haiku", aliases), "haiku")
  assert.equal(selectValue("claude-opus-5-5", aliases), CUSTOM)
  // The custom choice can't be mistaken for Default, whose value is "".
  assert.equal(CUSTOM, "custom")
})

test("a chip names the model and effort, or says what is default", () => {
  assert.equal(chipText(eff({ model: "opus", effort: "max" }), aliases), "Opus · max")
  assert.equal(chipText(eff({ model: "opus" }), aliases), "Opus · default effort")
  assert.equal(chipText(eff({}), aliases), "Default · default effort")
})

test("a chip's label names each value with its source, the run's model ID and a mismatch", () => {
  assert.equal(
    chipLabel(
      eff({ model: "opus", modelSource: "bot", modelId: "claude-opus-5-5", effort: "max", effortSource: "bot" }),
      aliases,
    ),
    "Model Opus (claude-opus-5-5), set on this bot. Requested effort max, set on this bot.",
  )
  assert.equal(
    chipLabel(eff({ model: "sonnet", modelSource: "team", effort: "medium", effortSource: "global" }), aliases),
    "Model Sonnet, the team's default. Requested effort medium, from global settings.",
  )
  assert.equal(
    chipLabel(eff({ modelSource: "claude-code" }), aliases),
    "Model Claude Code's default model, Claude Code's default. Effort: Default (model's own).",
  )
  assert.equal(
    chipLabel(
      eff({ model: "claude-x", modelId: "claude-x", modelSource: "env", effort: "low", effortSource: "env" }),
      aliases,
    ),
    "Model claude-x, from the environment. Requested effort low, from the environment.",
  )
  assert.equal(
    chipLabel(eff({ model: "opus", modelSource: "bot", modelId: "claude-sonnet-5-5", modelMismatch: true }), aliases),
    "Asked for Opus, ran claude-sonnet-5-5. Model Opus (claude-sonnet-5-5), set on this bot. Effort: Default (model's own).",
  )
  // A mismatch without a reported ID has nothing to name.
  assert.notInclude(chipLabel(eff({ model: "opus", modelMismatch: true }), aliases), "Asked for")
})

test("the Members tab names where a member's values come from", () => {
  assert.equal(sourceLine(eff({ modelSource: "bot", effortSource: "bot" })), "own")
  assert.equal(sourceLine(eff({ modelSource: "team", effortSource: "team" })), "team default")
  assert.equal(sourceLine(eff({ modelSource: "team", effortSource: "bot" })), "model team default · effort own")
  assert.equal(sourceLine(eff({ modelSource: "global", effortSource: "env" })), "model global · effort environment")
  assert.equal(sourceLine(eff({})), "Claude Code")
})

test("a post's tooltip names the model and effort that wrote it, and old posts have none", () => {
  assert.isNull(postTooltip({}, aliases))
  assert.equal(
    postTooltip({ model: "opus", modelId: "claude-opus-5-5", effort: "high" }, aliases),
    "Written with Opus (claude-opus-5-5) · effort high",
  )
  assert.equal(
    postTooltip({ model: "claude-opus-5-5", modelId: "claude-opus-5-5" }, aliases),
    "Written with claude-opus-5-5",
  )
  assert.equal(postTooltip({ modelId: "claude-opus-5-5" }, aliases), "Written with claude-opus-5-5")
  assert.equal(postTooltip({ effort: "low" }, aliases), "Written with Claude Code's default model · effort low")
})

test("a setup summary names only the values it sets", () => {
  assert.isNull(setupLine("", "", aliases))
  assert.equal(setupLine("haiku", "low", aliases), "Model: Haiku · Effort: low")
  assert.equal(setupLine("", "max", aliases), "Effort: max")
  assert.equal(setupLine("opus", "", aliases), "Model: Opus")
  assert.isNull(teamSetupLine("", "", aliases))
  assert.equal(teamSetupLine("sonnet", "medium", aliases), "Members default to Sonnet · medium")
  assert.equal(teamSetupLine("", "low", aliases), "Members default to the default model · low")
  assert.equal(teamSetupLine("haiku", "", aliases), "Members default to Haiku · default effort")
})

test("members with their own model count as overriding the team's", () => {
  assert.equal(
    overrides([{ effective: eff({ modelSource: "bot" }) }, { effective: eff({ modelSource: "team" }) }, {}]),
    1,
  )
})

test("a team's values come from its defaults, else from the global ones", () => {
  const d = defaults({ model: "opus", modelSource: "config", effort: "low", effortSource: "flag" })
  assert.deepEqual(teamEffective({ defaultModel: "sonnet", defaultEffort: "medium" }, d), {
    model: "sonnet",
    modelSource: "team",
    effort: "medium",
    effortSource: "team",
  })
  assert.deepEqual(teamEffective({}, d), {
    model: "opus",
    modelSource: "global",
    effort: "low",
    effortSource: "global",
  })
  assert.deepEqual(teamEffective({}, defaults({ effort: "x", effortSource: "env" })), {
    model: "",
    modelSource: "claude-code",
    effort: "x",
    effortSource: "env",
  })
})

test("only the subagent variables are listed as ignored", () => {
  assert.deepEqual(
    ignoredOverrides(defaults({ envOverrides: ["CLAUDE_CODE_EFFORT_LEVEL", "CLAUDE_CODE_SUBAGENT_MODEL"] })),
    ["CLAUDE_CODE_SUBAGENT_MODEL"],
  )
})
