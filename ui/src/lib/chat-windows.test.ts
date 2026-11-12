import { assert, test } from "vitest"
import type { Entry } from "./api"
import { ChatWindows, KEPT_CHATS } from "./chat-windows"
import { WINDOW_PAGE } from "./window"

const e = (seq: number, rev = seq, id = "c"): Entry =>
  ({ id: `${id}${seq}`, botId: id, seq, rev, kind: "user", turnId: "", body: {}, createdAt: 0 }) as Entry
const range = (from: number, to: number) => Array.from({ length: to - from + 1 }, (_, i) => e(from + i))

// fakeServer answers ChatWindows' requests from chat c's entries; each request waits until
// the test lets it answer with release.
function fakeServer(entries: Entry[]) {
  const asked: string[] = []
  const waiting: ((failure?: Error) => void)[] = []
  const calls = {
    status: [] as string[],
    unloaded: [] as string[],
    errors: [] as string[],
    windows: new Map<string, unknown>(),
  }
  const reply = <T>(url: string, value: () => T) => {
    asked.push(url)
    return new Promise<T>((resolve, reject) =>
      waiting.push((failure) => (failure ? reject(failure) : resolve(value()))),
    )
  }
  const chats = new ChatWindows({
    page: (id, limit, before) =>
      reply(`${id} limit=${limit}${before === undefined ? "" : ` before=${before}`}`, () =>
        entries.filter((x) => x.seq < (before ?? Infinity)).slice(-limit),
      ),
    changes: (id, after) => reply(`${id} after=${after}`, () => entries.filter((x) => x.rev > after)),
    onWindows: (w) => (calls.windows = w),
    onStatus: (id, event) => calls.status.push(`${id} ${event}`),
    onUnload: (id) => calls.unloaded.push(id),
    onError: (m) => calls.errors.push(m),
  })
  // release answers the oldest waiting request, or fails it, and lets the promises after it run.
  const release = async (failure?: Error) => {
    waiting.shift()?.(failure)
    await new Promise((r) => setTimeout(r, 0))
  }
  return { chats, asked, release, calls, entries }
}

const seqs = (chats: ChatWindows, id = "c") => chats.get(id)?.entries.map((x) => x.seq) ?? []

test("a chat loads its newest page once, and updates that arrive first wait for it", async () => {
  const s = fakeServer(range(1, WINDOW_PAGE + 5))
  void s.chats.select("c")
  void s.chats.select("c")
  s.chats.update("c", [e(WINDOW_PAGE + 6)])
  assert.deepEqual(s.asked, [`c limit=${WINDOW_PAGE}`])
  await s.release()
  assert.equal(seqs(s.chats).length, WINDOW_PAGE + 1)
  assert.equal(seqs(s.chats)[0], 6)
  assert.equal(seqs(s.chats).at(-1), WINDOW_PAGE + 6)
  assert.deepEqual(s.calls.status, ["c fetch", "c loaded"])
  s.chats.update("other", [e(1, 1, "other")])
  assert.isUndefined(s.chats.get("other"), "an update to a chat not loaded is dropped")
})

test("a reconnect fetches only the changes after each chat's rev, and waits for a page on its way", async () => {
  const s = fakeServer(range(1, 3))
  void s.chats.select("c")
  s.chats.reconnected()
  assert.deepEqual(s.asked, [`c limit=${WINDOW_PAGE}`], "no fetch before the newest page")
  await s.release()
  assert.deepEqual(s.asked.at(-1), "c after=3", "the page then asks for what it missed")
  s.entries.push(e(4))
  await s.release()
  assert.deepEqual(seqs(s.chats), [1, 2, 3, 4])
  s.chats.reconnected()
  assert.equal(s.asked.at(-1), "c after=4")
})

test("older pages load up to the first entry, and loading stops there", async () => {
  const s = fakeServer(range(1, WINDOW_PAGE + 3))
  void s.chats.select("c")
  await s.release()
  const older = s.chats.loadOlder("c")
  assert.strictEqual(s.chats.loadOlder("c"), older, "a second call returns the page on its way")
  assert.equal(s.asked.at(-1), `c limit=${WINDOW_PAGE} before=4`)
  await s.release()
  await older
  assert.equal(seqs(s.chats).length, WINDOW_PAGE + 3)
  assert.isTrue(s.chats.get("c")?.complete)
  const asked = s.asked.length
  await s.chats.loadOlder("c")
  assert.equal(s.asked.length, asked, "a complete window asks for nothing")
})

// An update older than the window can arrive while the page that holds the entry is on its
// way, and that page may hold the entry's previous version.
test("an update below the window that arrives while its page loads is applied after the page", async () => {
  const entries = range(1, WINDOW_PAGE + 3)
  const s = fakeServer(entries)
  void s.chats.select("c")
  await s.release()
  const older = s.chats.loadOlder("c")
  s.chats.update("c", [e(2, 900)]) // the page was read before this change
  await s.release()
  await older
  const two = s.chats.get("c")?.entries.find((x) => x.seq === 2)
  assert.equal(two?.rev, 900)
  assert.equal(s.chats.get("c")?.rev, 900)
})

