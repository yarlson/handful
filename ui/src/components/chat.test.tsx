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
  const html = renderToStaticMarkup(<EntryView entry={entry} chat={chat} skillIds={[]} />)
  return /text-(sky|emerald|violet|fuchsia|lime)-700/.exec(html)?.[0]
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

// A row leaves the page when it scrolls far from the view, so its chat keeps whether a tool
// run is expanded.
test("a tool run shows its output only while its chat has it expanded", () => {
  const run = tool(solo, { output: "file.txt" })
  const render = (expanded?: boolean) =>
    renderToStaticMarkup(<EntryView entry={run} chat={solo} skillIds={[]} expanded={expanded} />)
  assert.notInclude(render(), "file.txt", "a tool run starts collapsed")
  assert.notInclude(render(false), "file.txt")
  assert.include(render(true), "<pre")
  assert.include(render(true), "file.txt")
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
  const html = renderToStaticMarkup(<EntryView entry={entry} chat={solo} skillIds={[]} />)
  assert.include(html, 'role="alert"')
  assert.include(html, "claude-foo")
  assert.include(html, "Open settings")
  assert.include(html, "text-destructive")
  const mismatch = renderToStaticMarkup(
    <EntryView
      entry={{ ...entry, body: { ...entry.body, type: "model-mismatch", text: "Asked for opus" } }}
      chat={solo}
      skillIds={[]}
    />,
  )
  assert.include(mismatch, "text-attention-foreground")
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
    renderToStaticMarkup(<EntryView entry={withModel} chat={solo} skillIds={[]} />),
    '<button type="button"',
  )
  assert.notInclude(
    renderToStaticMarkup(<EntryView entry={post(solo)} chat={solo} skillIds={[]} />),
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

test("an open question is marked in words and offers Answer; a closed one says how it closed", () => {
  const ask = (questionState: "open" | "answered" | "dismissed" | "withdrawn"): Entry => ({
    ...base,
    id: "q1",
    botId: solo.id,
    kind: "bot",
    body: {
      text: "Ship it now?\nDetails.",
      fallback: false,
      needsReply: true,
      question: "Ship it now?",
      questionState,
    },
  })
  const open = renderToStaticMarkup(<EntryView entry={ask("open")} chat={solo} skillIds={[]} onAnswer={() => {}} />)
  assert.include(open, 'id="entry-q1"')
  assert.include(open, 'tabindex="-1"')
  assert.match(open, /data-variant="attention"[^>]*>Open question</)
  assert.include(open, "border-attention")
  assert.match(open, /data-variant="outline" data-size="sm" class="[^"]*" aria-label="Answer: Ship it now\?"/)
  const answered = renderToStaticMarkup(
    <EntryView entry={ask("answered")} chat={solo} skillIds={[]} onAnswer={() => {}} />,
  )
  assert.include(answered, "Answered")
  assert.notInclude(answered, "border-attention")
  assert.notInclude(answered, "Answer: ")
  assert.include(
    renderToStaticMarkup(<EntryView entry={ask("withdrawn")} chat={solo} skillIds={[]} />),
    "No longer needed",
  )
  assert.include(renderToStaticMarkup(<EntryView entry={ask("dismissed")} chat={solo} skillIds={[]} />), "Dismissed")
  assert.notInclude(renderToStaticMarkup(<EntryView entry={post(solo)} chat={solo} skillIds={[]} />), 'id="entry-')
})

const user = (text: string): Entry => ({ ...base, id: "u", botId: solo.id, kind: "user", body: { text } })

test("a code block has a header with its language, Wrap and Copy; inline code has none", () => {
  const html = renderToStaticMarkup(<MarkdownText text={"Run `ls` now.\n\n```go\nfmt.Println(1)\n```"} />)
  assert.match(html, /<span class="[^"]*font-mono[^"]*">go<\/span>/)
  const buttons = html.match(/<button[^>]*>/g) ?? []
  assert.match(buttons[0] ?? "", /aria-pressed="false"[^>]*title="Wrap long lines"/)
  assert.match(html, /<button[^>]*>.*Copy<\/button>/)
  for (const button of buttons) {
    assert.include(button, "aria-pressed:bg-foreground/10", "a pressed or hovered button shows on the muted header")
    assert.include(button, "hover:bg-foreground/10")
  }
  assert.include(html, '<code class="language-go">fmt.Println(1)\n</code>')
  assert.include(html, "<code>ls</code>")
  assert.equal(html.match(/<button/g)?.length, 2, "only the code block has buttons")
})

test("a code block without a language says text", () => {
  for (const text of ["```\nplain\n```", "    indented"]) {
    assert.match(renderToStaticMarkup(<MarkdownText text={text} />), /font-mono[^"]*">text<\/span>/, text)
  }
})

test("a table has Copy table", () => {
  const html = renderToStaticMarkup(<MarkdownText text={"| a | b |\n| - | - |\n| 1 | 2 |"} />)
  assert.match(html, /<table>.*<\/table><\/div><button[^>]*>.*Copy table<\/button>/)
})

test("an expanded tool run's output has Copy", () => {
  const html = renderToStaticMarkup(
    <EntryView entry={tool(solo, { output: "file.txt" })} chat={solo} skillIds={[]} expanded />,
  )
  assert.match(html, /<pre[^>]*>file.txt<\/pre><button[^>]*>.*Copy<\/button>/)
})

test("a message's Copy shows at rest on a touch screen", () => {
  const html = renderToStaticMarkup(<EntryView entry={post(solo)} chat={solo} skillIds={[]} />)
  const button = html.match(/<button[^>]*aria-label="Copy message"[^>]*>/)?.[0] ?? ""
  assert.include(button, " opacity-0 ", "hidden at rest on a desktop")
  assert.include(button, "pointer-coarse:opacity-100")
})

test("a user's message renders markdown, keeps typed line breaks and has the code block header", () => {
  const html = renderToStaticMarkup(
    <EntryView entry={user("**Fix** this:\nline two\n\n```sh\nmake check\n```")} chat={solo} skillIds={[]} />,
  )
  assert.include(html, "<strong>Fix</strong> this:<br/>\nline two")
  assert.match(html, /font-mono[^"]*">sh<\/span>/)
  assert.include(html, '<code class="language-sh">make check\n</code>')
})

test("raw HTML in a user's message shows as text", () => {
  const html = renderToStaticMarkup(
    <EntryView entry={user('<script>alert(1)</script>\n\n<img src=x onerror="alert(2)">')} chat={solo} skillIds={[]} />,
  )
  assert.notInclude(html, "<script")
  assert.notInclude(html, "<img")
  assert.include(html, "&lt;script&gt;alert(1)&lt;/script&gt;")
  assert.include(html, "&lt;img src=x onerror=&quot;alert(2)&quot;&gt;")
})
