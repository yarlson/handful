import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Bot } from "@/lib/api"
import type { OpenQuestion } from "@/lib/questions"
import { AnswerChip, QuestionStrip, WaitingView } from "./questions.tsx"

const q = (entryId: string, chatId: string, question: string, createdAt = 1): OpenQuestion => ({
  entryId,
  seq: 1,
  chatId,
  askerId: "b",
  askerName: "Bo",
  question,
  createdAt,
})
const noop = () => {}
const bot = (id: string, name: string, attention?: Bot["attention"]): Bot => ({
  id,
  kind: "agent",
  name,
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  attention,
})

test("the strip is a labelled section whose actions name their question", () => {
  const html = renderToStaticMarkup(
    <QuestionStrip questions={[q("e1", "c", "Ship it?")]} room={false} onAnswer={noop} onChange={noop} />,
  )
  assert.include(html, 'aria-labelledby="open-questions-heading"')
  assert.include(html, "Open questions (1)")
  for (const a of ["Go to", "Answer", "Dismiss"]) assert.include(html, `aria-label="${a}: Ship it?"`)
  assert.notInclude(html, "Bo · ")
})

test("in a room the strip names who asked", () => {
  const html = renderToStaticMarkup(
    <QuestionStrip questions={[q("e1", "c", "Ship it?")]} room onAnswer={noop} onChange={noop} />,
  )
  assert.include(html, "Bo · ")
})

test("with nothing open the strip is hidden, and past three it starts as one line", () => {
  assert.notInclude(
    renderToStaticMarkup(<QuestionStrip questions={[]} room={false} onAnswer={noop} onChange={noop} />),
    "Open questions",
  )
  const many = ["A?", "B?", "C?", "D?"].map((t, i) => q(`e${i}`, "c", t))
  const html = renderToStaticMarkup(<QuestionStrip questions={many} room={false} onAnswer={noop} onChange={noop} />)
  assert.include(html, "Open questions (4)")
  assert.include(html, "Show all 4")
  assert.include(html, 'aria-expanded="false"')
  assert.include(html, "A?")
  assert.notInclude(html, "D?")
})

test("the Answer chip names its question and has a labelled clear button", () => {
  const html = renderToStaticMarkup(<AnswerChip answering={{ entryId: "e1", question: "Ship it?" }} onClear={noop} />)
  assert.include(html, "Answering: Ship it?")
  assert.include(html, 'aria-label="Stop answering: Ship it?"')
})

test("Waiting for you groups questions by chat and links approvals and paused tasks", () => {
  const html = renderToStaticMarkup(
    <WaitingView
      waiting={{
        questions: [q("e2", "y", "Second?", 20), q("e1", "x", "First?", 10)],
        approvals: 1,
        pausedGoals: 1,
        closedRecently: 4,
      }}
      bots={[bot("x", "Alpha"), bot("y", "Beta"), bot("z", "Gamma", "approval"), bot("t", "Team", "goal")]}
      onOpen={noop}
      onAnswer={noop}
      onChange={noop}
    />,
  )
  assert.isBelow(html.indexOf("Alpha"), html.indexOf("Beta"))
  assert.include(html, 'aria-label="Answer: First?"')
  assert.include(html, 'aria-label="Dismiss: Second?"')
  assert.include(html, "Open chat")
  assert.include(html, "Gamma: needs your approval")
  assert.include(html, "Team: the task is paused")
  assert.include(html, "Answered or dismissed in the last 7 days: 4")
  assert.notInclude(html, "Nothing is waiting")
})

test("with nothing waiting the view says so", () => {
  const html = renderToStaticMarkup(
    <WaitingView
      waiting={{ questions: [], approvals: 0, pausedGoals: 0, closedRecently: 0 }}
      bots={[]}
      onOpen={noop}
      onAnswer={noop}
      onChange={noop}
    />,
  )
  assert.include(html, "Nothing is waiting for you.")
})

test("closed questions are counted, and Show is offered only when there are some", () => {
  const view = (closed: OpenQuestion[]) =>
    renderToStaticMarkup(
      <WaitingView
        waiting={{
          questions: [],
          approvals: 0,
          pausedGoals: 0,
          closedRecently: closed.length,
          closed,
        }}
        bots={[bot("x", "Alpha")]}
        onOpen={noop}
        onAnswer={noop}
        onChange={noop}
      />,
    )
  const some = view([{ ...q("e9", "x", "Old one?"), state: "dismissed", closedAt: 5 }])
  assert.include(some, "Answered or dismissed in the last 7 days: 1")
  assert.match(some, /<button[^>]*aria-expanded="false"[^>]*aria-controls="waiting-closed"[^>]*>Show</)
  // The list stays folded until Show is pressed.
  assert.notInclude(some, "Old one?")
  assert.notInclude(view([]), ">Show<")
})
