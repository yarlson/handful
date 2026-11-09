// Handoffs between teams: how the UI words them. The server keeps every state.

// MAX_ASK_ROUNDS is how often a receiving lead may ask the sender before the user decides.
export const MAX_ASK_ROUNDS = 3

export type HandoffGoal = {
  state: string
  origin?: string
  fromTeamId?: string
  intakeNote?: string
  askedAt?: number
  askRounds?: number
  needsUser?: boolean
  queuePos: number
  used: number
  budget: number
}

// isHandoff tells whether a goal came from another team or the user's hand-over.
export function isHandoff(g: { origin?: string }): boolean {
  return g.origin === "handoff"
}

// fromLabel is the chip on a handed-over goal: who handed it over.
export function fromLabel(g: { origin?: string; fromTeamId?: string }, name: (id: string) => string): string | null {
  if (!isHandoff(g)) return null
  return g.fromTeamId ? `From ${name(g.fromTeamId)}` : "From you"
}

// incomingState names an incoming handoff's state on the receiving board.
export function incomingState(g: HandoffGoal): string {
  if (g.needsUser) return "Needs you"
  const rounds = g.askRounds ?? 0
  if ((g.askedAt ?? 0) > 0) return `Asked the sender (${rounds} of ${MAX_ASK_ROUNDS})`
  if (rounds > 0) return `Answered (${rounds} of ${MAX_ASK_ROUNDS} asked)`
  return "New"
}

// handedOverState names a handoff's state on the sending board, with its place or turns.
export function handedOverState(g: HandoffGoal, receiver: string): string {
  if (g.state === "incoming") {
    if (g.needsUser) return `Needs you at ${receiver}`
    if ((g.askedAt ?? 0) > 0) return `Asked you (${g.askRounds ?? 0} of ${MAX_ASK_ROUNDS})`
    return `Waiting for ${receiver}'s lead`
  }
  if (g.state === "queued") return `Queued #${g.queuePos}`
  if (g.state === "active") return `Active · ${g.used}/${g.budget} turns, paid by ${receiver}`
  if (g.state === "paused") return `Paused · ${g.used}/${g.budget} turns, paid by ${receiver}`
  if (g.state === "delivered") return "Delivered"
  if (g.state === "done") return "Done"
  if (g.state === "declined") return "Declined"
  if (g.state === "ended") return "Ended"
  return g.state
}

// canWithdrawHandoff tells whether the sender can still take a handoff back.
export function canWithdrawHandoff(g: { state: string }): boolean {
  return g.state === "incoming" || g.state === "queued"
}

// awaitsSender tells whether the receiving lead waits for the sender's answer.
export function awaitsSender(g: HandoffGoal): boolean {
  return (g.askedAt ?? 0) > 0 && !g.needsUser
}

// firstLine is the first non-empty line of a brief, for a card.
export function firstLine(text: string | undefined): string {
  return (
    (text ?? "")
      .split("\n")
      .find((l) => l.trim() !== "")
      ?.trim() ?? ""
  )
}

// teamOption names a team in the hand-over dialog with what waits for it.
export function teamOption(t: { name: string; incomingHandoffs?: number }): string {
  const n = t.incomingHandoffs ?? 0
  if (n === 0) return t.name
  return `${t.name} (${n} ${n === 1 ? "handoff" : "handoffs"} waiting)`
}

// handoffError checks a hand-over before it is sent, or returns null when it's fine.
export function handoffError(to: string, text: string): string | null {
  if (!to) return "Choose the team to hand it to."
  if (!text.trim()) return "Write what the team should do."
  if (text.length > 4000) return "The goal can be at most 4000 characters; put the rest in the brief."
  return null
}
