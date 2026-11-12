import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Bot } from "@/lib/api"
import { botColor } from "@/lib/bot-color"
import { SidebarItem } from "../App.tsx"
import { SuggestionIcon, suggestionsFor } from "./chat.tsx"

const chat = (x: Partial<Bot>): Bot => ({
  id: "x",
  kind: "agent",
  name: "X",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  ...x,
})
const row = (bot: Bot) =>
  renderToStaticMarkup(
    <SidebarItem bot={bot} selected={false} unread={false} onSelect={() => {}} onKeyDown={() => {}} />,
  )
// iconClass is the class of a row's first svg, its icon.
const iconClass = (html: string) => /<svg[^>]*class="([^"]*)"/.exec(html)?.[1] ?? ""
// coloured tells whether html's icon has every class of id's colour.
const coloured = (html: string, id: string) => {
  const classes = iconClass(html).split(" ")
  return botColor(id)
    .text.split(" ")
    .every((c) => classes.includes(c))
}

test("a bot's sidebar row shows a round bot in the bot's colour", () => {
  const html = row(chat({ id: "bot-ana", name: "Ana" }))
  assert.isTrue(coloured(html, "bot-ana"), iconClass(html))
  assert.include(html, 'fill="currentColor"')
  assert.include(html, "<mask ")
})

test("a team's sidebar row shows three outlined bots in the team's colour", () => {
  const team = chat({
    id: "team-docs",
    kind: "room",
    name: "Docs",
    team: { leadId: "a", agreement: "", goalBudget: 0 } as Bot["team"],
  })
  const html = row(team)
  assert.isTrue(coloured(html, "team-docs"), iconClass(html))
  assert.equal((html.match(/<circle /g) ?? []).length, 3)
})

test("a room's sidebar row keeps its plain icon, with no colour", () => {
  const html = row(chat({ id: "room-ship", kind: "room", name: "Ship" }))
  assert.include(iconClass(html), "lucide-messages-square")
  assert.isFalse(coloured(html, "room-ship"), iconClass(html))
  assert.notInclude(html, "<mask ")
})

test("a bot in the @ menu carries its id and shows in its colour; @here keeps its icon", () => {
  const members = [
    { id: "id-ana", name: "Ana" },
    { id: "id-bo", name: "Bo" },
  ]
  const list = suggestionsFor({ start: 0, query: "", mode: "mention" }, members, [], true, false)
  assert.deepEqual(
    list.map((s) => [s.token, s.kind, s.botId]),
    [
      ["@here", "everyone", undefined],
      ["@Ana", "bot", "id-ana"],
      ["@Bo", "bot", "id-bo"],
    ],
  )
  const ana = renderToStaticMarkup(<SuggestionIcon s={list[1]} />)
  assert.isTrue(coloured(ana, "id-ana"), iconClass(ana))
  assert.include(ana, "<mask ")
  assert.include(iconClass(renderToStaticMarkup(<SuggestionIcon s={list[0]} />)), "lucide-users")
})
