import { assert, test } from "vitest"
import type { Entry } from "./api"
import { ENTRY_PAGE, loadEntriesAfter } from "./entries.ts"

function entries(from: number, n: number): Entry[] {
  return Array.from({ length: n }, (_, i) => ({ id: `e${from + i}`, rev: from + i }) as Entry)
}

// fakeServer answers like GET /api/bots/{id}/entries: at most ENTRY_PAGE
// entries with a rev above after, oldest first.
function fakeServer(all: Entry[]) {
  const asked: number[] = []
  const page = (after: number) => {
    asked.push(after)
    return Promise.resolve(all.filter((e) => e.rev > after).slice(0, ENTRY_PAGE))
  }
  return { asked, page }
}

test("fetches every page of a long chat, not only the first", async () => {
  const server = fakeServer(entries(1, ENTRY_PAGE + 5))
  const got: Entry[][] = []
  await loadEntriesAfter(server.page, 0, (page) => got.push(page))
  assert.deepEqual(server.asked, [0, ENTRY_PAGE])
  assert.deepEqual(
    got.map((p) => p.length),
    [ENTRY_PAGE, 5],
  )
  assert.equal(got.flat().length, ENTRY_PAGE + 5)
})

test("stops after a short page and starts after the given rev", async () => {
  const server = fakeServer(entries(1, 30))
  const got: Entry[][] = []
  await loadEntriesAfter(server.page, 20, (page) => got.push(page))
  assert.deepEqual(server.asked, [20])
  assert.deepEqual(
    got.flat().map((e) => e.rev),
    entries(21, 10).map((e) => e.rev),
  )
})

test("asks once more after an exactly full page and passes on no empty page", async () => {
  const server = fakeServer(entries(1, ENTRY_PAGE))
  const got: Entry[][] = []
  await loadEntriesAfter(server.page, 0, (page) => got.push(page))
  assert.deepEqual(server.asked, [0, ENTRY_PAGE])
  assert.equal(got.length, 1)
})

test("stops when a full page does not move past the rev it asked after", async () => {
  const asked: number[] = []
  const stuck = (after: number) => {
    asked.push(after)
    return Promise.resolve(entries(1, ENTRY_PAGE))
  }
  await loadEntriesAfter(stuck, ENTRY_PAGE, () => {})
  assert.deepEqual(asked, [ENTRY_PAGE])
})
