import { assert, test } from "vitest"
import type { Bot } from "./api"
import { commandsFor, matching, moveActive } from "./palette"

const chat = (id: string, name: string, kind: Bot["kind"] = "agent"): Bot => ({
  id,
  kind,
  name,
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
})
const chats = [chat("a", "Scout"), chat("t", "Release", "room"), chat("r", "Planning", "room")]

test("on a computer with a chat open, the palette lists the chats, every place and every action", () => {
  const commands = commandsFor({ chats, computer: true, chatOpen: true })
  assert.deepEqual(
    commands.map((c) => [c.id, c.group, c.label, c.action.kind, c.shortcut ?? ""]),
    [
      ["chat:a", "Chats", "Scout", "chat", ""],
      ["chat:t", "Chats", "Release", "chat", ""],
      ["chat:r", "Chats", "Planning", "chat", ""],
      ["waiting", "Go to", "Waiting for you", "waiting", ""],
      ["settings:general", "Go to", "Settings: General", "settings", ""],
      ["settings:usage", "Go to", "Settings: Usage", "settings", ""],
      ["settings:appearance", "Go to", "Settings: Appearance", "settings", ""],
      ["settings:updates", "Go to", "Settings: Updates", "settings", ""],
      ["settings:connectors", "Go to", "Settings: Connectors", "settings", ""],
      ["settings:phones", "Go to", "Settings: Phones", "settings", ""],
      ["settings:runtime", "Go to", "Settings: Runtime", "settings", ""],
      ["settings:file", "Go to", "Settings: Settings file", "settings", ""],
      ["new:bot", "Actions", "New bot", "new", ""],
      ["new:team", "Actions", "New team", "new", ""],
      ["new:room", "Actions", "New room", "new", ""],
      ["sidebar", "Actions", "Toggle sidebar", "sidebar", "sidebar"],
      ["panel", "Actions", "Toggle panel", "panel", "panel"],
      ["search", "Actions", "Search in this chat", "search", "search"],
      ["theme:system", "Actions", "Theme: System", "theme", ""],
      ["theme:light", "Actions", "Theme: Light", "theme", ""],
      ["theme:dark", "Actions", "Theme: Dark", "theme", ""],
    ],
  )
  assert.deepEqual(commands[1].action, { kind: "chat", chat: chats[1] })
  assert.deepEqual(commands[6].action, { kind: "settings", section: "appearance" })
  assert.deepEqual(commands[13].action, { kind: "new", what: "team" })
  assert.deepEqual(commands[20].action, { kind: "theme", theme: "dark" })
})

test("a paired phone gets no New commands and only the Appearance section", () => {
  const labels = commandsFor({ chats, computer: false, chatOpen: true }).map((c) => c.label)
  assert.notInclude(labels, "New bot")
  assert.notInclude(labels, "New team")
  assert.notInclude(labels, "New room")
  assert.deepEqual(
    labels.filter((l) => l.startsWith("Settings")),
    ["Settings: Appearance"],
  )
  assert.include(labels, "Scout")
  assert.include(labels, "Toggle panel")
})

test("without an open chat, the palette has no panel or chat search", () => {
  const labels = commandsFor({ chats, computer: true, chatOpen: false }).map((c) => c.label)
  assert.notInclude(labels, "Toggle panel")
  assert.notInclude(labels, "Search in this chat")
  assert.include(labels, "Toggle sidebar")
  assert.include(labels, "Theme: Dark")
})

test("typing keeps the commands whose name contains the text, with no case", () => {
  const commands = commandsFor({ chats, computer: true, chatOpen: true })
  assert.deepEqual(
    matching(commands, "  DARK ").map((c) => c.label),
    ["Theme: Dark"],
  )
  assert.deepEqual(
    matching(commands, "pl").map((c) => c.label),
    ["Planning"],
  )
  assert.deepEqual(matching(commands, "zzz"), [])
  assert.equal(matching(commands, "").length, commands.length)
})

test("the arrow keys move the active row and wrap at both ends", () => {
  assert.equal(moveActive(0, 3, "ArrowDown"), 1)
  assert.equal(moveActive(2, 3, "ArrowDown"), 0)
  assert.equal(moveActive(1, 3, "ArrowUp"), 0)
  assert.equal(moveActive(0, 3, "ArrowUp"), 2)
  assert.equal(moveActive(0, 0, "ArrowDown"), 0, "no rows")
  assert.equal(moveActive(0, 0, "ArrowUp"), 0, "no rows")
})
