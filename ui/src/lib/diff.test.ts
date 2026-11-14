import { assert, test } from "vitest"
import { changeLines, diffLines } from "./diff"

// The hunks have the shape that Claude Code 2.1.325 sends (internal/agent/testdata/claude-2.1.325).
test("a stored hunk gets its header and each line's old and new numbers", () => {
  assert.deepEqual(
    diffLines([{ oldStart: 4, oldLines: 3, newStart: 5, newLines: 3, lines: [" one", "-two", "+TWO", " three"] }]),
    [
      { kind: "hunk", text: "@@ -4,3 +5,3 @@" },
      { kind: "context", text: "one", old: 4, new: 5 },
      { kind: "remove", text: "two", old: 5 },
      { kind: "add", text: "TWO", new: 6 },
      { kind: "context", text: "three", old: 6, new: 7 },
    ],
  )
})

test("no newline at the end of the file is a note, not a line, and takes no number", () => {
  assert.deepEqual(
    diffLines([
      {
        oldStart: 1,
        oldLines: 2,
        newStart: 1,
        newLines: 2,
        lines: ["-old first", "+new first", " old second", "\\ No newline at end of file"],
      },
      { oldStart: 9, oldLines: 1, newStart: 9, newLines: 1, lines: [" last"] },
    ]),
    [
      { kind: "hunk", text: "@@ -1,2 +1,2 @@" },
      { kind: "remove", text: "old first", old: 1 },
      { kind: "add", text: "new first", new: 1 },
      { kind: "context", text: "old second", old: 2, new: 2 },
      { kind: "note", text: "No newline at end of file" },
      { kind: "hunk", text: "@@ -9,1 +9,1 @@" },
      { kind: "context", text: "last", old: 9, new: 9 },
    ],
  )
})

test("a created file is all added lines, numbered from 1", () => {
  assert.deepEqual(diffLines([{ oldStart: 0, oldLines: 0, newStart: 1, newLines: 2, lines: ["+alpha", "+"] }]), [
    { kind: "hunk", text: "@@ -0,0 +1,2 @@" },
    { kind: "add", text: "alpha", new: 1 },
    { kind: "add", text: "", new: 2 },
  ])
})

const c = (text: string) => ({ kind: "context", text })
const r = (text: string) => ({ kind: "remove", text })
const a = (text: string) => ({ kind: "add", text })

test("an edit keeps the lines both sides share at the start and the end as context", () => {
  assert.deepEqual(changeLines("a\nb\nc\nd", "a\nB\nC\nd"), [c("a"), r("b"), r("c"), a("B"), a("C"), c("d")])
  assert.deepEqual(changeLines("a\nb\n", "a\nb\nc\n"), [c("a"), c("b"), a("c")])
  assert.deepEqual(changeLines("x\ny", "y"), [r("x"), c("y")])
  assert.deepEqual(changeLines("two", "TWO"), [r("two"), a("TWO")])
  assert.deepEqual(changeLines("x\ny\nz", "X\ny\nz"), [r("x"), a("X"), c("y"), c("z")])
})

test("a line that matches both the start and the end counts once", () => {
  assert.deepEqual(changeLines("x\nx\n", "x\n"), [c("x"), r("x")])
  assert.deepEqual(changeLines("x\n", "x\nx\n"), [c("x"), a("x")])
  assert.deepEqual(changeLines("a\na\na", "a\na\na"), [c("a"), c("a"), c("a")])
})

const noNewline = { kind: "note", text: "No newline at end of file" }

test("a change of only the final newline shows the last line changed, with a note on the side without it", () => {
  assert.deepEqual(changeLines("x\n", "x"), [r("x"), a("x"), noNewline])
  assert.deepEqual(changeLines("x", "x\n"), [r("x"), noNewline, a("x")])
  assert.deepEqual(changeLines("a\nb", "a\nc\n"), [c("a"), r("b"), noNewline, a("c")])
  assert.deepEqual(changeLines("x\nx", "x"), [r("x"), c("x")])
})

test("new content is all added lines; the newline that ends it starts no line", () => {
  assert.deepEqual(changeLines("", "alpha\nbeta\n"), [a("alpha"), a("beta")])
  assert.deepEqual(changeLines("", "\n"), [a("")])
  assert.deepEqual(changeLines("gone\n", ""), [r("gone")])
  assert.deepEqual(changeLines("", ""), [])
})

test("a line with no diff prefix is a note; only a leading backslash and space are dropped", () => {
  assert.deepEqual(
    diffLines([{ oldStart: 1, oldLines: 0, newStart: 1, newLines: 0, lines: ["\\No newline", "?a\\ b"] }]),
    [
      { kind: "hunk", text: "@@ -1,0 +1,0 @@" },
      { kind: "note", text: "No newline" },
      { kind: "note", text: "?a\\ b" },
    ],
  )
})
