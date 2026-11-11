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

// render renders the app on the team's chat in a window wide enough, or not, for the panel,
// and returns the markup of its main element.
function render(wide: boolean): string {
  vi.stubGlobal("location", { pathname: "/chat/web", origin: "http://localhost" })
  vi.stubGlobal("localStorage", { getItem: () => null, setItem: () => {}, removeItem: () => {} })
  vi.stubGlobal("window", {
    location: { pathname: "/chat/web", origin: "http://localhost" },
    matchMedia: (q: string) => ({ matches: wide && q === "(min-width: 64rem)" }),
    innerHeight: 900,
  })
  const html = renderToStaticMarkup(<App />)
  return html.slice(html.indexOf("<main"), html.indexOf("</main>") + "</main>".length)
}

afterEach(() => {
  vi.unstubAllGlobals()
})

test("the chat panel is a full-height column of main, beside the column with the chat's header", () => {
  const main = render(true)
  // main holds two columns: the chat's column, which starts with its header, then the panel,
  // which is main's last child. The panel holds no other aside.
  assert.match(main, /^<main class="flex min-w-0 flex-1"><div class="flex min-w-0 flex-1 flex-col"><header /)
  assert.match(main, /<\/div><aside aria-label="Chat panel" class="[^"]*"[^>]*>(?:(?!<aside).)*<\/aside><\/main>$/s)
  const panel = main.slice(main.indexOf('<aside aria-label="Chat panel"'))
  assert.include(
    panel.slice(0, panel.indexOf(">")),
    "bg-sidebar text-sidebar-foreground",
    "the panel has the sidebar's colors",
  )
  assert.notInclude(panel, 'aria-label="Toggle panel"', "the toggle stays in the chat's header")
})

test("a narrow window opens the chat with its panel closed", () => {
  const main = render(false)
  assert.notInclude(main, "<aside")
  assert.include(main, 'aria-label="Toggle panel"')
})
