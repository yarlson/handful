import { renderToStaticMarkup } from "react-dom/server"
import { assert, test, vi } from "vitest"
import { AreaBoundary } from "./area-boundary.tsx"

// failedArea renders a boundary in the state React gives it after a child throws.
function failedArea(props: Omit<ConstructorParameters<typeof AreaBoundary>[0], "children">) {
  const area = new AreaBoundary({ ...props, children: <p>the chat</p> })
  area.state = { ...area.state, ...AreaBoundary.getDerivedStateFromError() }
  return renderToStaticMarkup(<>{area.render()}</>)
}

test("an area renders its children while nothing fails", () => {
  assert.equal(
    renderToStaticMarkup(
      <AreaBoundary area="chat">
        <p>the chat</p>
      </AreaBoundary>,
    ),
    "<p>the chat</p>",
  )
})

test("an area that failed shows the error and Reload instead of its children", () => {
  const html = failedArea({ area: "chat" })
  assert.include(html, 'role="alert"')
  assert.include(html, "Something went wrong in this view.")
  assert.match(html, /<button[^>]*>.*Reload<\/button>/)
  assert.notInclude(html, "the chat")
})

test("a failed area keeps its menu and its way out", () => {
  const html = failedArea({
    area: "reader",
    menu: <button type="button">Menu</button>,
    close: { label: "Back to chat", onClose: () => {} },
  })
  assert.include(html, ">Menu</button>")
  assert.include(html, 'aria-label="Back to chat"')
})

test("a failed area has no header when it has no menu and no way out", () => {
  assert.notInclude(failedArea({ area: "chat" }), "h-12")
})

test("a new reset key clears the error, and the same key keeps it", () => {
  const failed = { failed: true, resetKey: "bot-a" }
  assert.deepEqual(AreaBoundary.getDerivedStateFromProps({ area: "chat", resetKey: "bot-b", children: null }, failed), {
    failed: false,
    resetKey: "bot-b",
  })
  assert.isNull(AreaBoundary.getDerivedStateFromProps({ area: "chat", resetKey: "bot-a", children: null }, failed))
})

test("Reload loads the area's data again and renders the area again", () => {
  const onReload = vi.fn()
  const area = new AreaBoundary({ area: "chat", onReload, children: null })
  const setState = vi.spyOn(area, "setState").mockImplementation(() => {})
  area.reload()
  assert.equal(onReload.mock.calls.length, 1)
  assert.deepEqual(setState.mock.calls[0][0], { failed: false })
})

test("Reload without a way to load data only renders the area again", () => {
  const area = new AreaBoundary({ area: "panel", children: null })
  const setState = vi.spyOn(area, "setState").mockImplementation(() => {})
  area.reload()
  assert.deepEqual(setState.mock.calls[0][0], { failed: false })
})

test("a caught error goes to the console with the area's name", () => {
  const log = vi.spyOn(console, "error").mockImplementation(() => {})
  const error = new Error("boom")
  new AreaBoundary({ area: "panel", children: null }).componentDidCatch(error, { componentStack: "\n  at Tab" })
  assert.deepEqual(log.mock.calls[0], ["The panel view failed to render.", error, "\n  at Tab"])
  log.mockRestore()
})
