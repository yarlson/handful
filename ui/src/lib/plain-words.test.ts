import { readdirSync, readFileSync } from "node:fs"
import { join, relative } from "node:path"
import { parseSync } from "vite"
import { assert, test } from "vitest"

// The UI never names the workflow's old concepts (spec 9cf81412, principle 9 and AC23).
const BANNED = /\b(slots?|triage|handoffs?|goals?|items?)\b/i

// Attributes and calls whose strings are code: classes, ids, keys and kinds.
const CODE_ATTRS =
  /^(className|id|key|htmlFor|role|type|variant|size|href|src|value|name|defaultValue|aria-(controls|labelledby|describedby)|data-.*)$/
const CODE_CALLS = new Set([
  "cn",
  "cva",
  "request",
  "useLoad",
  "getElementById",
  "querySelector",
  "addEventListener",
  "removeEventListener",
  "startsWith",
  "replace",
  "includes",
])
// CODE_VALUES are the strings that stay as code on purpose: the old command and attention
// names the server still sends or accepts.
const CODE_VALUES = new Set(["goal", "/goal"])

type Node = { type: string; [key: string]: unknown }

// classList tells a Tailwind class list kept in a variable from a sentence: lower case, and
// at least half of its words are utilities with a "-" or ":".
function classList(text: string): boolean {
  const words = text.trim().split(/\s+/)
  if (!words.every((w) => /^[a-z0-9:/_[\]&.%()!-]+$/.test(w))) return false
  return words.filter((w) => /[-:]/.test(w)).length * 2 >= words.length
}

// shownStrings lists the strings of a TSX or TS source that the user may read: JSX text and
// every string and template, except code. Code is a class list, an import, a type, a key, an
// operand of a comparison or a case, the argument of a code call or the value of a code
// attribute, and the values in CODE_VALUES.
export function shownStrings(path: string, source: string): string[] {
  const { program, errors } = parseSync(path, source, { lang: path.endsWith(".tsx") ? "tsx" : "ts" })
  if (errors.length > 0) throw new Error(`${path}: ${errors[0].message}`)
  const out: string[] = []
  const add = (text: string) => {
    if (!classList(text) && !CODE_VALUES.has(text)) out.push(text)
  }
  const visit = (node: unknown, code: boolean) => {
    if (Array.isArray(node)) return node.forEach((n) => visit(n, code))
    if (!node || typeof node !== "object") return
    const n = node as Node
    // Objects without a type, such as a regex's flags, hold no strings the user reads.
    if (typeof n.type !== "string") return
    if (n.type === "JSXText") return void out.push(String(n.value))
    if (n.type === "ImportDeclaration" || n.type === "ExportAllDeclaration" || n.type.startsWith("TS")) return
    if (n.type === "Literal" && typeof n.value === "string") return void (code || add(n.value))
    if (n.type === "TemplateElement") return void (code || add((n.value as { cooked?: string }).cooked ?? ""))
    if (n.type === "JSXAttribute") {
      const name = (n.name as { name?: string }).name ?? ""
      return visit(n.value, code || CODE_ATTRS.test(name))
    }
    if (n.type === "CallExpression") {
      const callee = n.callee as { name?: string; property?: { name?: string } }
      const fn = callee.name ?? callee.property?.name ?? ""
      visit(n.callee, code)
      return visit(n.arguments, code || CODE_CALLS.has(fn))
    }
    if (n.type === "BinaryExpression" && /^[!=]==?$/.test(String(n.operator))) return visit([n.left, n.right], true)
    if (n.type === "SwitchCase") return void (visit(n.test, true), visit(n.consequent, code))
    if (n.type === "Property") return void (n.computed && visit(n.key, true), visit(n.value, code))
    if (n.type === "MemberExpression") return void (visit(n.object, code), n.computed && visit(n.property, true))
    for (const [k, v] of Object.entries(n)) if (k !== "type" && k !== "start" && k !== "end") visit(v, code)
  }
  visit(program, false)
  return out
}

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = join(dir, e.name)
    if (e.isDirectory()) return sources(path)
    return /\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name) ? [path] : []
  })
}

test("the scanner finds a banned word in any string the user may read, not in code", () => {
  const found = shownStrings(
    "x.tsx",
    `import { goal } from "@/lib/goal"
     type K = "goal"
     const a = s === "goal" ? 1 : 2
     switch (k) { case "item": break }
     const b = cn("flex items-center")
     const b2 = "grid size-10 place-items-center rounded-full"
     const c = request("POST", "/api/goals/1")
     const commands = ["clear", "goal"]
     const m = map["slots"]
     const d = \`Next goal: \${x}\`
     const e = <div className="items-start" id="goal" title="Your goal">End goal {"the item"}</div>
     const f = { label: "Slot", kind: "item" }
     const g = <h3>{"Goals"}</h3>
     const h = <h3>{x ? "Goals" : "Tasks"}</h3>
     const LABELS = { queued: "Goal", sent: "Handoff" }
     function word() { return x ? "Slot" : "Triage" }
     const TABS = ["Goals", "Members"]
     const i = <Badge>{pinned ? "item" : "step"}</Badge>
     const j = <Chip text="Handoff" />`,
  )
  assert.deepEqual(
    found.filter((s) => BANNED.test(s)),
    [
      "Next goal: ",
      "Your goal",
      "End goal ",
      "the item",
      "Slot",
      "item",
      "Goals",
      "Goals",
      "Goal",
      "Handoff",
      "Slot",
      "Triage",
      "Goals",
      "item",
      "Handoff",
    ],
  )
})

test("no string the user reads names a goal, an item, a slot, triage or a handoff", () => {
  const root = join(import.meta.dirname, "..")
  const hits = sources(root).flatMap((path) =>
    shownStrings(path, readFileSync(path, "utf8"))
      .filter((s) => BANNED.test(s))
      .map((s) => `${relative(root, path)}: ${JSON.stringify(s.trim())}`),
  )
  assert.deepEqual(hits, [])
})
