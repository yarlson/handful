import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { Bot, Entry } from "@/lib/api"
import { EntryView } from "./chat.tsx"

// session is what the server says of this browser; each test sets it.
const session = vi.hoisted(() => ({ phone: false }))
vi.mock("@/lib/session", () => ({ usePhoneSession: () => session.phone }))

afterEach(() => {
  session.phone = false
})

const chat: Bot = {
  id: "b",
  kind: "agent",
  name: "Builder",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
}
const card = (status: "pending" | "allowed" = "pending") =>
  renderToStaticMarkup(
    <EntryView
      entry={
        {
          id: "a",
          botId: "b",
          seq: 1,
          rev: 1,
          turnId: "t",
          createdAt: 0,
          kind: "approval",
          body: { tool: "Edit", input: { file_path: "/x" }, status },
        } as Entry
      }
      chat={chat}
      skill={null}
    />,
  )

test("on a paired phone an approval offers Allow once and Deny at 44 px, and says why Always allow isn't there", () => {
  session.phone = true
  const html = card()
  assert.notInclude(html, "Always allow Edit<")
  assert.match(html, /<button[^>]*class="[^"]*\bh-11 flex-1\b[^"]*"[^>]*>.*?Allow once<\/button>/)
  assert.match(html, /<button[^>]*data-variant="outline"[^>]*class="[^"]*\bh-11 flex-1\b[^"]*"[^>]*>Deny<\/button>/)
  assert.include(html, "Always allow Edit works only on your computer.")
})

test("on the computer an approval offers Always allow at every width", () => {
  const html = card()
  assert.include(html, ">Always allow Edit</button>")
  assert.include(html, "Allow once")
  assert.include(html, ">Deny</button>")
  assert.notInclude(html, "works only on your computer")
  // A narrow window makes the buttons tall, but keeps Always allow.
  assert.match(html, /class="[^"]*\bmax-md:h-11 max-md:flex-1\b[^"]*"[^>]*>.*?Allow once/)
})

test("a decided approval offers no buttons, on a phone too", () => {
  session.phone = true
  assert.notInclude(card("allowed"), "Allow once")
})
