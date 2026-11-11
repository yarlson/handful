// The page of the bridge window. It renders the view that bridge.js makes,
// as text only, and sends the buttons' actions through window.handful.
const actionOf = { move: "move", later: "later", retry: "retry", site: "openSite" }

function render(view) {
  if (!view) return
  document.title = view.heading
  document.getElementById("heading").textContent = view.heading
  document.getElementById("lines").replaceChildren(
    ...view.lines.map((text) => {
      const p = document.createElement("p")
      p.textContent = text
      return p
    }),
  )
  const status = document.getElementById("status")
  status.textContent = view.status
  status.hidden = !view.status
  const buttons = view.buttons.map((b) => {
    const button = document.createElement("button")
    button.type = "button"
    button.id = `button-${b.id}`
    button.textContent = b.label
    if (b.primary) button.className = "primary"
    button.addEventListener("click", () => {
      for (const other of document.querySelectorAll("#buttons button")) other.disabled = true
      window.handful[actionOf[b.id]]().finally(() => {
        for (const other of document.querySelectorAll("#buttons button")) other.disabled = false
      })
    })
    return button
  })
  document.getElementById("buttons").replaceChildren(...buttons)
  document.querySelector("#buttons .primary")?.focus()
}

window.handful.onChange(render)
window.handful.get().then(render)
