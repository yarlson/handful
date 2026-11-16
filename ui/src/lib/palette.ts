import type { Bot } from "@/lib/api"
import { THEMES, type Appearance } from "@/lib/appearance"
import { matchesName } from "@/lib/search"
import { SECTIONS, type Section } from "@/lib/settings"
import type { ShortcutName } from "@/lib/shortcuts"

// Action is what a palette command does when it runs.
export type Action =
  | { kind: "chat"; chat: Bot }
  | { kind: "waiting" }
  | { kind: "settings"; section: Section }
  | { kind: "new"; what: "bot" | "team" | "room" }
  | { kind: "sidebar" }
  | { kind: "panel" }
  | { kind: "search" }
  | { kind: "theme"; theme: Appearance["theme"] }

export type Command = {
  id: string
  group: "Chats" | "Go to" | "Actions"
  label: string
  action: Action
  shortcut?: ShortcutName
}

// Context is what the palette may offer: the chats in sidebar order, whether this device may
// change the computer's setup (create chats, open every Settings section) and whether a chat is
// open, for its panel and search.
export type Context = { chats: Bot[]; computer: boolean; chatOpen: boolean }

// commandsFor lists every command that the device may run where it is now, in the palette's
// order: the chats, then the places to go, then the actions.
export function commandsFor({ chats, computer, chatOpen }: Context): Command[] {
  const commands: Command[] = chats.map((chat) => ({
    id: `chat:${chat.id}`,
    group: "Chats",
    label: chat.name,
    action: { kind: "chat", chat },
  }))
  commands.push({ id: "waiting", group: "Go to", label: "Waiting for you", action: { kind: "waiting" } })
  for (const s of SECTIONS) {
    if (!computer && s.id !== "appearance") continue
    commands.push({
      id: `settings:${s.id}`,
      group: "Go to",
      label: `Settings: ${s.label}`,
      action: { kind: "settings", section: s.id },
    })
  }
  if (computer)
    for (const what of ["bot", "team", "room"] as const)
      commands.push({ id: `new:${what}`, group: "Actions", label: `New ${what}`, action: { kind: "new", what } })
  commands.push({
    id: "sidebar",
    group: "Actions",
    label: "Toggle sidebar",
    action: { kind: "sidebar" },
    shortcut: "sidebar",
  })
  if (chatOpen) {
    commands.push(
      { id: "panel", group: "Actions", label: "Toggle panel", action: { kind: "panel" }, shortcut: "panel" },
      { id: "search", group: "Actions", label: "Search in this chat", action: { kind: "search" }, shortcut: "search" },
    )
  }
  for (const t of THEMES)
    commands.push({
      id: `theme:${t.value}`,
      group: "Actions",
      label: `Theme: ${t.label}`,
      action: { kind: "theme", theme: t.value },
    })
  return commands
}

// matching keeps the commands whose label contains query, with no case.
export function matching(commands: Command[], query: string): Command[] {
  return commands.filter((c) => matchesName(c.label, query))
}

// moveActive is the active row after an arrow key in a list of count rows; it wraps at both ends.
export function moveActive(active: number, count: number, key: "ArrowDown" | "ArrowUp"): number {
  if (count === 0) return 0
  return (active + (key === "ArrowDown" ? 1 : -1) + count) % count
}
