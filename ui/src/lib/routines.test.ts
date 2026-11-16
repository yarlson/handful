import { assert, test } from "vitest"
import { fieldOfError, scheduleInWords } from "./routines"

const words: [string, string | null][] = [
  ["17 9 * * 1-5", "Weekdays at 09:17"],
  ["17 9 * * mon-fri", "Weekdays at 09:17"],
  ["0 10 * * 0,6", "Weekends at 10:00"],
  ["0 10 * * sat,sun", "Weekends at 10:00"],
  ["30 8 * * 1", "Mondays at 08:30"],
  ["30 8 * * 1,4", "Mondays and Thursdays at 08:30"],
  ["30 8 * * 1,3,5", "Mondays, Wednesdays and Fridays at 08:30"],
  ["30 8 * * 0-6", "Every day at 08:30"],
  ["5 23 * * *", "Every day at 23:05"],
  ["5 23 ? * *", "Every day at 23:05"],
  ["0 7 1 * *", "On the 1st of each month at 07:00"],
  ["0 7 2 * *", "On the 2nd of each month at 07:00"],
  ["0 7 3 * *", "On the 3rd of each month at 07:00"],
  ["0 7 11 * *", "On the 11th of each month at 07:00"],
  ["0 7 22 * *", "On the 22nd of each month at 07:00"],
  ["0 7 5 * *", "On the 5th of each month at 07:00"],
  ["0 7 10 * *", "On the 10th of each month at 07:00"],
  ["0 7 12 * *", "On the 12th of each month at 07:00"],
  ["0 7 13 * *", "On the 13th of each month at 07:00"],
  ["0 7 31 * *", "On the 31st of each month at 07:00"],
  ["0 9 * * 0,2", "Sundays and Tuesdays at 09:00"],
  ["0 9 * * 6", "Saturdays at 09:00"],
  ["0 9 * * wed", "Wednesdays at 09:00"],
  ["0 9 * * tue-thu", "Tuesdays, Wednesdays and Thursdays at 09:00"],
  ["0 9 * * SUN,Fri", "Sundays and Fridays at 09:00"],
  ["5  23 * * *", "Every day at 23:05"],
  ["*/15 * * * *", "Every 15 minutes"],
  ["17 * * * *", "Every hour at :17"],
  ["0 */2 * * *", "Every 2 hours"],
  ["0 */1 * * *", "Every hour"],
  ["20 */3 * * *", "Every 3 hours at :20"],
  ["  17 9 * * 1-5  ", "Weekdays at 09:17"],
  ["@daily", "Every day at 00:00"],
  ["@midnight", "Every day at 00:00"],
  ["@hourly", "Every hour"],
  ["@weekly", "Sundays at 00:00"],
  ["@monthly", "On the 1st of each month at 00:00"],
  ["@yearly", "Every year on 1 January at 00:00"],
  ["@annually", "Every year on 1 January at 00:00"],
  ["@every 2h", "Every 2 hours"],
  ["@every 1h", "Every hour"],
  ["@every 30m", "Every 30 minutes"],
  ["@every 1h30m", "Every 1 hour 30 minutes"],
  ["@every 90m", "Every 1 hour 30 minutes"],
  ["@every 2h0m0s", "Every 2 hours"],
  ["@every 600s", "Every 10 minutes"],
  ["@every 45s", "Every 45 seconds"],
  ["@every 1h0m5s", "Every 1 hour 5 seconds"],
  ["@every 12h", "Every 12 hours"],
  ["@every 1m", "Every minute"],
]

test("a schedule shows in words", () => {
  for (const [schedule, expected] of words) assert.equal(scheduleInWords(schedule), expected, schedule)
})

// Each of these has no short form, so the UI shows the cron text.
const cron = [
  "0 9 * 1 *",
  "0 9,17 * * *",
  "0 9-17 * * *",
  "0 9 1 * 1",
  "0 9 1,15 * *",
  "*/5 9 * * *",
  "0 9 * * 5-1",
  "0 9 * * 1-5/2",
  "0 9 * * 7",
  "0 24 * * *",
  "60 9 * * *",
  "0 9 32 * *",
  "0 9 0 * *",
  "*/0 * * * *",
  "*/90 * * * *",
  "*/45 * * * *",
  "*/7 * * * *",
  "0 */30 * * *",
  "0 */5 * * *",
  "0 */0 * * *",
  "0 9 * *",
  "0 9 * * * *",
  "@reboot",
  "@every 1.5h",
  "@every 0s",
  "@every ",
  "@every  2h",
  "@every 2h5x",
  "+5 9 * * *",
  "5.0 9 * * *",
  "1*/5 * * * *",
  "*/5x * * * *",
  "0 9 * * 1-2-3",
  "0 9 * * x-5",
  "0 9 * * sun-x",
  "0 9 * * x",
  "CRON_TZ=UTC 0 9 * * *",
]

test("a schedule without a short form has no words", () => {
  for (const schedule of cron) assert.isNull(scheduleInWords(schedule), schedule)
})

test("a server error names the field it is about", () => {
  assert.equal(fieldOfError('schedule "x": expected exactly 5 fields, found 1: [x]'), "schedule")
  assert.equal(fieldOfError('schedule "* * * * *" runs more often than every 5m0s'), "schedule")
  assert.equal(fieldOfError("routine name must be 1 to 80 characters"), "name")
  assert.equal(fieldOfError("routine prompt must be 1 to 4000 characters"), "prompt")
  assert.isNull(fieldOfError("a bot can have at most 20 routines"))
})
