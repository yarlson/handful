import { parseGoalCommand } from "./goal"

// What the message box does when the user sends: the one place that turns a draft into a
// request, so a new command changes only this table.
//   skip:    nothing to send (no text, no files); the box stays as it is.
//   invalid: the draft can't be sent; the box shows error and keeps the draft.
//   goal:    "/goal <text>" in a team's chat starts a goal.
//   clear:   "/clear" without files starts a fresh context.
//   compact: "/compact" without files compacts the context.
//   message: anything else is a message, with its files.
export type SendPlan =
  | { kind: "skip" }
  | { kind: "invalid"; error: string }
  | { kind: "goal"; goal: string }
  | { kind: "clear" }
  | { kind: "compact" }
  | { kind: "message"; text: string }

export const EMPTY_GOAL = "Write the goal after /goal, for example: /goal Add CSV export"
export const GOAL_WITH_FILES = "A goal is text only: remove the attachments, or send them in a message"

// COMMANDS are the commands that replace a message when the draft has no files.
const COMMANDS = new Map<string, SendPlan>([
  ["/clear", { kind: "clear" }],
  ["/compact", { kind: "compact" }],
])

// sendPlan decides what sending draft with files attached does in a chat; team says the
// chat is a team's, where /goal is a command.
export function sendPlan(draft: string, files: number, team: boolean): SendPlan {
  const text = draft.trim()
  if (!text && files === 0) return { kind: "skip" }
  const goal = team ? parseGoalCommand(text) : null
  if (goal === "") return { kind: "invalid", error: EMPTY_GOAL }
  if (goal !== null && files > 0) return { kind: "invalid", error: GOAL_WITH_FILES }
  if (goal !== null) return { kind: "goal", goal }
  const command = files === 0 ? COMMANDS.get(text) : undefined
  return command ?? { kind: "message", text }
}

// answers tells whether a sent plan answers the question of the Answer chip: only a
// message does, so the chip stays after a command.
export function answers(plan: SendPlan): boolean {
  return plan.kind === "message"
}
