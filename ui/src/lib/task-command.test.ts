import { assert, test } from "vitest"
import { applyCompletion, findSlashQuery } from "./completion.ts"
import { parseTaskCommand, taskCommand } from "./task-command.ts"

test("reads the task text after /task, and after its old name /goal", () => {
  assert.strictEqual(parseTaskCommand("/task Ship CSV export"), "Ship CSV export")
  assert.strictEqual(parseTaskCommand("  /task   Ship it  "), "Ship it")
  assert.strictEqual(parseTaskCommand("/task\nFirst line\nSecond line"), "First line\nSecond line")
  assert.strictEqual(parseTaskCommand("/task\tTabbed"), "Tabbed")
  assert.strictEqual(parseTaskCommand("/goal Ship CSV export"), "Ship CSV export")
  assert.strictEqual(parseTaskCommand("/goal\nFirst line"), "First line")
})

test("reads a bare command as an empty task", () => {
  assert.strictEqual(parseTaskCommand("/task"), "")
  assert.strictEqual(parseTaskCommand("/task   "), "")
  assert.strictEqual(parseTaskCommand(" /goal\n"), "")
})

test("ignores messages that are not the command", () => {
  for (const m of [
    "/tasks",
    "/taskmaster wins",
    "/goals",
    "/goalkeeper wins",
    "set a /task please",
    "task ship it",
    "/Task ship it",
    "",
  ])
    assert.strictEqual(parseTaskCommand(m), null, m)
})

test("choosing the /task suggestion leaves the caret after its space", () => {
  const typed = "/ta"
  const query = findSlashQuery(typed, typed.length)!
  assert.deepEqual(applyCompletion(typed, query, typed.length, taskCommand), {
    text: "/task ",
    caret: 6,
  })
})
