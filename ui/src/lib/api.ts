import { artifactsPath, type ArtifactFilter, type ArtifactKind } from "./artifacts"
import { errorMessage } from "./errors"
import type { ServerVersion } from "./updates"
import type { Key, Plain, SettingsView } from "./settings"
import type { ItemEvent, OpenQuestion, QuestionState, Waiting } from "./questions"
import type { Budget } from "./tasks"
import type { Defaults, Effective } from "./model"

export type Member = { id: string; name: string; effective?: Effective }

// TeamSettings is what a team adds to its room. leadId is the current lead.
export type TeamSettings = {
  leadId: string
  agreement: string
  goalBudget: number
  defaultModel?: string
  defaultEffort?: string
  // The team's inbox holds at most inboxMax tasks, and inboxPerSender from one sending team.
  inboxMax?: number
  inboxPerSender?: number
  decisionMinutes?: number // how long a task may wait in the inbox for the lead's decision
  dailyBudget?: number // turns the team may start in a day; 0 is no limit
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
  incomingHandoffs?: number // on a team: the tasks in its inbox that wait for its lead
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

type EntryBase = {
  id: string
  botId: string
  seq: number
  rev: number
  turnId: string
  createdAt: number
}
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
  // incoming and later are in the inbox; queued is planned; active and paused are Now;
  // delivered is done and waits for approval; done is approved; declined is pushed back.
  state: "active" | "delivered" | "paused" | "done" | "ended" | "queued" | "incoming" | "later" | "declined" | "stopped"
  budget: number
  used: number
  queuePos: number // 1-based place in the queue; 0 unless queued
  createdAt: number
  // Where the task came from: the user, another team (or the user for it), or a member.
  origin?: "user" | "handoff" | "proposal"
  fromTeamId?: string
  fromBotId?: string
  fromGoalId?: string
  brief?: string
  artifactIds?: string[]
  intakeNote?: string // the lead's latest question, deferral note or push-back reason
  askedAt?: number // >0 while a question waits for the sender's answer
  askRounds?: number
  depth?: number
  needsUser?: boolean // only the user decides on it now
  missedAt?: number // ms when the lead's decision time ran out
  decisionMs?: number // decision time counted so far
  pinned?: boolean
  // dismissedAt is when the sender put this sent task away; it then shows under Earlier.
  dismissedAt?: number
  laterUntil?: number // ms when a deferred task returns to the inbox
  passes?: number
  passedBy?: string // "Passed to Dev by Product Lead"
  waitsOnItemId?: string // the sender's step that waits on it
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
  // The change request the owner works on: the latest, while no submission came after it.
  latestChange?: { round: number; by: string; at: number; text: string }
  eventCount?: number
  historyFrom?: number // set when the item is older than its history
}

// Board is a team's Now task, or its latest one, with the task's steps, the team's other
// tasks by state, and the names of the teams they came from or went to.
export type Board = {
  goal: Goal | null
  items: Item[]
  handedOver?: Goal[] // the tasks this team sent in the last 30 days, newest first
  inbox?: Goal[] // tasks that wait for the lead's decision, oldest first
  plan?: Goal[] // taken tasks, in order
  done?: Goal[] // closed tasks that wait for approval
  stopped?: Goal[] // stopped tasks, newest first; reopening returns one to the inbox
  offTask?: OffTask // the team's turns on no task in the last hour
  budget?: Budget // the team's turns today against its daily limit
  teams: Record<string, string>
}

// NeedsYouItem is one thing only the user can settle: a result to approve, a decision a
// lead missed, a task asked about or passed on too often, or an open question.
export type NeedsYouItem = {
  kind: "approve" | "missed" | "stuck" | "question" | "budget_warning" | "budget_reached"
  chatId: string
  goalId?: string
  entryId: string
  text: string
  lead?: string
  reason?: string
  at: number
}

// OffTaskTurn is a team's turn that was on no task: who ran it, when, and its first line.
export type OffTaskTurn = { turnId: string; botId: string; botName: string; startedAt: number; firstLine: string }
export type OffTask = { count: number; turns: OffTaskTurn[] }

// NewTask is a task the user sends a team; startNow puts it at the top of the plan.
export type NewTask = { text: string; brief?: string; artifactIds?: string[]; startNow?: boolean }

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
  | { type: "restart"; state: "waiting" | "cancelled" | "expired" | "restarting" }

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

// Session is what the server knows of this browser: phone is true for a paired phone.
export type Session = { phone: boolean }

