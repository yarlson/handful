import type { Bot, Entry } from "./api"

// A start is a setup that Builder's first-run chat offers: a click puts its text in the message
// box, for the user to change or send.
export type Start = { label: string; text: string }

export const STARTS: readonly Start[] = [
  {
    label: "Personal assistant",
    text: "Set up a personal assistant: one bot for my day: plans, reminders, research and drafts.",
  },
  {
    label: "Software team",
    text: "Set up a software team: a lead, a developer and a reviewer who change my code and check it with tests.",
  },
  {
    label: "Research desk",
    text: "Set up a research desk: a lead, a researcher and a fact-checker who answer questions with sources.",
  },
  {
    label: "Writing studio",
    text: "Set up a writing studio: an editor and a writer who plan, draft and edit my texts.",
  },
]

export const BUILDER = "builder"

// offersStarts says whether a chat shows the starts: Builder's chat until the user's first message.
export function offersStarts(bot: Pick<Bot, "role">, entries: readonly Entry[]): boolean {
  return bot.role === BUILDER && !entries.some((e) => e.kind === "user")
}
