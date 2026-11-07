// A mention being typed: the "@" at start and the text typed after it.
export type MentionQuery = { start: number; query: string }

const maxQueryLength = 40

// findMentionQuery returns the mention the caret is in, if any. A mention
// starts with "@" at the start of the text or after whitespace, and the text
// between "@" and the caret has no whitespace.
export function findMentionQuery(text: string, caret: number): MentionQuery | null {
  const at = text.lastIndexOf("@", caret - 1)
  if (at < 0) return null
  if (at > 0 && !/\s/.test(text[at - 1])) return null
  const query = text.slice(at + 1, caret)
  if (query.length > maxQueryLength || /\s/.test(query)) return null
  return { start: at, query }
}

// matchNames returns the names that fit the query, case-insensitively: names
// that start with it first, then names that contain it, each in their given order.
export function matchNames(names: string[], query: string, limit = 8): string[] {
  const q = query.toLowerCase()
  const starts = names.filter((n) => n.toLowerCase().startsWith(q))
  const contains = names.filter((n) => !n.toLowerCase().startsWith(q) && n.toLowerCase().includes(q))
  return [...starts, ...contains].slice(0, limit)
}

// findSlashQuery returns the command or skill being typed: a "/" at the very
// start of the text, with the caret still in that first word.
export function findSlashQuery(text: string, caret: number): MentionQuery | null {
  if (!text.startsWith("/")) return null
  const firstSpace = text.search(/\s/)
  if (firstSpace !== -1 && caret > firstSpace) return null
  return { start: 0, query: text.slice(1, caret) }
}

// applyCompletion replaces the typed query (from its start to the caret) with
// token and a space, and returns the new text and caret position. It does not
// add a space when one follows already.
export function applyCompletion(
  text: string,
  query: MentionQuery,
  caret: number,
  token: string,
): { text: string; caret: number } {
  const rest = text.slice(caret)
  const spaced = rest.startsWith(" ")
  const insert = spaced ? token : `${token} `
  return { text: text.slice(0, query.start) + insert + rest, caret: query.start + insert.length + (spaced ? 1 : 0) }
}
