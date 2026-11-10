// A team's tasks: how the UI words their states, the lead's decision clock and what needs
// the user. The server decides every state; this file only names and arranges them.

import { eventLabel } from "./questions"

// MAX_ROUNDS is how often a lead may ask a task's sender, and how often a task may be passed
// on, before only the user decides on it.
export const MAX_ROUNDS = 3

export type TaskState =
  | "active"
  | "delivered"
  | "paused"
  | "done"
  | "ended"
  | "queued"
  | "incoming"
  | "later"
  | "declined"
  | "stopped"

// TaskLike is the part of a task that the words here read.
export type TaskLike = {
  state: TaskState
  text: string
  used: number
  budget: number
  queuePos: number
  origin?: string
  fromTeamId?: string
  fromBotId?: string
  askedAt?: number
  askRounds?: number
  needsUser?: boolean
  missedAt?: number
  decisionMs?: number
  laterUntil?: number
  passes?: number
}

// Names turns ids into names; at formats a time (ms) for the user.
export type Names = {
  team: (id: string) => string
  member: (id: string) => string
  at: (ms: number) => string
}

// sourceLabel names who a task came from: another team, a member's proposal, or the user.
export function sourceLabel(g: TaskLike, names: Names): string {
  if (g.origin === "proposal") return `proposed by ${names.member(g.fromBotId ?? "")}`
  if (g.origin === "handoff" && g.fromTeamId) return `from ${names.team(g.fromTeamId)}`
  return "from you"
}

// fromUser tells whether the user sent a task, so the user answers its questions.
export function fromUser(g: TaskLike): boolean {
  return g.origin !== "proposal" && !g.fromTeamId
}

// ClockStop is why a lead can't decide right now, so its decision clock stands.
export type ClockStop = "usage limit" | "server restart" | "team stopped" | null

// clockStop says why the decision clocks of a team stand: the runner waits for a usage limit,
// the server drains, or the team's Now task is paused with turns left (the user stopped it).
// A task paused at its budget doesn't stop the clock: decisions still run then.
export function clockStop(
  usageLimit: boolean,
  draining: boolean,
  now: { state: string; used: number; budget: number } | null | undefined,
): ClockStop {
  if (usageLimit) return "usage limit"
  if (draining) return "server restart"
  if (now?.state === "paused" && now.used < now.budget) return "team stopped"
  return null
}

// stuckReason says why only the user decides on a task now, or null when its lead still does.
export function stuckReason(g: TaskLike, lead: string, minutes: number): string | null {
  if (!g.needsUser) return null
  if ((g.missedAt ?? 0) > 0) return `${lead} didn't decide within ${minutes} min`
  if ((g.askRounds ?? 0) > MAX_ROUNDS) return `asked ${MAX_ROUNDS} times`
  return `passed on ${MAX_ROUNDS} times`
}

// clockLine is the decision clock of a task in the inbox, as text: when the lead decides by,
// or why the clock stands. minutes is the team's decision time; loadedAt is when the board
// was read, since decisionMs counts up to then.
export function clockLine(
  g: TaskLike,
  {
    minutes,
    lead,
    sender,
    stop,
    loadedAt,
  }: { minutes: number; lead: string; sender: string; stop: ClockStop; loadedAt: number },
  at: (ms: number) => string,
): string {
  if (g.state === "later") return `later, until ${at(g.laterUntil ?? 0)}`
  const stuck = stuckReason(g, lead, minutes)
  if (stuck) return `needs you: ${stuck}`
  if ((g.askedAt ?? 0) > 0) return `asked ${sender} · round ${g.askRounds ?? 1} of ${MAX_ROUNDS} · clock paused`
  if (stop) return `clock paused: ${stop}`
  const left = minutes * 60_000 - (g.decisionMs ?? 0)
  if (left <= 0) return `${lead} decides now`
  return `decide by ${at(loadedAt + left)}`
}

// Tone is the colour of a state chip. The chip's text always names the state too.
export type Tone = "progress" | "waiting" | "attention" | "over"

