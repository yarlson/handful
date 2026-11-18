import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import { usageMessage, warningText, type PlanWindow } from "@/lib/usage-banner"
import { UsageBanner } from "./usage-banner"

afterEach(() => {
  vi.unstubAllGlobals()
})

const stored = (closed: string | null, closedWarnings: string | null = null) =>
  vi.stubGlobal("localStorage", {
    getItem: (k: string) =>
      ({
        "handful.usageBannerClosed": closed,
        "handful.planWarningsClosed": closedWarnings,
      })[k] ?? null,
  })

const weekly: PlanWindow = {
  window: "seven_day",
  status: "allowed_warning",
  utilization: 0.78,
  resetsAt: 4102444800,
  surpassedThreshold: 0.75,
  receivedAt: 1,
}

test("the usage banner shows its message with a close button until the user closes that message", () => {
  const until = "2026-11-16T10:00:00Z"
  stored(null)
  const html = renderToStaticMarkup(<UsageBanner pausedUntil={until} windows={[]} />)
  assert.include(html, usageMessage(until))
  assert.match(html, /<button[^>]*aria-label="Close the usage limit message"/)
  stored(usageMessage(until))
  assert.equal(renderToStaticMarkup(<UsageBanner pausedUntil={until} windows={[]} />), "")
  assert.include(
    renderToStaticMarkup(<UsageBanner pausedUntil="2026-11-16T12:30:00Z" windows={[]} />),
    "Close the usage limit message",
  )
})

test("no pause, no usage banner", () => {
  stored(null)
  assert.equal(renderToStaticMarkup(<UsageBanner pausedUntil={null} windows={[]} />), "")
})

test("a plan warning shows its text, See usage when the page can open it, and a close button", () => {
  stored(null)
  const html = renderToStaticMarkup(<UsageBanner pausedUntil={null} windows={[weekly]} onSeeUsage={() => {}} />)
  assert.include(html, warningText(weekly).replace("'", "&#x27;"))
  assert.match(html, /<button[^>]*>See usage<\/button>/)
  assert.match(html, /<button[^>]*aria-label="Close the plan usage warning"/)
  const phone = renderToStaticMarkup(<UsageBanner pausedUntil={null} windows={[weekly]} />)
  assert.notInclude(phone, "See usage", "without a way to Settings")
  assert.include(phone, "Close the plan usage warning")
})

test("a closed plan warning stays closed, and a pause shows the usage limit message instead", () => {
  stored(null, JSON.stringify({ seven_day: "0.75|4102444800" }))
  assert.equal(renderToStaticMarkup(<UsageBanner pausedUntil={null} windows={[weekly]} onSeeUsage={() => {}} />), "")
  stored(null)
  const until = "2026-11-16T10:00:00Z"
  const html = renderToStaticMarkup(<UsageBanner pausedUntil={until} windows={[weekly]} onSeeUsage={() => {}} />)
  assert.include(html, usageMessage(until))
  assert.notInclude(html, "See usage")
})
