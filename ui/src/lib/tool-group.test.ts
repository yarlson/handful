import { assert, test } from "vitest"
import type { ActivityBody } from "./api"
import { groupKey, groupSummary } from "./tool-group"

const call = (tool: string, more: Partial<ActivityBody> = {}): ActivityBody => ({
  tool,
  summary: "",
  status: "done",
  subagent: false,
  ...more,
})

test("a group counts its calls of each tool, the most first, then in the order of their first call", () => {
  const g = groupSummary([call("Bash"), call("Read"), call("Edit"), call("Read"), call("Edit"), call("Read")])
  assert.deepEqual(g.counts, [
    { tool: "Read", count: 3 },
    { tool: "Edit", count: 2 },
    { tool: "Bash", count: 1 },
  ])
  assert.deepEqual(groupSummary([call("Grep"), call("Glob")]).counts, [
    { tool: "Grep", count: 1 },
    { tool: "Glob", count: 1 },
  ])
})

test("a group adds up its calls' diffs, a truncated one included", () => {
  const g = groupSummary([
    call("Edit", { diff: { added: 3, removed: 1, hunks: [] } }),
    call("Read"),
    call("Write", { diff: { added: 40, removed: 7, truncated: true } }),
  ])
  assert.deepEqual(g.diff, { added: 43, removed: 8 })
  assert.isNull(groupSummary([call("Read"), call("Bash")]).diff, "no call changed a file")
  assert.deepEqual(groupSummary([call("Edit", { diff: { added: 0, removed: 0 } }), call("Read")]).diff, {
    added: 0,
    removed: 0,
  })
})

test("a group runs while any call runs, and counts its failed calls", () => {
  const done = groupSummary([call("Read"), call("Bash")])
  assert.isFalse(done.running)
  assert.equal(done.failed, 0)
  const g = groupSummary([call("Read", { status: "running" }), call("Bash", { status: "error" }), call("Bash")])
  assert.isTrue(g.running)
  assert.equal(g.failed, 1)
  assert.isTrue(groupSummary([call("Read"), call("Bash", { status: "running" })]).running)
})

test("a group opens by a key of its own, apart from its first call's row", () => {
  assert.notEqual(groupKey("e1"), "e1")
  assert.notEqual(groupKey("e1"), groupKey("e2"))
})
