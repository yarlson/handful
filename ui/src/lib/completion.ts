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

// A command or skill being typed. atStart says its "/" begins the text, the one place where
// Handful's own commands (/clear, /compact, /task) work; elsewhere only skills do.
export type SlashQuery = MentionQuery & { atStart: boolean }

// findSlashQuery returns the command or skill being typed: a "/" at the start of the text or
// after whitespace, followed up to the caret by id characters only (a-z, 0-9 and "-"), outside
// code. So a path (src/a, /usr/bin), a URL or "and/or" never opens the menu.
export function findSlashQuery(text: string, caret: number): SlashQuery | null {
  const slash = text.lastIndexOf("/", caret - 1)
  if (slash < 0 || slash >= caret || (slash > 0 && !/\s/.test(text[slash - 1]))) return null
  const query = text.slice(slash + 1, caret)
  if (!/^[a-z0-9-]*$/.test(query) || inCode(text, slash)) return null
  return { start: slash, query, atStart: slash === 0 }
}

// namedSkills returns the ids among skillIds that text names, once each, in text order: "/<id>"
// at the start or after whitespace, ending where the id characters end, but not at a "/" (as
// in /usr/bin), and outside code. It is the menu's rule, for a whole sent message.
export function namedSkills(text: string, skillIds: string[]): string[] {
  const named: string[] = []
  for (const m of text.matchAll(/(?<=^|\s)\/([a-z0-9-]+)(?![a-z0-9/-])/g)) {
    if (skillIds.includes(m[1]) && !named.includes(m[1]) && !inCode(text, m.index)) named.push(m[1])
  }
  return named
}

// inCode tells whether position at of text is inside a fenced code block (an odd number of the
// lines before its line start with ```) or an inline code span (an odd number of backticks
// before it on its line).
function inCode(text: string, at: number): boolean {
  const lineStart = text.lastIndexOf("\n", at - 1) + 1
  const fences = text
    .slice(0, lineStart)
    .split("\n")
    .filter((line) => line.trimStart().startsWith("```")).length
  const ticks = text.slice(lineStart, at).replace(/[^`]/g, "").length
  return fences % 2 === 1 || ticks % 2 === 1
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
