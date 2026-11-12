import { assert, test } from "vitest"
import { offeredSkills, teamCommands } from "./skills.ts"

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
