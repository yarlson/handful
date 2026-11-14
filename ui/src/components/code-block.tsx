import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react"
import type { Element, RootContent } from "hast"
import { Check, Copy, WrapText, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { copyLabel, useCopy, type CopyState } from "@/components/copy"
import { codeLanguage, codeText, fadeEdges, tableMarkdown } from "@/lib/code-block"
import { cn } from "@/lib/utils"

// CopyIcon shows the state of a Copy control.
export function CopyIcon({ state }: { state: CopyState }) {
  return state === "copied" ? <Check /> : state === "failed" ? <X /> : <Copy />
}

let highlighter: Promise<typeof import("@/lib/highlight")> | undefined

// loadHighlighter loads the highlighter once, on the first code block with a language. A failed
// load leaves the blocks plain, and the next block tries again.
function loadHighlighter() {
  highlighter ??= import("@/lib/highlight").catch((e: unknown) => {
    highlighter = undefined
    throw e
  })
  return highlighter
}

function render(nodes: RootContent[]): ReactNode[] {
  return nodes.map((n, i) =>
    n.type === "text" ? (
      n.value
    ) : n.type === "element" ? (
      <span key={i} className={[n.properties.className ?? []].flat().join(" ")}>
        {render(n.children)}
      </span>
    ) : null,
  )
}

// useHighlighted is the code marked up for its language, or null until the highlighter has
// loaded, and for a language it does not know.
function useHighlighted(code: string, language: string) {
  const [marked, setMarked] = useState<{ code: string; language: string; nodes: RootContent[] | null }>()
  useEffect(() => {
    if (!language) return
    let live = true
    loadHighlighter().then(
      (m) => {
        if (live) setMarked({ code, language, nodes: m.highlight(code, language) })
      },
      (e: unknown) => console.error("The code highlighter did not load:", e),
    )
    return () => {
      live = false
    }
  }, [code, language])
  return marked?.code === code && marked.language === language && marked.nodes ? render(marked.nodes) : null
}

// useFade says at which edges a scrolled code block has more code, for the fade there.
function useFade(ref: RefObject<HTMLElement | null>, code: string, wrap: boolean) {
  const [edges, setEdges] = useState({ left: false, right: false })
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const update = () => {
      const next = fadeEdges(el.scrollLeft, el.scrollWidth, el.clientWidth)
      setEdges((prev) => (prev.left === next.left && prev.right === next.right ? prev : next))
    }
    update()
    el.addEventListener("scroll", update, { passive: true })
    const resize = new ResizeObserver(update)
    resize.observe(el)
    return () => {
      el.removeEventListener("scroll", update)
      resize.disconnect()
    }
  }, [ref, code, wrap])
  return edges
}

// onMuted is a ghost button on the muted code background, where the ghost hover would not show.
export const onMuted =
  "hover:bg-foreground/10 aria-pressed:bg-foreground/10 aria-pressed:text-foreground dark:hover:bg-foreground/10"

const fade = "pointer-events-none absolute inset-y-0 w-8 from-muted to-transparent"

// CodeBlock is a markdown code block with a header: its language, Wrap and Copy. Copy gives the
// plain code, never the highlighted markup.
export function CodeBlock({ node, children }: { node?: Element; children?: ReactNode }) {
  const code = node ? codeText(node) : ""
  const language = node ? codeLanguage(node) : ""
  const [wrap, setWrap] = useState(false)
  const [state, copy] = useCopy()
  const pre = useRef<HTMLPreElement>(null)
  const edges = useFade(pre, code, wrap)
  const marked = useHighlighted(code, language)
  return (
    <div className="code-block bg-muted text-foreground rounded-lg border">
      <div className="text-foreground/80 flex items-center gap-1 border-b py-0.5 pr-1 pl-3 text-xs">
        <span className="flex-1 truncate font-mono">{language || "text"}</span>
        <Button
          size="xs"
          variant="ghost"
          className={onMuted}
          aria-pressed={wrap}
          title="Wrap long lines"
          onClick={() => setWrap((w) => !w)}
        >
          <WrapText /> Wrap
        </Button>
        <Button size="xs" variant="ghost" className={onMuted} onClick={() => copy(code, () => pre.current)}>
          <CopyIcon state={state} /> {copyLabel(state, "Copy")}
        </Button>
      </div>
      <div className="relative overflow-hidden rounded-b-lg">
        <pre ref={pre} className={cn(wrap && "whitespace-pre-wrap [overflow-wrap:anywhere]")}>
          {marked ? <code className={`language-${language}`}>{marked}</code> : children}
        </pre>
        {!wrap && edges.left && <span aria-hidden className={cn(fade, "left-0 bg-linear-to-r")} />}
        {!wrap && edges.right && <span aria-hidden className={cn(fade, "right-0 bg-linear-to-l")} />}
      </div>
    </div>
  )
}

// TableBlock is a markdown table with Copy, which gives the table as GFM markdown.
export function TableBlock({ node, children }: { node?: Element; children?: ReactNode }) {
  const [state, copy] = useCopy()
  const table = useRef<HTMLTableElement>(null)
  return (
    <div className="table-block">
      <div className="overflow-x-auto">
        <table ref={table}>{children}</table>
      </div>
      <Button
        size="xs"
        variant="ghost"
        className="text-muted-foreground mt-0.5"
        onClick={() => copy(node ? tableMarkdown(node) : "", () => table.current)}
      >
        <CopyIcon state={state} /> {copyLabel(state, "Copy table")}
      </Button>
    </div>
  )
}
