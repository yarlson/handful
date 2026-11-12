import { assert, test } from "vitest"
import type { Entry } from "./api"
import {
  caughtUpTo,
  EMPTY_WINDOW,
  keptChats,
  MAX_AHEAD,
  openWindow,
  seqIndex,
  withOlderPage,
  withUpdates,
  type EntryWindow,
} from "./window"

const e = (seq: number, rev = seq): Entry =>
  ({
    id: `e${seq}`,
    botId: "b",
    seq,
    rev,
    kind: "user",
    turnId: "",
    body: { text: `${seq}@${rev}` },
    createdAt: 0,
  }) as Entry

const seqs = (w: EntryWindow) => w.entries.map((x) => x.seq)
const page = (from: number, to: number) => Array.from({ length: to - from + 1 }, (_, i) => e(from + i))

test("the newest page opens the window, and a full page leaves older ones to load", () => {
  const w = openWindow([e(11, 4), e(12, 9), e(13, 6)], 3)
  assert.deepEqual(seqs(w), [11, 12, 13])
  assert.equal(w.rev, 9)
  assert.equal(w.caughtUp, 9, "the catch-up mark is the page's highest rev")
  assert.isFalse(w.complete)
  assert.isTrue(openWindow([e(1)], 3).complete, "a short page holds the whole chat")
  assert.deepEqual(openWindow([], 3), { entries: [], rev: 0, caughtUp: 0, ahead: [], complete: true })
})

test("older pages go above the window until a short page reaches the first entry", () => {
  let w = openWindow(page(11, 13), 3)
  w = withOlderPage(w, page(8, 10), 3)
  assert.deepEqual(seqs(w), [8, 9, 10, 11, 12, 13])
  assert.isFalse(w.complete)
  w = withOlderPage(w, page(6, 7), 3)
  assert.deepEqual(seqs(w), [6, 7, 8, 9, 10, 11, 12, 13])
  assert.isTrue(w.complete)
})

// An old entry can carry a recent rev, for example a reaction. If its page raised the
// catch-up mark, a reconnect would skip the newer entries the window hasn't seen.
test("an older page keeps the catch-up mark, so a reconnect still brings what the window missed", () => {
  let w = openWindow([e(3), e(4)], 2)
  w = withOlderPage(w, [e(1, 6), e(2)], 2)
  assert.equal(w.rev, 4, "the page's rev 6 doesn't move the mark")
  assert.equal(w.caughtUp, 4)
  const missed = [e(5, 5), e(1, 6)] // what after=4 returns: the new entry, then the old one's change
  w = withUpdates(w, missed)
  assert.deepEqual(seqs(w), [1, 2, 3, 4, 5])
  assert.equal(w.rev, 6)
})

test("updates add new entries, replace newer versions, and keep the array when nothing changes", () => {
  const w = openWindow(page(11, 13), 3)
  const next = withUpdates(w, [e(14), e(12, 20)])
  assert.deepEqual(seqs(next), [11, 12, 13, 14])
  assert.equal(next.entries[1].rev, 20)
  assert.equal(next.rev, 20)
  assert.notStrictEqual(next.entries, w.entries, "a change makes a new array")
  assert.deepEqual(seqs(w), [11, 12, 13], "the old window stays as it was")
  assert.strictEqual(withUpdates(next, [e(12, 3)]), next, "an older version changes nothing")
  assert.strictEqual(withUpdates(next, [e(12, 20)]), next, "the same version changes nothing")
  assert.strictEqual(withUpdates(next, []), next)
  assert.equal(withUpdates(next, [e(11, 21)]).entries[0].rev, 21, "the window's first entry takes updates too")
  const two = withUpdates(w, [e(14), e(15), e(12, 22)])
  assert.deepEqual(seqs(two), [11, 12, 13, 14, 15])
  assert.equal(two.entries[1].rev, 22)
  assert.deepEqual(seqs(w), [11, 12, 13], "several updates leave the old window as it was")
})

test("an update older than the window is left for its page, but its rev counts", () => {
  const w = openWindow(page(11, 13), 3)
  const next = withUpdates(w, [e(5, 30)])
  assert.deepEqual(seqs(next), [11, 12, 13])
  assert.strictEqual(next.entries, w.entries)
  assert.equal(next.rev, 30)
})

