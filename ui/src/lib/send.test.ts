import { assert, test } from "vitest"
import { answers, EMPTY_TASK, TASK_WITH_FILES, sendPlan, type SendPlan } from "./send.ts"

test("an empty draft without files sends nothing", () => {
  assert.deepEqual(sendPlan("", 0, false), { kind: "skip" })
  assert.deepEqual(sendPlan("  \n ", 0, true), { kind: "skip" })
})

test("files alone send a message with no text", () => {
  assert.deepEqual(sendPlan("  ", 2, false), { kind: "message", text: "" })
})

test("text is sent trimmed as a message", () => {
  assert.deepEqual(sendPlan("  hi there \n", 0, false), { kind: "message", text: "hi there" })
  assert.deepEqual(sendPlan("hi", 1, true), { kind: "message", text: "hi" })
})

test("/task, or its old name /goal, sends a task only in a team's chat", () => {
  assert.deepEqual(sendPlan(" /task Ship CSV ", 0, true), { kind: "task", task: "Ship CSV" })
  assert.deepEqual(sendPlan("/goal Ship CSV", 0, true), { kind: "task", task: "Ship CSV" })
  assert.deepEqual(sendPlan("/task Ship CSV", 0, false), {
    kind: "message",
    text: "/task Ship CSV",
  })
  assert.deepEqual(sendPlan("/tasks", 0, true), { kind: "message", text: "/tasks" })
})

test("the errors say what to fix", () => {
  assert.equal(EMPTY_TASK, "Write the task after /task, for example: /task Add CSV export")
  assert.equal(TASK_WITH_FILES, "A task is text only: remove the attachments, or send them in a message")
})

test("an empty /task is rejected with an example", () => {
  assert.deepEqual(sendPlan("/task  ", 0, true), { kind: "invalid", error: EMPTY_TASK })
  // The empty task is the first problem named, even with files.
  assert.deepEqual(sendPlan("/task", 1, true), { kind: "invalid", error: EMPTY_TASK })
})

test("a task with files is rejected", () => {
  assert.deepEqual(sendPlan("/task Ship", 1, true), { kind: "invalid", error: TASK_WITH_FILES })
})

test("/clear and /compact without files are commands in any chat", () => {
  for (const team of [false, true]) {
    assert.deepEqual(sendPlan(" /clear ", 0, team), { kind: "clear" })
    assert.deepEqual(sendPlan("/compact", 0, team), { kind: "compact" })
  }
})

test("/clear and /compact with files are messages", () => {
  assert.deepEqual(sendPlan("/clear", 1, false), { kind: "message", text: "/clear" })
  assert.deepEqual(sendPlan("/compact", 3, true), { kind: "message", text: "/compact" })
})

test("other slash words are messages", () => {
  assert.deepEqual(sendPlan("/clear now", 0, false), { kind: "message", text: "/clear now" })
  assert.deepEqual(sendPlan("/toString", 0, false), { kind: "message", text: "/toString" })
  assert.deepEqual(sendPlan("constructor", 0, false), { kind: "message", text: "constructor" })
  assert.deepEqual(sendPlan("__proto__", 0, false), { kind: "message", text: "__proto__" })
})

test("only a message answers the Answer chip's question", () => {
  const plans: [SendPlan, boolean][] = [
    [{ kind: "message", text: "x" }, true],
    [{ kind: "task", task: "x" }, false],
    [{ kind: "clear" }, false],
    [{ kind: "compact" }, false],
    [{ kind: "skip" }, false],
    [{ kind: "invalid", error: "x" }, false],
  ]
  for (const [plan, want] of plans) assert.equal(answers(plan), want, plan.kind)
})
