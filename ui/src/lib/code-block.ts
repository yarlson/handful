import type { Element, ElementContent } from "hast"

// textOf is the plain text under a hast node.
export function textOf(node: ElementContent): string {
  if (node.type === "text") return node.value
  if (node.type === "element") return node.children.map(textOf).join("")
  return ""
}

const elements = (node: Element, tag: string): Element[] =>
  node.children.flatMap((c) => (c.type !== "element" ? [] : c.tagName === tag ? [c] : elements(c, tag)))

// codeText is what Copy puts on the clipboard for a code block: its exact code, without the
// one newline that ends every block.
export function codeText(pre: Element): string {
  return textOf(pre).replace(/\n$/, "")
}

// codeLanguage is a fenced block's language, from its language-* class, or "" when it has none.
export function codeLanguage(pre: Element): string {
  const names = [elements(pre, "code")[0]?.properties.className].flat().map(String)
  return names.find((c) => c.startsWith("language-"))?.slice("language-".length) ?? ""
}

const separator: Record<string, string> = { left: ":---", center: ":---:", right: "---:" }

// tableMarkdown writes a markdown table back as GFM markdown. A cell keeps its text and drops its
// inline formatting; a | in a cell is escaped.
export function tableMarkdown(table: Element): string {
  const rows = elements(table, "tr").map((tr) =>
    // A row's elements are its th or td cells.
    tr.children.filter((c) => c.type === "element"),
  )
  const line = (cells: string[]) => `| ${cells.join(" | ")} |`
  const [head, ...body] = rows
  const cell = (c: Element) => textOf(c).replace(/\|/g, "\\|").replace(/\n/g, " ")
  const align = head.map((c) => separator[String(c.properties.align)] ?? "---")
  return [line(head.map(cell)), line(align), ...body.map((r) => line(r.map(cell)))].join("\n")
}

// fadeEdges says at which edges a code block can scroll further, so that a fade shows there.
// One pixel of slack absorbs the fractional widths that zoom gives.
export function fadeEdges(scrollLeft: number, scrollWidth: number, clientWidth: number) {
  return { left: scrollLeft > 1, right: scrollLeft + clientWidth < scrollWidth - 1 }
}
