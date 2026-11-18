import { assert, test } from "vitest"
import type { PlanWindow } from "./usage-banner"
import {
  asOfLine,
  botShares,
  helperRunsText,
  PERIODS,
  resetLine,
  shownWindows,
  usedLine,
  windowPercent,
  windowTitle,
} from "./usage-view"

const RIGA = "Europe/Riga"
// 2026-11-20 01:00 UTC, 03:00 in Riga.
const NOV_20_3AM_RIGA = 1795136400

const win = (window: string, more: Partial<PlanWindow> = {}): PlanWindow => ({
  window,
  status: "allowed",
  receivedAt: 1,
  ...more,
})

test("each window has its title, and another window its warning label or its name with a capital", () => {
  assert.deepEqual(
    [
      "five_hour",
      "seven_day",
      "seven_day_sonnet",
      "seven_day_opus",
      "seven_day_overage_included",
      "overage",
      "new_window",
    ].map(windowTitle),
    [
      "Current session",
      "Current week (all models)",
      "Current week (Sonnet only)",
      "Current week (Opus)",
      "Current week (Fable)",
      "Usage credit limit",
      "New_window",
    ],
  )
})

test("the windows show in the order of their titles, then the others by name", () => {
  const names = (ws: PlanWindow[]) => shownWindows(ws).map((w) => w.window)
  const order = ["five_hour", "seven_day", "seven_day_sonnet", "seven_day_opus", "seven_day_overage_included", "a", "b"]
  assert.deepEqual(
    names(
      ["b", "seven_day_opus", "a", "seven_day", "seven_day_overage_included", "five_hour", "seven_day_sonnet"].map(
        (n) => win(n),
      ),
    ),
    order,
  )
  assert.deepEqual(names(order.toReversed().map((n) => win(n))), order)
})

test("a window's used line is its whole percent, a full bar when its limit is reached, or no report", () => {
  assert.equal(usedLine(win("seven_day", { utilization: 0.78 })), "78% used")
  assert.equal(usedLine(win("seven_day", { utilization: 0.29 })), "29% used")
  assert.equal(usedLine(win("seven_day", { utilization: 0 })), "0% used")
  assert.equal(windowPercent(win("five_hour", { status: "rejected" })), 100)
  assert.equal(usedLine(win("five_hour", { status: "rejected" })), "100% used")
  assert.equal(windowPercent(win("five_hour", { status: "allowed_warning", surpassedThreshold: 0.9 })), null)
  assert.equal(usedLine(win("five_hour")), "Usage not reported")
})

test("a window's reset line names the day, the hour and the time zone", () => {
  assert.equal(resetLine(win("seven_day", { resetsAt: NOV_20_3AM_RIGA }), RIGA), "Resets Nov 20 at 3am (Europe/Riga)")
  assert.equal(resetLine(win("seven_day", { resetsAt: NOV_20_3AM_RIGA }), "UTC"), "Resets Nov 20 at 1am (UTC)")
  const browserZone = Intl.DateTimeFormat().resolvedOptions().timeZone
  assert.equal(
    resetLine(win("seven_day", { resetsAt: NOV_20_3AM_RIGA })),
    resetLine(win("seven_day", { resetsAt: NOV_20_3AM_RIGA }), browserZone),
    "without a zone, the browser's",
  )
  assert.isNull(resetLine(win("seven_day")))
})

test("a window's as-of line is the time Claude Code reported it", () => {
  // 2026-11-18 12:05 UTC, 14:05 in Riga.
  assert.equal(
    asOfLine(win("seven_day", { receivedAt: Date.UTC(2026, 10, 18, 12, 5) }), { locale: "en-GB", timeZone: RIGA }),
    "As of 14:05, from the last bot turn",
  )
})

test("the periods are the last 24 hours and the last 7 days", () => {
  assert.deepEqual(PERIODS, [
    { id: "24h", label: "Last 24h" },
    { id: "7d", label: "Last 7d" },
  ])
})

test("each bot's share is its whole percent of the cost of all bots, the largest first, and the shares sum to 100", () => {
  const spend = [
    { botId: "c", costUsd: 1, helpers: 0 },
    { botId: "a", costUsd: 1, helpers: 2 },
    { botId: "b", costUsd: 1, helpers: 1 },
  ]
  // A third each: 33.3 rounds down twice and up once, for the first bot by name.
  const thirds = [
    { botId: "a", percent: 34, helpers: 2 },
    { botId: "b", percent: 33, helpers: 1 },
    { botId: "c", percent: 33, helpers: 0 },
  ]
  assert.deepEqual(botShares(spend), thirds)
  assert.deepEqual(botShares(spend.toReversed()), thirds)
  // 87.5, 10 and 2.5 percent: one of the halves rounds up.
  const uneven = [
    { botId: "x", costUsd: 0.025, helpers: 0 },
    { botId: "y", costUsd: 0.875, helpers: 3 },
    { botId: "z", costUsd: 0.1, helpers: 0 },
  ]
  assert.deepEqual(
    botShares(uneven).map((s) => [s.botId, s.percent]),
    [
      ["y", 88],
      ["z", 10],
      ["x", 2],
    ],
  )
  // 50.2, 29.7 and 20.1: the rest goes to the largest remainder, not to the first bot.
  assert.deepEqual(
    botShares([
      { botId: "a", costUsd: 502, helpers: 0 },
      { botId: "b", costUsd: 297, helpers: 0 },
      { botId: "c", costUsd: 201, helpers: 0 },
    ]).map((s) => s.percent),
    [50, 30, 20],
  )
  // 47, 46.5 and 6.5: of two equal remainders, the larger cost gets the rest.
  assert.deepEqual(
    botShares([
      { botId: "a", costUsd: 94, helpers: 0 },
      { botId: "b", costUsd: 93, helpers: 0 },
      { botId: "c", costUsd: 13, helpers: 0 },
    ]).map((s) => s.percent),
    [47, 47, 6],
  )
  assert.deepEqual(
    botShares([
      { botId: "big", costUsd: 0.6, helpers: 0 },
      { botId: "small", costUsd: 0.4, helpers: 0 },
    ]).map((s) => s.percent),
    [60, 40],
  )
  assert.deepEqual(botShares([{ botId: "only", costUsd: 0.004, helpers: 1 }]), [
    { botId: "only", percent: 100, helpers: 1 },
  ])
  assert.deepEqual(botShares([]), [])
})

test("with no cost every share is 0", () => {
  assert.deepEqual(
    botShares([
      { botId: "b", costUsd: 0, helpers: 1 },
      { botId: "a", costUsd: 0, helpers: 0 },
    ]),
    [
      { botId: "a", percent: 0, helpers: 0 },
      { botId: "b", percent: 0, helpers: 1 },
    ],
  )
})

test("helper runs are counted in words", () => {
  assert.deepEqual([0, 1, 2].map(helperRunsText), ["0 helper runs", "1 helper run", "2 helper runs"])
})
