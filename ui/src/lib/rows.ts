import type { Entry } from "./api"
import { keptChats } from "./window"

// ROW_GAP is the space between two rows of the transcript, in pixels: its column's gap-3.
export const ROW_GAP = 12
// ROW_MARGIN is how far above and below the view the transcript keeps rows rendered, in
// pixels, so that a scroll shows rendered rows before new ones replace the space.
export const ROW_MARGIN = 1200

// ROW_ESTIMATES are the heights, in pixels, that a row of each kind is taken to have until it
// has been on screen. A wrong estimate moves no row the user reads: the view keeps its entry in
// place when the real height replaces it.
export const ROW_ESTIMATES: Record<Entry["kind"], number> = {
  user: 60,
  bot: 120,
  agent: 100,
  activity: 16,
  approval: 160,
  event: 20,
}
// GROUP_ESTIMATE is the height, in pixels, that a group of tool calls is taken to have until it
// has been on screen.
export const GROUP_ESTIMATE = 28

// Row is one row of the transcript: one entry, or a group of two or more tool calls in a row.
// at is the index of its first entry in the transcript's entries. key is the row's id, one of its
// entries' ids: the row's element carries it, and its measured height is kept by it.
export type Row = { key: string; at: number; entries: Entry[] }

// rowsOf returns the rows of the transcript's entries. Tool calls in a row form one group while
// they are of one turn and one bot. Any other entry, another turn, another bot or a new day
// (days[i] is the day separator above entry i) ends the group.
// A row takes its first entry's id as its key. A group that holds entries of before, the rows
// of the entries shown last, keeps the key of the first such row, so its element stays when an
// older page adds calls above it or a new call joins it.
export function rowsOf(entries: Entry[], days: readonly (string | undefined)[], before: readonly Row[] = []): Row[] {
  const rows: Row[] = []
  for (const [i, e] of entries.entries()) {
    const last = rows.at(-1)
    if (last && !days[i] && joins(last.entries.at(-1)!, e)) last.entries.push(e)
    else rows.push({ key: e.id, at: i, entries: [e] })
  }
  const keys = new Map(before.flatMap((r) => r.entries.map((e) => [e.id, r.key] as const)))
  // A day separator can split a group. Its parts take no key that another row has, so no two
  // rows share one.
  const taken = new Set(rows.map((r) => r.key))
  for (const r of rows) {
    if (r.entries.length === 1) continue
    const kept = r.entries.map((e) => keys.get(e.id)).find((k) => k !== undefined)
    if (kept === undefined || taken.has(kept)) continue
    r.key = kept
    taken.add(kept)
  }
  return rows
}

function joins(a: Entry, b: Entry): boolean {
  return a.kind === "activity" && b.kind === "activity" && a.turnId === b.turnId && a.body.bot === b.body.bot
}

// rowWith returns the index of the row that holds entry id, or -1.
export function rowWith(rows: Row[], id: string): number {
  return rows.findIndex((r) => r.entries.some((e) => e.id === id))
}

// rowOffsets returns where each row of the transcript starts, below the top of its first row:
// offsets[i] is the top of row i, and offsets[rows.length] is the height of all the rows and one
// gap. A row has the height it had when last on screen, or else its kind's estimate. A group of
// tool calls is one row, however many entries it holds.
export function rowOffsets(rows: Row[], heights: ReadonlyMap<string, number>): number[] {
  const offsets = [0]
  let y = 0
  for (const r of rows) {
    const estimate = r.entries.length > 1 ? GROUP_ESTIMATE : ROW_ESTIMATES[r.entries[0].kind]
    y += (heights.get(r.key) ?? estimate) + ROW_GAP
    offsets.push(y)
  }
  return offsets
}

// RowRange is the rows the transcript renders, from start up to, not including, end.
export type RowRange = { start: number; end: number }

