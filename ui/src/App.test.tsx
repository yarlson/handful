import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import { SidebarFooter } from "./App.tsx"

test("the sidebar footer fits a narrow sidebar: icon buttons with names, and a label that truncates", () => {
  const html = renderToStaticMarkup(<SidebarFooter link="reconnecting" onConnectors={() => {}} onSettings={() => {}} />)
  for (const name of ["Connectors", "Settings"]) {
    assert.match(html, new RegExp(`<button[^>]*aria-label="${name}"[^>]*title="${name}"`), name)
    assert.notInclude(html, `> ${name}<`, `${name} has no text label`)
  }
  assert.include(html, '<span class="min-w-0 flex-1 truncate">Reconnecting…</span>')
})
