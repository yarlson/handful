import { assert, test } from "vitest"
import { offeredSkills, skillLayerLabel, teamCommands } from "./skills.ts"

const skills = [{ id: "grill-me" }, { id: "goal" }, { id: "clear" }, { id: "compact" }, { id: "review" }]

test("a bot's own chat offers every skill", () => {
  assert.deepEqual(offeredSkills(skills, { kind: "agent" }), skills)
})

test("a team's chat offers the skills that no team command hides", () => {
  assert.deepEqual(offeredSkills(skills, { kind: "room", team: {} }), [{ id: "grill-me" }, { id: "review" }])
  assert.deepEqual(teamCommands, ["clear", "compact", "task", "goal"])
})

test("a plain room offers no skills", () => {
  assert.deepEqual(offeredSkills(skills, { kind: "room" }), [])
})

test("a bot's own chat offers its own skill once, not the shared one it replaces", () => {
  const listed = [
    { id: "review", layer: "bot" },
    { id: "deploy", layer: "shared" },
    { id: "review", layer: "shared", replaced: true },
  ]
  assert.deepEqual(offeredSkills(listed, { kind: "agent" }), [
    { id: "review", layer: "bot" },
    { id: "deploy", layer: "shared" },
  ])
})

test("a skill's label says which bots it is for", () => {
  assert.equal(skillLayerLabel({ layer: "bot" }), "This bot only")
  assert.equal(skillLayerLabel({ layer: "shared" }), "Shared")
  assert.equal(skillLayerLabel({ layer: "shared", replaced: true }), "Replaced for this bot")
  assert.equal(skillLayerLabel({ layer: "shared", replaced: false }), "Shared")
})