export const api = {
  listBots: () => request<Bot[]>("GET", "/api/bots"),
  version: () => request<ServerVersion>("GET", "/api/version"),
  // session says whether this browser is a paired phone.
  session: () => request<Session>("GET", "/api/session"),
  settings: () => request<SettingsView>("GET", "/api/settings"),
  // The welcome notice after the data moved to ~/.handful shows until the user closes it.
  moveNotice: () => request<{ show: boolean }>("GET", "/api/move-notice"),
  closeMoveNotice: () => request<void>("POST", "/api/move-notice/close", {}),
  // A null value removes the key from config.json, so its default applies.
  patchSettings: (patch: Partial<Record<Key, Plain | null>>) => request<SettingsView>("PATCH", "/api/settings", patch),
  resetSettingsFile: () => request<SettingsView>("PUT", "/api/settings/file", {}),
  restart: (when: "now" | "idle") => request<ServerVersion>("POST", "/api/restart", { when }),
  // Cancels a waiting settings restart; the updater's own wait stays.
  cancelRestart: () => request<unknown>("DELETE", "/api/restart"),
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
  extendGoal: (id: string, turns: number) => request<Goal>("POST", `/api/goals/${id}/extend`, { turns }),
  startNextGoal: (teamId: string) => request<Goal>("POST", `/api/teams/${teamId}/goal-queue/start`, {}),
  handOver: (toTeamId: string, h: { fromTeamId?: string; text: string; brief?: string; artifactIds?: string[] }) =>
    request<Goal>("POST", `/api/teams/${toTeamId}/handoffs`, h),
  userTask: (teamId: string, t: NewTask) => request<Goal>("POST", `/api/teams/${teamId}/tasks`, t),
  orderPlan: (teamId: string, order: string[]) => request<void>("POST", `/api/teams/${teamId}/plan`, { order }),
  // position is the place in the plan, from 1; 0 is the end.
  takeTask: (id: string, position = 0) => request<Goal>("POST", `/api/tasks/${id}/take`, { position }),
  pushBack: (id: string, text: string) => request<Goal>("POST", `/api/tasks/${id}/push-back`, { text }),
  answerTask: (id: string, text: string) => request<Goal>("POST", `/api/tasks/${id}/answer`, { text }),
  pinTask: (id: string, pinned: boolean) => request<Goal>(pinned ? "POST" : "DELETE", `/api/tasks/${id}/pin`, {}),
  stopTask: (id: string) => request<Goal>("POST", `/api/tasks/${id}/stop`, {}),
  reopenTask: (id: string) => request<Goal>("POST", `/api/tasks/${id}/reopen`, {}),
  withdrawTask: (id: string) => request<Goal>("POST", `/api/tasks/${id}/withdraw`, {}),
  // dismissSent puts away a task the team sent that the other team pushed back.
  dismissSent: (id: string) => request<Goal>("POST", `/api/tasks/${id}/dismiss`, {}),
  // dismissOffTask puts away the team's turns on no task up to now; later ones count again.
  dismissOffTask: (teamId: string) => request<unknown>("POST", `/api/teams/${teamId}/off-task/dismiss`, {}),
  approveTask: (id: string) => request<Goal>("POST", `/api/tasks/${id}/approve`, {}),
  changesTask: (id: string, text: string) => request<Goal>("POST", `/api/tasks/${id}/changes`, { text }),
  // A task's history: the decisions on it and its steps' events, oldest first.
  taskEvents: (id: string) => request<ItemEvent[]>("GET", `/api/tasks/${id}/events`),
  needsYou: () => request<NeedsYouItem[]>("GET", "/api/needs-you"),
  // makeTask makes a task of turns on no task; the team takes it at once.
  makeTask: (teamId: string, t: { text: string; brief?: string; turnIds: string[] }) =>
    request<Goal>("POST", `/api/teams/${teamId}/tasks/from-turns`, t),
  // waitForBudget hides a team's budget_reached item until midnight.
  waitForBudget: (teamId: string) => request<Budget>("POST", `/api/teams/${teamId}/budget/wait`, {}),
  updateBot: (id: string, patch: { name?: string; instructions?: string; model?: string; effort?: string }) =>
    request<Bot>("PATCH", `/api/bots/${id}`, patch),
  deleteBot: (id: string) => request<void>("DELETE", `/api/bots/${id}`),
  entries: (id: string, afterRev: number) =>
    request<{ entries: Entry[] }>("GET", `/api/bots/${id}/entries?after=${afterRev}`).then((r) => r.entries),
  // entryWindow returns a chat's newest limit entries, below seq before when given, in chat order.
  entryWindow: (id: string, limit: number, before?: number) =>
    request<{ entries: Entry[] }>(
      "GET",
      `/api/bots/${id}/entries?limit=${limit}${before === undefined ? "" : `&before=${before}`}`,
    ).then((r) => r.entries),
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
