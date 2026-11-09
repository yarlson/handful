import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Bot, Goal } from "@/lib/api"
import { ArtifactPicker, HandedOverSection, HandOverDialog, IncomingSection } from "./handoff.tsx"

const act = async () => true
const name = (id: string) => ({ s: "Support", d: "Dev" })[id] ?? "a deleted team"
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
  ...x,
})

test("incoming handoffs are a headed section whose buttons name their goal", () => {
  const html = renderToStaticMarkup(
    <IncomingSection
      incoming={[goal({ brief: "\nThe chat jumps.\nMore", artifactIds: ["a1"] })]}
      name={name}
      act={act}
    />,
  )
  assert.include(html, 'aria-labelledby="incoming-heading"')
  assert.include(html, "Incoming from other teams (1)")
  assert.include(html, "From Support")
  assert.include(html, "New")
  assert.include(html, "The chat jumps.")
  assert.notInclude(html, "More")
  assert.include(html, "1 artifact attached")
  assert.include(html, 'aria-label="Accept: Fix the scroll"')
  assert.include(html, 'aria-label="Decline: Fix the scroll"')
  assert.notInclude(html, 'aria-label="Answer: Fix the scroll"')
})

test("a handoff the leads couldn't settle needs the user, who can answer it", () => {
  const html = renderToStaticMarkup(
    <IncomingSection
      incoming={[goal({ needsUser: true, askRounds: 3, askedAt: 1, intakeNote: "Which build?" })]}
      name={name}
      act={act}
    />,
  )
  assert.include(html, "Needs you")
  assert.include(html, "Question: Which build?")
  assert.include(html, 'aria-label="Answer: Fix the scroll"')
})

test("no incoming handoffs hide the section", () => {
  assert.equal(renderToStaticMarkup(<IncomingSection incoming={[]} name={name} act={act} />), "")
  assert.equal(renderToStaticMarkup(<HandedOverSection goals={[]} name={name} act={act} />), "")
})

test("the sender follows each handoff, answers questions and withdraws open ones", () => {
  const html = renderToStaticMarkup(
    <HandedOverSection
      goals={[
        goal({ id: "h1", text: "Asked one", askedAt: 1, askRounds: 1, intakeNote: "Which build?" }),
        goal({ id: "h2", text: "Queued one", state: "queued", queuePos: 2 }),
        goal({ id: "h3", text: "Active one", state: "active", used: 4 }),
        goal({ id: "h4", text: "Declined one", state: "declined", intakeNote: "Out of scope." }),
      ]}
      name={name}
      act={act}
    />,
  )
  assert.include(html, 'aria-labelledby="handed-over-heading"')
  assert.include(html, "Handed over (4)")
  assert.include(html, "Asked you (1 of 3)")
  assert.include(html, "Queued #2")
  assert.include(html, "Active · 4/30 turns, paid by Dev")
  assert.include(html, "Reason: Out of scope.")
  assert.include(html, 'aria-label="Answer: Asked one"')
  assert.include(html, 'aria-label="Withdraw: Asked one"')
  assert.include(html, 'aria-label="Withdraw: Queued one"')
  assert.notInclude(html, 'aria-label="Withdraw: Active one"')
  assert.notInclude(html, 'aria-label="Withdraw: Declined one"')
})

test("a closed hand-over dialog renders nothing", () => {
  const teams: Bot[] = []
  assert.equal(
    renderToStaticMarkup(<HandOverDialog open={false} onOpenChange={() => {}} from={null} teams={teams} />),
    "",
  )
})

test("a long artifact title wraps inside the hand-over dialog, with its full text in the tooltip", () => {
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
