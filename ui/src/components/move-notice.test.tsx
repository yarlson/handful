import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Bot } from "@/lib/api"
import { MoveNotice } from "./move-notice.tsx"

const team = (id: string, name: string) => ({ id, name, kind: "room", team: {} }) as unknown as Bot

// The notice says to leave the link in place (spec ad09e72c AC20) and offers the teams named
// after YarGPT their Handful name, without renaming any.
test("the welcome notice says to leave the link and offers each YarGPT team a new name", () => {
  const html = renderToStaticMarkup(
    <MoveNotice teams={[team("t1", "YarGPT Dev"), team("t2", "Support")]} onDone={() => {}} onRename={() => {}} />,
  )
  assert.include(
    html,
    "<strong>Welcome to Handful.</strong> Your bots, chats and teams moved to ~/.handful. ~/.yargpt is now a link to it, so leave it in place. You can delete YarGPT.app now.",
  )
  assert.include(html, "~/.yargpt is now a link to it, so leave it in place")
  assert.include(html, ">Rename to Handful Dev…</button>")
  assert.notInclude(html, "Support…")
  assert.include(html, ">Done</button>")
  assert.notInclude(html, "Show YarGPT.app in Finder")
})
