import { assert, test } from "vitest"
import type { Entry } from "./api.ts"
import { offersStarts, STARTS } from "./starts.ts"

const at = { botId: "b", seq: 1, rev: 1, turnId: "", createdAt: 0 }
const welcome: Entry = { ...at, id: "w", kind: "bot", body: { text: "Hi!", fallback: false } }
const first: Entry = { ...at, id: "u", kind: "user", body: { text: "A software team" } }

test("Builder offers the four starts until the user's first message in its chat", () => {
  assert.deepEqual(STARTS, [
    {
      label: "Personal assistant",
      text: "Set up a personal assistant: one bot for my day: plans, reminders, research and drafts.",
    },
    {
      label: "Software team",
      text: "Set up a software team: a lead, a developer and a reviewer who change my code and check it with tests.",
    },
    {
      label: "Research desk",
      text: "Set up a research desk: a lead, a researcher and a fact-checker who answer questions with sources.",
    },
    {
      label: "Writing studio",
      text: "Set up a writing studio: an editor and a writer who plan, draft and edit my texts.",
    },
  ])
  assert.isTrue(offersStarts({ role: "builder" }, []))
  assert.isTrue(offersStarts({ role: "builder" }, [welcome]))
  assert.isFalse(offersStarts({ role: "builder" }, [welcome, first]))
})

test("another bot's chat offers no starts", () => {
  assert.isFalse(offersStarts({}, [welcome]))
  assert.isFalse(offersStarts({ role: "" }, []))
})