// sentState names a task the team sent, as the sending team follows it, with its tone.
export function sentState(g: TaskLike, receiver: string, at: (ms: number) => string): { label: string; tone: Tone } {
  switch (g.state) {
    case "incoming":
      if (g.needsUser) return { label: "Needs you", tone: "attention" }
      if ((g.askedAt ?? 0) > 0) return { label: `${receiver} asked`, tone: "attention" }
      return { label: `In ${receiver}'s inbox`, tone: "waiting" }
    case "later":
      return { label: `Later, until ${at(g.laterUntil ?? 0)}`, tone: "waiting" }
    case "queued":
      return { label: `#${g.queuePos} in ${receiver}'s plan`, tone: "waiting" }
    case "active":
      return { label: `${receiver} is on it`, tone: "progress" }
    case "paused":
      return { label: `Paused at ${receiver}`, tone: "attention" }
    case "delivered":
      return { label: "Done, waiting for approval", tone: "progress" }
    case "done":
      return { label: "Done", tone: "progress" }
    case "declined":
      return { label: "Pushed back", tone: "attention" }
    case "stopped":
      return { label: "Stopped", tone: "over" }
    default:
      return { label: "Withdrawn", tone: "over" }
  }
}

// turnsLine says how many turns a task used, and which team pays for them.
export function turnsLine(used: number, payer: string): string {
  if (used === 0) return ""
  return `${used} ${used === 1 ? "turn" : "turns"}, paid by ${payer}`
}

// canWithdraw tells whether the sender can still take a task back: it hasn't started.
export function canWithdraw(g: { state: string }): boolean {
  return g.state === "incoming" || g.state === "later" || g.state === "queued"
}

// awaitsAnswer tells whether a task's lead waits for its sender's answer.
export function awaitsAnswer(g: TaskLike): boolean {
  return (g.askedAt ?? 0) > 0 && !g.needsUser
}

// TaskAction is a button of the task bar:
//   approve: approve a done task; its team doesn't wait for this.
//   changes: ask for changes to a done task, which comes back as Now.
//   resume:  let a paused task go on with the turns it has left.
//   extend:  add 10 turns to its budget.
//   stop:    stop it after a confirm; running turns finish and its steps stay.
export type TaskAction = "approve" | "changes" | "resume" | "extend" | "stop"

const TASK_ACTIONS: Partial<Record<TaskState, TaskAction[]>> = {
  active: ["extend", "stop"],
  paused: ["resume", "extend", "stop"],
  delivered: ["approve", "changes"],
}

// TaskBarView is what the task bar shows:
//   now:     the Now task, or a done one that waits for approval, with its buttons.
//   stopped: the latest task was stopped; the user can reopen it.
//   hold:    nothing is Now and a task is planned; the user can start it.
//   idle:    nothing is Now or planned; the bar says how to send a task.
export type TaskBarView =
  | { mode: "now"; actions: TaskAction[] }
  | { mode: "stopped" }
  | { mode: "hold" }
  | { mode: "idle" }

// taskBar decides the task bar's view from the team's latest task and the number of
// tasks planned after it.
export function taskBar(
  goal: { state: string; used: number; budget: number } | null | undefined,
  planned: number,
): TaskBarView {
  const actions = goal ? TASK_ACTIONS[goal.state as TaskState] : undefined
  if (goal && actions)
    return {
      mode: "now",
      actions: actions.filter((a) => a !== "resume" || goal.used < goal.budget),
    }
  if (goal?.state === "stopped") return { mode: "stopped" }
  return { mode: planned > 0 ? "hold" : "idle" }
}

// nextUp names the first planned task and how many follow it.
export function nextUp(plan: { text: string }[]): string | null {
  if (plan.length === 0) return null
  const more = plan.length > 1 ? ` (+${plan.length - 1})` : ""
  return `Next: ${plan[0].text}${more}`
}

// NeedsYou is the part of a Needs you item that its line reads.
export type NeedsYou = {
  kind: "approve" | "missed" | "stuck" | "question"
  text: string
  lead?: string
  reason?: string
}

// needsYouLine words a Needs you item: a bold lead-in and the rest. team names the chat.
export function needsYouLine(n: NeedsYou, team: string): { lead: string; rest: string } {
  if (n.kind === "approve") return { lead: "Approve:", rest: `“${n.text}” is done (${team})` }
  if (n.kind === "missed")
    return {
      lead: "Lead didn't decide in time:",
      rest: `${n.lead ?? "The lead"} left “${n.text}” in ${team}'s inbox`,
    }
  if (n.kind === "stuck") return { lead: "Needs a decision:", rest: `“${n.text}” was ${n.reason ?? "stuck"} (${team})` }
  return { lead: "Question:", rest: `${n.text} (${team})` }
}

// TeamSummary is one team's card in Across teams.
export type TeamSummary = {
  now: string | null
  paused: boolean
  next: number
  inbox: number
  sent: number
}

