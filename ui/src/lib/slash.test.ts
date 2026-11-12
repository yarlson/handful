import { assert, test } from "vitest"
import { findSlashQuery, namedSkills } from "./completion.ts"

// at types text and puts the caret at its end.
const at = (text: string) => findSlashQuery(text, text.length)

test("the menu opens at the start, with commands and skills", () => {
  assert.deepEqual(at("/"), { start: 0, query: "", atStart: true })
  assert.deepEqual(at("/comp"), { start: 0, query: "comp", atStart: true })
  assert.deepEqual(findSlashQuery("/make-proof yes", 3), { start: 0, query: "ma", atStart: true })
})

test("the menu opens after whitespace in the middle, with skills only", () => {
  assert.deepEqual(at("Please use /we"), { start: 11, query: "we", atStart: false })
  assert.deepEqual(at("first line\n/da"), { start: 11, query: "da", atStart: false })
  assert.deepEqual(at("tab\t/x-1"), { start: 4, query: "x-1", atStart: false })
  assert.deepEqual(at("@Copywriter /weekly"), { start: 12, query: "weekly", atStart: false })
  assert.deepEqual(findSlashQuery("use /a then more", 6), { start: 4, query: "a", atStart: false })
})

test("the menu closes once the caret leaves the id", () => {
  assert.strictEqual(findSlashQuery("/make-proof yes", 15), null)
  assert.strictEqual(at("use /weekly-report "), null)
  assert.strictEqual(at("/a."), null)
  assert.strictEqual(at("/A"), null, "upper case is no id")
  assert.strictEqual(findSlashQuery("/abc", 0), null, "the caret before the slash")
  assert.strictEqual(at("we"), null, "no slash")
  assert.strictEqual(findSlashQuery("a /b", 2), null, "the caret before a later slash")
  assert.deepEqual(
    findSlashQuery("/a /b", 2),
    { start: 0, query: "a", atStart: true },
    "the slash before the caret, not after it",
  )
})

test("paths, URLs and and/or never open the menu", () => {
  for (const text of ["src/a/b", "https://handful.dev/x", "and/or", "/usr/", "/usr/bin", "(/a", "x/a", "a /b/c"]) {
    assert.strictEqual(at(text), null, text)
  }
})

test("code spans and fenced blocks never open the menu", () => {
  for (const text of [
    "`/weekly",
    "see `/weekly-report",
    "```\n/we",
    "```js\nx\n/we",
    "a ``/b",
    "`see /we",
    "a `b` `c /we",
    "  ```\n/we",
    "x\n```\n/we",
  ]) {
    assert.strictEqual(at(text), null, text)
  }
  assert.deepEqual(at("`code` /we"), { start: 7, query: "we", atStart: false }, "after a closed span")
  assert.deepEqual(at("```\nx\n```\n/we"), { start: 10, query: "we", atStart: false }, "after a closed fence")
  assert.strictEqual(findSlashQuery("`x /\nmore", 4), null, "a bare slash in a span, before a line break")
  assert.deepEqual(at("`a` b\n/we"), { start: 6, query: "we", atStart: false }, "a span on an earlier line")
  assert.deepEqual(at("``a`` /we"), { start: 6, query: "we", atStart: false }, "after a closed double span")
})

const ids = ["weekly-report", "daily-standup", "a"]

test("a sent message names each known skill it mentions, once, in text order", () => {
  assert.deepEqual(namedSkills("/weekly-report for October", ids), ["weekly-report"])
  assert.deepEqual(namedSkills("Summarise last week, then use /weekly-report.", ids), ["weekly-report"])
  assert.deepEqual(namedSkills("/weekly-report, then also /daily-standup", ids), ["weekly-report", "daily-standup"])
  assert.deepEqual(namedSkills("/daily-standup and /weekly-report and /daily-standup", ids), [
    "daily-standup",
    "weekly-report",
  ])
  assert.deepEqual(namedSkills("@Copywriter /weekly-report for October", ids), ["weekly-report"])
  assert.deepEqual(namedSkills("line one\n/a.", ids), ["a"])
  assert.deepEqual(namedSkills("/a's turn", ids), ["a"])
  assert.deepEqual(namedSkills("/a `code` after", ids), ["a"], "code after the name")
  assert.deepEqual(namedSkills("/a\n```\nx\n```", ids), ["a"], "a fence after the name")
})

test("a sent message names no skill in paths, URLs, code or unknown words", () => {
  for (const text of [
    "src/a/b",
    "https://handful.dev/x",
    "and/or",
    "/usr/bin",
    "`/weekly-report`",
    "```\n/weekly-report\n```",
    "`see /a` x",
    "(/a)",
    "x/a",
    "try /no-such-skill",
    "/weekly-reports",
    "/a/b",
    "",
  ]) {
    assert.deepEqual(namedSkills(text, ids), [], text)
  }
})
