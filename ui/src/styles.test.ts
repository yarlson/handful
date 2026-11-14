import { readdirSync, readFileSync } from "node:fs"
import { join, relative } from "node:path"
import { assert, test } from "vitest"

const root = import.meta.dirname
const css = readFileSync(join(root, "index.css"), "utf8")

// State colours come from the attention, info and success tokens; bot colours are the one palette.
const RAW_COLOUR = /\b(?:amber|blue|emerald)-\d{2,3}\b/
const EXEMPT = new Set(["lib/bot-color.ts"])
const PIXEL_TEXT = /\btext-\[\d+(?:\.\d+)?px\]/
// Every control has the 3px ring of the shadcn Button. A bare ring or outline (1px), a 1, 2, 4 or
// 8px one, or an arbitrary width is a second style.
const OTHER_FOCUS_RING =
  /\b(?:focus|focus-visible|focus-within):(?:(?:ring|outline)-(?:[1248]\b|\[[^\]]*\])|(?:ring|outline)(?![\w/-]))/

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const path = join(dir, e.name)
    if (e.isDirectory()) return sources(path)
    return /\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name) ? [path] : []
  })
}

function hits(pattern: RegExp, path: string, text: string): string[] {
  return text.split("\n").flatMap((line, i) => (pattern.test(line) ? [`${path}:${i + 1}: ${line.trim()}`] : []))
}

function sourceHits(pattern: RegExp): string[] {
  return sources(root)
    .map((path) => relative(root, path))
    .filter((path) => !EXEMPT.has(path))
    .flatMap((path) => hits(pattern, path, readFileSync(join(root, path), "utf8")))
}

function block(selector: string): string {
  const start = css.indexOf(selector)
  assert.notEqual(start, -1, `index.css has no ${selector}`)
  let depth = 0
  for (let i = css.indexOf("{", start); i < css.length; i++) {
    if (css[i] === "{") depth++
    if (css[i] === "}" && --depth === 0) return css.slice(start, i + 1)
  }
  throw new Error(`index.css: ${selector} is not closed`)
}

test("the scan finds a raw amber, blue or emerald class and passes the state tokens", () => {
  assert.deepEqual(hits(RAW_COLOUR, "x.tsx", 'a\n<span className="text-amber-700 dark:bg-emerald-400/10" />\nb'), [
    'x.tsx:2: <span className="text-amber-700 dark:bg-emerald-400/10" />',
  ])
  assert.deepEqual(hits(RAW_COLOUR, "x.tsx", 'className="border-blue-600"'), ['x.tsx:1: className="border-blue-600"'])
  assert.deepEqual(hits(RAW_COLOUR, "x.tsx", 'className="bg-attention-surface text-info-foreground bg-success/10"'), [])
})

test("no component uses a raw amber, blue or emerald colour class", () => {
  assert.deepEqual(sourceHits(RAW_COLOUR), [])
})

test("the scan finds a pixel text size and passes the type scale", () => {
  assert.deepEqual(hits(PIXEL_TEXT, "x.tsx", '<p className="text-muted-foreground text-[13px]">'), [
    'x.tsx:1: <p className="text-muted-foreground text-[13px]">',
  ])
  assert.deepEqual(hits(PIXEL_TEXT, "x.tsx", 'className="text-xs text-[0.85em] text-[var(--x)]"'), [])
})

test("no component sets its text size in pixels", () => {
  assert.deepEqual(sourceHits(PIXEL_TEXT), [])
})

test("reduced motion stops every animation and transition", () => {
  const reduced = block("@media (prefers-reduced-motion: reduce)")
  assert.match(reduced, /\*,\s*::before,\s*::after\s*\{/)
  assert.match(reduced, /animation:\s*none\s*!important/)
  assert.match(reduced, /transition-duration:\s*0ms\s*!important/)
})

test("transitions take the base duration token by default", () => {
  assert.match(block("@theme inline"), /--default-transition-duration:\s*var\(--duration-base\)/)
  const light = block(":root {")
  assert.match(light, /--duration-fast:\s*100ms/)
  assert.match(light, /--duration-base:\s*150ms/)
})

test("every control without its own ring shows the 3px focus ring of the shadcn Button", () => {
  const base = block("@layer base")
  assert.match(
    base,
    /:focus-visible\s*\{\s*outline:\s*3px solid color-mix\(in oklab, var\(--ring\) 50%, transparent\);\s*outline-offset:\s*0;/,
  )
})

test("the scan finds a focus ring of another width and passes the 3px ring", () => {
  for (const ring of [
    "focus-visible:ring-2",
    "focus:outline-1",
    "focus-visible:ring",
    "focus-visible:outline",
    "focus-visible:ring-[2px]",
    "focus-within:outline-[3px]",
  ]) {
    assert.deepEqual(hits(OTHER_FOCUS_RING, "x.tsx", `className="a ${ring} b"`), [`x.tsx:1: className="a ${ring} b"`])
  }
  assert.deepEqual(
    hits(
      OTHER_FOCUS_RING,
      "x.tsx",
      'className="focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:ring-0 focus-visible:outline-none focus-visible:ring-destructive/20"',
    ),
    [],
  )
})

test("no control sets a focus ring of another width", () => {
  assert.deepEqual(sourceHits(OTHER_FOCUS_RING), [])
})
