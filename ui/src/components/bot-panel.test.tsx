import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { Bot } from "@/lib/api"
import { failed, saved } from "@/lib/save"
import { SaveNote, SettingsTab } from "./bot-panel.tsx"

test("a save that worked shows as a plain note", () => {
  const html = renderToStaticMarkup(<SaveNote result={saved()} />)
  assert.include(html, "Saved.")
  assert.include(html, 'data-save="saved"')
  assert.include(html, "text-muted-foreground")
  assert.notInclude(html, "text-destructive")
})

test("a failed save shows as an error", () => {
  const html = renderToStaticMarkup(<SaveNote result={failed("name is taken")} />)
  assert.include(html, "name is taken")
  assert.include(html, 'data-save="failed"')
  assert.include(html, "text-destructive")
  assert.notInclude(html, "text-muted-foreground")
})

test("nothing shows before the first save", () => {
  assert.equal(renderToStaticMarkup(<SaveNote result={null} />), "")
})

const agent: Bot = {
  id: "bot-ana",
  kind: "agent",
  name: "Ana",
  instructions: "",
  createdAt: 0,
  updatedAt: 0,
  running: false,
  tasks: [],
  hue: "violet",
}
const settings = (bot: Bot) => renderToStaticMarkup(<SettingsTab bot={bot} version={0} onDeleted={() => {}} />)
// swatches returns the hue of each colour swatch and whether it is picked.
const swatches = (html: string) =>
  [...html.matchAll(/<input type="radio"[^>]*name="bot-hue"( checked="")? value="(\w+)"/g)].map((m) => [m[2], !!m[1]])

test("a bot's settings show the five colours with its stored one picked", () => {
  const html = settings(agent)
  assert.include(html, "<legend")
  assert.include(html, ">Colour</legend>")
  assert.deepEqual(swatches(html), [
    ["sky", false],
    ["emerald", false],
    ["violet", true],
    ["fuchsia", false],
    ["lime", false],
  ])
  assert.include(html, "text-violet-700 dark:text-violet-400")
})

test("a room's settings have no colour to pick", () => {
  assert.deepEqual(swatches(settings({ ...agent, kind: "room", hue: undefined })), [])
})
