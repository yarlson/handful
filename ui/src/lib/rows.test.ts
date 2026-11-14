import { assert, test } from "vitest"
import type { ActivityBody, Entry } from "./api"
import {
  ChatRows,
  fits,
  GROUP_ESTIMATE,
  rangeOf,
  ROW_ESTIMATES,
  ROW_GAP,
  ROW_MARGIN,
  rowOffsets,
  rowsFor,
  rowsOf,
  rowWith,
  spanOf,
} from "./rows"

const e = (seq: number, kind: Entry["kind"] = "user"): Entry =>
  ({ id: `e${seq}`, botId: "b", seq, rev: seq, kind, turnId: "", body: {}, createdAt: 0 }) as Entry

// call is tool call seq of turn t, by bot (none for the chat's own bot).
const call = (seq: number, turnId = "t", bot?: string): Entry => ({
  id: `e${seq}`,
  botId: "b",
  seq,
  rev: seq,
  kind: "activity",
  turnId,
  createdAt: 0,
  body: { tool: "Read", summary: "", status: "done", subagent: false, bot } satisfies ActivityBody,
})

// rowsOfEntries returns the rows of entries that all fall on one day.
const rowsOfEntries = (entries: Entry[]) => rowsOf(entries, [])
// shape returns each row's entry seqs.
const shape = (entries: Entry[], days: (string | undefined)[] = []) =>
  rowsOf(entries, days).map((r) => r.entries.map((x) => x.seq))

// offsetsOf returns the offsets of rows of the given heights.
const offsetsOf = (heights: number[]) =>
  rowOffsets(rowsOfEntries(heights.map((_, i) => e(i + 1))), new Map(heights.map((h, i) => [`e${i + 1}`, h])))

test("tool calls in a row of one turn and one bot form one row", () => {
  assert.deepEqual(shape([e(1), call(2), call(3), call(4), e(5, "bot")]), [[1], [2, 3, 4], [5]])
  const rows = rowsOfEntries([e(1), call(2), call(3), e(4, "bot")])
  assert.deepEqual(
    rows.map((r) => r.at),
    [0, 1, 3],
    "each row knows where its first entry is",
  )
  assert.deepEqual(
    rows.map((r) => r.key),
    ["e1", "e2", "e4"],
    "a row's key is its first entry's id",
  )
})

test("a single tool call stays its own row", () => {
  assert.deepEqual(shape([e(1), call(2), e(3, "bot")]), [[1], [2], [3]])
  assert.deepEqual(shape([call(1)]), [[1]])
  assert.deepEqual(shape([]), [])
})

test("any other entry between tool calls ends their group", () => {
  for (const kind of ["user", "bot", "agent", "approval", "event"] as const)
    assert.deepEqual(shape([call(1), call(2), e(3, kind), call(4), call(5)]), [[1, 2], [3], [4, 5]], kind)
})

test("tool calls of another turn, of another bot or on a new day start a new group", () => {
  assert.deepEqual(shape([call(1, "t1"), call(2, "t1"), call(3, "t2"), call(4, "t2")]), [
    [1, 2],
    [3, 4],
  ])
  assert.deepEqual(shape([call(1, "t", "Ann"), call(2, "t", "Ann"), call(3, "t", "Bob")]), [[1, 2], [3]])
  assert.deepEqual(shape([call(1, "t"), call(2, "t", "Ann")]), [[1], [2]], "the chat's own bot and a member")
  assert.deepEqual(shape([call(1), call(2), call(3)], [undefined, undefined, "Today"]), [[1, 2], [3]])
  assert.deepEqual(shape([call(1), call(2)], ["Yesterday", undefined]), [[1, 2]], "a day above the first call")
})

// keys returns the row keys of entries, after the rows of shown.
const keys = (entries: Entry[], shown: Entry[], days: (string | undefined)[] = []) =>
  rowsOf(entries, days, rowsOfEntries(shown)).map((r) => r.key)

