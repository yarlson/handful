import { assert, test } from "vitest"
import { applyCompletion, findSlashQuery } from "./completion.ts"
import { goalCommand, parseGoalCommand } from "./goal.ts"

test("reads the goal text after /goal", () => {
  assert.strictEqual(parseGoalCommand("/goal Ship CSV export"), "Ship CSV export")
  assert.strictEqual(parseGoalCommand("  /goal   Ship it  "), "Ship it")
  assert.strictEqual(parseGoalCommand("/goal\nFirst line\nSecond line"), "First line\nSecond line")
  assert.strictEqual(parseGoalCommand("/goal\tTabbed"), "Tabbed")
})

test("reads a bare /goal as an empty goal", () => {
  assert.strictEqual(parseGoalCommand("/goal"), "")
  assert.strictEqual(parseGoalCommand("/goal   "), "")
  assert.strictEqual(parseGoalCommand(" /goal\n"), "")
})

test("ignores messages that are not the command", () => {
  assert.strictEqual(parseGoalCommand("/goals"), null)
  assert.strictEqual(parseGoalCommand("/goalkeeper wins"), null)
  assert.strictEqual(parseGoalCommand("set a /goal please"), null)
  assert.strictEqual(parseGoalCommand("goal ship it"), null)
  assert.strictEqual(parseGoalCommand("/Goal ship it"), null)
  assert.strictEqual(parseGoalCommand(""), null)
})

test("choosing the /goal suggestion leaves the caret after its space", () => {
  const typed = "/go"
  const query = findSlashQuery(typed, typed.length)!
  assert.deepEqual(applyCompletion(typed, query, typed.length, goalCommand), { text: "/goal ", caret: 6 })
})
