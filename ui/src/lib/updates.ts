// The update pill: what it says in each state. The desktop app (window.handfulUpdater)
// owns the update state machine; a source build learns of new releases from
// GET /api/version. This file only words the state.

export type RunningBot = { botId: string; name: string; kind: "turn" | "task"; since?: number | string }

export type UpdaterStateName =
  | "hidden"
  | "checking"
  | "available"
  | "downloading"
  | "ready"
  | "waiting"
  | "limit"
  | "restarting"
  | "error"
  | "source"

export type UpdaterState = {
  state: UpdaterStateName
  version?: string
  current: string
  percent?: number
  transferred?: number
  total?: number
  running?: RunningBot[]
  deadline?: string
  drainMinutes: number
  error?: "offline" | "checksum" | "signature" | "disk" | "other"
  errorText?: string
  releaseUrl: string
  hiddenByLater: boolean
}

export type ServerVersion = {
  version: string
  sourceBuild: boolean
  latest?: string
  releaseUrl?: string
  draining: boolean
  drainingSince?: number
  running: RunningBot[]
}

// shown tells whether the pill appears for a state.
export function shown(s: Pick<UpdaterState, "state" | "hiddenByLater">): boolean {
  return s.state !== "hidden" && s.state !== "checking" && !s.hiddenByLater
}

export type Tone = "accent" | "solid" | "neutral" | "warning"

// tone is the pill's look: only ready (and limit) is solid, an error is amber, never red.
export function tone(state: UpdaterStateName): Tone {
  if (state === "available") return "accent"
  if (state === "ready" || state === "limit") return "solid"
  if (state === "error") return "warning"
  return "neutral"
}

function bots(n: number): string {
  return n === 1 ? "1 bot" : `${n} bots`
}

// minutesLeft is the whole minutes until a deadline, never below 0.
export function minutesLeft(deadline: string | undefined, now: number): number {
  // A missing deadline reads as "undefined", which parses to NaN.
  const ms = Date.parse(String(deadline)) - now
  return Math.max(0, Math.ceil(ms / 60_000)) || 0
}

// label is the pill's full text; short is its text in a narrow column.
export function label(s: UpdaterState, now: number): { full: string; short: string } {
  const v = s.version ?? ""
  const pct = `${Math.round(s.percent ?? 0)}%`
  const left = `${minutesLeft(s.deadline, now)} min`
  switch (s.state) {
    case "available":
      return { full: `Update available: ${v}`, short: v }
    case "downloading":
      return { full: `Downloading ${v}… ${pct}`, short: pct }
    case "ready":
    case "limit":
      return { full: `Restart to update: ${v}`, short: `Restart: ${v}` }
    case "waiting":
      return { full: `Updating after ${bots(s.running?.length ?? 0)} finish · ${left}`, short: left }
    case "restarting":
      return { full: "Restarting…", short: "Restarting…" }
    case "error":
      return { full: "Update failed", short: "Update failed" }
    case "source":
      return { full: `${v} is out`, short: `${v} is out` }
    default:
      return { full: "", short: "" }
  }
}

// ariaLabel is the pill's whole state for screen readers.
export function ariaLabel(s: UpdaterState, now: number): string {
  if (s.state === "restarting") return "Restarting Handful to finish the update."
  return `${label(s, now).full}. Open update options.`
}

// announcement is what a screen reader hears once when the state changes, not on each
// progress tick.
export function announcement(s: UpdaterState): string {
  const v = s.version ?? ""
  switch (s.state) {
    case "available":
      return `Handful ${v} is available.`
    case "downloading":
      return `Downloading Handful ${v}.`
    case "ready":
      return `Update ${v} is ready to install.`
    case "waiting":
      return `Handful updates to ${v} when the running bots finish.`
    case "limit":
      return `Bots are still working after ${s.drainMinutes} minutes. Choose whether to restart.`
    case "restarting":
      return "Restarting Handful."
    case "error":
      return "The update failed."
    case "source":
      return `Handful ${v} is out.`
    default:
      return ""
  }
}

// errorReason words why an update failed.
export function errorReason(s: Pick<UpdaterState, "error" | "errorText">): string {
  if (s.errorText) return s.errorText
  if (s.error === "offline") return "No network. Check your connection."
  if (s.error === "checksum") return "Couldn't verify the update: the download didn't match the release."
  if (s.error === "signature") return "Couldn't verify the update's signature."
  if (s.error === "disk") return "Not enough space to update."
  return "Download failed."
}

function toMs(since: number | string | undefined): number | null {
  // A missing or unreadable time parses to NaN.
  const ms = typeof since === "number" ? since : Date.parse(String(since))
  return Number.isNaN(ms) ? null : ms
}

// doing says what a running bot does: its turn and for how long, or its background task,
// which only the time limit bounds.
export function doing(b: RunningBot, now: number): string {
  if (b.kind === "task") return "background task"
  const ms = toMs(b.since)
  if (ms === null) return "turn"
  return `turn · ${Math.max(0, Math.floor((now - ms) / 60_000))} min`
}

// drainFraction is how much of the time limit is left, from 1 to 0, for the waiting bar.
export function drainFraction(deadline: string | undefined, drainMinutes: number, now: number): number {
  if (drainMinutes <= 0) return 0
  return Math.min(1, minutesLeft(deadline, now) / drainMinutes)
}

// sourceState turns a source build's /api/version into the pill's state, or hidden when
// no newer release is known or the user skipped it.
export function sourceState(v: ServerVersion, skipped: string | null): UpdaterState {
  const base: UpdaterState = {
    state: "hidden",
    current: v.version,
    drainMinutes: 30,
    releaseUrl: v.releaseUrl ?? "https://github.com/yarlson/handful/releases/latest",
    hiddenByLater: false,
  }
  if (!v.sourceBuild || !v.latest || v.latest === skipped) return base
  return { ...base, state: "source", version: v.latest }
}

// LATER_MS is how long Later hides a source build's pill.
export const LATER_MS = 24 * 60 * 60 * 1000

// laterActive tells whether a Later click at time at (0 for none) still hides the pill.
export function laterActive(at: number, now: number): boolean {
  return now - at < LATER_MS
}