// rowsFor returns the rows that reach into the view from top to bottom, given in the rows'
// pixels, or into ROW_MARGIN above or below it.
export function rowsFor(offsets: number[], top: number, bottom: number): RowRange {
  if (offsets.length < 2) return { start: 0, end: 0 }
  return { start: rowAt(offsets, top - ROW_MARGIN), end: rowAt(offsets, bottom + ROW_MARGIN) + 1 }
}

// rowAt returns the row at y, in the rows' pixels: the last row that starts at or above it.
function rowAt(offsets: number[], y: number): number {
  let lo = 0
  let hi = offsets.length - 2
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (offsets[mid] <= y) lo = mid
    else hi = mid - 1
  }
  return lo
}

// fits tells whether the rendered rows, which reach from top to bottom in the rows' pixels,
// still suit the view: they cover it with half of ROW_MARGIN to spare, or reach the chat's
// edge, and they reach no more than twice ROW_MARGIN past it. While they fit, a scroll or a new
// entry renders no other rows; rows that reach too far, as when a chat that follows the bottom
// gets new entries, are trimmed.
export function fits(
  range: RowRange,
  count: number,
  rendered: { top: number; bottom: number },
  view: { top: number; bottom: number },
): boolean {
  const above = range.start === 0 || rendered.top <= view.top - ROW_MARGIN / 2
  const below = range.end === count || rendered.bottom >= view.bottom + ROW_MARGIN / 2
  const near = rendered.top >= view.top - 2 * ROW_MARGIN && rendered.bottom <= view.bottom + 2 * ROW_MARGIN
  return above && below && near
}

// ChatRows keeps, for the chats opened most recently, the rows each showed last and their measured
// heights, by row key: a chat's rows and heights last while it is one of the last max chats opened.
export class ChatRows {
  private chats = new Map<string, { rows: Row[]; heights: Map<string, number> }>()
  private recent: string[] = []

  constructor(private max: number) {}

  // heights returns chat id's heights.
  heights(id: string): Map<string, number> {
    return this.of(id).heights
  }

  // rows returns the rows of chat id's entries, which keep the keys of the rows it showed last.
  rows(id: string, entries: Entry[], days: readonly (string | undefined)[]): Row[] {
    const chat = this.of(id)
    chat.rows = rowsOf(entries, days, chat.rows)
    return chat.rows
  }

  // of returns chat id's rows and heights, and drops those of the chats opened before the last
  // max.
  private of(id: string) {
    this.recent = keptChats(this.recent, id, this.max)
    for (const old of this.chats.keys()) if (!this.recent.includes(old)) this.chats.delete(old)
    let chat = this.chats.get(id)
    if (!chat) this.chats.set(id, (chat = { rows: [], heights: new Map() }))
    return chat
  }
}

// RowSpan is a range of rows by their entries' seqs, so that it stays on the same entries when
// an older page arrives above them. last is null when the span reaches the newest entry: it
// then grows with each new one.
export type RowSpan = { first: number; last: number | null }

export function spanOf(rows: Row[], range: RowRange): RowSpan {
  return {
    first: rows[range.start].entries[0].seq,
    last: range.end === rows.length ? null : rows[range.end - 1].entries.at(-1)!.seq,
  }
}

// rangeOf returns the rows of a span. A group that gained entries at either end since the span
// was taken stays in it whole.
export function rangeOf(rows: Row[], { first, last }: RowSpan): RowRange {
  return {
    start: firstRow(rows, (r) => r.entries.at(-1)!.seq >= first),
    end: last === null ? rows.length : firstRow(rows, (r) => r.entries[0].seq > last),
  }
}

// firstRow returns the first of the rows, which are in seq order, that past holds for, or
// rows.length; past holds for every row after one it holds for.
function firstRow(rows: Row[], past: (r: Row) => boolean): number {
  let lo = 0
  let hi = rows.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (past(rows[mid])) hi = mid
    else lo = mid + 1
  }
  return lo
}
