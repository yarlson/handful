import type { Entry } from "./api"

// WINDOW_PAGE is how many entries a chat loads at once: its newest page when it opens, then
// each older page as the user scrolls up. The server allows at most 200.
export const WINDOW_PAGE = 200

// EntryWindow is the part of a chat's transcript that the page holds: its newest entries
// and the older pages loaded since, in chat order (seq), with no gap. A long chat never
// loads whole, so an update to an entry older than the window is left for its page.
export type EntryWindow = {
  entries: Entry[]
  // rev is the highest rev seen, which changes with every update.
  rev: number
  // caughtUp is the catch-up mark: every change up to it is known, so a reconnect fetches
  // only the changes after it. Each change takes the chat's next rev, but the server can
  // publish a change before one that committed just before it; a rev seen past a missing one
  // waits in ahead until the gap closes.
  caughtUp: number
  ahead: number[]
  // complete says the window reaches the chat's first entry: no older page is left.
  complete: boolean
}

export const EMPTY_WINDOW: EntryWindow = { entries: [], rev: 0, caughtUp: 0, ahead: [], complete: false }

// MAX_AHEAD bounds the revs that wait for a gap to close. Forgetting one only makes the next
// reconnect fetch a little more.
export const MAX_AHEAD = 1000

// openWindow starts a chat's window with its newest page, in chat order. Its rev is the
// catch-up mark: a reconnect fetches the changes after it. A page shorter than asked holds
// the whole chat.
export function openWindow(page: Entry[], asked: number): EntryWindow {
  const rev = Math.max(0, ...page.map((e) => e.rev))
  return { entries: page, rev, caughtUp: rev, ahead: [], complete: page.length < asked }
}

// withOlderPage adds the page just above the window, in chat order. It keeps the window's
// rev: an old entry may carry a recent rev, and the catch-up mark must not pass changes to
// the newer entries that the window hasn't seen.
export function withOlderPage(w: EntryWindow, page: Entry[], asked: number): EntryWindow {
  return { ...w, entries: [...page, ...w.entries], complete: page.length < asked }
}

// withUpdates applies new and changed entries, from the event stream or a reconnect. Its
// cost doesn't grow with the chat's history: each update is placed by a binary search and
// copies the loaded window's array once, with no sort and no map. An entry older than the
// window is left out: its page brings it, up to date, when the user scrolls up to it.
export function withUpdates(w: EntryWindow, updates: Entry[]): EntryWindow {
  let entries = w.entries
  let rev = w.rev
  const before = { caughtUp: w.caughtUp, ahead: w.ahead }
  let mark = before
  for (const e of updates) {
    rev = Math.max(rev, e.rev)
    mark = seen(mark, e.rev)
    if (!w.complete && (w.entries.length === 0 || e.seq < w.entries[0].seq)) continue
    const i = seqIndex(entries, e.seq)
    const old = entries[i]
    if (old?.seq !== e.seq) entries = entries.toSpliced(i, 0, e)
    else if (old.rev < e.rev) entries = entries.with(i, e)
  }
  // rev moves only with a rev above it, which moves the mark too.
  if (entries === w.entries && mark === before) return w
  return { entries, rev, ...mark, complete: w.complete }
}

// caughtUpTo says that every change up to rev is known: a fetch of the changes after the mark
// returned them all, up to rev.
export function caughtUpTo(w: EntryWindow, rev: number): EntryWindow {
  if (rev <= w.caughtUp) return w
  return {
    ...w,
    ...closeGaps(
      rev,
      w.ahead.filter((r) => r > rev),
    ),
  }
}

type Mark = { caughtUp: number; ahead: number[] }

// seen adds a rev that the stream brought to the catch-up mark.
function seen(m: Mark, rev: number): Mark {
  if (rev <= m.caughtUp || m.ahead.includes(rev)) return m
  const ahead = [...m.ahead, rev].sort((a, b) => a - b).slice(-MAX_AHEAD)
  return closeGaps(m.caughtUp, ahead)
}

// closeGaps moves the mark caughtUp over the revs in ahead (sorted) that follow it with no gap.
function closeGaps(caughtUp: number, ahead: number[]): Mark {
  let i = 0
  while (ahead[i] === caughtUp + 1) caughtUp = ahead[i++]
  return { caughtUp, ahead: ahead.slice(i) }
}

// seqIndex returns where seq is, or would go, in entries, which are in seq order.
export function seqIndex(entries: Entry[], seq: number): number {
  let lo = 0
  let hi = entries.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (entries[mid].seq < seq) lo = mid + 1
    else hi = mid
  }
  return lo
}

// keptChats returns the chats whose windows stay loaded after chat id opens: it and the
// chats opened most recently before it, at most max in all, newest first. The others
// unload, so a long chat once opened doesn't hold its memory for the rest of the session.
export function keptChats(recent: string[], id: string, max: number): string[] {
  return [id, ...recent.filter((r) => r !== id)].slice(0, max)
}
