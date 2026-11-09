import { artifactsPath, type ArtifactFilter, type ArtifactKind } from "./artifacts"
import { errorMessage } from "./errors"
import type { ServerVersion } from "./updates"
import type { Intake, Slots } from "./queue"
import type { ItemEvent, OpenQuestion, QuestionState, Waiting } from "./questions"
import type { Defaults, Effective } from "./model"

export type Member = { id: string; name: string; effective?: Effective }

// TeamSettings is what a team adds to its room. leadId is the current lead.
export type TeamSettings = {
  leadId: string
  agreement: string
  goalBudget: number
  defaultModel?: string
  defaultEffort?: string
  requestSlots?: number
  startHandoffs?: "auto" | "ask" // what an accepted handoff does when the team is free // requests of other teams worked on at once beside its own goal
}

export type Bot = {
  id: string
  kind: "agent" | "room"
  members?: Member[]
  team?: TeamSettings
  attention?: "approval" | "question" | "goal" | "model"
  name: string
  instructions: string
  model?: string // "" inherits
  effort?: string // "" inherits
  effective?: Effective // its values in its direct chat; absent on rooms
  openQuestions?: number
  incomingHandoffs?: number // on a team: handoffs that wait for it // its chat's open questions
  createdAt: number
  updatedAt: number
  running: boolean
  tasks: BackgroundTask[]
}

// A background shell, monitor or subagent that a bot runs between turns.
export type BackgroundTask = { id: string; description: string; bot?: string; botId?: string }

// reactions are the user's emoji on a message, in the order of REACTIONS.
export type UserBody = { text: string; media?: string[]; reactions?: string[]; answers?: string[] }
export type BotBody = {
  text: string
  media?: string[]
  fallback: boolean
  needsReply?: boolean
  question?: string // the one-line question, when it has one
  questionState?: QuestionState
  answeredBy?: string
  closedAt?: number
  speakerId?: string
  speakerName?: string
  reactions?: string[]
  model?: string // what the turn that wrote it asked for
  modelId?: string // what Claude Code reported running
  effort?: string
}
export type AgentBody = {
  direction: "in" | "out"
  peerId: string
  peerName: string
  text: string
  priority: boolean
  held?: boolean
  reactions?: string[]
}
export type EventBody = { type: string; text?: string; from?: string; to?: string; botId?: string }
export type ActivityBody = {
  bot?: string
  botId?: string
  tool: string
  summary: string
  status: "running" | "done" | "error"
  subagent: boolean
  output?: string
}
export type ApprovalBody = {
  bot?: string
  botId?: string
  tool: string
  input: unknown
  status: "pending" | "allowed" | "denied" | "expired"
  decision?: Decision
}
export type Decision = "once" | "always" | "deny"

type EntryBase = { id: string; botId: string; seq: number; rev: number; turnId: string; createdAt: number }
export type Entry =
  | (EntryBase & { kind: "user"; body: UserBody })
  | (EntryBase & { kind: "bot"; body: BotBody })
  | (EntryBase & { kind: "event"; body: EventBody })
  | (EntryBase & { kind: "activity"; body: ActivityBody })
  | (EntryBase & { kind: "approval"; body: ApprovalBody })
  | (EntryBase & { kind: "agent"; body: AgentBody })

export type Memory = {
  id: string
  scope: "agent" | "user" | "team"
  tier: "profile" | "log" | "note"
  fact: string
  learnedBy: string
  createdAt: number
}

export type Routine = {
  id: string
  botId: string
  name: string
  prompt: string
  schedule: string
  enabled: boolean
  nextRunAt: number | null
  lastRunAt: number | null
  lastRunBy: string
  createdAt: number
}

export type Artifact = {
  id: string
  kind: ArtifactKind
  title: string
  content?: string // only when the artifact is read by id
  authorId: string // a bot id, or "user"
  tags: string[]
  links: string[]
  createdAt: number
  updatedAt: number
}

export type Skill = { id: string; name: string; description: string; body?: string }

export type Goal = {
  id: string
  teamId: string
  text: string
  state: "active" | "delivered" | "paused" | "done" | "ended" | "queued" | "incoming" | "declined"
  budget: number
  used: number
  queuePos: number // 1-based place in the queue; 0 unless queued
  createdAt: number
  // A goal that another team, or the user, handed over to this team.
  origin?: "user" | "handoff"
  fromTeamId?: string
  fromBotId?: string
  fromGoalId?: string
  brief?: string
  artifactIds?: string[]
  intakeNote?: string // the receiver's latest question, or why it declined
  askedAt?: number
  askRounds?: number
  depth?: number
  needsUser?: boolean
}