// teamSummary sums up a team's board for Across teams. Only an active or paused task is Now.
export function teamSummary(board: {
  goal: { state: string; text: string } | null
  plan?: unknown[]
  inbox?: unknown[]
  handedOver?: { state: string }[]
}): TeamSummary {
  const g = board.goal
  const now = g && (g.state === "active" || g.state === "paused") ? g : null
  return {
    now: now?.text ?? null,
    paused: now?.state === "paused",
    next: board.plan?.length ?? 0,
    inbox: board.inbox?.length ?? 0,
    sent: (board.handedOver ?? []).filter(open).length,
  }
}

// open tells whether a sent task is still in the other team's inbox or plan, or being done.
function open(g: { state: string }): boolean {
  return g.state !== "done" && g.state !== "ended" && g.state !== "declined" && g.state !== "stopped"
}

// countsLine is the counts line of a team's card.
export function countsLine(s: TeamSummary): string {
  const parts = [`Next ${s.next}`, `Inbox ${s.inbox}`]
  if (s.sent > 0) parts.push(`sent ${s.sent}`)
  return parts.join(" · ")
}

// decisionError checks the decision time setting, in minutes, or returns null when it's fine.
export function decisionError(value: string): string | null {
  const n = Number(value)
  // Blank reads as 0.
  if (!Number.isInteger(n) || n < 5 || n > 1440) return "The decision time must be 5 to 1440 minutes."
  return null
}

// inboxError checks the inbox limits, or returns null when they're fine.
export function inboxError(max: string, perSender: string): string | null {
  const m = Number(max)
  const p = Number(perSender)
  if (!Number.isInteger(m) || m < 1 || m > 50) return "The inbox can hold 1 to 50 tasks."
  if (!Number.isInteger(p) || p < 1 || p > m) return `From one team: 1 to ${m} tasks.`
  return null
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

// teamOption names a team in the send dialog with how many tasks wait in its inbox.
export function teamOption(t: { name: string; incomingHandoffs?: number }): string {
  const n = t.incomingHandoffs ?? 0
  if (n === 0) return t.name
  return `${t.name} (${n} in its inbox)`
}

// taskError checks a task before it is sent, or returns null when it's fine.
export function taskError(to: string, text: string): string | null {
  if (!to) return "Choose the team to send it to."
  if (!text.trim()) return "Write what the team should do."
  if (text.length > 4000) return "The task can be at most 4000 characters; put the rest in the brief."
  return null
}

// tasksKey changes whenever a team's tasks or what needs the user may have changed: a turn
// starts or ends, a question opens, or a task arrives in an inbox.
export function tasksKey(
  bots: {
    id: string
    running: boolean
    attention?: string
    openQuestions?: number
    incomingHandoffs?: number
  }[],
): string {
  return bots
    .map((b) => `${b.id}:${b.running ? 1 : 0}:${b.attention ?? ""}:${b.openQuestions ?? 0}:${b.incomingHandoffs ?? 0}`)
    .join(",")
}

type TeamLike = { members?: { id: string; name: string }[]; team?: { leadId: string } }

// memberName names a member of a team, or says the member left.
export function memberName(team: TeamLike, id: string): string {
  return team.members?.find((m) => m.id === id)?.name ?? "a former member"
}

// leadName names a team's lead.
export function leadName(team: TeamLike): string {
  return team.team?.leadId ? memberName(team, team.team.leadId) : "The lead"
}

const TASK_EVENTS: Record<string, string> = {
  sent: "Sent",
  proposed: "Proposed",
  taken: "Taken",
  ordered: "Moved in the plan",
  pinned: "Pinned",
  unpinned: "Unpinned",
  asked: "Asked the sender",
  answered: "Answered",
  later: "Deferred",
  due: "Back in the inbox",
  passed: "Passed on",
  pushed_back: "Pushed back",
  closed: "Closed",
  approved: "Approved",
  changes: "Changes asked",
  stopped: "Stopped",
  reopened: "Reopened",
  withdrawn: "Withdrawn",
  missed: "Decision missed",
}

// taskEventLabel names an event in a task's history: a decision on the task, or an event of
// one of its steps, led by the step's title. A step event has no goalId; step is its title,
// when the board still lists it.
export function taskEventLabel(e: { kind: string; round: number; goalId?: string }, step?: string): string {
  if (e.goalId) return TASK_EVENTS[e.kind] ?? e.kind
  return `${step ?? "A step"}: ${eventLabel(e)}`
}

// historyKey is the load key of a task's open history: it changes with the task and with each
// load of the board the history shows on, so the history reloads whenever the board does.
export function historyKey(goalId: string, boardRev: number): string {
  return `${goalId}:${boardRev}`
}
