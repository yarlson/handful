import type { ApprovalBody } from "@/lib/api"

// toolTarget is what a tool call acts on, in one line: the file, the command, the pattern or the
// address; "" when its input names none.
export function toolTarget(input: unknown): string {
  const fields = fieldsOf(input)
  for (const key of ["file_path", "notebook_path", "command", "pattern", "url", "query", "path"]) {
    const value = fields?.[key]
    if (typeof value === "string" && value !== "") return value.split("\n")[0]
  }
  return ""
}

// fieldsOf is a tool's input as named fields, or null when the input is not an object.
export function fieldsOf(input: unknown): Record<string, unknown> | null {
  return typeof input === "object" && !Array.isArray(input) ? (input as Record<string, unknown> | null) : null
}

// InputField is one named value of a tool's input. A block value is long or has several lines,
// so it shows in a code block.
export type InputField = { name: string; value: string; block: boolean }

// The longest value that still shows on the line of its name.
const maxInline = 80

// inputFields lists a tool's input as names and values. A value that is not a string shows as
// JSON. Input that is not an object is one field named "input".
export function inputFields(input: unknown): InputField[] {
  const fields = fieldsOf(input) ?? { input }
  return Object.entries(fields).map(([name, v]) => {
    const value = typeof v === "string" ? v : (JSON.stringify(v, null, 2) ?? String(v))
    return {
      name,
      value,
      block: value.length > maxInline || value.includes("\n"),
    }
  })
}

// decisionLabel is an answered approval's decision, as its badge and its folded line show it.
export function decisionLabel(b: Pick<ApprovalBody, "status" | "decision">): string {
  if (b.status === "allowed") return b.decision === "always" ? "always allowed" : "allowed"
  return b.status
}
