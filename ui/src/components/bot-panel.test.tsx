import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import { failed, saved } from "@/lib/save"
import { SaveNote } from "./bot-panel.tsx"

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
