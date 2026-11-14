import type { Entry } from "@/lib/api"

// The server searches from MIN_SEARCH to MAX_SEARCH characters and returns at most
// MAX_RESULTS messages (internal/core/search.go).
export const MIN_SEARCH = 2
export const MAX_SEARCH = 200
export const MAX_RESULTS = 50

// matchesName tells whether a chat's name contains the sidebar filter, with no case. A blank
// filter matches every name.
export function matchesName(name: string, filter: string): boolean {
  return name.toLowerCase().includes(filter.trim().toLowerCase())
}

// searchable is the query that the chat search sends, or null while it has too few characters.
// Characters are code points, as the server counts them.
export function searchable(query: string): string | null {
  const q = query.trim()
  return Array.from(q).length >= MIN_SEARCH ? q : null
}

// findKey tells whether a key press opens the chat search: ⌘F on a Mac, Ctrl+F elsewhere, so
// that Ctrl+F still moves the cursor in a Mac text field. The key's place counts, not its
// letter, so it works in every keyboard layout.
export function findKey(
  e: { code: string; metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean },
  mac: boolean,
): boolean {
  return e.code === "KeyF" && (mac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey) && !e.altKey && !e.shiftKey
}

// speakerOf names who wrote a search result: the user, the bot or room member, or the other
// bot of a direct message.
export function speakerOf(e: Entry, chatName: string): string {
  if (e.kind === "user") return "You"
  if (e.kind === "bot") return e.body.speakerName ?? chatName
  if (e.kind === "agent" && e.body.direction === "in") return e.body.peerName
  return chatName
}

// textOf is the text that the search looked in.
export function textOf(e: Entry): string {
  return e.kind === "user" || e.kind === "bot" || e.kind === "agent" ? e.body.text : ""
}

export type Snippet = { before: string; match: string; after: string }

const AROUND = { before: 40, after: 100 }

// snippetOf is the part of text around the first match of query, with no case: up to 40
// characters before it and 100 after, each run of whitespace as one space, and "…" where the
// text goes on. A text without the match gives its start.
export function snippetOf(text: string, query: string): Snippet {
  const found = new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "iu").exec(text)
  const at = found?.index ?? 0
  const end = at + (found?.[0].length ?? 0)
  const start = Math.max(0, at - AROUND.before)
  const stop = end + AROUND.after
  const flat = (s: string) => s.replace(/\s+/g, " ")
  return {
    before: (start > 0 ? "…" : "") + flat(text.slice(start, at)).trimStart(),
    match: flat(text.slice(at, end)),
    after: flat(text.slice(end, stop)).trimEnd() + (stop < text.length ? "…" : ""),
  }
}
