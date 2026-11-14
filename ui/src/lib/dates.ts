// The app's date and time formats, in the browser's locale and time zone. A zone's fields are
// for tests; undefined uses the browser's.
export type Zone = { locale?: string; timeZone?: string }

// A transcript formats each entry's day on each render, so the formats are made once.
const formats = new Map<string, Intl.DateTimeFormat>()
function format(locale: string | undefined, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = JSON.stringify([locale, options])
  let f = formats.get(key)
  // Stryker disable next-line ConditionalExpression: the cache only saves time; a new format gives the same text
  if (!f) {
    f = new Intl.DateTimeFormat(locale, options)
    // Stryker disable next-line all: the cache only saves time; a new format gives the same text
    formats.set(key, f)
  }
  return f
}

// clock is the time of day: "14:05", or "2:05 PM" in a 12-hour locale.
export function clock(ms: number, zone: Zone = {}): string {
  return format(zone.locale, {
    timeStyle: "short",
    timeZone: zone.timeZone,
  }).format(ms)
}

// fullDate is the date and time in full, for a post's tooltip.
export function fullDate(ms: number, zone: Zone = {}): string {
  return format(zone.locale, {
    dateStyle: "full",
    timeStyle: "short",
    timeZone: zone.timeZone,
  }).format(ms)
}

type Day = { year: number; month: number; day: number }

// dayOf is the calendar day of ms in the zone's time zone.
function dayOf(ms: number, zone: Zone): Day {
  const parts = format("en-US", {
    year: "numeric",
    month: "numeric",
    day: "numeric",
    timeZone: zone.timeZone,
  }).formatToParts(ms)
  const part = Object.fromEntries(parts.map((p) => [p.type, Number(p.value)]))
  return { year: part.year, month: part.month, day: part.day }
}

// daysBetween counts the calendar days from a to b; Date.UTC rolls a day 0 back into the month before.
function daysBetween(a: Day, b: Day): number {
  return (Date.UTC(b.year, b.month - 1, b.day) - Date.UTC(a.year, a.month - 1, a.day)) / 86_400_000
}

// dayLabel names the local day of ms as seen at now: "Today", "Yesterday", "Fri 13 Nov" in the
// current year and "13 Nov 2025" in another; the locale sets the order and the words.
export function dayLabel(ms: number, now: number, zone: Zone = {}): string {
  const day = dayOf(ms, zone)
  const today = dayOf(now, zone)
  const ago = daysBetween(day, today)
  if (ago === 0) return "Today"
  if (ago === 1) return "Yesterday"
  const sameYear = day.year === today.year
  return format(zone.locale, {
    weekday: sameYear ? "short" : undefined,
    day: "numeric",
    month: "short",
    year: sameYear ? undefined : "numeric",
    timeZone: zone.timeZone,
  }).format(ms)
}

// dateTime is a day and a time, as memory, routines and artifacts show them: "Today, 14:05".
export function dateTime(ms: number, now: number, zone: Zone = {}): string {
  return `${dayLabel(ms, now, zone)}, ${clock(ms, zone)}`
}

// daySeparators gives each of a transcript's times the label of the day separator above it, or
// undefined when it is on the same local day as the time before it. The first time gets one.
export function daySeparators(times: number[], now: number, zone: Zone = {}): (string | undefined)[] {
  let previous: Day | undefined
  return times.map((ms) => {
    const day = dayOf(ms, zone)
    const sameDay = previous !== undefined && daysBetween(previous, day) === 0
    previous = day
    return sameDay ? undefined : dayLabel(ms, now, zone)
  })
}
