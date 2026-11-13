// CREATE_SETUP is Builder's tool. Its approval card shows the setup that the
// user creates or cancels; internal/agent/builder.go writes the card's input.
export const CREATE_SETUP = "create_setup"

export type SetupProposal = {
  bots: { name: string; instructions: string; model: string; effort: string }[]
  teams: {
    name: string
    purpose: string
    members: string[]
    lead: string
    agreement: string
    defaultModel: string
    defaultEffort: string
  }[]
  rooms: { name: string; purpose: string; members: string[] }[]
}

type Fields = Record<string, unknown>

const isFields = (v: unknown): v is Fields => typeof v === "object" && v !== null && !Array.isArray(v)
const text = (v: unknown) => (typeof v === "string" ? v : "")
const names = (v: unknown) => (Array.isArray(v) ? v.filter((n): n is string => typeof n === "string") : [])

// list reads an optional list of objects: [] when it is missing, null when it
// is not a list of objects.
function list<T>(v: unknown, read: (f: Fields) => T): T[] | null {
  if (v === undefined) return []
  if (!Array.isArray(v) || !v.every(isFields)) return null
  return v.map(read)
}

// setupProposal reads the input of a create_setup card, or returns null when
// the input is not a setup. A team without a lead is led by its first member.
export function setupProposal(input: unknown): SetupProposal | null {
  if (!isFields(input)) return null
  const bots = list(input.bots, (b) => ({
    name: text(b.name),
    instructions: text(b.instructions),
    model: text(b.model),
    effort: text(b.effort),
  }))
  // A task has no turn limit, so a goal_budget that an older card still holds is not shown.
  const teams = list(input.teams, (t) => {
    const members = names(t.members)
    return {
      name: text(t.name),
      purpose: text(t.purpose),
      members,
      lead: text(t.lead) || (members[0] ?? ""),
      agreement: text(t.agreement),
      defaultModel: text(t.default_model),
      defaultEffort: text(t.default_effort),
    }
  })
  const rooms = list(input.rooms, (r) => ({ name: text(r.name), purpose: text(r.purpose), members: names(r.members) }))
  if (!bots || !teams || !rooms) return null
  return { bots, teams, rooms }
}
