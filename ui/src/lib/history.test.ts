import { assert, test, vi } from "vitest"
import {
  endBrowse,
  forgetHistory,
  historyCap,
  historyKey,
  historyMove,
  loadHistory,
  recordSent,
  stepBrowse,
  type HistoryEntry,
  type HistoryKeyPress,
  withStorage,
} from "./history"

// memory is a Storage stand-in. full makes setItem throw, as a full or private-window storage does.
function memory(seed: Record<string, string> = {}, full = false) {
  const map = new Map(Object.entries(seed))
  return {
    map,
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => {
      if (full) throw new Error("QuotaExceededError")
      map.set(k, v)
    },
    removeItem: (k: string) => void map.delete(k),
  }
}
const texts = (entries: HistoryEntry[]) => entries.map((e) => e.text)

test("each chat has its own key", () => {
  assert.equal(historyKey("bot-1"), "handful.history.bot-1")
})

test("a send is recorded trimmed, newest last, with its time", () => {
  const s = memory()
  recordSent(s, "a", "  one \n", 10)
  recordSent(s, "a", "two", 20)
  assert.deepEqual(loadHistory(s, "a"), [
    { text: "one", at: 10 },
    { text: "two", at: 20 },
  ])
  assert.equal(s.map.get("handful.history.a"), '[{"text":"one","at":10},{"text":"two","at":20}]')
})

test("an empty send and a repeat of the newest entry add nothing; an older repeat does", () => {
  const s = memory()
  recordSent(s, "a", "one", 1)
  recordSent(s, "a", "   ", 2)
  recordSent(s, "a", "", 3)
  recordSent(s, "a", " one ", 4)
  assert.deepEqual(loadHistory(s, "a"), [{ text: "one", at: 1 }])
  recordSent(s, "a", "two", 5)
  recordSent(s, "a", "one", 6)
  assert.deepEqual(texts(loadHistory(s, "a")), ["one", "two", "one"])
})

test("a chat keeps at most 1,000 entries, and the oldest goes first", () => {
  assert.equal(historyCap, 1000)
  const seeded = Array.from({ length: 1000 }, (_, i) => ({ text: `m${i}`, at: i }))
  const s = memory({ "handful.history.a": JSON.stringify(seeded) })
  recordSent(s, "a", "m1000", 1000)
  const got = loadHistory(s, "a")
  assert.equal(got.length, 1000)
  assert.equal(got[0].text, "m1")
  assert.equal(got.at(-1)?.text, "m1000")
  assert.equal(JSON.parse(s.map.get("handful.history.a")!).length, 1000, "the stored list itself holds 1,000")
  // A store that already holds more (written by hand, or by an older version) reads as the newest 1,000.
  const over = memory({ "handful.history.b": JSON.stringify([...seeded, { text: "m1000", at: 1000 }]) })
  assert.deepEqual([loadHistory(over, "b").length, loadHistory(over, "b")[0].text], [1000, "m1"])
})

test("chats don't share history, and a deleted chat's history is gone", () => {
  const s = memory()
  recordSent(s, "a", "for a", 1)
  recordSent(s, "b", "for b", 2)
  assert.deepEqual(texts(loadHistory(s, "a")), ["for a"])
  forgetHistory(s, "a")
  assert.deepEqual(loadHistory(s, "a"), [])
  assert.deepEqual(texts(loadHistory(s, "b")), ["for b"])
})

test("storage that holds something else reads as no history", () => {
  for (const raw of ["not json", '{"text":"x"}', "null", "7"]) {
    assert.deepEqual(loadHistory(memory({ "handful.history.a": raw }), "a"), [], raw)
  }
  const mixed = '[{"text":"ok","at":1},{"text":2,"at":2},{"at":3},null,"x",{"text":"no time"},{"text":"fine","at":4}]'
  assert.deepEqual(texts(loadHistory(memory({ "handful.history.a": mixed }), "a")), ["ok", "fine"])
  const corrupt = memory({ "handful.history.a": "not json" })
  recordSent(corrupt, "a", "fresh", 9)
  assert.deepEqual(loadHistory(corrupt, "a"), [{ text: "fresh", at: 9 }], "a send writes a valid history over it")
})

test("withStorage gives the browser's storage, or the fallback when it fails", () => {
  const s = memory()
  vi.stubGlobal("localStorage", s)
  try {
    assert.equal(
      withStorage((store) => store, null),
      s,
    )
    withStorage((store) => recordSent(store, "a", "one", 1), undefined)
    assert.deepEqual(texts(loadHistory(s, "a")), ["one"])
    const full = memory({}, true)
    vi.stubGlobal("localStorage", full)
    assert.equal(
      withStorage((store): string => (recordSent(store, "a", "two", 2), "saved"), "lost"),
      "lost",
    )
    assert.deepEqual(loadHistory(full, "a"), [])
    vi.stubGlobal("localStorage", undefined)
    assert.deepEqual(
      withStorage((store) => loadHistory(store, "a"), []),
      [],
      "no storage at all",
    )
  } finally {
    vi.unstubAllGlobals()
  }
})

const press = (p: Partial<HistoryKeyPress>): HistoryKeyPress => ({
  key: "ArrowUp",
  modified: false,
  composing: false,
  menuOpen: false,
  browsing: false,
  value: "",
  start: 0,
  end: 0,
  ...p,
})