// Go to can start while a page that a scroll asked for is still on its way.
test("reveal waits for the page on its way and goes on until the entry is loaded", async () => {
  const s = fakeServer(range(1, WINDOW_PAGE * 2 + 3))
  void s.chats.select("c")
  await s.release()
  void s.chats.loadOlder("c")
  const found = s.chats.reveal("c", "c2")
  await s.release()
  await s.release()
  assert.isTrue(await found)
  assert.deepEqual(s.asked.slice(1), [`c limit=${WINDOW_PAGE} before=204`, `c limit=${WINDOW_PAGE} before=4`])
  assert.isFalse(await s.chats.reveal("c", "missing"), "an entry the chat doesn't have isn't found")
})

test("reveal waits for the newest page of a chat that is opening", async () => {
  const s = fakeServer(range(1, 3))
  void s.chats.select("c")
  const found = s.chats.reveal("c", "c1")
  await s.release()
  assert.isTrue(await found)
})

test("a chat beyond the ones kept unloads, and loads its newest page again when reopened", async () => {
  const s = fakeServer(range(1, 2))
  for (let i = 0; i <= KEPT_CHATS; i++) {
    void s.chats.select(`k${i}`)
    await s.release()
  }
  assert.deepEqual(s.calls.unloaded, ["k0"])
  assert.isUndefined(s.chats.get("k0"))
  assert.isDefined(s.chats.get("k1"))
  void s.chats.select("k0")
  assert.equal(s.asked.at(-1), `k0 limit=${WINDOW_PAGE}`)
})

test("a page that arrives after its chat unloaded changes nothing", async () => {
  const s = fakeServer(range(1, 2))
  void s.chats.select("c")
  for (let i = 1; i <= KEPT_CHATS; i++) void s.chats.select(`k${i}`)
  await s.release()
  assert.isUndefined(s.chats.get("c"))
  assert.deepEqual(s.calls.status, ["c fetch", "k1 fetch", "k2 fetch", "k3 fetch", "k4 fetch"])
})

test("a failed newest page says so, and the next open asks again", async () => {
  let fail = true
  const status: string[] = []
  const errors: string[] = []
  const chats = new ChatWindows({
    page: async () => {
      if (fail) throw new Error("offline")
      return [e(1)]
    },
    changes: async () => [],
    onWindows: () => {},
    onStatus: (id, event) => status.push(`${id} ${event}`),
    onUnload: () => {},
    onError: (m) => errors.push(m),
  })
  await chats.select("c")
  assert.deepEqual(status, ["c fetch", "c failed"])
  assert.deepEqual(errors, ["offline"])
  fail = false
  await chats.select("c")
  assert.deepEqual(seqs(chats), [1])
})

test("the page gets every loaded window, and a chat that unloads leaves it", async () => {
  const s = fakeServer(range(1, 2))
  void s.chats.select("c")
  await s.release()
  assert.deepEqual([...s.calls.windows.keys()], ["c"])
  assert.strictEqual(s.calls.windows.get("c"), s.chats.get("c"))
  void s.chats.select("d")
  s.chats.update("c", [e(3)])
  assert.isFalse(s.calls.windows.has("d"), "a chat whose newest page is on its way has no window yet")
  for (let i = 1; i < KEPT_CHATS; i++) void s.chats.select(`k${i}`)
  assert.isFalse(s.calls.windows.has("c"), "an unloaded chat leaves at once")
})

test("an update below the window counts its rev at once when no older page is on its way", async () => {
  const s = fakeServer(range(1, WINDOW_PAGE + 3))
  void s.chats.select("c")
  await s.release()
  const before = s.chats.get("c")
  s.chats.update("c", [e(2, 900)])
  assert.equal(s.chats.get("c")?.rev, 900)
  assert.strictEqual(s.chats.get("c")?.entries, before?.entries)
})

test("while an older page loads, updates inside the window apply at once", async () => {
  const s = fakeServer(range(1, WINDOW_PAGE + 3))
  void s.chats.select("c")
  await s.release()
  void s.chats.loadOlder("c")
  s.chats.update("c", [e(WINDOW_PAGE + 4)])
  assert.equal(seqs(s.chats).at(-1), WINDOW_PAGE + 4)
})

test("the updates held for one older page aren't applied again with the next", async () => {
  const s = fakeServer(range(1, WINDOW_PAGE * 2 + 3))
  void s.chats.select("c")
  await s.release()
  const first = s.chats.loadOlder("c")
  s.chats.update("c", [e(150, 900)])
  await s.release()
  await first
  const second = s.chats.loadOlder("c")
  await s.release()
  await second
  assert.equal(s.chats.get("c")?.rev, 900)
  assert.equal(s.chats.get("c")?.entries.find((x) => x.seq === 150)?.rev, 900)
  assert.equal(seqs(s.chats).length, WINDOW_PAGE * 2 + 3)
})

