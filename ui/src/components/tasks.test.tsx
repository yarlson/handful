import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Board, Bot, Goal, NeedsYouItem } from "@/lib/api"
import {
  AcrossTeams,
  ArtifactPicker,
  InboxView,
  SendTaskDialog,
  SentSection,
  TaskHistory,
  TaskHistoryList,
  type InboxContext,
} from "./tasks.tsx"
import type { ItemEvent } from "@/lib/questions"

const act = async () => true
const names = {
  team: (id: string) => ({ s: "Support", d: "Dev", p: "Product" })[id] ?? "a deleted team",
  member: (id: string) => ({ sl: "Support Lead", m: "Mo" })[id] ?? "a deleted bot",
  at: (ms: number) => `t${ms}`,
}
const goal = (x: Partial<Goal>): Goal => ({
  id: "g1",
  teamId: "d",
  text: "Fix the scroll",
  state: "incoming",
  budget: 30,
  used: 0,
  queuePos: 0,
  createdAt: 0,
  origin: "handoff",
  fromTeamId: "s",
  fromBotId: "sl",
  ...x,
})
const team: Bot = {
  id: "d",
  kind: "room",
  name: "Dev",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  team: { leadId: "a", agreement: "", goalBudget: 30, decisionMinutes: 30 },
  members: [{ id: "a", name: "Dee" }],
}
const board = (b: Partial<Board>): Board => ({
  goal: null,
  items: [],
  requests: [],
  queue: [],
  slots: { limit: 1, used: 0, applies: false },
  asked: [],
  teams: {},
  ...b,
})
const view = (x: Partial<InboxContext> = {}): InboxContext => ({
  team,
  lead: "Dee",
  minutes: 30,
  planned: 1,
  stop: null,
  loadedAt: 0,
  names,
  act,
  ...x,
})
const inbox = (goals: Goal[], x: Partial<InboxContext> = {}) =>
  renderToStaticMarkup(<InboxView board={board({ inbox: goals })} view={view(x)} error={null} onBack={() => {}} />)

test("the inbox is a list of headed cards with who sent each task and the lead's decision clock", () => {
  const html = inbox([goal({ brief: "\nThe chat jumps.\nMore", artifactIds: ["a1"], waitsOnItemId: "i9" })])
  assert.include(html, "Dev · Inbox")
  assert.include(html, "Dee decides what the team takes. 1 waiting.")
  assert.include(html, 'aria-label="Dev&#x27;s inbox"')
  assert.match(html, /<li aria-labelledby="inbox-g1"/)
  assert.include(html, '<h3 id="inbox-g1" class="font-medium">Fix the scroll</h3>')
  assert.include(html, "from Support")
  assert.include(html, "Support Lead · ")
  assert.include(html, "Support&#x27;s task waits on this")
  assert.match(html, /decide by [^<]+/)
  assert.include(html, "The chat jumps.")
  assert.notInclude(html, "More")
  assert.include(html, "1 artifact attached")
})

test("the lead decides on a task in the inbox: the user gets no Take, no Push back and no Accept", () => {
  const html = inbox([goal({})])
  for (const label of ["Take", "Push back", "Accept", "Withdraw", "Answer"])
    assert.notInclude(html, `aria-label="${label}: Fix the scroll"`, label)
})

test("a task its lead couldn't settle needs the user, who takes it at a place in the plan or pushes it back", () => {
  const html = inbox([goal({ needsUser: true, missedAt: 5 })], { planned: 2 })
  assert.include(html, "needs you: Dee didn&#x27;t decide within 30 min")
  assert.include(html, 'aria-label="Take: Fix the scroll"')
  assert.include(html, 'aria-label="Place in the plan: Fix the scroll"')
  assert.include(html, '<option value="1">#1 in the plan</option>')
  assert.include(html, '<option value="3" selected="">#3 in the plan</option>')
  assert.include(html, 'aria-label="Push back: Fix the scroll"')
})

test("an asked task shows the question, waits for the answer, and the user answers what the user sent", () => {
  const theirs = inbox([goal({ askedAt: 1, askRounds: 1, intakeNote: "Which build?" })])
  assert.include(theirs, "Dee asked:</strong> Which build?")
  assert.include(theirs, "Waiting for Support&#x27;s answer. The decision clock is paused.")
  assert.include(theirs, "asked Support · round 1 of 3 · clock paused")
  assert.notInclude(theirs, 'aria-label="Answer: Fix the scroll"')
  const mine = inbox([
    goal({
      origin: "user",
      fromTeamId: undefined,
      fromBotId: undefined,
      askedAt: 1,
      intakeNote: "Which build?",
    }),
  ])
  assert.include(mine, "from you")
  assert.include(mine, "Waiting for your answer.")
  assert.include(mine, 'aria-label="Answer: Fix the scroll"')
  assert.include(mine, 'aria-label="Withdraw: Fix the scroll"')
})

