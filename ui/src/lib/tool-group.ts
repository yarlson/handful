import type { ActivityBody, ActivityDiff } from "./api"

// GroupSummary is what the row of a group of tool calls shows while it is folded.
export type GroupSummary = {
  // counts are the calls of each tool, the most first; tools with as many calls keep the order
  // of their first call.
  counts: { tool: string; count: number }[]
  // diff adds up the calls' diffs; it is null when no call has one.
  diff: ActivityDiff | null
  running: boolean
  failed: number
}

// groupKey is the key that keeps a group of tool calls open in its chat, apart from the key of
// its first call's own row.
export const groupKey = (firstId: string) => `group:${firstId}`

export function groupSummary(calls: ActivityBody[]): GroupSummary {
  const counts = new Map<string, number>()
  for (const c of calls) counts.set(c.tool, (counts.get(c.tool) ?? 0) + 1)
  const diffs = calls.flatMap((c) => (c.diff ? [c.diff] : []))
  return {
    counts: [...counts].map(([tool, count]) => ({ tool, count })).sort((a, b) => b.count - a.count),
    diff:
      diffs.length === 0
        ? null
        : {
            added: diffs.reduce((n, d) => n + d.added, 0),
            removed: diffs.reduce((n, d) => n + d.removed, 0),
          },
    running: calls.some((c) => c.status === "running"),
    failed: calls.filter((c) => c.status === "error").length,
  }
}
