import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import { usageMessage } from "@/lib/usage-banner"
import { UsageBanner } from "./usage-banner"

afterEach(() => {
  vi.unstubAllGlobals()
})

const stored = (closed: string | null) =>
  vi.stubGlobal("localStorage", { getItem: (k: string) => (k === "handful.usageBannerClosed" ? closed : null) })

test("the usage banner shows its message with a close button until the user closes that message", () => {
  const until = "2026-11-16T10:00:00Z"
  stored(null)
  const html = renderToStaticMarkup(<UsageBanner pausedUntil={until} />)
  assert.include(html, usageMessage(until))
  assert.match(html, /<button[^>]*aria-label="Close the usage limit message"/)
  stored(usageMessage(until))
  assert.equal(renderToStaticMarkup(<UsageBanner pausedUntil={until} />), "")
  assert.include(
    renderToStaticMarkup(<UsageBanner pausedUntil="2026-11-16T12:30:00Z" />),
    "Close the usage limit message",
  )
})

test("no pause, no usage banner", () => {
  stored(null)
  assert.equal(renderToStaticMarkup(<UsageBanner pausedUntil={null} />), "")
})
