import { assert, test } from "vitest"
import type { Entry } from "./api"
import { fits, rangeOf, ROW_ESTIMATES, ROW_GAP, ROW_MARGIN, RowHeights, rowOffsets, rowsFor, spanOf } from "./rows"

const e = (seq: number, kind: Entry["kind"] = "user"): Entry =>
  ({ id: `e${seq}`, botId: "b", seq, rev: seq, kind, turnId: "", body: {}, createdAt: 0 }) as Entry

// offsetsOf returns the offsets of rows of the given heights.
const offsetsOf = (heights: number[]) =>
  rowOffsets(
    heights.map((_, i) => e(i + 1)),
    new Map(heights.map((h, i) => [`e${i + 1}`, h])),
  )

test("a row is as high as it was on screen, plus the gap below it", () => {
  assert.deepEqual(offsetsOf([100, 40, 300]), [0, 100 + ROW_GAP, 140 + 2 * ROW_GAP, 440 + 3 * ROW_GAP])
  assert.deepEqual(rowOffsets([], new Map()), [0])
})

test("a row that hasn't been on screen has its kind's estimate", () => {
  const kinds: Entry["kind"][] = ["user", "bot", "agent", "activity", "approval", "event"]
  const offsets = rowOffsets(
    kinds.map((k, i) => e(i + 1, k)),
    new Map([["e2", 7]]),
  )
  const heights = offsets.slice(1).map((y, i) => y - offsets[i] - ROW_GAP)
  assert.deepEqual(heights, [
    ROW_ESTIMATES.user,
    7,
    ROW_ESTIMATES.agent,
    ROW_ESTIMATES.activity,
    ROW_ESTIMATES.approval,
    ROW_ESTIMATES.event,
  ])
  for (const k of kinds) assert.isAbove(ROW_ESTIMATES[k], 0, k)
})

// Rows of 100px each, with the gap: row i starts at 112 * i.
const even = offsetsOf(Array(100).fill(100))

test("the rows rendered reach a margin above and below the view", () => {
  const top = 5000
  const r = rowsFor(even, top, top + 800)
  assert.deepEqual(r, {
    start: Math.floor((top - ROW_MARGIN) / 112),
    end: Math.floor((top + 800 + ROW_MARGIN) / 112) + 1,
  })
  assert.isAtMost(even[r.start], top - ROW_MARGIN)
  assert.isAbove(even[r.start + 1], top - ROW_MARGIN)
  assert.isAtMost(even[r.end - 1], top + 800 + ROW_MARGIN)
  assert.isAbove(even[r.end], top + 800 + ROW_MARGIN)
})

test("the rows rendered stop at the chat's first and last rows", () => {
  assert.deepEqual(rowsFor(even, 0, 800), { start: 0, end: Math.floor((800 + ROW_MARGIN) / 112) + 1 })
  assert.deepEqual(rowsFor(even, even[100] - 800, even[100]).end, 100)
  assert.deepEqual(rowsFor(even, -5000, 99999), { start: 0, end: 100 })
  assert.deepEqual(rowsFor(offsetsOf([100]), 0, 800), { start: 0, end: 1 })
  assert.deepEqual(rowsFor([0], 0, 800), { start: 0, end: 0 }, "a chat with no rows renders none")
})

test("a view on a row's first pixel renders it, and one past its gap doesn't", () => {
  const at = (y: number) => rowsFor(even, y + ROW_MARGIN, y + ROW_MARGIN).start
  assert.equal(at(112 * 40), 40)
  assert.equal(at(112 * 40 - 1), 39)
})

test("rendered rows fit the view while half a margin is left above and below", () => {
  const range = { start: 10, end: 60 }
  const view = { top: 3000, bottom: 3800 }
  const rendered = { top: view.top - ROW_MARGIN / 2, bottom: view.bottom + ROW_MARGIN / 2 }
  assert.isTrue(fits(range, 100, rendered, view))
  assert.isFalse(fits(range, 100, { ...rendered, top: rendered.top + 1 }, view))
  assert.isFalse(fits(range, 100, { ...rendered, bottom: rendered.bottom - 1 }, view))
})

