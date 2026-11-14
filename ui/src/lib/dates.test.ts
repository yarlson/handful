import { assert, test } from "vitest"
import { clock, dateTime, dayLabel, daySeparators, fullDate } from "./dates.ts"

// Times are fixed instants; the zone is the browser's locale and time zone that each test sets.
const kyiv = { locale: "en-GB", timeZone: "Europe/Kyiv" }
const now = Date.parse("2026-11-14T10:00:00Z") // Sat 14 Nov 2026, 12:00 in Kyiv

test("a day is named Today, Yesterday, by weekday in the current year and with its year in another", () => {
  assert.equal(dayLabel(Date.parse("2026-11-14T08:00:00Z"), now, kyiv), "Today")
  assert.equal(dayLabel(Date.parse("2026-11-13T08:00:00Z"), now, kyiv), "Yesterday")
  assert.equal(dayLabel(Date.parse("2026-11-12T08:00:00Z"), now, kyiv), "Thu 12 Nov")
  assert.equal(dayLabel(Date.parse("2026-01-01T08:00:00Z"), now, kyiv), "Thu 1 Jan")
  assert.equal(dayLabel(Date.parse("2025-11-13T08:00:00Z"), now, kyiv), "13 Nov 2025")
  assert.equal(
    dayLabel(Date.parse("2026-11-15T08:00:00Z"), now, kyiv),
    "Sun 15 Nov",
    "a day ahead, as a routine's next run",
  )
})

test("the day boundary is local midnight, not UTC midnight", () => {
  // 22:30 UTC on 13 Nov is 00:30 on 14 Nov in Kyiv, and still 13 Nov in UTC.
  const lateUtc = Date.parse("2026-11-13T22:30:00Z")
  assert.equal(dayLabel(lateUtc, now, kyiv), "Today")
  assert.equal(dayLabel(lateUtc, now, { locale: "en-GB", timeZone: "UTC" }), "Yesterday")
  assert.equal(dayLabel(Date.parse("2026-11-13T21:59:00Z"), now, kyiv), "Yesterday", "23:59 in Kyiv")
})

test("Yesterday counts calendar days across a clock change and a year's end", () => {
  // Kyiv moves its clocks back on 25 Oct 2026: that day has 25 hours.
  const afterChange = Date.parse("2026-10-26T09:00:00Z")
  assert.equal(dayLabel(Date.parse("2026-10-25T00:30:00Z"), afterChange, kyiv), "Yesterday")
  assert.equal(dayLabel(Date.parse("2026-12-31T20:00:00Z"), Date.parse("2027-01-01T09:00:00Z"), kyiv), "Yesterday")
  assert.equal(dayLabel(Date.parse("2026-12-30T20:00:00Z"), Date.parse("2027-01-01T09:00:00Z"), kyiv), "30 Dec 2026")
})

test("a day separator stands above the first entry and between entries on different local days", () => {
  const times = [
    Date.parse("2026-11-12T08:00:00Z"),
    Date.parse("2026-11-12T21:00:00Z"), // 23:00 in Kyiv, the same day
    Date.parse("2026-11-13T22:30:00Z"), // 00:30 on 14 Nov in Kyiv
    Date.parse("2026-11-14T09:00:00Z"),
  ]
  assert.deepEqual(daySeparators(times, now, kyiv), ["Thu 12 Nov", undefined, "Today", undefined])
  assert.deepEqual(daySeparators([], now, kyiv), [])
  assert.deepEqual(
    daySeparators([Date.parse("2026-11-14T09:00:00Z")], now, kyiv),
    ["Today"],
    "a first entry from today",
  )
})

test("times follow the locale and the time zone", () => {
  const at = Date.parse("2026-11-13T12:05:00Z")
  assert.equal(clock(at, kyiv), "14:05")
  assert.equal(clock(at, { locale: "en-US", timeZone: "America/New_York" }), "7:05 AM")
  assert.equal(fullDate(at, kyiv), "Friday, 13 November 2026 at 14:05")
  assert.equal(dateTime(at, now, kyiv), "Yesterday, 14:05")
  assert.equal(dateTime(Date.parse("2025-03-02T12:05:00Z"), now, kyiv), "2 Mar 2025, 14:05")
})
