// Open questions and item history: how the UI words, groups and counts them.
// The server keeps every state; this file only arranges it.

export type QuestionState = "open" | "answered" | "dismissed" | "withdrawn"

export type OpenQuestion = {
  entryId: string
  seq: number
  chatId: string
  askerId: string
  askerName: string
  question: string
  createdAt: number
  // A closed question of the Waiting view says how and when it closed.
  state?: QuestionState
  closedAt?: number
}

export type Waiting = {
  questions: OpenQuestion[]
  approvals: number
  pausedGoals: number
  closedRecently: number
  closed?: OpenQuestion[] // closed in the last 7 days, newest first
}

// stateLabel names a question's state as text, so it never rests on color alone.
export function stateLabel(state: QuestionState | undefined): string | null {
  if (state === "open") return "Open question"
  if (state === "answered") return "Answered"
  if (state === "dismissed") return "Dismissed"
  if (state === "withdrawn") return "No longer needed"
  return null
}

// waitingCount is the number on the "Waiting for you" entry: open questions, chats with a
// pending approval and paused goals.
export function waitingCount(w: Pick<Waiting, "questions" | "approvals" | "pausedGoals">): number {
  return w.questions.length + w.approvals + w.pausedGoals
}

// byChat groups questions by their chat, keeping each group oldest first and ordering the
// groups by their oldest question.
export function byChat(questions: OpenQuestion[]): { chatId: string; questions: OpenQuestion[] }[] {
  const sorted = [...questions].sort((a, b) => a.createdAt - b.createdAt || a.seq - b.seq)
  const groups = new Map<string, OpenQuestion[]>()
  for (const q of sorted) groups.set(q.chatId, [...(groups.get(q.chatId) ?? []), q])
  return [...groups].map(([chatId, qs]) => ({ chatId, questions: qs }))
}

// countBadge is the sidebar's count of a chat's open questions, shown only for more than one.
export function countBadge(open: number | undefined): string | null {
  return (open ?? 0) > 1 ? String(open) : null
}

// stripCollapsed tells whether the open-questions strip starts as one line.
export function stripCollapsed(n: number): boolean {
  return n > 3
}

// UNDO_MS is how long the user can undo a dismissal; the server allows 10 s.
export const UNDO_MS = 10_000

export type ItemEvent = {
  id: string
  itemId: string
  kind: string
  actorId: string
  actorName: string
  round: number
  text: string
  createdAt: number
}

// eventLabel names an item event for the history timeline.
export function eventLabel(e: Pick<ItemEvent, "kind" | "round">): string {
  const labels: Record<string, string> = {
    created: "Created",
    assigned: "Assigned",
    submitted: "Submitted",
    passed: "Passed",
    blocked: "Blocked",
    unblocked: "Unblocked",
    dropped: "Dropped",
    deferred: "Deferred",
    undeferred: "Brought back",
    withdrawn: "Withdrawn",
  }
  if (e.kind === "changes") return `Changes asked · round ${e.round}`
  return labels[e.kind] ?? e.kind
}

// eventTone marks change requests amber and passes green; the label says the same in words.
export function eventTone(kind: string): "changes" | "passed" | "plain" {
  if (kind === "changes") return "changes"
  if (kind === "passed") return "passed"
  return "plain"
}

// actorOf names who made an event: the user when no bot did.
export function actorOf(e: Pick<ItemEvent, "actorId" | "actorName">): string {
  return e.actorName || (e.actorId ? "a former member" : "You")
}

// historyToggle is the text of an item's history button: its change requests when it has
// any, else its history's length.
export function historyToggle(reviewRounds: number, eventCount: number): string {
  if (reviewRounds === 1) return "1 change request"
  if (reviewRounds) return `${reviewRounds} change requests`
  return `History (${eventCount})`
}

// Asked is the question an Answer chip answers: its entry and its line.
export type Asked = { entryId: string; question: string }

const MAX_LINE = 140

// questionOf is the line that names a bot's question: its summary, or the first line of its
// text, cut to 140 characters.
export function questionOf(entryId: string, body: { question?: string; text: string }): Asked {
  const line = body.question?.trim() || (body.text.split("\n").find((l) => l.trim() !== "") ?? "").trim()
  return { entryId, question: line.length > MAX_LINE ? `${line.slice(0, MAX_LINE - 1)}…` : line }
}

// answersFor is the answers field of a send: the chip's question, unless the message is a
// command that sends no message.
export function answersFor(answering: Asked | null, command: boolean): string[] {
  return answering && !command ? [answering.entryId] : []
}

// waitingKey changes whenever what waits for the user may have changed, to reload the view.
export function waitingKey(bots: { id: string; attention?: string; openQuestions?: number }[]): string {
  return bots.map((b) => `${b.id}:${b.attention ?? ""}:${b.openQuestions ?? 0}`).join(",")
}