test("↑ goes back from an empty box, or from position 0", () => {
  assert.equal(historyMove(press({})), "older")
  assert.equal(historyMove(press({ value: "half done", start: 0, end: 0 })), "older")
  assert.equal(
    historyMove(press({ value: "half done", start: 9, end: 9 })),
    null,
    "the caret at the end moves natively first",
  )
  assert.equal(historyMove(press({ value: "a\nb\nc", start: 0, end: 0 })), "older")
  assert.equal(historyMove(press({ value: "a\nb\nc", start: 1, end: 1 })), null, "first line, not at 0")
  assert.equal(historyMove(press({ value: "a\nb\nc", start: 2, end: 2 })), null, "second line, at its start")
  assert.equal(historyMove(press({ value: "a\nb\nc", start: 5, end: 5 })), null, "last line")
})

test("while browsing, ↑ goes back from anywhere on the first line", () => {
  assert.equal(historyMove(press({ browsing: true, value: "three", start: 5, end: 5 })), "older")
  assert.equal(historyMove(press({ browsing: true, value: "a\nbc", start: 1, end: 1 })), "older")
  assert.equal(historyMove(press({ browsing: true, value: "a\nbc", start: 2, end: 2 })), null, "second line")
  assert.equal(historyMove(press({ browsing: true, value: "a\nbc", start: 4, end: 4 })), null, "end of the last line")
})

test("↓ goes forward only while browsing, from the end of the text", () => {
  const down = (p: Partial<HistoryKeyPress>) => historyMove(press({ key: "ArrowDown", ...p }))
  assert.equal(down({ browsing: true, value: "two", start: 3, end: 3 }), "newer")
  assert.equal(down({ browsing: true, value: "", start: 0, end: 0 }), "newer")
  assert.equal(down({ browsing: true, value: "two", start: 2, end: 2 }), null)
  assert.equal(down({ browsing: true, value: "a\nb", start: 1, end: 1 }), null, "first line")
  assert.equal(down({ browsing: false, value: "two", start: 3, end: 3 }), null)
  assert.equal(down({ browsing: false }), null)
})

test("an IME, the open menu, a modifier and a selection keep the arrows native, in that order", () => {
  const recalls = press({ browsing: true, value: "three", start: 5, end: 5 })
  assert.equal(historyMove(recalls), "older")
  assert.equal(historyMove({ ...recalls, composing: true }), null)
  assert.equal(historyMove({ ...recalls, menuOpen: true }), null)
  assert.equal(historyMove({ ...recalls, modified: true }), null)
  assert.equal(historyMove({ ...recalls, start: 0 }), null, "a selection")
  assert.equal(historyMove({ ...recalls, key: "ArrowDown", modified: true }), null)
  assert.equal(historyMove({ ...recalls, key: "ArrowDown", start: 4 }), null, "a selection to the end")
  assert.equal(historyMove({ ...recalls, key: "ArrowLeft" }), null)
  assert.equal(historyMove(press({ key: "Enter" })), null)
})

const sent = (...t: string[]) => t.map((text, i) => ({ text, at: i }))

test("↑ walks back to the oldest and stays; ↓ walks forward and back to the draft", () => {
  const entries = sent("one", "two", "three")
  const seen: string[] = []
  let step = stepBrowse(null, entries, "", "older")!
  seen.push(`${step.text}|${step.status}`)
  for (const move of ["older", "older"] as const) {
    step = stepBrowse(step.browse, entries, "", move)!
    seen.push(`${step.text}|${step.status}`)
  }
  assert.isNull(stepBrowse(step.browse, entries, "", "older"), "at the oldest, ↑ does nothing")
  for (let i = 0; i < 3; i++) {
    step = stepBrowse(step.browse, entries, "", "newer")!
    seen.push(`${step.text}|${step.status}`)
  }
  assert.deepEqual(seen, [
    "three|Sent message 1 of 3",
    "two|Sent message 2 of 3",
    "one|Sent message 3 of 3",
    "two|Sent message 2 of 3",
    "three|Sent message 1 of 3",
    "|Your draft",
  ])
  assert.isNull(step.browse)
})

test("the draft is saved when the walk begins and comes back at its end", () => {
  const entries = sent("one")
  const first = stepBrowse(null, entries, "half done", "older")!
  assert.deepEqual(first.browse, { entries, index: 0, draft: "half done" })
  assert.deepEqual(stepBrowse(first.browse, entries, "ignored", "newer"), {
    browse: null,
    text: "half done",
    status: "Your draft",
  })
  assert.deepEqual(endBrowse(first.browse!), { browse: null, text: "half done", status: "Your draft" })
})

test("with no history, or ↓ outside a walk, nothing moves", () => {
  assert.isNull(stepBrowse(null, [], "draft", "older"))
  assert.isNull(stepBrowse(null, sent("one"), "draft", "newer"))
})

test("a walk keeps the entries it began with", () => {
  const first = stepBrowse(null, sent("one", "two"), "", "older")!
  const next = stepBrowse(first.browse, sent("one", "two", "new elsewhere"), "", "older")!
  assert.deepEqual([next.text, next.status], ["one", "Sent message 2 of 2"])
})
