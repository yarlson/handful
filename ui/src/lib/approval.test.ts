import { assert, test } from "vitest"
import { decisionLabel, inputFields, toolTarget } from "./approval"

test("a call's target is its file, command, pattern or address, in one line", () => {
  assert.equal(toolTarget({ file_path: "/w/a.txt", old_string: "x" }), "/w/a.txt")
  assert.equal(toolTarget({ notebook_path: "/w/n.ipynb" }), "/w/n.ipynb")
  assert.equal(toolTarget({ command: "make check\nmake crap", description: "Run checks" }), "make check")
  assert.equal(toolTarget({ pattern: "TODO", path: "/w" }), "TODO")
  assert.equal(toolTarget({ url: "https://example.com/a" }), "https://example.com/a")
  assert.equal(toolTarget({ query: "go 1.27" }), "go 1.27")
  assert.equal(toolTarget({ path: "/w" }), "/w")
  assert.equal(toolTarget({ file_path: "", command: "ls" }), "ls", "an empty value names nothing")
  assert.equal(toolTarget({ file_path: 3, command: "ls" }), "ls", "only text names a target")
  assert.equal(toolTarget({ name: "x" }), "")
  assert.equal(toolTarget("ls"), "")
  assert.equal(toolTarget(null), "")
  assert.equal(toolTarget(["ls"]), "")
})

const long = "x".repeat(80)

test("an input is a list of names and values; a long or multi-line value is a block", () => {
  assert.deepEqual(inputFields({ to: "Ann", text: "hi\nthere", note: long, more: long + "x" }), [
    { name: "to", value: "Ann", block: false },
    { name: "text", value: "hi\nthere", block: true },
    { name: "note", value: long, block: false },
    { name: "more", value: long + "x", block: true },
  ])
})

test("a value that is not text shows as JSON", () => {
  assert.deepEqual(inputFields({ limit: 3, all: true, none: null, ids: ["a"], opts: { a: 1 } }), [
    { name: "limit", value: "3", block: false },
    { name: "all", value: "true", block: false },
    { name: "none", value: "null", block: false },
    { name: "ids", value: '[\n  "a"\n]', block: true },
    { name: "opts", value: '{\n  "a": 1\n}', block: true },
  ])
})

test("input that is not an object is one field", () => {
  assert.deepEqual(inputFields("ls"), [{ name: "input", value: "ls", block: false }])
  assert.deepEqual(inputFields(["a"]), [{ name: "input", value: '[\n  "a"\n]', block: true }])
  assert.deepEqual(inputFields(undefined), [{ name: "input", value: "undefined", block: false }])
})

test("a decision reads as the card's badge", () => {
  assert.equal(decisionLabel({ status: "allowed", decision: "once" }), "allowed")
  assert.equal(decisionLabel({ status: "allowed", decision: "always" }), "always allowed")
  assert.equal(decisionLabel({ status: "allowed" }), "allowed")
  assert.equal(decisionLabel({ status: "denied", decision: "deny" }), "denied")
  assert.equal(decisionLabel({ status: "expired" }), "expired")
})
