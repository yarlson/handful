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
  status: "pending" | "allowed" | "denied" | "expired" = "pending",
  extra: object = {},
  tool = "Edit",
  input: unknown = { file_path: "/x" },
  expanded = false,
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
      skillIds={[]}
      expanded={expanded}
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

test("a phone's decision shows on the opened card with the phone's name (P7)", () => {
  const opened = (status: "allowed" | "denied", extra: object) => card(status, extra, "Edit", { file_path: "/x" }, true)
  assert.include(opened("allowed", { decision: "once", decidedOn: "Pixel 8" }), "Approved once from Pixel 8.")
  assert.include(opened("denied", { decision: "deny", decidedOn: "Pixel 8" }), "Denied from Pixel 8.")
  assert.notInclude(opened("allowed", { decision: "always" }), " from ")
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

test("an opened expired approval or a setup says why it expired, and an old card without a cause shows only its badge", () => {
  assert.include(
    card("expired", { expiry: "limit" }, "Edit", { file_path: "/x" }, true),
    "Expired: no answer within 4.5 minutes. Ask the bot to try again.",
  )
  assert.include(
    card("expired", { expiry: "turn-ended" }, "Edit", { file_path: "/x" }, true),
    "Expired: the turn ended before you answered.",
  )
  assert.include(
    card("expired", { expiry: "restart" }, "create_setup", setup),
    "Expired: Handful restarted before you answered. Send your message again.",
  )
  for (const old of [
    card("expired", {}, "Edit", { file_path: "/x" }, true),
    card("expired", {}, "create_setup", setup),
  ]) {
    assert.match(old, />expired</, "the badge says it expired")
    assert.notInclude(old, "Expired")
  }
  assert.notInclude(card("denied", {}, "Edit", { file_path: "/x" }, true), "Expired")
})

const at = new Intl.DateTimeFormat(undefined, { timeStyle: "short" }).format(0)

test("an answered approval folds to one line: the tool, its target, the decision and the time", () => {
  for (const [status, extra, label] of [
    ["allowed", { decision: "once" }, "allowed"],
    ["allowed", { decision: "always" }, "always allowed"],
    ["denied", { decision: "deny" }, "denied"],
    ["expired", {}, "expired"],
  ] as const) {
    const html = card(status as "allowed", extra, "Bash", { command: "make check\nmake crap" })
    assert.match(
      html,
      new RegExp(
        `^<div><button type="button" aria-expanded="false".*>Bash</span>.*>make check</span>.*>${label}</span>.*>${at}</time></button></div>$`,
      ),
      label,
    )
    assert.notInclude(html, "Allow Bash?", label)
    assert.notInclude(html, "make crap", label)
  }
})

test("a click opens an answered approval's full card; a pending card is full and has no fold line", () => {
  const opened = card("allowed", { decision: "once" }, "Bash", { command: "make check" }, true)
  assert.include(opened, 'aria-expanded="true"')
  assert.include(opened, "Allow Bash?")
  assert.include(opened, ">allowed</span>")
  const pending = card("pending", {}, "Bash", { command: "make check" })
  assert.notInclude(pending, 'aria-expanded="')
  assert.include(pending, "Allow Bash?")
  assert.include(pending, "Allow once")
})

// diffRows are the rendered diff's line rows: their colour and their text.
const diffRows = (html: string) =>
  [...html.matchAll(/<tr class="([^"]*)">(?:<td[^>]*>[^<]*<\/td>)*?<td class="pr-2[^"]*">([^<]*)<\/td><\/tr>/g)].map(
    ([, tone, text]) => `${tone.includes("success") ? "+" : tone.includes("destructive") ? "-" : " "}${text}`,
  )

test("an Edit approval shows the file and a diff of the change, without line numbers", () => {
  const html = card("pending", {}, "Edit", {
    file_path: "/w/a.txt",
    old_string: "one\ntwo\nend",
    new_string: "one\nTWO\nend",
  })
  assert.include(html, ">/w/a.txt</span>")
  assert.deepEqual(diffRows(html), [" one", "-two", "+TWO", " end"])
  assert.notInclude(html, "text-right", "no line numbers")
  assert.notInclude(html, "All matches")
  assert.notInclude(html, "old_string")
  const all = card("pending", {}, "Edit", {
    file_path: "/w/a.txt",
    old_string: "a",
    new_string: "b",
    replace_all: true,
  })
  assert.include(all, ">All matches</span>")
})

test("a Write approval shows the file and its content as added lines", () => {
  const html = card("pending", {}, "Write", { file_path: "/w/c.txt", content: "alpha\nbeta\n" })
  assert.include(html, ">/w/c.txt</span>")
  assert.deepEqual(diffRows(html), ["+alpha", "+beta"])
})

test("a Bash approval shows its description and the command in a code block with Copy", () => {
  const html = card("pending", {}, "Bash", { command: "make check", description: "Run the checks" })
  assert.include(html, ">Run the checks</p>")
  assert.match(html, />bash<\/span>.*Copy.*<pre[^>]*><code>make check<\/code><\/pre>/)
  assert.notInclude(card("pending", {}, "Bash", { command: "ls" }), '<p class="text-muted-foreground text-xs">')
})

test("another tool's input is a list of names and values; a multi-line value is in a code block", () => {
  const html = card("pending", {}, "mcp__mail__send", { to: "Ann", body: "line one\nline two" })
  assert.match(html, /<dt[^>]*>to<\/dt><dd[^>]*>Ann<\/dd>/)
  assert.match(html, /<dt[^>]*>body<\/dt><dd[^>]*>.*Copy.*<code>line one\nline two<\/code>.*<\/dd>/s)
  assert.notInclude(html, "{")
  // An Edit without the strings it needs shows its fields, too.
  assert.match(card("pending", {}, "Edit", { file_path: "/x" }), /<dt[^>]*>file_path<\/dt><dd[^>]*>\/x<\/dd>/)
})
