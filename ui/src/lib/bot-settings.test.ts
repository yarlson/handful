import { assert, test } from "vitest"
import type { Bot } from "./api"
import { draftOf, rebase, savePlan, unsaved, type BotDraft } from "./bot-settings"

const bot: Bot = {
  id: "bot-ana",
  kind: "agent",
  name: "Ana",
  instructions: "Be brief.",
  model: "opus",
  effort: "",
  hue: "teal",
  createdAt: 1,
  updatedAt: 1,
  running: false,
  tasks: [],
}

const team: Bot = {
  id: "team-dev",
  kind: "room",
  name: "Dev",
  instructions: "Build it.",
  team: {
    leadId: "bot-ana",
    agreement: "Tests pass.",
    goalBudget: 0,
    dailyBudget: 200,
    decisionMinutes: 45,
    inboxMax: 8,
    inboxPerSender: 2,
    defaultModel: "sonnet",
    defaultEffort: "high",
  },
  createdAt: 1,
  updatedAt: 1,
  running: false,
  tasks: [],
}

test("a bot's draft shows its saved values, with absent ones as empty", () => {
  assert.deepEqual(draftOf({ ...bot, model: undefined, effort: undefined, hue: undefined }), {
    name: "Ana",
    instructions: "Be brief.",
    model: "",
    effort: "",
    hue: "",
    team: null,
  })
})

test("a team's draft shows its numbers as text", () => {
  assert.deepEqual(draftOf(team).team, {
    leadId: "bot-ana",
    agreement: "Tests pass.",
    budget: "200",
    decision: "45",
    inboxMax: "8",
    perSender: "2",
    defaultModel: "sonnet",
    defaultEffort: "high",
  })
})

test("a team without its own limits shows the server's defaults", () => {
  const t = draftOf({ ...team, team: { leadId: "bot-ana", agreement: "", goalBudget: 0 } }).team
  assert.deepInclude(t, {
    budget: "0",
    decision: "30",
    inboxMax: "10",
    perSender: "3",
    defaultModel: "",
    defaultEffort: "",
  })
})

test("a draft equal to the saved values has nothing unsaved", () => {
  assert.isFalse(unsaved(draftOf(team), draftOf(team)))
  assert.isFalse(unsaved(draftOf(bot), draftOf(bot)))
})

test("a draft without team fields has no team change", () => {
  assert.isFalse(unsaved({ ...draftOf(team), team: null }, draftOf(team)))
})

test("each changed field makes the draft unsaved", () => {
  const saved = draftOf(team)
  const edits: Partial<BotDraft>[] = [
    { name: "Dev 2" },
    { instructions: "" },
    { model: "haiku" },
    { effort: "low" },
    { hue: "rose" },
  ]
  for (const edit of edits) assert.isTrue(unsaved({ ...saved, ...edit }, saved), JSON.stringify(edit))
  for (const key of Object.keys(saved.team!)) {
    const draft = { ...saved, team: { ...saved.team!, [key]: "x" } }
    assert.isTrue(unsaved(draft, saved), key)
  }
})

test("Save sends only the bot fields that changed", () => {
  const saved = draftOf(bot)
  assert.deepEqual(savePlan({ ...saved, name: "Ana 2", effort: "max" }, saved, false), {
    bot: { name: "Ana 2", effort: "max" },
  })
})

test("Save sends a changed colour, and never an empty one", () => {
  const saved = draftOf(bot)
  assert.deepEqual(savePlan({ ...saved, hue: "rose" }, saved, false), { bot: { hue: "rose" } })
  assert.deepEqual(savePlan({ ...saved, name: "Ana 2", hue: "" }, saved, false), { bot: { name: "Ana 2" } })
})

test("Save sends a room's name and purpose only", () => {
  const saved = draftOf(team)
  assert.deepEqual(savePlan({ ...saved, instructions: "Ship it.", model: "opus", hue: "rose" }, saved, true), {
    bot: { instructions: "Ship it." },
  })
})

