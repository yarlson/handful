// Routines: a schedule in words, and which form field a server error is about. The server
// parses schedules (core/routines.go): a 5-field cron expression or a descriptor such as
// @daily or "@every 2h". This file only words the common forms.

const DAYS = ["Sundays", "Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays"]
const DAY_NAMES = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"]

function number(field: string, min: number, max: number): number | null {
  if (!/^\d{1,2}$/.test(field)) return null
  const n = Number(field)
  return n >= min && n <= max ? n : null
}

function day(field: string): number | null {
  const named = DAY_NAMES.indexOf(field.toLowerCase())
  return named >= 0 ? named : number(field, 0, 6)
}

// daysOf reads a weekday field made of single days and ranges, such as "1-5" or "mon,wed".
function daysOf(field: string): number[] | null {
  const days = new Set<number>()
  for (const part of field.split(",")) {
    const [from, to = from, ...rest] = part.split("-")
    const a = day(from)
    const b = day(to)
    if (rest.length > 0 || a === null || b === null || a > b) return null
    for (let d = a; d <= b; d++) days.add(d)
  }
  return [...days].sort((x, y) => x - y)
}

function list(words: string[]): string {
  if (words.length === 1) return words[0]
  return `${words.slice(0, -1).join(", ")} and ${words[words.length - 1]}`
}

function daysInWords(days: number[]): string {
  const key = days.join(",")
  if (key === "0,1,2,3,4,5,6") return "Every day"
  if (key === "1,2,3,4,5") return "Weekdays"
  if (key === "0,6") return "Weekends"
  return list(days.map((d) => DAYS[d]))
}

function plural(n: number, unit: string): string {
  return `${n} ${unit}${n === 1 ? "" : "s"}`
}

function every(n: number, unit: string): string {
  return n === 1 ? `Every ${unit}` : `Every ${plural(n, unit)}`
}

function ordinal(n: number): string {
  const suffix = n >= 11 && n <= 13 ? "th" : (["th", "st", "nd", "rd"][n % 10] ?? "th")
  return `${n}${suffix}`
}

const pad = (n: number) => String(n).padStart(2, "0")

// durationInWords words a Go duration made of hours, minutes and seconds, such as "1h30m".
function durationInWords(duration: string): string | null {
  const m = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(duration)
  if (!m) return null
  const total = Number(m[1] ?? 0) * 3600 + Number(m[2] ?? 0) * 60 + Number(m[3] ?? 0)
  const parts = (
    [
      [Math.floor(total / 3600), "hour"],
      [Math.floor((total % 3600) / 60), "minute"],
      [total % 60, "second"],
    ] as const
  ).filter(([n]) => n > 0)
  if (parts.length === 0) return null
  if (parts.length === 1) return every(parts[0][0], parts[0][1])
  return `Every ${parts.map(([n, unit]) => plural(n, unit)).join(" ")}`
}

const DESCRIPTORS: Record<string, string> = {
  "@yearly": "Every year on 1 January at 00:00",
  "@annually": "Every year on 1 January at 00:00",
  "@monthly": "On the 1st of each month at 00:00",
  "@weekly": "Sundays at 00:00",
  "@daily": "Every day at 00:00",
  "@midnight": "Every day at 00:00",
  "@hourly": "Every hour",
}

// scheduleInWords words a routine's schedule, such as "Weekdays at 09:17", or returns null
// for a schedule that has no short form in words; the UI then shows the schedule itself.
export function scheduleInWords(schedule: string): string | null {
  const s = schedule.trim()
  if (s.startsWith("@every ")) return durationInWords(s.slice("@every ".length))
  if (s.startsWith("@")) return DESCRIPTORS[s] ?? null
  const fields = s.split(/\s+/)
  if (fields.length !== 5) return null
  const [minute, hour, dom, month, dow] = fields.map((f) => (f === "?" ? "*" : f))
  if (month !== "*") return null
  // A step restarts at each hour or day, so it is a fixed interval only when it divides 60
  // minutes or 24 hours: "*/45 * * * *" runs at :00 and :45.
  const step = (f: string, period: number) => {
    const m = /^\*\/([1-9]\d*)$/.exec(f)
    return m && period % Number(m[1]) === 0 ? Number(m[1]) : null
  }
  const min = number(minute, 0, 59)
  const anyDay = dom === "*" && dow === "*"
  if (hour === "*" && anyDay) {
    const minutes = step(minute, 60)
    if (minutes) return every(minutes, "minute")
    return min === null ? null : `Every hour at :${pad(min)}`
  }
  if (min === null) return null
  const hours = step(hour, 24)
  if (hours && anyDay) return min === 0 ? every(hours, "hour") : `${every(hours, "hour")} at :${pad(min)}`
  const h = number(hour, 0, 23)
  if (h === null) return null
  const at = `${pad(h)}:${pad(min)}`
  if (anyDay) return `Every day at ${at}`
  if (dom === "*") {
    const days = daysOf(dow)
    return days && `${daysInWords(days)} at ${at}`
  }
  const date = number(dom, 1, 31)
  if (dow === "*" && date !== null) return `On the ${ordinal(date)} of each month at ${at}`
  return null
}

export type RoutineField = "name" | "prompt" | "schedule"

// fieldOfError names the form field a server error is about, or null for another error.
export function fieldOfError(message: string): RoutineField | null {
  if (message.startsWith("schedule ")) return "schedule"
  if (message.startsWith("routine name ")) return "name"
  if (message.startsWith("routine prompt ")) return "prompt"
  return null
}
