import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { Bot, Entry } from "@/lib/api"
import { EntryView } from "./chat.tsx"

// session is what the server says of this browser; each test sets it.
const session = vi.hoisted(() => ({ phone: false, scope: "chat+approve" }))
vi.mock("@/lib/session", async () => {
  const { rightsOf } = await import("@/lib/phones")
  return { usePhoneSession: () => session.phone, useRights: () => rightsOf(session) }
})

afterEach(() => {
  session.phone = false
  session.scope = "chat+approve"
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
const card = (
  status: "pending" | "allowed" | "denied" = "pending",
  extra: object = {},
  tool = "Edit",
  input: unknown = { file_path: "/x" },
) =>
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
          body: { tool, input, status, ...extra },
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
  assert.include(html, "Always allow can only be chosen on the computer.")
})

test("on the computer an approval offers Always allow at every width", () => {
  const html = card()
  assert.include(html, ">Always allow Edit</button>")
  assert.include(html, "Allow once")
  assert.include(html, ">Deny</button>")
  assert.notInclude(html, "can only be chosen on the computer")
  // A narrow window makes the buttons tall, but keeps Always allow.
  assert.match(html, /class="[^"]*\bmax-md:h-11 max-md:flex-1\b[^"]*"[^>]*>.*?Allow once/)
})

test("a decided approval offers no buttons, on a phone too", () => {
  session.phone = true
  assert.notInclude(card("allowed"), "Allow once")
})

test("a phone paired for Chat or View reads an approval but has no buttons for it", () => {
  session.phone = true
  for (const scope of ["chat", "view"]) {
    session.scope = scope
    const html = card()
    assert.include(html, "Allow Edit?", scope)
    assert.notInclude(html, "Allow once", scope)
    assert.notInclude(html, ">Deny<", scope)
  }
})

test("a phone's decision shows on the card with the phone's name (P7)", () => {
  assert.include(card("allowed", { decision: "once", decidedOn: "Pixel 8" }), "Approved once from Pixel 8.")
  assert.include(card("denied", { decision: "deny", decidedOn: "Pixel 8" }), "Denied from Pixel 8.")
  assert.notInclude(card("allowed", { decision: "always" }), " from ")
})

const setup = { bots: [{ name: "Ann", instructions: "y" }], teams: [], rooms: [] }

test("a setup is only cancelled from a phone that may approve, and never created from one", () => {
  assert.include(card("pending", {}, "create_setup", setup), "Create this setup")
  session.phone = true
  let html = card("pending", {}, "create_setup", setup)
  assert.notInclude(html, 'Create this setup"')
  assert.include(html, "Cancel this setup")
  session.scope = "chat"
  html = card("pending", {}, "create_setup", setup)
  assert.notInclude(html, "Cancel this setup")
})
