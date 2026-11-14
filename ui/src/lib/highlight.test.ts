import type { RootContent } from "hast"
import { assert, test } from "vitest"
import { highlight } from "./highlight"

// classed lists the class and text of each highlighted span.
function classed(nodes: RootContent[] | null): string[] {
  return (nodes ?? []).flatMap((n) =>
    n.type !== "element"
      ? []
      : [`${[n.properties.className].flat().join(" ")}: ${textOf(n.children)}`, ...classed(n.children)],
  )
}
const textOf = (nodes: RootContent[]): string =>
  nodes.map((n) => (n.type === "text" ? n.value : n.type === "element" ? textOf(n.children) : "")).join("")

test("code in a known language comes back as classed spans with the same text", () => {
  const nodes = highlight('const n: number = 1 // one\nlet s = "x"', "typescript")
  assert.equal(textOf(nodes ?? []), 'const n: number = 1 // one\nlet s = "x"')
  assert.includeMembers(classed(nodes), [
    "hljs-keyword: const",
    "hljs-number: 1",
    "hljs-comment: // one",
    'hljs-string: "x"',
  ])
})

test("each of the 12 languages and their usual aliases highlight", () => {
  for (const language of [
    "bash",
    "sh",
    "zsh",
    "css",
    "diff",
    "go",
    "golang",
    "javascript",
    "js",
    "jsx",
    "json",
    "python",
    "py",
    "sql",
    "typescript",
    "ts",
    "tsx",
    "xml",
    "html",
    "svg",
    "yaml",
    "yml",
    "markdown",
    "md",
  ]) {
    assert.isNotNull(highlight("x", language), language)
  }
})

test("a diff marks its added and removed lines", () => {
  assert.includeMembers(classed(highlight("-old\n+new", "diff")), ["hljs-deletion: -old", "hljs-addition: +new"])
})

test("a language it does not know, or none, gives no highlighting", () => {
  assert.isNull(highlight("x", "cobol"))
  assert.isNull(highlight("x", ""))
  assert.isNull(highlight("x", "text"))
})
