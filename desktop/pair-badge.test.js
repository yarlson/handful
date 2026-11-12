const assert = require("node:assert/strict")
const { test } = require("node:test")
const { createPairBadge } = require("./pair-badge")

function setup({ focused = false } = {}) {
  const calls = []
  const state = { focused }
  const dock = {
    setBadge: (text) => calls.push(["setBadge", text]),
    bounce: (type) => calls.push(["bounce", type]),
  }
  const badge = createPairBadge({ dock, isFocused: () => state.focused })
  return { badge, calls, state }
}

test("a pair request in the background sets the badge and bounces once", () => {
  const { badge, calls } = setup()
  badge.change(true)
  assert.deepEqual(calls, [
    ["setBadge", "1"],
    ["bounce", "informational"],
  ])
})

test("the end of a pair request clears the badge", () => {
  const { badge, calls } = setup()
  badge.change(true)
  badge.change(false)
  assert.deepEqual(calls.at(-1), ["setBadge", ""])
})

test("focus clears the badge while the request still waits", () => {
  const { badge, calls } = setup()
  badge.change(true)
  badge.focus()
  assert.deepEqual(calls.at(-1), ["setBadge", ""])
})

test("closing the window clears the badge, and a later request sets it again", () => {
  const { badge, calls } = setup()
  badge.change(true)
  badge.closed()
  assert.deepEqual(calls.at(-1), ["setBadge", ""])
  badge.change(true)
  assert.deepEqual(calls.slice(-2), [
    ["setBadge", "1"],
    ["bounce", "informational"],
  ])
})

test("a request while the window is in front sets no badge", () => {
  const { badge, calls } = setup({ focused: true })
  badge.change(true)
  badge.change(false)
  assert.deepEqual(calls, [["setBadge", ""]])
})

test("without a pair request the badge is never set", () => {
  const { badge, calls } = setup()
  badge.change(false)
  badge.change(0)
  badge.change("1")
  badge.focus()
  assert.ok(!calls.some(([name, text]) => name === "bounce" || text === "1"), JSON.stringify(calls))
})

test("a repeated start does not bounce again", () => {
  const { badge, calls } = setup()
  badge.change(true)
  badge.change(true)
  assert.equal(calls.filter(([name]) => name === "bounce").length, 1)
})

test("with no Dock nothing fails", () => {
  const badge = createPairBadge({ dock: null, isFocused: () => false })
  badge.change(true)
  badge.focus()
  badge.change(false)
  badge.closed()
})
