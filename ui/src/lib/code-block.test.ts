import type { Element, ElementContent, Properties } from "hast"
import { assert, test } from "vitest"
import { codeLanguage, codeText, fadeEdges, tableMarkdown } from "./code-block"

const el = (tagName: string, properties: Properties, ...children: (ElementContent | string)[]): Element => ({
  type: "element",
  tagName,
  properties,
  children: children.map((c) => (typeof c === "string" ? { type: "text", value: c } : c)),
})

// The hast that markdown gives for ```go\nfmt.Println("hi")\n```, after highlighting split it up.
const goBlock = el(
  "pre",
  {},
  el("code", { className: ["language-go"] }, el("span", { className: ["hljs-title"] }, "fmt"), '.Println("hi")\n'),
)

test("a code block copies its exact code without the one newline that ends it", () => {
  assert.equal(codeText(goBlock), 'fmt.Println("hi")')
  assert.equal(codeText(el("pre", {}, el("code", {}, "a\n\n"))), "a\n", "only one newline goes")
  assert.equal(codeText(el("pre", {}, el("code", {}, "  indented"))), "  indented")
  assert.equal(codeText(el("pre", {}, el("code", {}, "one\ntwo\n"))), "one\ntwo")
  assert.equal(codeText(el("pre", {}, el("code", {}, { type: "comment", value: "note" }, "a"))), "a")
})

test("a code block's language comes from its language- class, and is empty without one", () => {
  assert.equal(codeLanguage(goBlock), "go")
  assert.equal(codeLanguage(el("pre", {}, el("code", { className: ["x", "language-ts"] }, "a"))), "ts")
  assert.equal(codeLanguage(el("pre", {}, el("code", {}, "a"))), "")
  assert.equal(codeLanguage(el("pre", {}, el("code", { className: ["nolanguage"] }, "a"))), "")
  assert.equal(codeLanguage(el("pre", {}, "a")), "")
  assert.equal(
    codeLanguage(
      el("pre", {}, "\n", el("span", { className: ["language-py"] }), el("code", { className: ["language-sql"] })),
    ),
    "sql",
    "the language is the code element's",
  )
})

test("a table copies as GFM markdown with its alignment, and escapes a | in a cell", () => {
  // Markdown puts a newline text node between the rows and cells of a table.
  const row = (...cells: Element[]) => el("tr", {}, "\n", ...cells.flatMap((c) => [c, "\n"]))
  const table = el(
    "table",
    {},
    "\n",
    el(
      "thead",
      {},
      "\n",
      row(
        el("th", {}, "Name"),
        el("th", { align: "left" }, "Kind"),
        el("th", { align: "center" }, "Size"),
        el("th", { align: "right" }, "Cost"),
      ),
      "\n",
    ),
    "\n",
    el(
      "tbody",
      {},
      "\n",
      row(el("td", {}, el("code", {}, "a|b")), el("td", {}, "x"), el("td", {}, "1"), el("td", {}, "line\nbreak")),
      "\n",
      row(el("td", {}, el("strong", {}, "bold")), el("td", {}), el("td", {}, "2"), el("td", {}, "3")),
      "\n",
    ),
    "\n",
  )
  assert.equal(
    tableMarkdown(table),
    [
      "| Name | Kind | Size | Cost |",
      "| --- | :--- | :---: | ---: |",
      "| a\\|b | x | 1 | line break |",
      "| bold |  | 2 | 3 |",
    ].join("\n"),
  )
})

test("a fade shows at each edge past which the code can scroll", () => {
  assert.deepEqual(fadeEdges(0, 100, 100), { left: false, right: false }, "code that fits")
  assert.deepEqual(fadeEdges(0, 300, 100), { left: false, right: true }, "at the start")
  assert.deepEqual(fadeEdges(100, 300, 100), { left: true, right: true }, "in the middle")
  assert.deepEqual(fadeEdges(200, 300, 100), { left: true, right: false }, "at the end")
})

test("a fade ignores the one pixel that zoom can leave at an edge", () => {
  assert.deepEqual(fadeEdges(1, 300, 100), { left: false, right: true })
  assert.deepEqual(fadeEdges(1.5, 300, 100), { left: true, right: true })
  assert.deepEqual(fadeEdges(199, 300, 100), { left: true, right: false })
  assert.deepEqual(fadeEdges(198.5, 300, 100), { left: true, right: true })
})