test("a group keeps its key when an older page adds calls above it", () => {
  assert.deepEqual(keys([call(1), call(2), call(3), e(4, "bot")], [call(3), e(4, "bot")]), ["e3", "e4"])
  assert.deepEqual(keys([call(1), call(2), call(3)], [call(2), call(3)]), ["e2"])
  assert.deepEqual(
    rowsOf([e(0), call(1), call(2), call(3)], [], rowsOf([call(2), call(3)], [], rowsOfEntries([call(3)]))).map(
      (r) => r.key,
    ),
    ["e0", "e3"],
    "over two older pages",
  )
})

test("a group keeps its key when new calls join it", () => {
  assert.deepEqual(keys([e(1), call(2), call(3), call(4)], [e(1), call(2)]), ["e1", "e2"])
  assert.deepEqual(keys([e(1), call(2), call(3), call(4)], [e(1), call(2), call(3)]), ["e1", "e2"])
})

test("rows new to the entries shown last take their first entry's id", () => {
  assert.deepEqual(keys([call(1), call(2), e(3, "bot"), call(4), call(5)], [call(1), call(2)]), ["e1", "e3", "e4"])
  assert.deepEqual(keys([call(1), call(2)], [e(7), call(8), call(9)]), ["e1"], "another chat's rows")
})

// grown returns the row keys of entries after two older pages: first, then page, then entries.
const grown = (first: Entry[], page: Entry[], entries: Entry[], days: (string | undefined)[]) =>
  rowsOf(entries, days, rowsOf(page, [], rowsOfEntries(first))).map((r) => r.key)

test("the parts of a group that a day separator splits keep keys of their own", () => {
  const calls = [call(1), call(2), call(3), call(4), call(5)]
  const at = (i: number) => calls.map((_, j) => (j === i ? "Today" : undefined))
  assert.deepEqual(keys(calls.slice(0, 4), calls.slice(0, 4), at(2)), ["e1", "e3"])
  assert.deepEqual(grown(calls.slice(2, 4), calls.slice(0, 4), calls.slice(0, 4), at(2)), ["e1", "e3"])
  assert.deepEqual(grown(calls.slice(3), calls, calls, at(2)), ["e4", "e3"])
  assert.deepEqual(
    grown(calls.slice(2, 4), calls.slice(0, 4), calls.slice(0, 4), at(1)),
    ["e1", "e3"],
    "a single call keeps its own id",
  )
})

test("rowWith finds the row that holds an entry, also inside a group", () => {
  const rows = rowsOfEntries([e(1), call(2), call(3), call(4), e(5, "bot")])
  assert.equal(rowWith(rows, "e1"), 0)
  assert.equal(rowWith(rows, "e3"), 1)
  assert.equal(rowWith(rows, "e4"), 1)
  assert.equal(rowWith(rows, "e5"), 2)
  assert.equal(rowWith(rows, "e9"), -1)
})

// A turn of 50 tool calls between a message and the answer: three rows, not 52.
const turn = [e(1), ...Array.from({ length: 50 }, (_, i) => call(i + 2)), e(52, "bot")]

test("a group of tool calls takes one row's height, however many calls it holds", () => {
  const measured = rowOffsets(
    rowsOfEntries(turn),
    new Map([
      ["e1", 60],
      ["e2", 30],
      ["e52", 100],
    ]),
  )
  assert.deepEqual(measured, [0, 60 + ROW_GAP, 90 + 2 * ROW_GAP, 190 + 3 * ROW_GAP])
  const estimated = rowOffsets(rowsOfEntries(turn), new Map())
  assert.equal(estimated[2] - estimated[1], GROUP_ESTIMATE + ROW_GAP, "an unmeasured group has the group estimate")
})

test("the rows rendered at the bottom of a turn of tool calls count the group once", () => {
  const rows = rowsOfEntries(turn)
  const offsets = rowOffsets(rows, new Map())
  const r = rowsFor(offsets, offsets[3] - 800, offsets[3])
  assert.deepEqual(r, { start: 0, end: 3 })
  assert.deepEqual(rows[r.end - 1].entries[0].seq, 52, "the answer is the last row rendered")
})

