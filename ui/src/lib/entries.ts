import type { Entry } from "./api"

// ENTRY_PAGE is the most entries GET /api/bots/{id}/entries returns at once.
export const ENTRY_PAGE = 1000

// loadEntriesAfter fetches every entry with a rev above after, a page at a
// time, oldest first, and passes each page to onPage. A long chat has more
// than one page, so it asks again after the highest rev until a page is short.
export async function loadEntriesAfter(
  page: (after: number) => Promise<Entry[]>,
  after: number,
  onPage: (entries: Entry[]) => void,
): Promise<void> {
  for (;;) {
    const entries = await page(after)
    if (entries.length > 0) onPage(entries)
    const last = Math.max(after, ...entries.map((e) => e.rev))
    if (entries.length < ENTRY_PAGE || last === after) return
    after = last
  }
}
