// The Usage section of Settings: a bar for each usage window of the Claude plan, and each bot's
// share of what the Handful bots on this computer spent in a period.

import type { BotSpend } from "./api"
import { clock, type Zone } from "./dates"
import { resetTime, usedPercent, WARNING_LABELS, type PlanWindow } from "./usage-banner"

// WINDOW_TITLES are the titles of the windows that the section shows first, in its order.
const WINDOW_TITLES = new Map([
  ["five_hour", "Current session"],
  ["seven_day", "Current week (all models)"],
  ["seven_day_sonnet", "Current week (Sonnet only)"],
  ["seven_day_opus", "Current week (Opus)"],
  ["seven_day_overage_included", "Current week (Fable)"],
])

// windowTitle is the title of a window's bar; a window without one has its warning label, or its
// name, with a capital first letter.
export function windowTitle(window: string): string {
  const title = WINDOW_TITLES.get(window) ?? WARNING_LABELS.get(window) ?? window
  return title.charAt(0).toUpperCase() + title.slice(1)
}

// shownWindows are the windows in the order of WINDOW_TITLES, then the others by name.
export function shownWindows(windows: PlanWindow[]): PlanWindow[] {
  const order = [...WINDOW_TITLES.keys()]
  const rank = (w: PlanWindow) => {
    const i = order.indexOf(w.window)
    return i === -1 ? order.length : i
  }
  return windows.toSorted((a, b) => rank(a) - rank(b) || a.window.localeCompare(b.window))
}

// windowPercent is the whole percent of the window used: its utilization, 100 when its limit is
// reached, or null when Claude Code did not say.
export function windowPercent(w: PlanWindow): number | null {
  if (w.utilization !== undefined) return usedPercent(w.utilization)
  return w.status === "rejected" ? 100 : null
}

// usedLine says how much of the window is used: "78% used".
export function usedLine(w: PlanWindow): string {
  const percent = windowPercent(w)
  return percent === null ? "Usage not reported" : `${percent}% used`
}

// resetLine says when the window resets, with the time zone: "Resets Nov 20 at 3am
// (Europe/Riga)"; null without a reset time.
export function resetLine(w: PlanWindow, timeZone?: string): string | null {
  if (!w.resetsAt) return null
  const zone = timeZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone
  return `Resets ${resetTime(w.resetsAt, timeZone)} (${zone})`
}

// asOfLine says when Claude Code reported the window: "As of 14:05, from the last bot turn".
export function asOfLine(w: PlanWindow, zone: Zone = {}): string {
  return `As of ${clock(w.receivedAt, zone)}, from the last bot turn`
}

// Period is the time back from now that the spend covers.
export type Period = "24h" | "7d"

export const PERIODS: { id: Period; label: string }[] = [
  { id: "24h", label: "Last 24h" },
  { id: "7d", label: "Last 7d" },
]

// BotShare is a bot's whole percent of the spend of all bots, and its helpers.
export type BotShare = { botId: string; percent: number; helpers: number }

// botShares gives each bot its share of the cost of all bots, the largest first. Each percent is
// its exact share rounded down or up, and the shares sum to 100: the rest goes to the bots with
// the largest remainders. With no cost, every share is 0.
export function botShares(spend: BotSpend[]): BotShare[] {
  const sorted = spend.toSorted((a, b) => b.costUsd - a.costUsd || a.botId.localeCompare(b.botId))
  const total = sorted.reduce((sum, s) => sum + s.costUsd, 0)
  if (total <= 0) return sorted.map((s) => ({ botId: s.botId, percent: 0, helpers: s.helpers }))
  const exact = sorted.map((s) => (s.costUsd / total) * 100)
  const percents = exact.map(Math.floor)
  const rest = 100 - percents.reduce((sum, p) => sum + p, 0)
  // toSorted is stable, so of equal remainders the bot with the larger cost comes first.
  const byRemainder = exact.map((_, i) => i).toSorted((a, b) => exact[b] - percents[b] - (exact[a] - percents[a]))
  for (const i of byRemainder.slice(0, rest)) percents[i]++
  return sorted.map((s, i) => ({ botId: s.botId, percent: percents[i], helpers: s.helpers }))
}

// helperRunsText counts a bot's helper runs: "1 helper run", "3 helper runs".
export function helperRunsText(helpers: number): string {
  return helpers === 1 ? "1 helper run" : `${helpers} helper runs`
}