test("a row is as high as it was on screen, plus the gap below it", () => {
  assert.deepEqual(offsetsOf([100, 40, 300]), [0, 100 + ROW_GAP, 140 + 2 * ROW_GAP, 440 + 3 * ROW_GAP])
  assert.deepEqual(rowOffsets([], new Map()), [0])
  assert.isAbove(GROUP_ESTIMATE, 0)
})

test("a row that hasn't been on screen has its kind's estimate", () => {
  const kinds: Entry["kind"][] = ["user", "bot", "agent", "activity", "approval", "event"]
  const offsets = rowOffsets(rowsOfEntries(kinds.map((k, i) => e(i + 1, k))), new Map([["e2", 7]]))
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
  const h = new ChatRows(2)
  h.heights("a").set("e1", 50)
  h.heights("b").set("e1", 70)
  assert.equal(h.heights("a").get("e1"), 50)
  assert.equal(h.heights("b").get("e1"), 70)
  h.heights("c") // a was opened before b and c
  assert.isUndefined(h.heights("a").get("e1"), "a's heights were dropped")
  assert.isUndefined(h.heights("c").get("e1"))
  const c = h.heights("c")
  assert.strictEqual(h.heights("c"), c, "the open chat keeps its map")
})

test("each chat's rows keep their keys while it is one of the chats opened most recently", () => {
  const h = new ChatRows(2)
  const newest = [call(3), call(4)]
  const page = [call(1), call(2), ...newest]
  assert.deepEqual(
    h.rows("a", newest, []).map((r) => r.key),
    ["e3"],
  )
  h.rows("b", [e(1)], [])
  assert.deepEqual(
    h.rows("a", page, []).map((r) => r.key),
    ["e3"],
    "a's rows after another chat's",
  )
  h.rows("b", [e(1)], [])
  h.rows("c", [e(1)], [])
  assert.deepEqual(
    h.rows("a", page, []).map((r) => r.key),
    ["e1"],
    "a's rows were dropped",
  )
})

test("a span keeps its entries when an older page arrives above them", () => {
  const entries = [5, 6, 7, 8, 9].map((s) => e(s))
  const span = spanOf(rowsOfEntries(entries), { start: 1, end: 3 })
  assert.deepEqual(span, { first: 6, last: 7 })
  const more = [1, 2, 3, 4, ...entries.map((x) => x.seq)].map((s) => e(s))
  assert.deepEqual(rangeOf(rowsOfEntries(more), span), { start: 5, end: 7 })
})

test("a span that reaches the newest entry grows with new ones", () => {
  const entries = [5, 6, 7].map((s) => e(s))
  const span = spanOf(rowsOfEntries(entries), { start: 0, end: 3 })
  assert.deepEqual(span, { first: 5, last: null })
  assert.deepEqual(rangeOf(rowsOfEntries([...entries, e(8)]), span), { start: 0, end: 4 })
})

test("a span of rows holds its groups whole, from their first call to their last", () => {
  const rows = rowsOfEntries([e(1), call(2), call(3), call(4), e(5, "bot"), call(6), call(7), e(8, "bot")])
  assert.deepEqual(spanOf(rows, { start: 1, end: 4 }), { first: 2, last: 7 })
  assert.deepEqual(rangeOf(rows, { first: 2, last: 7 }), { start: 1, end: 4 })
  assert.deepEqual(rangeOf(rows, { first: 3, last: 6 }), { start: 1, end: 4 }, "a seq inside a group takes it whole")
})

test("a group that gains calls at either edge of a span stays in it whole", () => {
  const span = spanOf(rowsOfEntries([e(1), call(3), call(4), e(9, "bot")]), { start: 1, end: 2 })
  assert.deepEqual(span, { first: 3, last: 4 })
  // An older page brings the group's first call; a late update brings one between 4 and 9.
  const grown = rowsOfEntries([e(1), call(2), call(3), call(4), call(5), e(9, "bot")])
  assert.deepEqual(rangeOf(grown, span), { start: 1, end: 2 })
})