test("a complete window takes updates anywhere, also when it has no entries yet", () => {
  const w = openWindow([e(2), e(4)], 5)
  assert.deepEqual(seqs(withUpdates(w, [e(1), e(3)])), [1, 2, 3, 4])
  assert.deepEqual(seqs(withUpdates(openWindow([], 5), [e(1)])), [1])
  assert.deepEqual(seqs(withUpdates(EMPTY_WINDOW, [e(1)])), [], "a window not loaded yet takes nothing")
  assert.deepEqual(EMPTY_WINDOW.ahead, [], "an empty window waits for no rev")
})

test("seqIndex finds an entry or the place it goes", () => {
  const list = [e(2), e(4), e(6)]
  assert.deepEqual(
    [1, 2, 3, 4, 5, 6, 7].map((s) => seqIndex(list, s)),
    [0, 0, 1, 1, 2, 2, 3],
  )
  assert.equal(seqIndex([], 5), 0)
})

test("the open chat and the most recent ones before it stay loaded", () => {
  assert.deepEqual(keptChats([], "a", 3), ["a"])
  assert.deepEqual(keptChats(["c", "b", "a"], "d", 3), ["d", "c", "b"])
  assert.deepEqual(keptChats(["c", "b", "a"], "b", 3), ["b", "c", "a"])
})

// #28: each change takes the chat's next rev, but the stream can bring them out of order.
test("the catch-up mark moves only over revs with no gap before them", () => {
  let w = openWindow(page(1, 3), 3)
  w = withUpdates(w, [e(5)])
  assert.equal(w.rev, 5)
  assert.equal(w.caughtUp, 3, "rev 4 is missing")
  assert.deepEqual(w.ahead, [5])
  w = withUpdates(w, [e(7), e(5), e(2, 3)])
  assert.equal(w.caughtUp, 3, "a rev seen twice, or one at or below the mark, changes nothing")
  assert.deepEqual(w.ahead, [5, 7])
  w = withUpdates(w, [e(4)])
  assert.equal(w.caughtUp, 5, "4 closes the gap up to 5; 6 is still missing")
  assert.deepEqual(w.ahead, [7])
  w = withUpdates(w, [e(6)])
  assert.equal(w.caughtUp, 7)
  assert.deepEqual(w.ahead, [])
  assert.strictEqual(withUpdates(w, [e(3)]), w, "an old version of an entry changes nothing at all")
})

test("an update below the window still counts toward the catch-up mark", () => {
  let w = openWindow([e(10, 10), e(11, 11)], 2)
  w = withUpdates(w, [e(2, 12)])
  assert.deepEqual(seqs(w), [10, 11], "the old entry is left for its page")
  assert.equal(w.caughtUp, 12)
})

test("a fetch of the changes after the mark moves it to the fetch's highest rev", () => {
  let w = withUpdates(openWindow(page(1, 3), 3), [e(6), e(9)])
  assert.strictEqual(caughtUpTo(w, 3), w, "a mark at or below the current one changes nothing")
  w = caughtUpTo(w, 7)
  assert.equal(w.caughtUp, 7, "the fetch brought every change up to 7")
  assert.deepEqual(w.ahead, [9], "9 still waits for 8")
  w = caughtUpTo(w, 8)
  assert.equal(w.caughtUp, 9, "8 closes the gap to 9")
  assert.deepEqual(w.ahead, [])
  w = caughtUpTo(withUpdates(w, [e(11), e(12)]), 11)
  assert.equal(w.caughtUp, 12, "a fetch up to a rev that waited drops it, and the gap after it closes")
  assert.deepEqual(w.ahead, [])
})

test("the revs that wait for a gap are bounded, and the oldest are dropped first", () => {
  let w = openWindow([e(1)], 1)
  w = withUpdates(
    w,
    Array.from({ length: MAX_AHEAD + 5 }, (_, i) => e(3 + i)),
  )
  assert.equal(w.ahead.length, MAX_AHEAD)
  assert.equal(w.ahead[0], 8, "the lowest five went")
  assert.equal(w.caughtUp, 1)
  w = caughtUpTo(w, 7)
  assert.equal(w.caughtUp, MAX_AHEAD + 7, "a fetch past the dropped ones closes the gap")
})