test("rendered rows at the chat's edges fit the view however close it is", () => {
  const view = { top: 0, bottom: 800 }
  const rendered = { top: 0, bottom: 800 }
  assert.isTrue(fits({ start: 0, end: 100 }, 100, rendered, view))
  assert.isFalse(fits({ start: 1, end: 100 }, 100, rendered, view))
  assert.isFalse(fits({ start: 0, end: 99 }, 100, rendered, view))
})

// A chat that follows the bottom keeps the newest entry in its rows, so each new entry adds a
// row: once they reach too far above the view, they are trimmed.
test("rendered rows that reach more than twice the margin past the view no longer fit", () => {
  const range = { start: 10, end: 60 }
  const view = { top: 6000, bottom: 6800 }
  const rendered = { top: view.top - 2 * ROW_MARGIN, bottom: view.bottom + 2 * ROW_MARGIN }
  assert.isTrue(fits(range, 100, rendered, view))
  assert.isFalse(fits(range, 100, { ...rendered, top: rendered.top - 1 }, view), "too far above")
  assert.isFalse(fits(range, 100, { ...rendered, bottom: rendered.bottom + 1 }, view), "too far below")
  assert.isFalse(fits({ start: 0, end: 100 }, 100, { top: 0, bottom: 9000 }, view), "also at the chat's edges")
})

test("a chat that follows the bottom keeps a bounded number of rows however many entries arrive", () => {
  const offsets = (n: number) => offsetsOf(Array(n).fill(100))
  const view = (o: number[]) => ({ top: o.at(-1)! - 800, bottom: o.at(-1)! })
  let o = offsets(1000)
  let range = rowsFor(o, view(o).top, view(o).bottom)
  const opened = range.end - range.start
  for (let n = 1001; n <= 1600; n++) {
    o = offsets(n)
    range = { start: range.start, end: n } // the newest entry joins the rows
    const rendered = { top: o[range.start], bottom: o[range.end] }
    if (!fits(range, n, rendered, view(o))) range = rowsFor(o, view(o).top, view(o).bottom)
  }
  assert.isAtMost(
    range.end - range.start,
    opened + Math.ceil((2 * ROW_MARGIN) / 112),
    `${range.end - range.start} rows`,
  )
})

test("each chat has its own heights, kept for the chats opened most recently", () => {
  const h = new RowHeights(2)
  h.of("a").set("e1", 50)
  h.of("b").set("e1", 70)
  assert.equal(h.of("a").get("e1"), 50)
  assert.equal(h.of("b").get("e1"), 70)
  h.of("c") // a was opened before b and c
  assert.isUndefined(h.of("a").get("e1"), "a's heights were dropped")
  assert.isUndefined(h.of("c").get("e1"))
  const c = h.of("c")
  assert.strictEqual(h.of("c"), c, "the open chat keeps its map")
})

test("a span keeps its entries when an older page arrives above them", () => {
  const entries = [5, 6, 7, 8, 9].map((s) => e(s))
  const span = spanOf(entries, { start: 1, end: 3 })
  assert.deepEqual(span, { first: 6, last: 7 })
  const more = [1, 2, 3, 4, ...entries.map((x) => x.seq)].map((s) => e(s))
  assert.deepEqual(rangeOf(more, span), { start: 5, end: 7 })
})

test("a span that reaches the newest entry grows with new ones", () => {
  const entries = [5, 6, 7].map((s) => e(s))
  const span = spanOf(entries, { start: 0, end: 3 })
  assert.deepEqual(span, { first: 5, last: null })
  assert.deepEqual(rangeOf([...entries, e(8)], span), { start: 0, end: 4 })
})
