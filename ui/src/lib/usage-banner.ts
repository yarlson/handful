// The banner that says new turns wait for the Claude usage limit, or warns that a usage window of
// the plan is close to its limit, and whether the user closed it. Close stores what it closed in
// this browser, so the banner comes back only with another message: another end time of the
// pause, or another threshold or reset time of the window.

import { loadStored, saveStored } from "./layout"

const CLOSED_KEY = "handful.usageBannerClosed"

// usageMessage is the banner's text for a pause that ends at pausedUntil, an RFC 3339 time.
export function usageMessage(pausedUntil: string): string {
  const until = new Date(pausedUntil).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })
  return `Claude usage limit reached. New turns wait until ${until}.`
}

// closedUsageMessage is the message that the user closed last in this browser.
export function closedUsageMessage(): string | null {
  return loadStored(CLOSED_KEY)
}

export function closeUsageBanner(message: string) {
  saveStored(CLOSED_KEY, message)
}

// usageBannerShown tells whether the banner shows message, given the message the user closed.
export function usageBannerShown(message: string | null, closed: string | null): message is string {
  return message !== null && message !== closed
}

// PlanWindow is the latest state of a usage window of the Claude plan, as Claude Code reported it.
export type PlanWindow = {
  window: string
  status: string
  utilization?: number // the fraction of the limit used
  resetsAt?: number // Unix seconds
  surpassedThreshold?: number // the fraction of the limit whose warning fired
  receivedAt: number // ms
}

// WARNING_LABELS names the windows that a warning can be about, as Claude Code names them.
export const WARNING_LABELS = new Map([
  ["five_hour", "5-hour limit"],
  ["seven_day", "weekly limit"],
  ["seven_day_opus", "Opus limit"],
  ["seven_day_sonnet", "Sonnet limit"],
  ["seven_day_overage_included", "Fable limit"],
  ["overage", "usage credit limit"],
])

// Claude Code shows no warning for a window used less than this.
const WARNING_FLOOR = 0.7

const WARNINGS_CLOSED_KEY = "handful.planWarningsClosed"

// mergeWindows is current with each window of incoming that is not older than the one it has.
export function mergeWindows(current: PlanWindow[], incoming: PlanWindow[]): PlanWindow[] {
  const byName = new Map(current.map((w) => [w.window, w]))
  for (const w of incoming) {
    const had = byName.get(w.window)
    if (!had || had.receivedAt <= w.receivedAt) byName.set(w.window, w)
  }
  return [...byName.values()]
}

// usedPercent is the whole percent of a utilization, at most 100: 0.785 is 78.
export function usedPercent(utilization: number): number {
  return Math.floor(Math.min(Math.round(utilization * 1000) / 10, 100))
}

// thresholdPercent is the whole percent of a threshold between 0 and 1, at most 99; null outside.
function thresholdPercent(threshold: number | undefined): number | null {
  if (threshold === undefined || threshold <= 0 || threshold >= 1) return null
  return Math.min(99, Math.floor(Math.round(threshold * 10000) / 100))
}

// resetTime is a reset time in Unix seconds as "Nov 20 at 3am", or "Nov 20 at 3:30am", in
// timeZone; undefined is the browser's.
export function resetTime(resetsAt: number, timeZone?: string): string {
  const parts = new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
    timeZone,
  }).formatToParts(new Date(resetsAt * 1000))
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((p) => p.type === type)!.value
  const minute = part("minute") === "00" ? "" : `:${part("minute")}`
  return `${part("month")} ${part("day")} at ${part("hour")}${minute}${part("dayPeriod").toLowerCase()}`
}

// warningText is the warning about window w, which has a label, in the words Claude Code uses.
export function warningText(w: PlanWindow, timeZone?: string): string {
  const label = WARNING_LABELS.get(w.window)
  const reset = w.resetsAt ? ` · resets ${resetTime(w.resetsAt, timeZone)}` : ""
  if (w.utilization !== undefined) return `You've used ${usedPercent(w.utilization)}% of your ${label}${reset}`
  const threshold = thresholdPercent(w.surpassedThreshold)
  if (threshold !== null) return `Over ${threshold}% of your ${label} used${reset}`
  return `Approaching ${label}${reset}`
}

// warningMark is what a closed warning about w keeps: it shows again when either part changes.
function warningMark(w: PlanWindow): string {
  return `${w.surpassedThreshold}|${w.resetsAt}`
}

// planWarning is the window that the banner warns about, or null: of the windows with a label
// whose status is allowed_warning, used from WARNING_FLOOR or with no utilization, not closed in
// this browser and not reset by now (ms), the most used one; without utilization, its threshold counts.
export function planWarning(windows: PlanWindow[], closed: Record<string, string>, now: number): PlanWindow | null {
  const warnings = windows.filter(
    (w) =>
      w.status === "allowed_warning" &&
      WARNING_LABELS.has(w.window) &&
      (w.utilization === undefined || w.utilization >= WARNING_FLOOR) &&
      !(w.resetsAt && w.resetsAt * 1000 <= now) &&
      closed[w.window] !== warningMark(w),
  )
  warnings.sort((a, b) => (b.utilization ?? b.surpassedThreshold ?? 0) - (a.utilization ?? a.surpassedThreshold ?? 0))
  return warnings[0] ?? null
}

// closedPlanWarnings is, by window, the warning that the user closed last in this browser.
export function closedPlanWarnings(): Record<string, string> {
  try {
    // Stryker disable next-line StringLiteral: "" fails to parse, and the catch returns the same {}
    const closed: unknown = JSON.parse(loadStored(WARNINGS_CLOSED_KEY) ?? "{}")
    return typeof closed === "object" && closed !== null ? (closed as Record<string, string>) : {}
  } catch {
    return {}
  }
}

export function closePlanWarning(w: PlanWindow) {
  saveStored(WARNINGS_CLOSED_KEY, JSON.stringify({ ...closedPlanWarnings(), [w.window]: warningMark(w) }))
}
