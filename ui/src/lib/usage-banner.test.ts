import { afterEach, assert, test, vi } from "vitest"
import { closeUsageBanner, closedUsageMessage, usageBannerShown, usageMessage } from "./usage-banner"

afterEach(() => {
  vi.unstubAllGlobals()
})

// browserStorage is a localStorage that keeps its values, as one browser does across reloads.
function browserStorage() {
  const values: Record<string, string> = {}
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => values[k] ?? null,
    setItem: (k: string, v: string) => {
      values[k] = v
    },
  })
  return values
}

test("the message names the weekday and the time when new turns start", () => {
  assert.match(
    usageMessage("2026-11-16T10:00:00Z"),
    /^Claude usage limit reached\. New turns wait until (Mon|Tue|Wed|Thu|Fri|Sat|Sun)\b[^,]*\d\d:\d\d[^:]*\.$/,
  )
})

test("the same end time gives the same message, as when the server sends the pause again on reconnect", () => {
  assert.equal(usageMessage("2026-11-16T10:00:00Z"), usageMessage("2026-11-16T10:00:00.000Z"))
  assert.notEqual(usageMessage("2026-11-16T10:00:00Z"), usageMessage("2026-11-16T12:30:00Z"))
})

test("a closed banner stays closed after a reload and comes back when its message changes", () => {
  const values = browserStorage()
  const first = usageMessage("2026-11-16T10:00:00Z")
  assert.isNull(closedUsageMessage())
  assert.isTrue(usageBannerShown(first, closedUsageMessage()))
  closeUsageBanner(first)
  assert.deepEqual(values, { "handful.usageBannerClosed": first })
  assert.isFalse(usageBannerShown(first, closedUsageMessage()), "the same message after a reload")
  assert.isTrue(usageBannerShown(usageMessage("2026-11-16T12:30:00Z"), closedUsageMessage()), "a later end time")
})

test("no pause shows no banner", () => {
  assert.isFalse(usageBannerShown(null, null))
  assert.isFalse(usageBannerShown(null, "Claude usage limit reached. New turns wait until Mon 10:00."))
})

test("a browser that refuses storage shows the banner again after a reload", () => {
  vi.stubGlobal("localStorage", {
    getItem: () => {
      throw new Error("denied")
    },
    setItem: () => {
      throw new Error("denied")
    },
  })
  closeUsageBanner("x")
  assert.isNull(closedUsageMessage())
})
