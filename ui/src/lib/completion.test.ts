import { assert, test } from "vitest"
import { applyCompletion, findMentionQuery, findSlashQuery, matchNames } from "./completion.ts"

test("finds the mention the caret is in", () => {
  assert.deepEqual(findMentionQuery("hi @Bo", 6), { start: 3, query: "Bo" })
  assert.deepEqual(findMentionQuery("@", 1), { start: 0, query: "" })
  assert.deepEqual(findMentionQuery("ask @an please", 7), { start: 4, query: "an" })
})

test("ignores an @ inside a word, after a space, or far from the caret", () => {
  assert.strictEqual(findMentionQuery("mail me@example.com", 19), null)
  assert.strictEqual(findMentionQuery("@Bo thanks", 10), null)
  assert.strictEqual(findMentionQuery("no mention here", 5), null)
})

test("matches names by prefix first, then by substring, ignoring case", () => {
  const names = ["Ana", "Bo", "Banana", "Cy"]
  assert.deepEqual(matchNames(names, "an"), ["Ana", "Banana"])
  assert.deepEqual(matchNames(names, "B"), ["Bo", "Banana"])
  assert.deepEqual(matchNames(names, ""), names)
  assert.deepEqual(matchNames(names, "zz"), [])
})

test("replaces the typed mention with the full name and a space", () => {
  assert.deepEqual(applyCompletion("hi @b", { start: 3, query: "b" }, 5, "@Bo"), { text: "hi @Bo ", caret: 7 })
  assert.deepEqual(applyCompletion("@a what is 2+2", { start: 0, query: "a" }, 2, "@Ana"), {
    text: "@Ana what is 2+2",
    caret: 5,
  })
  assert.deepEqual(applyCompletion("x @R team", { start: 2, query: "R" }, 4, "@Research Bot"), {
    text: "x @Research Bot team",
    caret: 16,
  })
})

test("finds a command or skill only in the first word", () => {
  assert.deepEqual(findSlashQuery("/", 1), { start: 0, query: "" })
  assert.deepEqual(findSlashQuery("/comp", 5), { start: 0, query: "comp" })
  assert.deepEqual(findSlashQuery("/make-proof yes", 3), { start: 0, query: "ma" })
  assert.strictEqual(findSlashQuery("/make-proof yes", 15), null)
  assert.strictEqual(findSlashQuery("see /clear", 10), null)
})

test("replaces a typed command with the full command", () => {
  assert.deepEqual(applyCompletion("/cl", { start: 0, query: "cl" }, 3, "/clear"), { text: "/clear ", caret: 7 })
  assert.deepEqual(applyCompletion("/ma yes", { start: 0, query: "ma" }, 3, "/make-proof"), {
    text: "/make-proof yes",
    caret: 12,
  })
})