test("a failed reconnect or older page says why, and the window stays", async () => {
  const s = fakeServer(range(1, WINDOW_PAGE + 3))
  void s.chats.select("c")
  await s.release()
  const w = s.chats.get("c")
  s.chats.reconnected()
  await s.release(new Error("offline"))
  const older = s.chats.loadOlder("c")
  await s.release(new Error("gone"))
  await older
  assert.deepEqual(s.calls.errors, ["offline", "gone"])
  assert.strictEqual(s.chats.get("c"), w)
})

test("a page that arrives or fails after its chat unloaded changes nothing", async () => {
  const s = fakeServer(range(1, WINDOW_PAGE + 3))
  void s.chats.select("c")
  await s.release()
  const older = s.chats.loadOlder("c")
  for (let i = 1; i <= KEPT_CHATS; i++) void s.chats.select(`k${i}`)
  void s.chats.select("c") // reopened: a new newest page is on its way
  await s.release() // the old older page
  await older
  assert.isUndefined(s.chats.get("c"))
  await s.release(new Error("late")) // k1's page fails
  assert.notInclude(s.calls.status, "c failed")
})

test("a newest page that fails after its chat was reopened leaves the new load alone", async () => {
  const s = fakeServer(range(1, 2))
  void s.chats.select("c")
  for (let i = 1; i <= KEPT_CHATS; i++) void s.chats.select(`k${i}`)
  void s.chats.select("c")
  await s.release(new Error("old")) // c's first page
  assert.deepEqual(s.calls.errors, [])
  assert.notInclude(s.calls.status, "c failed")
  for (let i = 1; i <= KEPT_CHATS; i++) await s.release()
  await s.release() // c's second page
  assert.deepEqual(seqs(s.chats), [1, 2])
})

test("loadOlder and reveal on a chat that isn't loaded do nothing", async () => {
  const s = fakeServer(range(1, 2))
  await s.chats.loadOlder("nope")
  assert.isFalse(await s.chats.reveal("nope", "c1"))
  assert.deepEqual(s.asked, [])
})

test("reveal on a chat whose newest page failed finds nothing", async () => {
  const s = fakeServer(range(1, 2))
  void s.chats.select("c")
  const found = s.chats.reveal("c", "c1")
  await s.release(new Error("offline"))
  assert.isFalse(await found)
})

// #28: the server can publish rev 5 before rev 4, which committed first. A stream that drops
// between the two loses rev 4; the reconnect must still fetch it.
test("a reconnect fetches from below a rev the stream skipped, so no change is lost", async () => {
  const s = fakeServer(range(1, 3))
  void s.chats.select("c")
  await s.release()
  s.entries.push(e(4), e(5))
  s.chats.update("c", [e(5)])
  assert.deepEqual(seqs(s.chats), [1, 2, 3, 5])
  s.chats.reconnected()
  assert.equal(s.asked.at(-1), "c after=3", "rev 4 never came, so the catch-up starts below it")
  await s.release()
  assert.deepEqual(seqs(s.chats), [1, 2, 3, 4, 5], "rev 4 arrives, and rev 5 isn't doubled")
  s.chats.reconnected()
  assert.equal(s.asked.at(-1), "c after=5", "after the fetch, the catch-up starts at its highest rev")
})

test("revs that arrive out of order move the catch-up mark once the gap closes", async () => {
  const s = fakeServer(range(1, 3))
  void s.chats.select("c")
  await s.release()
  s.chats.update("c", [e(6)])
  s.chats.update("c", [e(5)])
  s.chats.reconnected()
  assert.equal(s.asked.at(-1), "c after=3", "4 is still missing")
  await s.release()
  s.chats.update("c", [e(4)])
  s.chats.reconnected()
  assert.equal(s.asked.at(-1), "c after=6", "4, 5 and 6 are all in")
})

// A change replaces an entry's rev, so a fetch can bring revs with gaps: 4 and 6, where 5 was
// the entry's version that 6 replaced.
test("after a fetch whose revs have gaps, the catch-up starts at its highest rev", async () => {
  const s = fakeServer(range(1, 3))
  void s.chats.select("c")
  await s.release()
  s.entries.push(e(4), e(5, 6))
  s.chats.reconnected()
  await s.release()
  assert.deepEqual(seqs(s.chats), [1, 2, 3, 4, 5])
  s.chats.reconnected()
  assert.equal(s.asked.at(-1), "c after=6")
})

test("changes that arrive after their chat unloaded are dropped quietly", async () => {
  const s = fakeServer(range(1, 3))
  void s.chats.select("c")
  await s.release()
  s.entries.push(e(4))
  s.chats.reconnected()
  for (const other of ["a", "b", "d", "f"]) void s.chats.select(other)
  await s.release()
  assert.isUndefined(s.chats.get("c"))
  assert.deepEqual(s.calls.errors, [])
})
