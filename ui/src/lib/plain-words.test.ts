import { readdirSync, readFileSync } from "node:fs"
import { join, relative } from "node:path"
import { parseSync } from "vite"
import { assert, test } from "vitest"

// The UI never names the workflow's old concepts (spec 9cf81412, principle 9 and AC23).
const BANNED = /\b(slots?|triage|handoffs?|goals?|items?)\b/i

// Attributes whose values the user reads or hears.
const SHOWN = new Set(["aria-label", "title", "placeholder", "alt", "label", "open", "submit", "lead", "rest"])
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

type Node = { type: string; [key: string]: unknown }

// classList tells a Tailwind class list kept in a variable from a sentence: lower case, and
// at least half of its words are utilities with a "-" or ":".
function classList(text: string): boolean {
  const words = text.trim().split(/\s+/)
  if (!words.every((w) => /^[a-z0-9:/_[\]&.%()!-]+$/.test(w))) return false
  return words.filter((w) => /[-:]/.test(w)).length * 2 >= words.length
}

// shownStrings lists the strings of a TSX or TS source that the user can read: JSX text,
// the values of shown attributes, and every other string or template with a space in it,
// except class lists, imports, types and the arguments of code calls.
export function shownStrings(path: string, source: string): string[] {
  const { program, errors } = parseSync(path, source, { lang: path.endsWith(".tsx") ? "tsx" : "ts" })
  if (errors.length > 0) throw new Error(`${path}: ${errors[0].message}`)
  const out: string[] = []
  const visit = (node: unknown, code: boolean, shown: boolean) => {
    if (Array.isArray(node)) return node.forEach((n) => visit(n, code, shown))
    if (!node || typeof node !== "object") return
    const n = node as Node
    // Objects without a type, such as a regex's flags, hold no strings the user reads.
    if (typeof n.type !== "string") return
    if (n.type === "JSXText") return void out.push(String(n.value))
    if (n.type === "ImportDeclaration" || n.type === "ExportAllDeclaration" || n.type.startsWith("TS")) return
    if (n.type === "Literal" && typeof n.value === "string") {
      if (!code && (shown || (/\s/.test(n.value) && !classList(n.value)))) out.push(n.value)
      return
    }
    if (n.type === "TemplateElement") {
      const text = (n.value as { cooked?: string }).cooked ?? ""
      if (!code && (shown || /\s/.test(text))) out.push(text)
      return
    }
    if (n.type === "JSXAttribute") {
      const name = (n.name as { name?: string }).name ?? ""
      return visit(n.value, CODE_ATTRS.test(name), SHOWN.has(name))
    }
    if (n.type === "CallExpression") {
      const callee = n.callee as { name?: string; property?: { name?: string } }
      const fn = callee.name ?? callee.property?.name ?? ""
      visit(n.callee, code, false)
      return visit(n.arguments, code || CODE_CALLS.has(fn), false)
    }
    if (n.type === "Property" && !n.computed)
      return visit(n.value, code, shown || SHOWN.has(String((n.key as { name?: string }).name)))
    for (const [k, v] of Object.entries(n)) if (k !== "type" && k !== "start" && k !== "end") visit(v, code, shown)
  }
  visit(program, false, false)
  return out
}

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = join(dir, e.name)
    if (e.isDirectory()) return sources(path)
    return /\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name) ? [path] : []
  })
}

test("the scanner finds a banned word in JSX text, shown attributes and sentences, not in code", () => {
  const found = shownStrings(
    "x.tsx",
    `import { goal } from "@/lib/goal"
     type K = "goal"
     const a = s === "goal" ? 1 : 2
     const b = cn("flex items-center")
     const b2 = "grid size-10 place-items-center rounded-full"
     const c = request("POST", "/api/goals/1")
     const d = \`Next goal: \${x}\`
     const e = <div className="items-start" id="goal" title="Your goal">End goal {"the item"}</div>
     const f = { label: "Slot", kind: "item" }`,
  )
  assert.deepEqual(
    found.filter((s) => BANNED.test(s)),
    ["Next goal: ", "Your goal", "End goal ", "the item", "Slot"],
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
