// A chat's address: /chat/<bot id>, with what else is open in the query: the panel's tab
// (?tab=), an artifact or a skill in the reader (?artifact= or ?skill=), and a post to show
// (?post=). Bot and entry ids are UUIDs; a skill id is made from its name.

export const PANEL_TABS = ["board", "members", "memory", "routines", "skills", "artifacts", "settings"] as const
export type PanelTab = (typeof PANEL_TABS)[number]

// Reading is what the reader shows: an artifact or a skill, by id.
export type Reading = { kind: "artifact" | "skill"; id: string }

export type ChatPlace = { chat: string; tab: PanelTab | null; reading: Reading | null; post: string | null }

const CHAT_PATH = /^\/chat\/([^/]+)$/

// chatPlace reads a chat's place from a path and its query; a path that is not a chat's is null.
// A tab that no panel has counts as none.
export function chatPlace(pathname: string, search: string): ChatPlace | null {
  const chat = CHAT_PATH.exec(pathname)?.[1]
  if (!chat) return null
  const q = new URLSearchParams(search)
  const tab = PANEL_TABS.find((t) => t === q.get("tab")) ?? null
  const artifact = q.get("artifact")
  const skill = q.get("skill")
  const reading: Reading | null = artifact
    ? { kind: "artifact", id: artifact }
    : skill
      ? { kind: "skill", id: skill }
      : null
  return { chat, tab, reading, post: q.get("post") || null }
}

// chatUrl is the address of a chat's place.
export function chatUrl({ chat, tab = null, reading = null, post = null }: Partial<ChatPlace> & { chat: string }) {
  const q = new URLSearchParams()
  if (tab) q.set("tab", tab)
  if (reading) q.set(reading.kind, reading.id)
  if (post) q.set("post", post)
  const query = q.toString()
  return `/chat/${chat}${query ? `?${query}` : ""}`
}

// postUrl is the full link to a post, to copy and share.
export function postUrl(origin: string, chat: string, post: string) {
  return origin + chatUrl({ chat, post })
}

// artifactUrl is the full link to an artifact, opened in the reader of a chat.
export function artifactUrl(origin: string, chat: string, id: string) {
  return origin + chatUrl({ chat, reading: { kind: "artifact", id } })
}

// LAST_CHAT_KEY keeps the last chat address in sessionStorage, which a full load of the same
// window keeps: the desktop app opens Settings with one.
const LAST_CHAT_KEY = "handful.lastChat"

// PLACE_ORIGIN resolves a remembered address; one that resolves to another origin is not a chat's.
const PLACE_ORIGIN = "http://place.invalid"

// saveLastChat remembers a chat's place, without its post, for Settings to close back to.
export function saveLastChat(place: ChatPlace) {
  try {
    sessionStorage.setItem(LAST_CHAT_KEY, chatUrl({ ...place, post: null }))
  } catch {
    // Not kept: Settings then close to the selected chat.
  }
}

// lastChat is the remembered chat's place, or null when there is none or it is not a chat's.
export function lastChat(): ChatPlace | null {
  let url: string | null = null
  try {
    url = sessionStorage.getItem(LAST_CHAT_KEY)
  } catch {
    // Not readable: no chat is remembered.
  }
  // Stryker disable next-line ConditionalExpression: without it, null parses as the address /null, which is not a chat's
  if (!url) return null
  let address: URL
  try {
    address = new URL(url, PLACE_ORIGIN)
  } catch {
    return null
  }
  return address.origin === PLACE_ORIGIN ? chatPlace(address.pathname, address.search) : null
}

type PanelBot = { kind: string; team?: unknown }

// panelTabs are the panel's tabs for a chat, in order. A phone has the team's Work, or else the
// artifacts; the other tabs change settings.
export function panelTabs(bot: PanelBot, computer: boolean): { tab: PanelTab; label: string }[] {
  const team = Boolean(bot.team)
  if (!computer) return team ? [{ tab: "board", label: "Work" }] : [{ tab: "artifacts", label: "Artifacts" }]
  if (bot.kind === "room")
    return [
      ...(team ? [{ tab: "board" as const, label: "Work" }] : []),
      { tab: "members", label: "Members" },
      ...(team ? [{ tab: "memory" as const, label: "Memory" }] : []),
      { tab: "settings", label: "Settings" },
    ]
  return [
    { tab: "memory", label: "Memory" },
    { tab: "routines", label: "Routines" },
    { tab: "skills", label: "Skills" },
    { tab: "artifacts", label: "Artifacts" },
    { tab: "settings", label: "Settings" },
  ]
}

// shownTab is the tab the panel shows for the tab asked: that tab when the panel has it, else
// its first.
export function shownTab(tabs: { tab: PanelTab }[], asked: PanelTab | null): PanelTab {
  return tabs.find((t) => t.tab === asked)?.tab ?? tabs[0].tab
}
