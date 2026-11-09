import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { ActivityBody, Bot, Entry } from "@/lib/api"
import { EntryView, MarkdownText } from "./chat.tsx"

afterEach(() => {
  vi.unstubAllGlobals()
})

test("an external link opens in a new tab and a relative link does not", () => {
  vi.stubGlobal("window", { location: { origin: "http://localhost:4317" } })
  const html = renderToStaticMarkup(
    <MarkdownText text={'[x](https://example.com "Example") [y](/rel) [z](http://localhost:4317/a)'} />,
  )
  assert.include(html, '<a href="https://example.com" target="_blank" rel="noopener noreferrer" title="Example">x</a>')
  assert.include(html, '<a href="/rel">y</a>')
  assert.include(html, '<a href="http://localhost:4317/a">z</a>')
  assert.notInclude(html, "node=")
})

const base = { seq: 1, rev: 1, turnId: "t", createdAt: 0 }
const room: Bot = {
  id: "room-1",
  kind: "room",
  name: "Dev",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  members: [
    { id: "core-id", name: "Core Dev" },
    { id: "ui-id", name: "UI Dev" },
  ],
}
const solo: Bot = { ...room, id: "solo-id", kind: "agent", name: "Solo", members: undefined }

// colorOf renders an entry and returns the color class on the bot's name.
function colorOf(entry: Entry, chat: Bot) {
  const html = renderToStaticMarkup(<EntryView entry={entry} chat={chat} skill={null} />)
  return /text-(sky|emerald|rose|violet|fuchsia|lime)-600/.exec(html)?.[0]
}
const post = (chat: Bot, speakerId?: string, speakerName?: string): Entry => ({
  ...base,
  id: "p",
  botId: chat.id,
  kind: "bot",
  body: { text: "hi", fallback: false, speakerId, speakerName },
})
const tool = (chat: Bot, body: Partial<ActivityBody>): Entry => ({
  ...base,
  id: "a",
  botId: chat.id,
  kind: "activity",
  body: { tool: "Bash", summary: "ls", status: "done", subagent: false, ...body },
})

test("a bot's tool rows have the color of its posts", () => {
  const core = colorOf(post(room, "core-id", "Core Dev"), room)
  assert.isDefined(core)
  // In a room the row names the bot; a subagent's row too; a newer server also sends its id.
  assert.equal(colorOf(tool(room, { bot: "Core Dev" }), room), core)
  assert.equal(colorOf(tool(room, { bot: "Core Dev", subagent: true }), room), core)
  assert.equal(colorOf(tool(room, { bot: "Core Dev", botId: "core-id" }), room), core)
  // In a bot's own chat, its rows match its posts.
  assert.equal(colorOf(tool(solo, { bot: "Solo" }), solo), colorOf(post(solo), solo))
})

test("a model error names the problem as an alert with a way to the settings", () => {
  const entry: Entry = {
    ...base,
    id: "m",
    botId: "solo-id",
    kind: "event",
    body: {
      type: "model-error",
      text: "Claude Code couldn't run model `claude-foo` (set on this bot): not found",
      botId: "solo-id",
    },
  }
  const html = renderToStaticMarkup(<EntryView entry={entry} chat={solo} skill={null} />)
  assert.include(html, 'role="alert"')
  assert.include(html, "claude-foo")
  assert.include(html, "Open settings")
  assert.include(html, "text-destructive")
  const mismatch = renderToStaticMarkup(
    <EntryView
      entry={{ ...entry, body: { ...entry.body, type: "model-mismatch", text: "Asked for opus" } }}
      chat={solo}
      skill={null}
    />,
  )
  assert.include(mismatch, "amber")
  assert.include(mismatch, 'role="alert"')
})

test("a post that records its model makes the speaker a focusable button for its tooltip", () => {
  const withModel: Entry = {
    ...base,
    id: "p",
    botId: solo.id,
    kind: "bot",
    body: { text: "hi", fallback: false, model: "opus", effort: "high" },
  }
  assert.include(
    renderToStaticMarkup(<EntryView entry={withModel} chat={solo} skill={null} />),
    '<button type="button"',
  )
  assert.notInclude(
    renderToStaticMarkup(<EntryView entry={post(solo)} chat={solo} skill={null} />),
    '<button type="button" class="cursor-default',
  )
})

test("a post loads the app's own images, and shows other sites' images as links", () => {
  vi.stubGlobal("window", { location: { origin: "http://localhost:4317" } })
  const html = renderToStaticMarkup(
    <MarkdownText
      text={"![chart](/api/media/c.png) ![logo](https://example.com/l.png?t=1) ![](https://cdn.example.org/x.png)"}
    />,
  )
  assert.include(html, '<img src="/api/media/c.png" alt="chart"/>')
  assert.notInclude(html, 'src="https://example.com')
  assert.notInclude(html, '<img src="https://cdn')
  assert.include(html, 'href="https://example.com/l.png?t=1" target="_blank" rel="noopener noreferrer"')
  assert.include(html, "logo (example.com)")
  assert.include(html, "Image (cdn.example.org)")
})
