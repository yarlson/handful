// The goal queue and the request lane of a team's board: how the UI groups,
// orders and words them. The server decides every state and writes each
// request's reason line; this file only arranges them.

export type Intake = "" | "requested" | "accepted" | "declined" | "deferred" | "withdrawn"

// RequestLike is the part of a request that grouping reads.
export type RequestLike = {
  intake?: Intake
  state: string
  slotAt?: number
}

export type Group = "progress" | "waiting" | "triage" | "deferred" | "closed"

// GROUPS are the request groups in the order the board shows them, with their headings.
export const GROUPS: { group: Group; label: string }[] = [
  { group: "progress", label: "In progress" },
  { group: "waiting", label: "Waiting for a slot" },
  { group: "triage", label: "Awaiting triage" },
  { group: "deferred", label: "Deferred" },
  { group: "closed", label: "Closed requests" },
]

const over = (r: RequestLike) => r.state === "done" || r.state === "dropped"

// requestGroup places a request in a group of the serving board. Done and dropped requests
// are closed: a declined or withdrawn request is always dropped.
export function requestGroup(r: RequestLike): Group {
  if (r.intake === "deferred") return "deferred"
  if (r.intake === "requested") return "triage"
  if (over(r)) return "closed"
  return (r.slotAt ?? 0) > 0 ? "progress" : "waiting"
}

// grouped splits requests into the board's groups, keeping their order and leaving out
// empty groups.
export function grouped<T extends RequestLike>(requests: T[]): { group: Group; label: string; items: T[] }[] {
  return GROUPS.map((g) => ({ ...g, items: requests.filter((r) => requestGroup(r) === g.group) })).filter(
    (g) => g.items.length > 0,
  )
}

export type Slots = { limit: number; used: number; applies: boolean }

// slotHeader is the heading of the requests section: how many slots are in use and how
// many accepted requests wait for one.
export function slotHeader(slots: Slots, requests: RequestLike[]): string {
  const waiting = requests.filter((r) => requestGroup(r) === "waiting").length
  if (!slots.applies) return "Requests · no slot limit while the team has no active goal"
  const unit = slots.limit === 1 ? "slot" : "slots"
  return `Requests · ${slots.used} of ${slots.limit} ${unit} in use · ${waiting} waiting`
}

// intakeLabel names a request's state for the asking team's board, as text, never color
// alone.
export function intakeLabel(r: RequestLike): string {
  if (r.state === "done") return "done"
  if (r.intake === "requested") return "requested"
  if (r.intake === "deferred") return "deferred"
  if (r.intake === "declined") return "declined"
  if (r.intake === "withdrawn") return "withdrawn"
  if (r.state === "dropped") return "dropped"
  return (r.slotAt ?? 0) > 0 ? "in progress" : "waiting"
}

// canWithdraw tells whether the asking team can still take a request back.
export function canWithdraw(r: RequestLike): boolean {
  return (r.intake === "requested" || r.intake === "accepted" || r.intake === "deferred") && !over(r)
}

// showsPosition tells whether a request's #n position chip means anything: only while it
// waits, for triage or for a slot.
export function showsPosition(r: RequestLike & { position?: number }): boolean {
  const g = requestGroup(r)
  return (g === "triage" || g === "waiting") && (r.position ?? 0) > 0
}

// reasonLine joins a request's reason with what its work waits on.
export function reasonLine(reason: { text: string; waitsOn?: string } | undefined): string {
  if (!reason) return ""
  return reason.waitsOn ? `${reason.text} ${reason.waitsOn}` : reason.text
}

// moved returns ids with the one at index moved by step (-1 up, 1 down), or null when it
// can't move that way.
export function moved(ids: string[], index: number, step: -1 | 1): string[] | null {
  const to = index + step
  if (index < 0 || index >= ids.length || to < 0 || to >= ids.length) return null
  const next = [...ids]
  ;[next[index], next[to]] = [next[to], next[index]]
  return next
}

// placeNote announces where a moved goal is now, for screen readers.
export function placeNote(title: string, position: number, total: number): string {
  return `${title} is now ${position} of ${total}.`
}

// openGoal tells whether a goal state still has work: the team can't start another.
export function openGoal(state: string | undefined): boolean {
  return state === "active" || state === "delivered" || state === "paused"
}

// onHold tells whether a team's queue waits for the user: no open goal and a goal queued.
export function onHold(goalState: string | undefined, queued: number): boolean {
  return queued > 0 && !openGoal(goalState)
}

// nextUp names the head of the queue and how many follow it.
export function nextUp(queue: { text: string }[]): string | null {
  if (queue.length === 0) return null
  const more = queue.length > 1 ? ` (+${queue.length - 1})` : ""
  return `Next up: ${queue[0].text}${more}`
}

// slotsError checks the "Requests in progress at once" setting, or returns null when it's fine.
export function slotsError(value: string, members: number): string | null {
  const n = Number(value)
  // An empty or blank value reads as 0.
  if (!Number.isInteger(n) || n < 1 || n > members) {
    return `Requests in progress at once must be from 1 to ${members}.`
  }
  return null
}
