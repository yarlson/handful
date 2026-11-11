import type { Entry } from "./api"
import { keptChats, seqIndex } from "./window"

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

// rowOffsets returns where each row of the transcript starts, below the top of its first row:
// offsets[i] is the top of row i, and offsets[entries.length] is the height of all the rows
// and one gap. A row has the height it had when last on screen, or else its kind's estimate.
export function rowOffsets(entries: Entry[], heights: ReadonlyMap<string, number>): number[] {
  const offsets = [0]
  let y = 0
  for (const e of entries) {
    y += (heights.get(e.id) ?? ROW_ESTIMATES[e.kind]) + ROW_GAP
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

// RowHeights keeps the measured heights of rows, by entry id, for the chats opened most
// recently: a chat's map lasts while it is one of the last max chats opened.
export class RowHeights {
  private chats = new Map<string, Map<string, number>>()
  private recent: string[] = []

  constructor(private max: number) {}

  // of returns chat id's heights, and drops those of the chats opened before the last max.
  of(id: string): Map<string, number> {
    this.recent = keptChats(this.recent, id, this.max)
    for (const old of this.chats.keys()) if (!this.recent.includes(old)) this.chats.delete(old)
    let heights = this.chats.get(id)
    if (!heights) this.chats.set(id, (heights = new Map()))
    return heights
  }
}

// RowSpan is a range of rows by their entries' seqs, so that it stays on the same entries when
// an older page arrives above them. last is null when the span reaches the newest entry: it
// then grows with each new one.
export type RowSpan = { first: number; last: number | null }

export function spanOf(entries: Entry[], range: RowRange): RowSpan {
  return {
    first: entries[range.start].seq,
    last: range.end === entries.length ? null : entries[range.end - 1].seq,
  }
}

export function rangeOf(entries: Entry[], span: RowSpan): RowRange {
  return {
    start: seqIndex(entries, span.first),
    end: span.last === null ? entries.length : seqIndex(entries, span.last + 1),
  }
}
