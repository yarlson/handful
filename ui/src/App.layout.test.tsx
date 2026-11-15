import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { Bot } from "@/lib/api"
import { App } from "./App.tsx"

// The store reads the server's bots in an effect, which a static render doesn't run, so the
// test hands App a team as the store's first state.
const team: Bot = {
  id: "web",
  kind: "room",
  name: "Web app",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  members: [{ id: "lead", name: "Lead" }],
  team: { leadId: "lead", agreement: "", goalBudget: 10 },
} as Bot
vi.mock("@/lib/store", () => ({
  useHandful: () => ({
    bots: [team],
    transcripts: new Map(),
    transcriptStatus: new Map(),
    unread: new Set(),
    link: "live",
    error: null,
    setError: () => {},
    pausedUntil: null,
    draining: false,
    restartRev: 0,
    select: async () => {},
    loadOlder: async () => {},
    reveal: async () => false,
    refreshBots: async () => {},
    reorder: async () => {},
  }),
}))

// render renders the app at an address in a window of a width, with the stored layout choices,
// and returns its markup.
function render(width: number, address = "/chat/web", stored: Record<string, string> = {}): string {
  const url = new URL(address, "http://localhost")
  const location = { pathname: url.pathname, search: url.search, origin: url.origin }
  vi.stubGlobal("location", location)
  vi.stubGlobal("localStorage", { getItem: (k: string) => stored[k] ?? null, setItem: () => {}, removeItem: () => {} })
  vi.stubGlobal("window", {
    location,
    innerWidth: width,
    innerHeight: 900,
    matchMedia: (q: string) => ({ matches: q === "(max-width: 47.99rem)" && width < 768 }),
    addEventListener: () => {},
    removeEventListener: () => {},
  })
  return renderToStaticMarkup(<App />)
}

function mainOf(html: string) {
  return html.slice(html.indexOf("<main"), html.indexOf("</main>") + "</main>".length)
}

// tag is the opening tag of the first element that has the attribute given.
function tag(html: string, attribute: string) {
  const at = html.indexOf(attribute)
  assert.isAtLeast(at, 0, `no element with ${attribute}`)
  return html.slice(html.lastIndexOf("<", at), html.indexOf(">", at) + 1)
}

afterEach(() => {
  vi.unstubAllGlobals()
})

test("the chat panel is a full-height column of main, beside the column with the chat's header", () => {
  const main = mainOf(render(1440))
  // main holds two columns: the chat's column, which starts with its header, then the panel
  // with its handle, the last children of main. The panel holds no other aside.
  assert.match(
    main,
    /^<main class="flex min-w-0 flex-1"><div class="flex min-w-0 flex-1 flex-col"><div class="relative flex min-h-0 flex-1 flex-col"><header /,
  )
  assert.match(
    main,
    /<\/div><div role="separator"[^>]*><\/div><aside id="chat-panel"[^>]*>(?:(?!<aside).)*<\/aside><\/main>$/s,
  )
  const panel = main.slice(main.indexOf('<aside id="chat-panel"'))
  assert.include(
    tag(panel, 'id="chat-panel"'),
    "bg-sidebar text-sidebar-foreground",
    "the panel has the sidebar's colors",
  )
  assert.include(tag(panel, 'id="chat-panel"'), "width:320px")
  assert.notInclude(panel, 'aria-label="Toggle panel"', "the toggle stays in the chat's header")
})

test("the panel's handle says its width and its bounds, and the toggle says the panel is open", () => {
  const html = render(1440)
  const handle = tag(html, 'aria-label="Resize panel"')
  assert.include(handle, 'aria-controls="chat-panel"')
  assert.include(handle, 'aria-valuenow="320"')
  assert.include(handle, 'aria-valuemin="280"')
  assert.include(handle, 'aria-valuemax="560"')
  const toggle = tag(html, 'aria-label="Toggle panel"')
  assert.include(toggle, 'aria-expanded="true"')
  assert.include(toggle, 'aria-controls="chat-panel"')
  assert.include(tag(html, 'aria-label="Resize sidebar"'), 'aria-controls="app-sidebar"')
})

test("a stored panel width shows only as wide as the chat's 480 pixels allow", () => {
  // 1280 - (240 + 4) sidebar - 4 handle - 480 chat leaves 552.
  const handle = tag(render(1280, "/chat/web", { "handful.panelWidth": "560" }), 'aria-label="Resize panel"')
  assert.include(handle, 'aria-valuenow="552"')
  assert.include(handle, 'aria-valuemax="552"')
})

test("a narrow window opens the chat with its panel closed", () => {
  const html = render(900)
  assert.notInclude(mainOf(html), "<aside")
  assert.include(tag(html, 'aria-label="Toggle panel"'), 'aria-expanded="false"')
})

test("an address with a panel tab opens the panel on that tab, over the chat in a narrow window", () => {
  const main = mainOf(render(900, "/chat/web?tab=members"))
  const panel = tag(main, 'id="chat-panel"')
  assert.include(panel, "absolute")
  assert.notInclude(main, 'aria-label="Resize panel"', "a panel over the chat has no handle")
  assert.match(main, /<button[^>]*role="tab"[^>]*aria-selected="true"[^>]*>Members</)
})

test("an address with an artifact opens the reader over the chat, which the keyboard can't reach", () => {
  const main = mainOf(render(1440, "/chat/web?artifact=a1"))
  assert.include(main, '<section aria-label="Loading artifact"')
  assert.match(main, /<header inert=""/)
})

test("a stored collapsed sidebar stays hidden, with a button to open it", () => {
  const html = render(1440, "/chat/web", { "handful.sidebarCollapsed": "1" })
  assert.include(tag(html, 'id="app-sidebar"'), "hidden")
  assert.notInclude(html, 'aria-label="Resize sidebar"')
  assert.include(tag(mainOf(html), 'aria-label="Open sidebar"'), 'aria-expanded="false"')
  // With no sidebar, 1440 - 4 - 480 leaves the panel its whole 560.
  assert.include(
    tag(
      render(1440, "/chat/web", { "handful.sidebarCollapsed": "1", "handful.panelWidth": "560" }),
      'aria-label="Resize panel"',
    ),
    'aria-valuemax="560"',
  )
})

test("Settings show in the main area, beside the sidebar", () => {
  const html = render(1440, "/settings/general")
  assert.include(html, 'id="app-sidebar"')
  assert.include(mainOf(html), 'aria-labelledby="settings-heading"')
  assert.notInclude(mainOf(html), 'id="chat-panel"')
})
