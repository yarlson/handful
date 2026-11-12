import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { Bot, Session } from "@/lib/api"
import { App } from "./App.tsx"

// A bot with its model, whose chat the app opens.
const bot: Bot = {
  id: "b",
  kind: "agent",
  name: "Builder",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  effective: { model: "sonnet", modelSource: "default", effort: "high", effortSource: "default" },
} as Bot
vi.mock("@/lib/store", () => ({
  useHandful: () => ({
    bots: [bot],
    transcripts: new Map(),
    transcriptStatus: new Map(),
    unread: new Set(),
    link: "live",
    error: null,
    setError: () => {},
    pausedUntil: null,
    draining: false,
    restartRev: 0,
    pairRev: 0,
    select: async () => {},
    loadOlder: async () => {},
    reveal: async () => false,
    refreshBots: async () => {},
    reorder: async () => {},
  }),
}))

// session is what the server says of this browser; each test sets it.
const session = vi.hoisted(() => ({ value: { phone: false, key: true } as Session }))
vi.mock("@/lib/session", async (actual) => {
  const { rightsOf } = await import("@/lib/phones")
  const { needsKey } = await actual<typeof import("@/lib/session")>()
  return {
    needsKey,
    usePhoneSession: () => session.value.phone,
    useSession: () => session.value,
    useRights: () => rightsOf(session.value),
    phoneSession: { recheck: () => {} },
  }
})

// render renders the app at path in a 390 px window, with its drawer's controls in the markup.
function render(path = "/chat/b"): string {
  vi.stubGlobal("location", { pathname: path, hash: "", origin: "http://localhost" })
  vi.stubGlobal("localStorage", { getItem: () => null, setItem: () => {}, removeItem: () => {} })
  vi.stubGlobal("window", {
    location: { pathname: path, hash: "", origin: "http://localhost" },
    matchMedia: () => ({ matches: false, addEventListener: () => {}, removeEventListener: () => {} }),
    innerHeight: 844,
    innerWidth: 390,
  })
  return renderToStaticMarkup(<App />)
}

afterEach(() => {
  vi.unstubAllGlobals()
  session.value = { phone: false, key: true }
})

// The controls a phone never has (spec 6a748f8a section 4.4), as their markup shows them.
const desktopOnly = [
  'aria-label="New bot"',
  'aria-label="New team"',
  'aria-label="New room"',
  'aria-label="Settings"',
  'aria-label="Connectors"',
  "Sonnet",
]

test("a 390 px window on the computer shows every desktop control", () => {
  const html = render()
  for (const control of desktopOnly) assert.include(html, control, control)
  assert.include(html, 'aria-label="Send"')
})

test("a phone paired for Chat has no desktop controls, and keeps the message box", () => {
  session.value = { phone: true, scope: "chat", device: "Pixel 8" }
  const html = render()
  for (const control of desktopOnly) assert.notInclude(html, control, control)
  assert.include(html, 'aria-label="Send"')
  assert.notInclude(html, "Your phone can only view.")
})

test("a phone paired for View has no message box, and says it can only view (P10)", () => {
  session.value = { phone: true, scope: "view", device: "Pixel 8" }
  const html = render()
  for (const control of desktopOnly) assert.notInclude(html, control, control)
  assert.notInclude(html, 'aria-label="Send"')
  assert.include(html, "Your phone can only view.")
})

test("a phone whose session ended is offered to pair again", () => {
  session.value = { phone: true, lost: true }
  const html = render()
  assert.include(html, "Pair again")
  assert.notInclude(html, 'aria-label="Send"')
})

test("/pair is the pair page, with an empty code field when the link brought no code (P4b)", () => {
  session.value = { phone: true, lost: true }
  const html = render("/pair")
  assert.include(html, "Enter the pairing code shown on your computer.")
  assert.match(html, /<input[^>]*id="pair-code"[^>]*value=""/)
  assert.include(html, ">Pair</button>")
})

test("a phone has no Settings page: /settings shows the chats", () => {
  session.value = { phone: true, scope: "chat+approve", device: "Pixel 8" }
  const html = render("/settings/phones")
  assert.notInclude(html, "Back to chats")
  assert.include(html, 'aria-label="Chats"')
})

test("a browser on the computer without Handful's key gets the key page, with the key file's path", () => {
  session.value = { phone: false, key: false, keyFile: "/srv/h/ui-key" }
  const html = render()
  assert.include(html, "Handful needs its key on this browser.")
  assert.include(html, "cat /srv/h/ui-key</code>")
  assert.notInclude(html, "pbcopy")
  assert.notInclude(html, 'aria-label="Send"')
})

test("a browser with the key, and a phone, get no key page", () => {
  for (const value of [
    { phone: false, key: true },
    { phone: true, scope: "chat" as const, device: "Pixel 8" },
  ]) {
    session.value = value
    assert.notInclude(render(), "Handful needs its key", JSON.stringify(value))
  }
})
