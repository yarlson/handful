import { afterEach, assert, test, vi } from "vitest"
import { artifactUrl, chatPlace, chatUrl, lastChat, panelTabs, postUrl, saveLastChat, shownTab } from "./place"

afterEach(() => {
  vi.unstubAllGlobals()
})

const chat = "6f1c2b9e-0d1a-4c55-9a43-1f2e3d4c5b6a"

test("a chat's address holds its panel tab, its reader and its post", () => {
  assert.deepEqual(chatPlace(`/chat/${chat}`, ""), { chat, tab: null, reading: null, post: null })
  assert.deepEqual(chatPlace(`/chat/${chat}`, "?tab=skills"), { chat, tab: "skills", reading: null, post: null })
  assert.deepEqual(chatPlace(`/chat/${chat}`, "?tab=artifacts&artifact=a1"), {
    chat,
    tab: "artifacts",
    reading: { kind: "artifact", id: "a1" },
    post: null,
  })
  assert.deepEqual(chatPlace(`/chat/${chat}`, "?skill=release-notes"), {
    chat,
    tab: null,
    reading: { kind: "skill", id: "release-notes" },
    post: null,
  })
  assert.deepEqual(chatPlace(`/chat/${chat}`, "?post=e9"), { chat, tab: null, reading: null, post: "e9" })
})

test("a tab that no panel has, and an empty post, count as none", () => {
  assert.deepEqual(chatPlace(`/chat/${chat}`, "?tab=secrets&post="), { chat, tab: null, reading: null, post: null })
})

test("an artifact wins over a skill when an address has both", () => {
  assert.deepEqual(chatPlace(`/chat/${chat}`, "?skill=s&artifact=a")?.reading, { kind: "artifact", id: "a" })
})

test("an address that is not a chat's has no chat place", () => {
  assert.isNull(chatPlace("/", ""))
  assert.isNull(chatPlace("/settings/general", "?tab=memory"))
  assert.isNull(chatPlace("/needs-you", ""))
  assert.isNull(chatPlace(`/chat/${chat}/more`, ""))
  assert.isNull(chatPlace(`/settings/chat/${chat}`, ""))
  assert.isNull(chatPlace("/chat/", ""))
})

test("a chat's place reads back from its address", () => {
  const places = [
    { chat, tab: null, reading: null, post: null },
    { chat, tab: "settings" as const, reading: null, post: null },
    { chat, tab: "skills" as const, reading: { kind: "skill" as const, id: "a b&c" }, post: null },
    { chat, tab: null, reading: { kind: "artifact" as const, id: "x" }, post: "e/1?" },
  ]
  for (const p of places) {
    const url = new URL(chatUrl(p), "http://localhost")
    assert.deepEqual(chatPlace(url.pathname, url.search), p, chatUrl(p))
  }
  assert.equal(chatUrl({ chat }), `/chat/${chat}`)
  assert.equal(
    chatUrl({ chat, tab: "artifacts", reading: { kind: "artifact", id: "a1" }, post: "e2" }),
    `/chat/${chat}?tab=artifacts&artifact=a1&post=e2`,
  )
})

test("a link to a post or an artifact is the full address", () => {
  assert.equal(postUrl("http://192.168.1.5:7777", chat, "e9"), `http://192.168.1.5:7777/chat/${chat}?post=e9`)
  assert.equal(artifactUrl("http://localhost:7777", chat, "a1"), `http://localhost:7777/chat/${chat}?artifact=a1`)
})

test("each kind of chat has its own panel tabs, and a phone has one", () => {
  const tabs = (bot: { kind: string; team?: unknown }, computer: boolean) =>
    panelTabs(bot, computer)
      .map((t) => `${t.tab}:${t.label}`)
      .join(" ")
  assert.equal(
    tabs({ kind: "agent" }, true),
    "memory:Memory routines:Routines skills:Skills artifacts:Artifacts settings:Settings",
  )
  assert.equal(tabs({ kind: "room" }, true), "members:Members settings:Settings")
  assert.equal(tabs({ kind: "room", team: {} }, true), "board:Work members:Members memory:Memory settings:Settings")
  assert.equal(tabs({ kind: "room", team: {} }, false), "board:Work")
  assert.equal(tabs({ kind: "agent" }, false), "artifacts:Artifacts")
  assert.equal(tabs({ kind: "room" }, false), "artifacts:Artifacts")
})

test("the panel shows the tab asked when it has it, else its first", () => {
  const room = panelTabs({ kind: "room" }, true)
  assert.equal(shownTab(room, "settings"), "settings")
  assert.equal(shownTab(room, "skills"), "members", "a room has no skills")
  assert.equal(shownTab(room, null), "members")
})

// session stubs sessionStorage with the entries given.
function session(entries: Record<string, string> = {}) {
  const store = new Map(Object.entries(entries))
  vi.stubGlobal("sessionStorage", {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => store.set(k, v),
  })
  return store
}

test("the last chat's place is remembered for the window, without its post", () => {
  const store = session()
  assert.isNull(lastChat(), "nothing remembered")
  saveLastChat({ chat, tab: "memory", reading: { kind: "artifact", id: "a1" }, post: "e9" })
  assert.equal(store.get("handful.lastChat"), `/chat/${chat}?tab=memory&artifact=a1`)
  assert.deepEqual(lastChat(), { chat, tab: "memory", reading: { kind: "artifact", id: "a1" }, post: null })
  saveLastChat({ chat, tab: null, reading: null, post: null })
  assert.deepEqual(lastChat(), { chat, tab: null, reading: null, post: null })
})

test("a remembered address that is not a chat's is not used", () => {
  session({ "handful.lastChat": "/settings/general" })
  assert.isNull(lastChat())
  session({ "handful.lastChat": "//example.com/chat/x" })
  assert.isNull(lastChat())
  session({ "handful.lastChat": "//[" })
  assert.isNull(lastChat(), "not an address")
})

test("storage that refuses reads and writes remembers no chat, without an error", () => {
  vi.stubGlobal("sessionStorage", {
    getItem: () => {
      throw new Error("denied")
    },
    setItem: () => {
      throw new Error("denied")
    },
  })
  saveLastChat({ chat, tab: null, reading: null, post: null })
  assert.isNull(lastChat())
})