export type Item = {
  id: string
  teamId: string
  goalId: string
  title: string
  brief: string
  acceptance: string
  state: "todo" | "doing" | "review" | "done" | "blocked" | "dropped"
  ownerId: string
  reviewerId: string
  dependsOn: string[]
  result: string
  note: string
  reviewRounds: number
  createdAt?: number
  updatedAt: number
  // A request to another team carries its intake: how the serving team took it.
  intake?: Intake
  intakeNote?: string
  deferRevisit?: "goal_end" | "date" | "manual"
  deferUntil?: number
  slotAt?: number // >0 while it holds one of the serving team's slots
  jumpedAt?: number
  // The change request the owner works on: the latest, while no submission came after it.
  latestChange?: { round: number; by: string; at: number; text: string }
  eventCount?: number
  historyFrom?: number // set when the item is older than its history
}

// Request is an item that another team's goal asked a team to do, with its place and the
// line that says why it waits.
export type Request = Item & {
  fromTeamId: string
  position?: number
  total?: number
  reason?: { code: string; text: string; waitsOn?: string }
}

// Board is a team's open goal, or its latest one, with the goal's items, and
// the requests of other teams to it. An item of the goal with another teamId
// is a request that the team asked of another team. teams names those teams.
export type Board = {
  goal: Goal | null
  items: Item[]
  requests: Request[]
  queue: Goal[] // queued goals, in order
  slots: Slots
  asked: Request[] // what this team asked of other teams
  incoming?: Goal[] // handoffs that wait for this team, oldest first
  handedOver?: Goal[] // this team's handoffs of the last 30 days, newest first
  teams: Record<string, string>
}

// Connector is an external MCP server. The API never returns its tokens or headers.
export type Connector = {
  name: string
  url: string
  bots: string[]
  status: "static" | "connected" | "needs-sign-in" | "expired"
}

