import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { ActivityBody, Bot, Entry } from "@/lib/api"
import type { TranscriptStatus } from "@/lib/transcript"
import { groupKey } from "@/lib/tool-group"
import { Chat, EntryView, MarkdownText, ToolGroup } from "./chat.tsx"

// session is what the server says of this browser; a test that needs a paired phone sets it.
const session = vi.hoisted(() => ({ phone: false, scope: "chat+approve" }))
vi.mock("@/lib/session", async (importOriginal) => {
  const { rightsOf } = await import("@/lib/phones")
  return { ...(await importOriginal<typeof import("@/lib/session")>()), useRights: () => rightsOf(session) }
})

afterEach(() => {
  vi.unstubAllGlobals()
  session.phone = false
  session.scope = "chat+approve"
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
  assert.notMatch(
    renderToStaticMarkup(<EntryView entry={post(solo)} chat={solo} skillIds={[]} />),
    /<button[^>]*>Solo<\/button>/,
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
  const copy = html.match(/<button(?:(?!<\/button>).)*Copy(?:(?!<\/button>).)*<\/button>/g)?.at(-1) ?? ""
  assert.match(copy, /<span class="col-start-1 row-start-1">Copy<\/span>/, "the idle label shows")
  assert.match(copy, /class="[^"]*invisible">Copied</, "a hidden label keeps the button's width")
  assert.match(copy, /class="[^"]*invisible">Copy failed</)
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

const edit = (diff: ActivityBody["diff"], output?: string) =>
  tool(solo, { tool: "Edit", summary: "/w/a.txt", diff, output })
const row = (entry: Entry, expanded = false) =>
  renderToStaticMarkup(<EntryView entry={entry} chat={solo} skillIds={[]} expanded={expanded} />)

test("an Edit row shows +N −M, and expanded, a unified diff with line numbers in place of its output", () => {
  const diff = {
    added: 1,
    removed: 1,
    hunks: [
      {
        oldStart: 1,
        oldLines: 2,
        newStart: 1,
        newLines: 2,
        lines: ["-old first", "+new first", " old second", "\\ No newline at end of file"],
      },
    ],
  }
  const folded = row(edit(diff, "The file was updated."))
  assert.match(folded, /<span class="text-success-foreground">\+1<\/span> <span class="text-destructive">−1<\/span>/)
  assert.notInclude(folded, "<table")
  const open = row(edit(diff, "The file was updated."), true)
  assert.notInclude(open, "The file was updated.")
  assert.include(open, ">@@ -1,2 +1,2 @@</td>")
  const lines = [...open.matchAll(/<tr class="([^"]*)">(.*?)<\/tr>/g)].map(([, tone, cells]) => [
    tone.split(" ")[0],
    ...[...cells.matchAll(/<td[^>]*>([^<]*)<\/td>/g)].map((m) => m[1]),
  ])
  assert.deepEqual(lines, [
    ["bg-destructive/10", "1", "", "-", "old first"],
    ["bg-success/10", "", "1", "+", "new first"],
    ["", "2", "2", " ", "old second"],
  ])
  assert.match(open, /<td colSpan="4" class="[^"]*italic[^"]*">No newline at end of file<\/td>/)
})

test("a diff too large to store shows its counts and says so", () => {
  const big = edit({ added: 500, removed: 2, truncated: true })
  assert.include(row(big), ">+500</span>")
  assert.include(row(big), ">−2</span>")
  assert.notInclude(row(big), "disabled", "a row with a diff opens")
  assert.include(row(big, true), "Diff too large to show")
  assert.notInclude(row(big, true), "<table")
})

test("a row stored before diffs shows its output and no counts", () => {
  const old = edit(undefined, "The file was updated.")
  assert.notInclude(row(old), "−")
  assert.include(row(old, true), ">The file was updated.</pre>")
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

const failure: Entry = {
  ...base,
  id: "f",
  botId: "solo-id",
  kind: "event",
  body: { type: "turn-failed", text: "Claude Code exited with status 1: " + "the session file is locked. ".repeat(8) },
}
const ask: Extract<Entry, { kind: "user" }> = {
  ...base,
  id: "u",
  botId: "solo-id",
  kind: "user",
  body: { text: "Plan my day", media: ["a.png"] },
}

test("a failed turn shows its whole failure text in the page, and Retry only when it offers it", () => {
  const html = renderToStaticMarkup(<EntryView entry={failure} chat={solo} skillIds={[]} retry={ask} />)
  assert.include(html, failure.body.text ?? "")
  assert.notInclude(html, "truncate")
  assert.notInclude(html, "title=", "the text is read without a hover")
  assert.match(html, /<button[^>]*>.*Retry<\/button>/)
  const without = renderToStaticMarkup(<EntryView entry={failure} chat={solo} skillIds={[]} />)
  assert.include(without, failure.body.text ?? "")
  assert.notInclude(without, "Retry")
})

test("a reaction chip shows only its emoji and says it is the user's own", () => {
  const html = renderToStaticMarkup(
    <EntryView entry={{ ...ask, body: { text: "hi", reactions: ["👍"] } }} chat={solo} skillIds={[]} />,
  )
  assert.match(html, /<button[^>]*aria-label="Your 👍 reaction. Remove it"[^>]*>👍<\/button>/)
})

test("a post's time is a time element that a focus or a hover opens the full date of", () => {
  const html = renderToStaticMarkup(
    <EntryView entry={{ ...post(solo), createdAt: Date.parse("2026-11-13T12:05:00Z") }} chat={solo} skillIds={[]} />,
  )
  assert.match(html, /<button type="button"[^>]*><time datetime="2026-11-13T12:05:00.000Z">[^<]+<\/time><\/button>/i)
})

// chatPage renders a chat's page with its entries, at a window tall enough to render all rows.
function chatPage(bot: Bot, entries: Entry[], status: TranscriptStatus = "ready") {
  vi.stubGlobal("window", { innerHeight: 10_000, location: { origin: "http://localhost:4317" } })
  return renderToStaticMarkup(<Chat bot={bot} entries={entries} mentionables={[]} status={status} />)
}

test("Retry shows on the user's failed turn only on a device that may send", () => {
  assert.match(chatPage(solo, [ask, failure]), /<button[^>]*>.*Retry<\/button>/)
  session.phone = true
  session.scope = "view"
  const viewOnly = chatPage(solo, [ask, failure])
  assert.include(viewOnly, failure.body.text ?? "", "the failure still shows")
  assert.notInclude(viewOnly, "Retry")
})

test("a day separator stands above the first entry and where a new local day starts", () => {
  const day = 86_400_000
  const now = Date.now()
  const entries = [
    { ...post(solo), id: "p1", createdAt: now - 400 * day },
    { ...post(solo), id: "p2", createdAt: now - 400 * day },
    { ...post(solo), id: "p3", createdAt: now },
  ]
  const html = chatPage(solo, entries)
  assert.equal(html.split("Today").length - 1, 1, "one separator for today")
  assert.equal(html.match(/bg-border h-px flex-1/g)?.length, 4, "two separators, each with two rules")
})

const builder: Bot = { ...solo, id: "builder-id", name: "Builder", role: "builder" }
const welcome: Entry = { ...post(builder), id: "w", kind: "bot", body: { text: "Hi! I set up bots.", fallback: false } }

test("Builder's chat offers the four starts until the user's first message", () => {
  const html = chatPage(builder, [welcome])
  for (const label of ["Personal assistant", "Software team", "Research desk", "Writing studio"])
    assert.match(html, new RegExp(`<button[^>]*>${label}</button>`))
  const after = chatPage(builder, [welcome, { ...ask, id: "first", body: { text: "A software team" } }])
  assert.notInclude(after, "Research desk")
})

test("Builder's chat with no entries shows its intro with the starts; another empty chat says No messages yet", () => {
  const html = chatPage(builder, [])
  assert.include(html, "Builder sets up bots, teams and rooms for you.")
  assert.include(html, "Writing studio")
  assert.notInclude(html, "No messages yet.")
  const other = chatPage(solo, [])
  assert.include(other, ">Solo</p>")
  assert.include(other, "No messages yet.")
  assert.notInclude(other, "Writing studio")
})

test("Builder's empty chat shows the load state, not the starts, until the messages load", () => {
  for (const [status, text] of [
    ["loading", "Loading messages…"],
    ["unloaded", "Loading messages…"],
    ["failed", "load the messages."],
  ] as const) {
    const html = chatPage(builder, [], status)
    assert.include(html, text, `status ${status}`)
    assert.notInclude(html, "Writing studio", `status ${status}`)
    assert.notInclude(html, "sets up bots", `status ${status}`)
  }
})

test("Builder's empty chat on a view-only phone says No messages yet and offers no starts", () => {
  session.phone = true
  session.scope = "view"
  const html = chatPage(builder, [])
  assert.include(html, "No messages yet.")
  assert.notInclude(html, "Writing studio")
})

// call is tool call n of turn t in the solo chat.
const call = (n: number, body: Partial<ActivityBody>): Extract<Entry, { kind: "activity" }> => ({
  ...base,
  seq: n,
  id: `c${n}`,
  botId: solo.id,
  kind: "activity",
  body: { tool: "Read", summary: `/w/f${n}.go`, status: "done", subagent: false, ...body },
})
const editCall = (n: number, added: number, removed: number) =>
  call(n, { tool: "Edit", diff: { added, removed, hunks: [] } })
// bare drops the icons' shapes, so a test can match an icon and the text next to it.
const bare = (html: string) => html.replace(/<(path|circle|rect|line)\b[^>]*><\/\1>/g, "")
const icon = (name: string) => `<svg[^>]*class="lucide lucide-${name}[ "][^>]*></svg>`
const answer: Entry = { ...post(solo), id: "answer", seq: 99 }
const turnCalls = [
  call(2, {}),
  call(3, {}),
  editCall(4, 3, 1),
  call(5, { tool: "Bash", summary: "make check", output: "all checks passed" }),
  editCall(6, 10, 2),
  call(7, {}),
]
const group = (calls: Extract<Entry, { kind: "activity" }>[], expanded: string[] = []) =>
  renderToStaticMarkup(<ToolGroup calls={calls} chat={solo} expanded={new Set(expanded)} onExpand={() => {}} />)

test("a turn's tool calls show as one folded row: the count, each tool with its icon and count, and the diff totals", () => {
  const html = bare(chatPage(solo, [{ ...ask, seq: 1 }, ...turnCalls, answer]))
  assert.equal(html.match(/data-entry=/g)?.length, 3, "the message, the group and the answer")
  assert.match(html, /<button type="button" aria-expanded="false"[^>]*>.*6 tool calls/)
  const counts = [...html.matchAll(/class="lucide lucide-([a-z-]+)[^>]*><\/svg>(\w+) (\d+)<\/span>/g)].map(
    ([, name, t, n]) => `${name} ${t} ${n}`,
  )
  assert.deepEqual(counts, ["file-text Read 3", "file-pen Edit 2", "terminal Bash 1"])
  assert.include(html, ">+13</span>")
  assert.include(html, ">−3</span>")
  assert.notInclude(html, "/w/f2.go", "the calls are folded")
  assert.notInclude(html, "make check")
})

test("a single tool call stays its own row", () => {
  const html = chatPage(solo, [{ ...ask, seq: 1 }, call(2, {}), answer])
  assert.notInclude(html, "tool calls")
  assert.include(html, "/w/f2.go")
})

test("an open group shows its calls as rows, each opening on its own", () => {
  const closed = group(turnCalls)
  const open = group(turnCalls, [groupKey("c2")])
  assert.match(open, /^<div[^>]*><button type="button" aria-expanded="true"/)
  for (const n of [2, 3, 4, 6, 7]) assert.include(open, `/w/f${n}.go`)
  assert.include(open, "make check")
  assert.notInclude(open, "all checks passed", "a call's output stays folded")
  assert.include(group(turnCalls, [groupKey("c2"), "c5"]), "all checks passed")
  assert.notInclude(group(turnCalls, ["c2"]), "/w/f3.go", "the first call's own key does not open the group")
  assert.notInclude(closed, "/w/f3.go")
})

test("a group stays open when an older page adds calls above the one it opened by", () => {
  const html = group([call(1, {}), ...turnCalls], [groupKey("c2")])
  assert.match(html, /^<div[^>]*><button type="button" aria-expanded="true"/)
  assert.include(html, "/w/f1.go")
})

test("a running group shows its newest call and a running mark in place of the counts", () => {
  const html = bare(group([call(2, {}), call(3, { tool: "Bash", summary: "make check", status: "running" })]))
  assert.include(html, ">Running:</span>")
  assert.include(html, "2 tool calls")
  assert.match(html, new RegExp(`${icon("terminal")}<span[^>]*>Bash</span><span[^>]*>make check</span>`))
  assert.notInclude(html, "Read 1")
  assert.notInclude(group(turnCalls), ">Running:</span>")
})

test("a group says how many of its calls failed", () => {
  const html = bare(group([call(2, {}), call(3, { status: "error" }), call(4, { status: "error" })]))
  assert.match(html, new RegExp(`text-destructive[^>]*>${icon("x")}2 failed</span>`))
  assert.notInclude(group(turnCalls), "failed")
})

test("each kind of tool has its own icon", () => {
  const icons = [
    ["Read", "file-text"],
    ["Edit", "file-pen"],
    ["Write", "file-pen"],
    ["Bash", "terminal"],
    ["Grep", "search"],
    ["Glob", "search"],
    ["WebFetch", "globe"],
    ["WebSearch", "globe"],
    ["Task", "bot"],
    ["Agent", "bot"],
    ["mcp__github__get_issue", "wrench"],
    ["NotebookEdit", "wrench"],
    ["constructor", "wrench"],
  ]
  for (const [name, kind] of icons) {
    const html = bare(renderToStaticMarkup(<EntryView entry={tool(solo, { tool: name })} chat={solo} skillIds={[]} />))
    assert.match(html, new RegExp(icon(kind)), name)
  }
})

test("a tool row with output is a button that says whether it is expanded; one without output is not a button", () => {
  const row = (body: Partial<ActivityBody>, expanded?: boolean) =>
    renderToStaticMarkup(<EntryView entry={tool(solo, body)} chat={solo} skillIds={[]} expanded={expanded} />)
  assert.include(row({ output: "x" }), '<button type="button" aria-expanded="false"')
  assert.include(row({ output: "x" }, true), '<button type="button" aria-expanded="true"')
  assert.include(row({ diff: { added: 1, removed: 0, hunks: [] } }), 'aria-expanded="false"')
  const plain = row({})
  assert.notInclude(plain, "<button")
  assert.notInclude(plain, "aria-expanded")
  assert.notInclude(plain, "tabindex")
  assert.notInclude(plain, "lucide-chevron-right")
  assert.include(plain, ">ls</span>")
})