test("the clock says why it stands, and a deferred task says until when", () => {
  assert.include(inbox([goal({})], { stop: "usage limit" }), "clock paused: usage limit")
  const later = inbox([goal({ state: "later", laterUntil: 9, intakeNote: "After the release." })])
  assert.include(later, "later, until ")
  assert.include(later, "Dee&#x27;s note:</strong> After the release.")
})

test("an empty inbox says who decides", () => {
  assert.include(inbox([]), "The inbox is empty.")
  assert.include(inbox([]), "and Dee decides on each.")
})

test("no sent tasks hide the section", () => {
  assert.equal(renderToStaticMarkup(<SentSection goals={[]} names={names} act={act} />), "")
})

test("the sender follows each task it sent, answers questions and withdraws those that haven't started", () => {
  const html = renderToStaticMarkup(
    <SentSection
      goals={[
        goal({ id: "h1", text: "Asked one", askedAt: 1, askRounds: 1, intakeNote: "Which build?" }),
        goal({
          id: "h2",
          text: "Planned one",
          state: "queued",
          queuePos: 2,
          passedBy: "Passed to Dev by Sam",
        }),
        goal({ id: "h3", text: "Active one", state: "active", used: 4 }),
        goal({ id: "h4", text: "Declined one", state: "declined", intakeNote: "Out of scope." }),
      ]}
      names={names}
      act={act}
    />,
  )
  assert.include(html, 'aria-labelledby="sent-heading"')
  assert.include(html, "Sent to other teams (4)")
  assert.include(html, "Dev asked")
  assert.include(html, "Dev asked:</strong> Which build?")
  assert.include(html, "#2 in Dev&#x27;s plan")
  assert.include(html, "To Dev · Passed to Dev by Sam")
  assert.include(html, "Dev is on it")
  assert.include(html, "To Dev · 4 turns, paid by Dev")
  assert.include(html, "Pushed back")
  assert.include(html, "Dev:</strong> Out of scope.")
  assert.include(html, 'aria-label="Answer: Asked one"')
  assert.include(html, 'aria-label="Withdraw: Asked one"')
  assert.include(html, 'aria-label="Withdraw: Planned one"')
  assert.notInclude(html, 'aria-label="Withdraw: Active one"')
  assert.notInclude(html, 'aria-label="Withdraw: Declined one"')
})

const need = (x: Partial<NeedsYouItem>): NeedsYouItem => ({
  kind: "approve",
  chatId: "p",
  goalId: "g1",
  entryId: "e1",
  text: "Spec it",
  at: 0,
  ...x,
})

test("Across teams lists what needs the user with the buttons that settle it, and each team's work", () => {
  const html = renderToStaticMarkup(
    <AcrossTeams
      needs={[
        need({}),
        need({ kind: "missed", chatId: "d", goalId: "g2", text: "Retry button", lead: "Dee" }),
        need({
          kind: "stuck",
          chatId: "d",
          goalId: "g3",
          text: "Export",
          reason: "passed on 3 times",
        }),
        need({
          kind: "question",
          chatId: "s",
          goalId: undefined,
          entryId: "e4",
          text: "Which build?",
        }),
      ]}
      boards={
        new Map([
          [
            "d",
            board({
              goal: goal({ state: "active", text: "Build Settings" }),
              plan: [goal({ id: "q1" }), goal({ id: "q2" })],
              inbox: [goal({ id: "i1" })],
            }),
          ],
          ["s", board({ goal: goal({ state: "paused", text: "Answer issues" }) })],
        ])
      }
      teams={[team, { ...team, id: "s", name: "Support" }, { ...team, id: "p", name: "Product" }]}
      error={null}
      act={act}
      onOpen={() => {}}
      onNewTask={() => {}}
      chatName={(id) => names.team(id)}
    />,
  )
  assert.include(html, "Needs you (4)")
  assert.include(html, "<strong>Approve:</strong> “Spec it” is done (Product)")
  assert.include(html, 'aria-label="Approve: Spec it"')
  assert.include(html, 'aria-label="Ask for changes: Spec it"')
  assert.include(html, "<strong>Lead didn&#x27;t decide in time:</strong> Dee left “Retry button” in Dev&#x27;s inbox")
  assert.include(html, 'aria-label="Take: Retry button"')
  assert.include(html, 'aria-label="Push back: Retry button"')
  assert.include(html, "“Export” was passed on 3 times (Dev)")
  assert.include(html, 'aria-label="Take: Export"')
  assert.include(html, "<strong>Question:</strong> Which build? (Support)")
  assert.notInclude(html, 'aria-label="Take: Which build?"')
  assert.equal(html.split("Open chat").length - 1, 4)
  assert.include(html, "<strong>Now:</strong> Build Settings")
  assert.include(html, "Next 2 · Inbox 1")
  assert.include(html, "<strong>Now:</strong> Answer issues")
  assert.include(html, ">paused<")
  assert.include(html, "New task…")
})

