import type { DiffHunk } from "@/lib/api"

// DiffLine is one line of a unified diff as the UI shows it. old and new are the line's numbers
// in the file before and after the change, when the line has one there.
export type DiffLine =
  | { kind: "hunk"; text: string }
  | {
      kind: "context" | "add" | "remove"
      text: string
      old?: number
      new?: number
    }
  | { kind: "note"; text: string }

const kinds: Record<string, "context" | "add" | "remove"> = {
  " ": "context",
  "+": "add",
  "-": "remove",
}

// diffLines numbers the lines of a stored diff's hunks. A hunk starts with its header; a
// "\ No newline at end of file" line becomes a note.
export function diffLines(hunks: DiffHunk[]): DiffLine[] {
  return hunks.flatMap((h) => {
    let old = h.oldStart
    let next = h.newStart
    const lines: DiffLine[] = [
      {
        kind: "hunk",
        text: `@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@`,
      },
    ]
    for (const line of h.lines) {
      const kind = kinds[line[0]]
      if (!kind) {
        lines.push({ kind: "note", text: line.replace(/^\\ ?/, "") })
        continue
      }
      const text = line.slice(1)
      if (kind === "context") lines.push({ kind, text, old: old++, new: next++ })
      else if (kind === "remove") lines.push({ kind, text, old: old++ })
      else lines.push({ kind, text, new: next++ })
    }
    return lines
  })
}

// linesOf splits text into lines; the newline that ends the last line starts no new one.
function linesOf(text: string): string[] {
  return text === "" ? [] : text.replace(/\n$/, "").split("\n")
}

// changeLines is the diff that replacing before with after makes, without line numbers: the
// lines both share at the start and the end are context, the rest is removed and added.
export function changeLines(before: string, after: string): DiffLine[] {
  const a = linesOf(before)
  const b = linesOf(after)
  let start = 0
  // Stryker disable next-line EqualityOperator: past the end of equal lists, start stops one later, and every slice below is the same.
  while (start < a.length && a[start] === b[start]) start++
  let end = 0
  while (end < a.length - start && end < b.length - start && a[a.length - 1 - end] === b[b.length - 1 - end]) end++
  const context = (text: string): DiffLine => ({ kind: "context", text })
  return [
    ...a.slice(0, start).map(context),
    ...a.slice(start, a.length - end).map((text): DiffLine => ({ kind: "remove", text })),
    ...b.slice(start, b.length - end).map((text): DiffLine => ({ kind: "add", text })),
    ...a.slice(a.length - end).map(context),
  ]
}
