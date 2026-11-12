import { parseTaskCommand } from "./task-command"

// What the message box does when the user sends: the one place that turns a draft into a
// request, so a new command changes only this table.
//   skip:    nothing to send (no text, no files); the box stays as it is.
//   invalid: the draft can't be sent; the box shows error and keeps the draft.
//   task:    "/task <text>" in a team's chat sends its lead a task.
//   clear:   "/clear" without files starts a fresh context.
//   compact: "/compact" without files compacts the context.
//   message: anything else is a message, with its files.
export type SendPlan =
  | { kind: "skip" }
  | { kind: "invalid"; error: string }
  | { kind: "task"; task: string }
  | { kind: "clear" }
  | { kind: "compact" }
  | { kind: "message"; text: string }

export const EMPTY_TASK = "Write the task after /task, for example: /task Add CSV export"
export const TASK_WITH_FILES = "A task is text only: remove the attachments, or send them in a message"

// COMMANDS are the commands that replace a message when the draft has no files.
const COMMANDS = new Map<string, SendPlan>([
  ["/clear", { kind: "clear" }],
  ["/compact", { kind: "compact" }],
])

// sendPlan decides what sending draft with files attached does in a chat; team says the
// chat is a team's, where /task is a command.
export function sendPlan(draft: string, files: number, team: boolean): SendPlan {
  const text = draft.trim()
  if (!text && files === 0) return { kind: "skip" }
  const task = team ? parseTaskCommand(text) : null
  if (task === "") return { kind: "invalid", error: EMPTY_TASK }
  if (task !== null && files > 0) return { kind: "invalid", error: TASK_WITH_FILES }
  if (task !== null) return { kind: "task", task }
  const command = files === 0 ? COMMANDS.get(text) : undefined
  return command ?? { kind: "message", text }
}

// answers tells whether a sent plan answers the question of the Answer chip: only a
// message does, so the chip stays after a command.
export function answers(plan: SendPlan): boolean {
  return plan.kind === "message"
}

// DraftNonce is the clientNonce of a chat's sends. A send of the same draft again, after an
// error, keeps its nonce, so the server stores the message once; a changed draft, or the
// next one after a send went through, gets a new nonce. key names the draft: its text, its
// files and the questions it answers.
export type DraftNonce = { of: (key: string) => string; sent: () => void }

export function draftNonce(make: () => string = () => crypto.randomUUID()): DraftNonce {
  let draft: string | null = null
  let nonce = make()
  return {
    of(key) {
      if (draft !== null && key !== draft) nonce = make()
      draft = key
      return nonce
    },
    sent() {
      draft = null
      nonce = make()
    },
  }
}
