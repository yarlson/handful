import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Bot } from "@/lib/api"
import { botColor } from "@/lib/bot-color"
import { SidebarItem } from "../App.tsx"
import { BotHuesProvider } from "./bot-colors.tsx"
import { EntryView, SuggestionIcon, suggestionsFor } from "./chat.tsx"

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

test("a room's sidebar row keeps its plain icon, in the muted colour", () => {
  const html = row(chat({ id: "room-ship", kind: "room", name: "Ship" }))
  assert.include(iconClass(html), "lucide-messages-square")
  assert.include(iconClass(html), "text-muted-foreground")
  assert.isFalse(coloured(html, "room-ship"), iconClass(html))
  assert.notInclude(html, "<mask ")
})

// Ana's id alone gives fuchsia; the server stored lime for her.
const ana = chat({ id: "bot-ana", name: "Ana", hue: "lime" })
const withHues = (node: React.ReactNode) => renderToStaticMarkup(<BotHuesProvider bots={[ana]}>{node}</BotHuesProvider>)

test("a bot shows in the hue the server stored for it, not the one its id gives", () => {
  const html = withHues(
    <SidebarItem bot={ana} selected={false} unread={false} onSelect={() => {}} onKeyDown={() => {}} />,
  )
  assert.include(iconClass(html), "text-lime-700 dark:text-lime-400")
  assert.notInclude(html, "fuchsia")
})

test("a bot's post shows its dot and name in its stored hue, with no letter avatar", () => {
  const room = chat({ id: "room-1", kind: "room", name: "Dev", members: [{ id: "bot-ana", name: "Ana" }] })
  const entry = {
    id: "p",
    seq: 1,
    rev: 1,
    turnId: "t",
    createdAt: 0,
    botId: "room-1",
    kind: "bot",
    body: { text: "hi", fallback: false, speakerId: "bot-ana", speakerName: "Ana" },
  } as const
  const html = withHues(<EntryView entry={entry} chat={room} skillIds={[]} />)
  assert.include(iconClass(html), "size-5 text-lime-700 dark:text-lime-400")
  assert.include(html, "<mask ")
  assert.include(html, 'font-semibold text-lime-700 dark:text-lime-400">Ana<')
  assert.notInclude(html, ">A</")
  assert.notInclude(html, "fuchsia")
})

test("a bot in the @ menu carries its id and shows in its colour; @here shows a muted team", () => {
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
  const here = renderToStaticMarkup(<SuggestionIcon s={list[0]} />)
  assert.include(iconClass(here), "text-muted-foreground")
  assert.equal((here.match(/<circle /g) ?? []).length, 3)
})
