import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Bot } from "@/lib/api"
import type { PlanWindow } from "@/lib/usage-banner"
import { PlanWindowBars, SpendList } from "./usage-section"

const riga = { locale: "en-GB", timeZone: "Europe/Riga" }

const weekly: PlanWindow = {
  window: "seven_day",
  status: "allowed_warning",
  utilization: 0.78,
  resetsAt: 1795136400, // Nov 20, 3am in Riga
  surpassedThreshold: 0.75,
  receivedAt: Date.UTC(2026, 10, 18, 12, 5), // 14:05 in Riga
}

const bot = (id: string, name: string): Bot => ({
  id,
  kind: "agent",
  name,
  instructions: "",
  effective: { model: "opus", effort: "high", modelSource: "bot", effortSource: "bot" },
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
})

test("a window's bar has its title, the percent used, the reset time with the zone and when it was reported", () => {
  const html = renderToStaticMarkup(<PlanWindowBars windows={[weekly]} zone={riga} />)
  assert.include(html, "Current week (all models)")
  assert.include(html, "78% used")
  assert.include(html, "Resets Nov 20 at 3am (Europe/Riga) · As of 14:05, from the last bot turn")
  assert.include(html, 'style="width:78%"')
})

test("the bars follow the order of their titles, and a window used 29% shows 29%", () => {
  const html = renderToStaticMarkup(
    <PlanWindowBars
      windows={[
        { ...weekly, window: "seven_day_opus", status: "allowed", utilization: 0.29 },
        { ...weekly, window: "five_hour", status: "allowed", utilization: 0.05 },
      ]}
      zone={riga}
    />,
  )
  assert.isBelow(html.indexOf("Current session"), html.indexOf("Current week (Opus)"))
  assert.include(html, "29% used")
})

test("with no window reported, the bars give way to a note", () => {
  assert.include(
    renderToStaticMarkup(<PlanWindowBars windows={[]} />),
    "No plan usage reported yet. It shows after a bot&#x27;s next turn.",
  )
})

test("each bot's row has its share, a link to its Settings, its model and effort and its helper runs", () => {
  const html = renderToStaticMarkup(
    <SpendList
      shares={[
        { botId: "b1", percent: 67, helpers: 1 },
        { botId: "b2", percent: 33, helpers: 0 },
        { botId: "gone", percent: 0, helpers: 2 },
      ]}
      bots={[bot("b2", "Scout"), bot("b1", "Dev")]}
      aliases={["opus"]}
    />,
  )
  assert.match(
    html,
    /67%<\/span><a href="\/chat\/b1\?tab=settings"[^>]*>Dev<\/a><span[^>]*>Opus · high<\/span><span[^>]*>1 helper run</,
  )
  assert.match(html, /33%<\/span><a href="\/chat\/b2\?tab=settings"[^>]*>Scout<\/a>.*0 helper runs/)
  assert.isBelow(html.indexOf("Dev"), html.indexOf("Scout"))
  assert.match(html, /0%<\/span><span[^>]*>A deleted bot<\/span><span[^>]*>2 helper runs/)
})

test("a period with no bot turn says so", () => {
  assert.include(renderToStaticMarkup(<SpendList shares={[]} bots={[]} aliases={[]} />), "No bot turns in this period.")
})
