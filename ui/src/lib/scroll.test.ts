import { assert, test } from "vitest"
import { anchorIndex, atBottom, PIN_THRESHOLD, ScrollMemory } from "./scroll.ts"

// view returns the metrics of a 1000px transcript in a 200px viewport, scrolled to top.
const view = (scrollTop: number) => ({ scrollTop, scrollHeight: 1000, clientHeight: 200 })
const bottom = 800

test("the view is at the bottom within the threshold, and not beyond it", () => {
  assert.isTrue(atBottom(view(bottom)))
  assert.isTrue(atBottom(view(bottom - PIN_THRESHOLD)))
  assert.isFalse(atBottom(view(bottom - PIN_THRESHOLD - 1)))
  assert.isFalse(atBottom(view(0)))
})

test("the threshold is small", () => {
  assert.isAtLeast(PIN_THRESHOLD, 20)
  assert.isAtMost(PIN_THRESHOLD, 80)
})

test("a chat opened for the first time starts at the bottom and follows it", () => {
  const m = new ScrollMemory()
  assert.isTrue(m.pinned("a"))
  assert.equal(m.target("a", true, 1000), 1000)
  assert.equal(m.target("a", false, 1200), 1200)
})

test("while pinned, new content keeps the view at the bottom", () => {
  const m = new ScrollMemory()
  assert.isTrue(m.record("a", view(bottom)))
  assert.isTrue(m.pinned("a"))
  assert.equal(m.target("a", false, 1300), 1300)
})

test("scrolling up unpins the chat, and new content does not move the view", () => {
  const m = new ScrollMemory()
  assert.isFalse(m.record("a", view(300)))
  assert.isFalse(m.pinned("a"))
  assert.isNull(m.target("a", false, 1300))
})

test("scrolling back to the bottom pins the chat again and forgets its position", () => {
  const m = new ScrollMemory()
  m.record("a", view(300))
  assert.isTrue(m.record("a", view(bottom - 10)))
  assert.isTrue(m.pinned("a"))
  assert.equal(m.target("a", true, 1000), 1000)
})

test("switching back to a chat restores its own position", () => {
  const m = new ScrollMemory()
  m.record("a", view(300))
  m.record("b", view(120))
  m.record("c", view(bottom))
  assert.equal(m.target("a", true, 1000), 300)
  assert.equal(m.target("b", true, 1000), 120)
  assert.equal(m.target("c", true, 1000), 1000)
})

test("the latest scroll replaces a chat's saved position", () => {
  const m = new ScrollMemory()
  m.record("a", view(300))
  m.record("a", view(50))
  assert.equal(m.target("a", true, 1000), 50)
})

test("sending a message pins the chat", () => {
  const m = new ScrollMemory()
  m.record("a", view(300))
  m.record("b", view(300))
  m.pin("a")
  assert.isTrue(m.pinned("a"))
  assert.equal(m.target("a", false, 1100), 1100)
  assert.isFalse(m.pinned("b"))
})

test("the anchor is the first entry that reaches into the view", () => {
  const rects = [{ bottom: -50 }, { bottom: 0 }, { bottom: 1 }, { bottom: 300 }]
  assert.equal(anchorIndex(rects, 0), 2)
  assert.equal(anchorIndex(rects, -100), 0)
  assert.equal(anchorIndex(rects, 300), -1)
  assert.equal(anchorIndex([], 0), -1)
})
