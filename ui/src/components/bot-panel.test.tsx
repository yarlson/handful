import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Bot, Routine } from "@/lib/api"
import { failed, saved } from "@/lib/save"
import { AllowedTools, MemoryFact, RoutineForm, RoutineItem, SaveNote, SettingsTab } from "./bot-panel.tsx"

test("a save that worked shows as a plain note", () => {
  const html = renderToStaticMarkup(<SaveNote result={saved()} />)
  assert.include(html, "Saved.")
  assert.include(html, 'data-save="saved"')
  assert.include(html, "text-muted-foreground")
  assert.notInclude(html, "text-destructive")
})

test("a failed save shows as an error", () => {
  const html = renderToStaticMarkup(<SaveNote result={failed("name is taken")} />)
  assert.include(html, "name is taken")
  assert.include(html, 'data-save="failed"')
  assert.include(html, "text-destructive")
  assert.notInclude(html, "text-muted-foreground")
})

test("nothing shows before the first save", () => {
  assert.equal(renderToStaticMarkup(<SaveNote result={null} />), "")
})

const agent: Bot = {
  id: "bot-ana",
  kind: "agent",
  name: "Ana",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  hue: "violet",
}
const settings = (bot: Bot) => renderToStaticMarkup(<SettingsTab bot={bot} version={0} onDeleted={() => {}} />)
// swatches returns the hue of each colour swatch and whether it is picked.
const swatches = (html: string) =>
  [...html.matchAll(/<button type="button" role="radio" aria-checked="(true|false)"[^>]*value="(\w+)"/g)].map((m) => [
    m[2],
    m[1] === "true",
  ])

test("a bot's settings show the five colours with its stored one picked", () => {
  const html = settings(agent)
  assert.include(html, '<span id="bot-hue-label" class="mb-1 text-sm font-medium">Colour</span>')
  assert.match(html, /<div role="radiogroup"[^>]*aria-labelledby="bot-hue-label"/)
  assert.deepEqual(swatches(html), [
    ["sky", false],
    ["emerald", false],
    ["violet", true],
    ["fuchsia", false],
    ["lime", false],
  ])
  assert.include(html, "text-violet-700 dark:text-violet-400")
})

test("a bot's settings show the full path of its Claude Code folder", () => {
  const html = settings({ ...agent, configDir: "/Users/ana/.handful/bots/bot-ana/claude" })
  assert.include(html, "Claude Code folder")
  assert.include(
    html,
    '<code class="font-mono text-xs [overflow-wrap:anywhere]">/Users/ana/.handful/bots/bot-ana/claude</code>',
  )
  assert.include(html, ">Copy path</button>")
})

test("settings without a Claude Code folder, as a room's, show none", () => {
  assert.notInclude(settings(agent), "Claude Code folder")
})

test("a room's settings have no colour to pick", () => {
  assert.deepEqual(swatches(settings({ ...agent, kind: "room", hue: undefined })), [])
})

test("a team's settings have one Save for every field, off while nothing changed", () => {
  const team: Bot = {
    ...agent,
    id: "team-dev",
    kind: "room",
    hue: undefined,
    members: [{ id: "bot-ana", name: "Ana" }],
    team: { leadId: "bot-ana", agreement: "", goalBudget: 0 },
  }
  const html = settings(team)
  assert.include(html, 'id="team-settings-agreement"')
  const saves = [...html.matchAll(/<button[^>]*>Save[^<]*<\/button>/g)].map((m) => m[0])
  assert.lengthOf(saves, 1)
  assert.include(saves[0], "disabled")
})

test("a fact in the log tier shows the tier's own name", () => {
  const fact = {
    id: "m1",
    scope: "agent",
    tier: "log",
    fact: "Ships on Fridays",
    learnedBy: "bot",
    createdAt: 1,
  } as const
  const html = renderToStaticMarkup(<MemoryFact fact={fact} when={() => "today"} onForget={() => {}} />)
  assert.match(html, />log<\/span>/)
  assert.notInclude(html, "journal")
})

