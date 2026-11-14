import type { ActivityDiff } from "@/lib/api"
import type { DiffLine } from "@/lib/diff"
import { cn } from "@/lib/utils"

const tone = {
  context: "",
  add: "bg-success/10 text-success-foreground",
  remove: "bg-destructive/10 text-destructive",
}
const sign = { context: " ", add: "+", remove: "-" }

// muted-foreground on the muted surface is under 4.5:1 in the light theme.
const muted = "text-foreground/70"
const number = cn(muted, "w-0 px-1.5 text-right align-top select-none")

// DiffView shows a unified diff: hunk headers, removed and added lines in their colours, and the
// "No newline at end of file" note. numbers adds each line's old and new numbers.
export function DiffView({ lines, numbers = false }: { lines: DiffLine[]; numbers?: boolean }) {
  return (
    <div className="bg-muted max-h-80 overflow-auto rounded-md py-1 font-mono text-xs">
      <table className="w-full border-collapse">
        <tbody>
          {lines.map((l, i) =>
            l.kind === "hunk" || l.kind === "note" ? (
              <tr key={i}>
                <td
                  colSpan={numbers ? 4 : 2}
                  className={cn(muted, "px-2", l.kind === "hunk" ? "bg-foreground/5" : "font-sans italic")}
                >
                  {l.text}
                </td>
              </tr>
            ) : (
              <tr key={i} className={tone[l.kind]}>
                {numbers && <td className={number}>{l.old}</td>}
                {numbers && <td className={number}>{l.new}</td>}
                <td className="w-0 pl-2 align-top select-none">{sign[l.kind]}</td>
                <td className="pr-2 pl-1 whitespace-pre-wrap [overflow-wrap:anywhere]">{l.text}</td>
              </tr>
            ),
          )}
        </tbody>
      </table>
    </div>
  )
}

// DiffCounts is a diff's added and removed line counts, as +N −M.
export function DiffCounts({ diff }: { diff: ActivityDiff }) {
  return (
    <span className="shrink-0 font-mono">
      <span className="text-success-foreground">+{diff.added}</span>{" "}
      <span className="text-destructive">−{diff.removed}</span>
    </span>
  )
}
