import type { Entry } from "./api"

// WINDOW_PAGE is how many entries a chat loads at once: its newest page when it opens, then
// each older page as the user scrolls up. The server allows at most 200.
export const WINDOW_PAGE = 200

// EntryWindow is the part of a chat's transcript that the page holds: its newest entries
// and the older pages loaded since, in chat order (seq), with no gap. A long chat never
// loads whole, so an update to an entry older than the window is left for its page.
export type EntryWindow = {
  entries: Entry[]
  // rev is the highest rev seen, so a reconnect fetches only the changes after it.
  rev: number
  // complete says the window reaches the chat's first entry: no older page is left.
  complete: boolean
}

export const EMPTY_WINDOW: EntryWindow = { entries: [], rev: 0, complete: false }

// openWindow starts a chat's window with its newest page, in chat order. Its rev is the
// catch-up mark: a reconnect fetches the changes after it. A page shorter than asked holds
// the whole chat.
export function openWindow(page: Entry[], asked: number): EntryWindow {
  return { entries: page, rev: Math.max(0, ...page.map((e) => e.rev)), complete: page.length < asked }
}

// withOlderPage adds the page just above the window, in chat order. It keeps the window's
// rev: an old entry may carry a recent rev, and the catch-up mark must not pass changes to
// the newer entries that the window hasn't seen.
export function withOlderPage(w: EntryWindow, page: Entry[], asked: number): EntryWindow {
  return { entries: [...page, ...w.entries], rev: w.rev, complete: page.length < asked }
}

// withUpdates applies new and changed entries, from the event stream or a reconnect. Its
// cost doesn't grow with the chat's history: each update is placed by a binary search and
// copies the loaded window's array once, with no sort and no map. An entry older than the
// window is left out: its page brings it, up to date, when the user scrolls up to it.
export function withUpdates(w: EntryWindow, updates: Entry[]): EntryWindow {
  let entries = w.entries
  let rev = w.rev
  for (const e of updates) {
    rev = Math.max(rev, e.rev)
    if (!w.complete && (w.entries.length === 0 || e.seq < w.entries[0].seq)) continue
    const i = seqIndex(entries, e.seq)
    const old = entries[i]
    if (old?.seq !== e.seq) entries = entries.toSpliced(i, 0, e)
    else if (old.rev < e.rev) entries = entries.with(i, e)
  }
  return entries === w.entries && rev === w.rev ? w : { entries, rev, complete: w.complete }
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
