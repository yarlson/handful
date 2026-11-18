// The Settings tab of a bot, a room or a team: its fields as the user typed them, what
// changed against the saved values, and what one Save sends.

import type { Bot, Member, TeamSettings } from "./api"
import { budgetError, decisionError, inboxError } from "./tasks"

// TeamDraft is a team's own fields; the numbers stay text until Save checks them.
export type TeamDraft = {
  leadId: string
  reviewerId: string
  agreement: string
  budget: string
  decision: string
  inboxMax: string
  perSender: string
  defaultModel: string
  defaultEffort: string
}

// BotDraft is every field of the tab; team is null outside a team.
export type BotDraft = {
  name: string
  instructions: string
  model: string
  effort: string
  hue: string
  team: TeamDraft | null
}

type BotFields = Omit<BotDraft, "team">

export type BotPatch = Partial<BotFields>

// SavePlan is what Save sends: a bot patch, a team patch or both, or the reason it can't.
export type SavePlan = { error: string } | { bot?: BotPatch; team?: Partial<TeamSettings> }

// draftOf is the saved values of a bot as the tab's fields show them.
export function draftOf(bot: Bot): BotDraft {
  const t = bot.team
  return {
    name: bot.name,
    instructions: bot.instructions,
    model: bot.model ?? "",
    effort: bot.effort ?? "",
    hue: bot.hue ?? "",
    team: t
      ? {
          leadId: t.leadId,
          reviewerId: t.reviewerId ?? "",
          agreement: t.agreement,
          budget: String(t.dailyBudget ?? 0),
          decision: String(t.decisionMinutes ?? 30),
          inboxMax: String(t.inboxMax ?? 10),
          perSender: String(t.inboxPerSender ?? 3),
          defaultModel: t.defaultModel ?? "",
          defaultEffort: t.defaultEffort ?? "",
        }
      : null,
  }
}

// reviewerCandidates are the members that the Reviewer select offers after None: all but the lead.
export function reviewerCandidates(members: Member[], leadId: string): Member[] {
  return members.filter((m) => m.id !== leadId)
}

// leadEdit makes leadId the team's lead; a reviewer that becomes the lead leaves the reviewer None.
export function leadEdit(team: TeamDraft, leadId: string): Partial<TeamDraft> {
  return leadId === team.reviewerId ? { leadId, reviewerId: "" } : { leadId }
}

function botFields({ name, instructions, model, effort, hue }: BotDraft): BotFields {
  return { name, instructions, model, effort, hue }
}

function changedKeys<T extends Record<string, string>>(draft: T, saved: T): (keyof T)[] {
  return (Object.keys(draft) as (keyof T)[]).filter((k) => draft[k] !== saved[k])
}

function botChanged(draft: BotDraft, saved: BotDraft): boolean {
  return changedKeys(botFields(draft), botFields(saved)).length > 0
}

function teamChanged(draft: BotDraft, saved: BotDraft): boolean {
  return !!draft.team && !!saved.team && changedKeys(draft.team, saved.team).length > 0
}

// unsaved tells whether the fields differ from the saved values.
export function unsaved(draft: BotDraft, saved: BotDraft): boolean {
  return botChanged(draft, saved) || teamChanged(draft, saved)
}

function merged<T extends Record<string, string>>(draft: T, old: T, fresh: T): T {
  const out = { ...fresh }
  for (const k of changedKeys(draft, old)) out[k] = draft[k]
  return out
}

// rebase is the draft after the saved values changed from old to fresh: the user's edits
// stay, and every field the user did not edit takes its fresh value.
export function rebase(draft: BotDraft, old: BotDraft, fresh: BotDraft): BotDraft {
  return {
    ...merged(botFields(draft), botFields(old), botFields(fresh)),
    team: draft.team && old.team && fresh.team ? merged(draft.team, old.team, fresh.team) : fresh.team,
  }
}

const teamPatch: { [K in keyof TeamDraft]: (value: string) => Partial<TeamSettings> } = {
  leadId: (leadId) => ({ leadId }),
  reviewerId: (reviewerId) => ({ reviewerId }),
  agreement: (agreement) => ({ agreement }),
  budget: (v) => ({ dailyBudget: Number(v) }),
  decision: (v) => ({ decisionMinutes: Number(v) }),
  inboxMax: (v) => ({ inboxMax: Number(v) }),
  perSender: (v) => ({ inboxPerSender: Number(v) }),
  defaultModel: (defaultModel) => ({ defaultModel }),
  defaultEffort: (defaultEffort) => ({ defaultEffort }),
}

// savePlan is what Save sends: only the fields that changed, so that Save does not replace
// what someone else changed meanwhile. A room has no model, effort or colour, and an empty
// colour is never sent.
export function savePlan(draft: BotDraft, saved: BotDraft, room: boolean): SavePlan {
  const t = draft.team
  const teamKeys = t && saved.team ? changedKeys(t, saved.team) : []
  if (t && teamKeys.length) {
    const bad = budgetError(t.budget) ?? decisionError(t.decision) ?? inboxError(t.inboxMax, t.perSender)
    if (bad) return { error: bad }
  }
  const fields = botFields(draft)
  const botKeys = changedKeys(fields, botFields(saved)).filter(
    (k) => (!room || k === "name" || k === "instructions") && (k !== "hue" || fields.hue),
  )
  const bot: BotPatch = Object.fromEntries(botKeys.map((k) => [k, fields[k]]))
  const team: Partial<TeamSettings> = t ? Object.assign({}, ...teamKeys.map((k) => teamPatch[k](t[k]))) : {}
  return { ...(botKeys.length > 0 && { bot }), ...(teamKeys.length > 0 && { team }) }
}