export type ServerEvent =
  | { type: "entry"; botId: string; entry: Entry }
  | { type: "state"; botId: string; running: boolean }
  | { type: "tasks"; botId: string; tasks: BackgroundTask[] }
  | { type: "bots" }
  | { type: "pause"; pausedUntil?: string }
  | { type: "drain"; draining: boolean }

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) {
    let message = `${method} ${path} failed with ${res.status}`
    try {
      const data = (await res.json()) as { error?: string }
      if (data.error) message = data.error
    } catch {
      // The body is not JSON; keep the status message.
    }
    throw new Error(errorMessage(message))
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  listBots: () => request<Bot[]>("GET", "/api/bots"),
  version: () => request<ServerVersion>("GET", "/api/version"),
  reorderBots: (ids: string[]) => request<void>("PUT", "/api/bots/order", { ids }),
  createBot: (name: string, instructions: string, values: { model?: string; effort?: string } = {}) =>
    request<Bot>("POST", "/api/bots", { name, instructions, ...values }),
  defaults: () => request<Defaults>("GET", "/api/defaults"),
  createRoom: (name: string, instructions: string, memberIds: string[]) =>
    request<Bot>("POST", "/api/rooms", { name, instructions, memberIds }),
  setRoomMembers: (id: string, memberIds: string[]) => request<Bot>("PUT", `/api/rooms/${id}/members`, { memberIds }),
  createTeam: (team: { name: string; instructions: string; memberIds: string[]; leadId: string; agreement: string }) =>
    request<Bot>("POST", "/api/teams", team),
  updateTeam: (id: string, patch: Partial<TeamSettings>) => request<Bot>("PATCH", `/api/teams/${id}`, patch),
  board: (id: string) => request<Board>("GET", `/api/teams/${id}/board`),
  startGoal: (id: string, text: string) => request<Goal>("POST", `/api/teams/${id}/goals`, { text }),
  extendGoal: (id: string, turns: number) => request<Goal>("POST", `/api/goals/${id}/extend`, { turns }),
  endGoal: (id: string, startNext = true) => request<void>("POST", `/api/goals/${id}/end`, { startNext }),
  acceptGoal: (id: string, startNext = true) => request<Goal>("POST", `/api/goals/${id}/accept`, { startNext }),
  cancelGoal: (id: string) => request<void>("POST", `/api/goals/${id}/cancel`, {}),
  reorderGoals: (teamId: string, order: string[]) =>
    request<void>("POST", `/api/teams/${teamId}/goal-queue`, { order }),
  handOver: (toTeamId: string, h: { fromTeamId?: string; text: string; brief?: string; artifactIds?: string[] }) =>
    request<Goal>("POST", `/api/teams/${toTeamId}/handoffs`, h),
  acceptHandoff: (goalId: string) => request<Goal>("POST", `/api/goals/${goalId}/accept-handoff`, {}),
  answerHandoff: (goalId: string, text: string) =>
    request<Goal>("POST", `/api/goals/${goalId}/answer-handoff`, { text }),
  declineHandoff: (goalId: string, text: string) =>
    request<Goal>("POST", `/api/goals/${goalId}/decline-handoff`, { text }),
  withdrawHandoff: (goalId: string) => request<Goal>("POST", `/api/goals/${goalId}/withdraw-handoff`, {}),
  startNextGoal: (teamId: string) => request<Goal>("POST", `/api/teams/${teamId}/goal-queue/start`, {}),
  withdrawItem: (id: string) => request<Item>("POST", `/api/items/${id}/withdraw`, {}),
  undeferItem: (id: string) => request<Item>("POST", `/api/items/${id}/undefer`, {}),
  updateBot: (id: string, patch: { name?: string; instructions?: string; model?: string; effort?: string }) =>
    request<Bot>("PATCH", `/api/bots/${id}`, patch),
  deleteBot: (id: string) => request<void>("DELETE", `/api/bots/${id}`),
  entries: (id: string, afterRev: number) =>
    request<{ entries: Entry[] }>("GET", `/api/bots/${id}/entries?after=${afterRev}`).then((r) => r.entries),
  // media are base64-encoded file contents.
  send: (id: string, text: string, media: string[] = [], answers: string[] = []) =>
    request<{ turnId: string }>("POST", `/api/bots/${id}/messages`, {
      text,
      media,
      clientNonce: crypto.randomUUID(),
      ...(answers.length > 0 && { answers }),
    }),
  itemEvents: (id: string) => request<ItemEvent[]>("GET", `/api/items/${id}/events`),
  waiting: () => request<Waiting>("GET", "/api/questions"),
  chatQuestions: (id: string) => request<OpenQuestion[]>("GET", `/api/bots/${id}/questions`),
  dismissQuestion: (entryId: string) => request<Entry>("POST", `/api/entries/${entryId}/dismiss`, {}),
  reopenQuestion: (entryId: string) => request<Entry>("POST", `/api/entries/${entryId}/reopen`, {}),
  mediaUrl: (name: string) => `/api/media/${encodeURIComponent(name)}`,
  interrupt: (id: string) => request<{ stopped: boolean }>("POST", `/api/bots/${id}/interrupt`, {}),
  clearContext: (id: string) => request<void>("POST", `/api/bots/${id}/clear`, {}),
  compactContext: (id: string) => request<{ queued: number }>("POST", `/api/bots/${id}/compact`, {}),
  memory: (id: string) => request<Memory[]>("GET", `/api/bots/${id}/memory`),
  addMemory: (id: string, fact: { scope: Memory["scope"]; tier: Memory["tier"]; fact: string }) =>
    request<void>("POST", `/api/bots/${id}/memory`, fact),
  deleteMemory: (id: string) => request<void>("DELETE", `/api/memory/${id}`),
  routines: (id: string) => request<Routine[]>("GET", `/api/bots/${id}/routines`),
  setRoutineEnabled: (id: string, enabled: boolean) => request<Routine>("PATCH", `/api/routines/${id}`, { enabled }),
  runRoutine: (id: string) => request<{ turnId: string }>("POST", `/api/routines/${id}/run`, {}),
  deleteRoutine: (id: string) => request<void>("DELETE", `/api/routines/${id}`),
  skills: () => request<Skill[]>("GET", "/api/skills"),
  skill: (id: string) => request<Skill>("GET", `/api/skills/${id}`),
  deleteSkill: (id: string) => request<void>("DELETE", `/api/skills/${id}`),
  artifacts: (filter: ArtifactFilter) => request<Artifact[]>("GET", artifactsPath(filter)),
  artifact: (id: string) => request<Artifact>("GET", `/api/artifacts/${encodeURIComponent(id)}`),
  deleteArtifact: (id: string) => request<void>("DELETE", `/api/artifacts/${encodeURIComponent(id)}`),
  grants: (id: string) => request<string[]>("GET", `/api/bots/${id}/grants`),
  revokeGrant: (id: string, tool: string) =>
    request<void>("DELETE", `/api/bots/${id}/grants/${encodeURIComponent(tool)}`),
  connectors: () => request<Connector[]>("GET", "/api/connectors"),
  connect: (name: string) => request<{ url: string }>("POST", `/api/connectors/${name}/connect`, {}),
  disconnect: (name: string) => request<void>("DELETE", `/api/connectors/${name}/token`),
  resolveApproval: (entryId: string, decision: Decision) =>
    request<void>("POST", `/api/approvals/${entryId}`, { decision }),
  react: (entryId: string, emoji: string, on: boolean) =>
    request<Entry>("POST", `/api/entries/${entryId}/reactions`, { emoji, on }),
}