test("Save sends only the team fields that changed, as the server names them", () => {
  const saved = draftOf(team)
  const cases = [
    { edit: { leadId: "bot-bo" }, sent: { leadId: "bot-bo" } },
    { edit: { agreement: "Docs too." }, sent: { agreement: "Docs too." } },
    { edit: { budget: "300" }, sent: { dailyBudget: 300 } },
    { edit: { decision: "60" }, sent: { decisionMinutes: 60 } },
    { edit: { inboxMax: "12" }, sent: { inboxMax: 12 } },
    { edit: { perSender: "4" }, sent: { inboxPerSender: 4 } },
    { edit: { defaultModel: "opus" }, sent: { defaultModel: "opus" } },
    { edit: { defaultEffort: "low" }, sent: { defaultEffort: "low" } },
  ]
  for (const { edit, sent } of cases) {
    const draft = { ...saved, team: { ...saved.team!, ...edit } }
    assert.deepEqual(savePlan(draft, saved, true), { team: sent }, JSON.stringify(edit))
  }
})

test("Save sends both patches when bot and team fields changed", () => {
  const saved = draftOf(team)
  const draft = { ...saved, name: "Core", team: { ...saved.team!, leadId: "bot-bo", budget: "0" } }
  assert.deepEqual(savePlan(draft, saved, true), {
    bot: { name: "Core" },
    team: { leadId: "bot-bo", dailyBudget: 0 },
  })
})

test("Save sends nothing when nothing changed", () => {
  assert.deepEqual(savePlan(draftOf(team), draftOf(team), true), {})
})

test("a reload during an edit keeps the edit and takes the new value of each field the user did not edit", () => {
  const old = draftOf(team)
  const draft = { ...old, name: "Core", team: { ...old.team!, budget: "300" } }
  const fresh = draftOf({
    ...team,
    name: "Dev 2",
    instructions: "Ship it.",
    team: { ...team.team!, dailyBudget: 50, agreement: "Docs too." },
  })
  assert.deepEqual(rebase(draft, old, fresh), {
    ...fresh,
    name: "Core",
    team: { ...fresh.team!, budget: "300" },
  })
})

test("a reload without edits takes the fresh values", () => {
  const old = draftOf(team)
  const fresh = draftOf({ ...bot, name: "Ana 2" })
  assert.deepEqual(rebase(old, old, fresh), fresh)
  assert.deepEqual(rebase(draftOf(bot), draftOf(bot), old), old)
})

test("a reload takes the fresh team for a draft without team fields", () => {
  const old = draftOf(team)
  const fresh = draftOf({ ...team, team: { ...team.team!, inboxMax: 20 } })
  assert.deepEqual(rebase({ ...old, team: null }, old, fresh), fresh)
})

test("Save sends no team patch when the saved values have no team", () => {
  const draft = { ...draftOf(team), name: "Core" }
  assert.deepEqual(savePlan(draft, { ...draftOf(team), team: null }, true), { bot: { name: "Core" } })
})

test("Save checks the team limits only when a team field changed", () => {
  const saved = { ...draftOf(team), team: { ...draftOf(team).team!, budget: "-1" } }
  assert.deepEqual(savePlan({ ...saved, name: "Core" }, saved, true), { bot: { name: "Core" } })
})

test("Save after a reload sends only the user's edit, not the values others saved", () => {
  const old = draftOf(team)
  const draft = { ...old, instructions: "Mine." }
  const fresh = draftOf({ ...team, name: "Theirs", team: { ...team.team!, inboxMax: 20 } })
  assert.deepEqual(savePlan(rebase(draft, old, fresh), fresh, true), { bot: { instructions: "Mine." } })
})

test("Save stops at a team limit out of range, before it sends anything", () => {
  const saved = draftOf(team)
  const cases = [
    { budget: "-1", error: "A team's turns per day must be 0 (no limit) to 10000." },
    { decision: "2", error: "The decision time must be 5 to 1440 minutes." },
    { inboxMax: "60", error: "The inbox can hold 1 to 50 tasks." },
  ]
  for (const { error, ...edit } of cases) {
    const draft = { ...saved, name: "Core", team: { ...saved.team!, ...edit } }
    assert.deepEqual(savePlan(draft, saved, true), { error }, JSON.stringify(edit))
  }
})