test("Across teams says when nothing needs the user and there are no teams", () => {
  const html = renderToStaticMarkup(
    <AcrossTeams
      needs={[]}
      boards={new Map()}
      teams={[]}
      error={null}
      act={act}
      onOpen={() => {}}
      onNewTask={() => {}}
      chatName={() => ""}
    />,
  )
  assert.include(html, "Nothing needs you.")
  assert.include(html, "No teams yet.")
  assert.match(html, /<button[^>]*disabled=""[^>]*>New task…/)
})

test("a closed send dialog renders nothing", () => {
  const teams: Bot[] = []
  assert.equal(
    renderToStaticMarkup(<SendTaskDialog open={false} onOpenChange={() => {}} from={null} teams={teams} />),
    "",
  )
})

test("a long artifact title wraps inside the send dialog, with its full text in the tooltip", () => {
  const long = "Spec: " + "a very long artifact title that goes on and on ".repeat(5)
  const word = "x".repeat(80)
  const html = renderToStaticMarkup(
    <ArtifactPicker
      artifacts={[
        { id: "a1", title: long },
        { id: "a2", title: word },
      ]}
      picked={[]}
      onPick={() => {}}
    />,
  )
  assert.isAbove(long.length, 200)
  // A fieldset is at least as wide as its content unless told otherwise.
  assert.match(html, /<fieldset class="[^"]*\bmin-w-0\b/)
  assert.match(html, /<label class="[^"]*\bmin-w-0\b/)
  for (const t of [long, word]) {
    assert.include(html, `title="${t}"`)
    assert.include(html, `class="line-clamp-2 min-w-0 flex-1 [overflow-wrap:anywhere]" title="${t}"`)
  }
  assert.equal(renderToStaticMarkup(<ArtifactPicker artifacts={[]} picked={[]} onPick={() => {}} />), "")
})

const event = (x: Partial<ItemEvent>): ItemEvent => ({
  id: "e",
  itemId: "",
  kind: "sent",
  actorId: "",
  actorName: "",
  round: 0,
  text: "",
  createdAt: 0,
  ...x,
})

test("a task's history lists every event in order, with who, when and its text, under a heading", () => {
  const html = renderToStaticMarkup(
    <TaskHistoryList
      id="task-history-g1"
      goal={goal({})}
      steps={[{ id: "i1", title: "Settings API" }]}
      events={[
        event({ id: "e1", goalId: "g1", kind: "sent", actorId: "sl", actorName: "Support Lead", createdAt: 1 }),
        event({
          id: "e2",
          goalId: "g1",
          kind: "asked",
          actorId: "a",
          actorName: "Dee",
          text: "Which build?",
          createdAt: 2,
        }),
        event({ id: "e3", goalId: "g1", kind: "answered", text: "macOS 15.", createdAt: 3 }),
        event({
          id: "e4",
          itemId: "i1",
          kind: "changes",
          round: 1,
          actorName: "Rae",
          text: "Add a test.",
          createdAt: 4,
        }),
        event({ id: "e5", itemId: "i9", kind: "passed", actorId: "x", createdAt: 5 }),
        event({ id: "e6", goalId: "g1", kind: "stopped", createdAt: 6 }),
      ]}
    />,
  )
  assert.match(html, /<section id="task-history-g1" aria-labelledby="task-history-g1-heading"/)
  assert.include(html, '<h4 id="task-history-g1-heading" class="font-medium">History of Fix the scroll</h4>')
  const order = [
    "Sent",
    "Asked the sender",
    "Answered",
    "Settings API: Changes asked · round 1",
    "A step: Passed",
    "Stopped",
  ]
  const at = order.map((label) => html.indexOf(`>${label}</span>`))
  for (const [i, label] of order.entries()) assert.isAbove(at[i], -1, label)
  assert.deepEqual(
    [...at].sort((a, b) => a - b),
    at,
  )
  assert.include(html, "· Support Lead · ")
  assert.include(html, "· Dee · ")
  assert.include(html, "· You · ")
  assert.include(html, "· a former member · ")
  assert.include(html, "Which build?")
  assert.include(html, "macOS 15.")
  assert.include(html, "Add a test.")
  assert.include(html, "bg-amber-500/10")
})

test("an empty history says so, and the history opens from a named button", () => {
  assert.include(
    renderToStaticMarkup(<TaskHistoryList id="h" goal={goal({})} events={[]} />),
    "Nothing has happened to it yet.",
  )
  const button = renderToStaticMarkup(<TaskHistory goal={goal({})} />)
  assert.include(button, 'aria-label="History: Fix the scroll"')
  assert.include(button, 'aria-expanded="false"')
  assert.include(button, 'aria-controls="task-history-g1"')
})

test("inbox and sent cards open each task's history", () => {
  assert.include(inbox([goal({})]), 'aria-label="History: Fix the scroll"')
  assert.include(
    renderToStaticMarkup(<SentSection goals={[goal({ state: "queued" })]} names={names} act={act} />),
    'aria-label="History: Fix the scroll"',
  )
})
