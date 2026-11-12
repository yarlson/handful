// The history of the messages sent from this device in one chat, which ↑ and ↓ bring back in
// the message box, as in Claude Code. It lives in the browser's storage beside the drafts, so it
// survives a reload and stays on this device.

export type HistoryEntry = { text: string; at: number }

type Store = Pick<Storage, "getItem" | "setItem" | "removeItem">

// historyCap is the most entries a chat keeps; the oldest goes first.
export const historyCap = 1000

// withStorage runs fn on the browser's storage. Storage can be unavailable or full, as in a
// private window; history is a convenience, so fallback stands in and nothing is reported.
export function withStorage<T>(fn: (store: Store) => T, fallback: T): T {
  try {
    return fn(localStorage)
  } catch {
    return fallback
  }
}

export function historyKey(chatId: string): string {
  return `handful.history.${chatId}`
}

// loadHistory returns chatId's entries, oldest first. Storage that holds something else gives
// none, so the next send writes a valid history over it.
export function loadHistory(store: Store, chatId: string): HistoryEntry[] {
  let raw: unknown
  try {
    // A missing key reads as "null", which is no list.
    raw = JSON.parse(String(store.getItem(historyKey(chatId))))
  } catch {
    // Not JSON: no history.
  }
  return Array.isArray(raw)
    ? raw.filter((e): e is HistoryEntry => typeof e?.text === "string" && typeof e.at === "number").slice(-historyCap)
    : []
}

// recordSent adds text, trimmed, sent at the time at (ms), as chatId's newest entry. An empty
// text, or one equal to the newest entry, adds nothing.
export function recordSent(store: Store, chatId: string, text: string, at: number): void {
  const trimmed = text.trim()
  const entries = loadHistory(store, chatId)
  if (!trimmed || entries.at(-1)?.text === trimmed) return
  entries.push({ text: trimmed, at })
  store.setItem(historyKey(chatId), JSON.stringify(entries.slice(-historyCap)))
}

// forgetHistory removes chatId's history, for a chat that was deleted.
export function forgetHistory(store: Store, chatId: string): void {
  store.removeItem(historyKey(chatId))
}

// HistoryKeyPress is an arrow key in the message box, with the box's text and selection.
export type HistoryKeyPress = {
  key: string
  modified: boolean // Shift, Alt, Ctrl or ⌘ held
  composing: boolean // an IME composes
  menuOpen: boolean // the suggestion menu is open
  browsing: boolean
  value: string
  start: number
  end: number
}

// historyMove tells whether a key press goes to an older or a newer entry, or does what it does
// natively (null). An IME, the open menu, a modifier key and a selection keep the key's own
// meaning, in that order. ↑ goes back from the first line: while browsing, from anywhere on it;
// otherwise from position 0, or in an empty box. ↓ goes forward only while browsing, from the end.
export function historyMove(p: HistoryKeyPress): "older" | "newer" | null {
  if (p.composing || p.menuOpen || p.modified || p.start !== p.end) return null
  if (p.key === "ArrowUp") {
    const onFirstLine = !p.value.slice(0, p.start).includes("\n")
    return (p.browsing ? onFirstLine : p.start === 0) ? "older" : null
  }
  if (p.key === "ArrowDown" && p.browsing && p.start === p.value.length) return "newer"
  return null
}

// Browse is a walk through a chat's history: the entries as they were when it began, the one
// shown, and the draft that was in the box.
export type Browse = { entries: HistoryEntry[]; index: number; draft: string }

// Step is the box after a move: the walk (null when it ended), the text to show, and what a
// screen reader hears.
export type Step = { browse: Browse | null; text: string; status: string }

// stepBrowse moves the walk one entry older or newer. Going older begins a walk over entries
// with draft saved; going newer past the newest entry ends it and brings the draft back. It
// returns null when there is nowhere to go: no entries, or the oldest is shown already.
export function stepBrowse(
  browse: Browse | null,
  entries: HistoryEntry[],
  draft: string,
  move: "older" | "newer",
): Step | null {
  if (browse === null) {
    if (move === "newer" || entries.length === 0) return null
    return shown({ entries, index: entries.length - 1, draft })
  }
  const index = browse.index + (move === "older" ? -1 : 1)
  if (index < 0) return null
  if (index === browse.entries.length) return endBrowse(browse)
  return shown({ ...browse, index })
}

// endBrowse ends the walk and brings the draft back.
export function endBrowse(browse: Browse): Step {
  return { browse: null, text: browse.draft, status: "Your draft" }
}

function shown(browse: Browse): Step {
  const total = browse.entries.length
  return {
    browse,
    text: browse.entries[browse.index].text,
    status: `Sent message ${total - browse.index} of ${total}`,
  }
}
