import { afterEach, assert, test, vi } from "vitest"
import {
  closePlanWarning,
  closeUsageBanner,
  closedPlanWarnings,
  closedUsageMessage,
  mergeWindows,
  planWarning,
  resetTime,
  usageBannerShown,
  usageMessage,
  usedPercent,
  warningText,
  type PlanWindow,
} from "./usage-banner"

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

const RIGA = "Europe/Riga"
// NOV_20_3AM is Nov 20, 2026 at 3am in Riga, the reset in Claude Code's recorded event.
const NOV_20_3AM = 1795136400
const BEFORE_RESET = NOV_20_3AM * 1000 - 1

function window(fields: Partial<PlanWindow>): PlanWindow {
  return {
    window: "seven_day",
    status: "allowed_warning",
    utilization: 0.78,
    resetsAt: NOV_20_3AM,
    surpassedThreshold: 0.75,
    receivedAt: 1,
    ...fields,
  }
}

test("the warning names the percent used, the window and the reset, in Claude Code's words", () => {
  assert.equal(warningText(window({}), RIGA), "You've used 78% of your weekly limit · resets Nov 20 at 3am")
  assert.equal(
    warningText(window({ utilization: 0.785 }), RIGA),
    "You've used 78% of your weekly limit · resets Nov 20 at 3am",
  )
  assert.equal(
    warningText(window({ window: "seven_day_opus", utilization: 0.8 }), RIGA),
    "You've used 80% of your Opus limit · resets Nov 20 at 3am",
  )
  assert.equal(
    warningText(window({ utilization: undefined, surpassedThreshold: 0.9 }), RIGA),
    "Over 90% of your weekly limit used · resets Nov 20 at 3am",
  )
  assert.equal(
    warningText(
      window({
        utilization: undefined,
        surpassedThreshold: undefined,
        resetsAt: undefined,
      }),
    ),
    "Approaching weekly limit",
  )
})

test("each window has Claude Code's label", () => {
  const labels = {
    five_hour: "5-hour limit",
    seven_day: "weekly limit",
    seven_day_opus: "Opus limit",
    seven_day_sonnet: "Sonnet limit",
    seven_day_overage_included: "Fable limit",
    overage: "usage credit limit",
  }
  for (const [name, label] of Object.entries(labels)) {
    assert.equal(warningText(window({ window: name, resetsAt: undefined })), `You've used 78% of your ${label}`)
  }
})

test("the percent used rounds to a tenth, then drops it, and stops at 100", () => {
  assert.equal(usedPercent(0.785), 78)
  assert.equal(usedPercent(0.29), 29)
  assert.equal(usedPercent(0.7999), 80)
  assert.equal(usedPercent(0.7994), 79)
  assert.equal(usedPercent(1.5), 100)
})

test("a threshold shows as a whole percent below 100, and only between 0 and 1", () => {
  const over = (surpassedThreshold: number | undefined) =>
    warningText(
      window({
        utilization: undefined,
        surpassedThreshold,
        resetsAt: undefined,
      }),
    )
  assert.equal(over(0.755), "Over 75% of your weekly limit used")
  assert.equal(over(0.99999), "Over 99% of your weekly limit used")
  assert.equal(over(0.001), "Over 0% of your weekly limit used")
  assert.equal(over(0), "Approaching weekly limit")
  assert.equal(over(1), "Approaching weekly limit")
  assert.equal(over(undefined), "Approaching weekly limit")
})

test("a reset time names its minutes only when they are not zero", () => {
  assert.equal(resetTime(NOV_20_3AM, RIGA), "Nov 20 at 3am")
  assert.equal(resetTime(NOV_20_3AM + 30 * 60, RIGA), "Nov 20 at 3:30am")
  assert.equal(resetTime(NOV_20_3AM + 12 * 3600 + 5 * 60, RIGA), "Nov 20 at 3:05pm")
})

test("only a warning window with a label, used from 70% or with no utilization, warns", () => {
  const warns = (w: PlanWindow) => planWarning([w], {}, BEFORE_RESET)
  assert.deepEqual(warns(window({})), window({}))
  assert.deepEqual(warns(window({ utilization: 0.7 })), window({ utilization: 0.7 }))
  assert.deepEqual(warns(window({ utilization: undefined })), window({ utilization: undefined }))
  assert.isNull(warns(window({ utilization: 0.65 })), "below 70%")
  assert.isNull(warns(window({ utilization: 0.6999 })), "just below 70%")
  assert.isNull(warns(window({ status: "allowed" })), "allowed")
  assert.isNull(warns(window({ status: "rejected" })), "rejected keeps the usage limit message")
  assert.isNull(warns(window({ window: "seven_day_cowork" })), "a window without a label")
  assert.isNull(warns(window({ window: "toString" })), "a name of an object's own property")
})

test("a window that has reset warns no more", () => {
  assert.isNull(planWarning([window({})], {}, NOV_20_3AM * 1000))
  assert.deepEqual(
    planWarning([window({ resetsAt: undefined })], {}, NOV_20_3AM * 1000),
    window({ resetsAt: undefined }),
  )
})

test("of several warnings, the most used window's shows", () => {
  const session = window({ window: "five_hour", utilization: 0.9 })
  const weekly = window({ utilization: 0.8 })
  const opus = window({ window: "seven_day_opus", utilization: undefined })
  assert.deepEqual(planWarning([weekly, opus, session], {}, BEFORE_RESET), session)
  assert.deepEqual(planWarning([opus, weekly], {}, BEFORE_RESET), weekly)
})

test("a closed warning comes back when its window's threshold or reset changes, after a reload too", () => {
  const values = browserStorage()
  const weekly = window({})
  const session = window({ window: "five_hour", utilization: 0.72 })
  assert.deepEqual(closedPlanWarnings(), {})
  closePlanWarning(weekly)
  assert.deepEqual(values, {
    "handful.planWarningsClosed": JSON.stringify({
      seven_day: "0.75|1795136400",
    }),
  })
  assert.deepEqual(
    planWarning([weekly, session], closedPlanWarnings(), BEFORE_RESET),
    session,
    "the other window still warns",
  )
  closePlanWarning(session)
  assert.isNull(planWarning([weekly, session], closedPlanWarnings(), BEFORE_RESET), "both closed")
  const higher = window({ surpassedThreshold: 0.9, utilization: 0.91 })
  assert.deepEqual(planWarning([higher], closedPlanWarnings(), BEFORE_RESET), higher, "a new threshold")
  const reset = window({ resetsAt: NOV_20_3AM + 7 * 86400 })
  assert.deepEqual(planWarning([reset], closedPlanWarnings(), BEFORE_RESET), reset, "a new reset")
})

test("stored closed warnings that are not an object count as none", () => {
  for (const stored of ["not json", "null", "3"]) {
    vi.stubGlobal("localStorage", { getItem: () => stored, setItem: () => {} })
    assert.deepEqual(closedPlanWarnings(), {}, stored)
  }
})

test("a window's newer state replaces its older one, in either order of arrival", () => {
  const old = window({ utilization: 0.7, receivedAt: 1 })
  const fresh = window({ utilization: 0.8, receivedAt: 2 })
  const session = window({ window: "five_hour", receivedAt: 1 })
  assert.deepEqual(mergeWindows([old, session], [fresh]), [fresh, session])
  assert.deepEqual(mergeWindows([fresh], [old]), [fresh])
  const again = window({ utilization: 0.81, receivedAt: 2 })
  assert.deepEqual(mergeWindows([fresh], [again]), [again], "the same time: the incoming one")
  assert.deepEqual(mergeWindows([], [session]), [session])
})