test("a bot with no allowed tools shows None", () => {
  const html = renderToStaticMarkup(<AllowedTools grants={[]} onRevoke={() => {}} />)
  assert.include(html, "None. Each command and file change asks first.")
})

test("allowed tools show each tool with its own Revoke", () => {
  const html = renderToStaticMarkup(<AllowedTools grants={["Bash(go test:*)"]} onRevoke={() => {}} />)
  assert.include(html, 'aria-label="Revoke Bash(go test:*)"')
  assert.notInclude(html, "None.")
})

const fact = {
  id: "m1",
  scope: "agent",
  tier: "profile",
  fact: "Ships on Fridays",
  learnedBy: "bot",
  createdAt: 1,
} as const

test("Forget opens a confirm instead of forgetting at once", () => {
  const html = renderToStaticMarkup(<MemoryFact fact={fact} when={() => "today"} onForget={() => {}} />)
  const forget = html.match(/<button[^>]*aria-label="Forget"[^>]*>/)?.[0] ?? ""
  assert.include(forget, 'aria-haspopup="dialog"')
})

test("Forget shows on keyboard focus, on touch screens and while its confirm is open", () => {
  const html = renderToStaticMarkup(<MemoryFact fact={fact} when={() => "today"} onForget={() => {}} />)
  const forget = html.match(/<button[^>]*aria-label="Forget"[^>]*>/)?.[0] ?? ""
  assert.include(forget, "focus-visible:opacity-100")
  assert.include(forget, "pointer-coarse:opacity-100")
  assert.include(forget, "data-[state=open]:opacity-100")
})

const routine: Routine = {
  id: "r1",
  botId: "bot-ana",
  name: "Brief",
  prompt: "Brief me",
  schedule: "17 9 * * 1-5",
  enabled: true,
  nextRunAt: 1,
  lastRunAt: null,
  lastRunBy: "",
  createdAt: 1,
}

const item = (r: Routine) =>
  renderToStaticMarkup(
    <RoutineItem
      routine={r}
      when={() => "today"}
      onEnabled={() => {}}
      onRun={() => {}}
      onEdit={() => {}}
      onDelete={() => {}}
    />,
  )

test("Delete routine opens a confirm instead of deleting at once", () => {
  const remove = item(routine).match(/<button[^>]*aria-label="Delete routine: Brief"[^>]*>/)?.[0] ?? ""
  assert.include(remove, 'aria-haspopup="dialog"')
})

test("a routine shows its schedule in words, its next run and its actions", () => {
  const html = item(routine)
  assert.include(html, ">Weekdays at 09:17</span>")
  assert.notInclude(html, "<code")
  assert.include(html, "next today")
  assert.include(html, "Run now")
  assert.include(html, "Edit")
})

test("each routine's controls carry the routine's name, so rows tell apart", () => {
  const html = item(routine)
  for (const label of ["Enabled: Brief", "Run now: Brief", "Edit: Brief", "Delete routine: Brief"])
    assert.include(html, `aria-label="${label}"`)
})

test("a routine whose schedule has no words shows the cron line", () => {
  assert.match(item({ ...routine, schedule: "0 9,17 * * *" }), /<code[^>]*>0 9,17 \* \* \*<\/code>/)
})

test("a paused routine shows Paused instead of its next run", () => {
  const html = item({ ...routine, enabled: false })
  assert.include(html, "Paused")
  assert.notInclude(html, "next today")
})

test("the routine form shows the routine and its schedule in words", () => {
  const html = renderToStaticMarkup(<RoutineForm routine={routine} onSave={async () => {}} onCancel={() => {}} />)
  assert.include(html, 'value="Brief"')
  assert.include(html, ">Brief me</textarea>")
  assert.include(html, 'value="17 9 * * 1-5"')
  assert.include(html, "Weekdays at 09:17")
})

test("a new routine's form explains the schedule forms and cannot be saved empty", () => {
  const html = renderToStaticMarkup(<RoutineForm onSave={async () => {}} onCancel={() => {}} />)
  assert.include(html, "@every 2h")
  assert.match(html, /<button[^>]*type="submit"[^>]*disabled=""[^>]*>Save<\/button>/)
})
