import { assert, test } from "vitest"
import { offeredSkills, teamCommands, teamSkillOf, withoutMentions } from "./skills.ts"

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

test("finds the skill that a team post invokes", () => {
  const ids = skills.map((s) => s.id)
  const members = ["Forge", "Forge Dev", "Bo"]
  assert.strictEqual(teamSkillOf("/grill-me the plan", ids, members), "grill-me")
  assert.strictEqual(teamSkillOf("  /review", ids, members), "review")
  assert.strictEqual(teamSkillOf("@Forge Dev /grill-me x", ids, members), "grill-me")
  assert.strictEqual(teamSkillOf("@bo, @here: /review", ids, members), "review")
})

test("finds no skill in other posts", () => {
  const ids = skills.map((s) => s.id)
  const members = ["Bo"]
  assert.strictEqual(teamSkillOf("/goal ship it", ids, members), null)
  assert.strictEqual(teamSkillOf("/clear", ids, members), null)
  assert.strictEqual(teamSkillOf("/compact", ids, members), null)
  assert.strictEqual(teamSkillOf("/nope x", ids, members), null)
  assert.strictEqual(teamSkillOf("/grill-mex", ids, members), null)
  assert.strictEqual(teamSkillOf("please /review", ids, members), null)
  assert.strictEqual(teamSkillOf("@Cy /review", ids, members), null)
  assert.strictEqual(teamSkillOf("", ids, members), null)
  assert.strictEqual(teamSkillOf("xreview", ids, members), null)
})

test("drops the leading mentions of members", () => {
  const members = ["Forge", "Forge Dev", "Bo"]
  assert.strictEqual(withoutMentions("/x go", members), "/x go")
  assert.strictEqual(withoutMentions("  /x go ", members), "/x go")
  assert.strictEqual(withoutMentions("@Forge Dev /x", members), "/x")
  assert.strictEqual(withoutMentions("@forge /x", members), "/x")
  assert.strictEqual(withoutMentions("@Bo, @here: /x", members), "/x")
  assert.strictEqual(withoutMentions("@HERE\n/x", members), "/x")
  assert.strictEqual(withoutMentions("@Bob /x", members), "@Bob /x")
  assert.strictEqual(withoutMentions("@heresy /x", members), "@heresy /x")
  assert.strictEqual(withoutMentions("@Bo7 /x", members), "@Bo7 /x")
  assert.strictEqual(withoutMentions("@Bo", members), "")
  assert.strictEqual(withoutMentions("@", members), "@")
  assert.strictEqual(withoutMentions("@Bo hi /x", members), "hi /x")
  assert.strictEqual(withoutMentions("xBo /x", members), "xBo /x")
  assert.strictEqual(withoutMentions("@Bo/x go", members), "/x go")
})
