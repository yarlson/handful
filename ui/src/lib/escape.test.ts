import { assert, test } from "vitest"
import { escapeAction, turnStoppable } from "./escape"

const idle = { handled: false, menuOpen: false, browsing: false, answering: false, running: false, stopping: false }

test("Escape closes the menu, then ends the history walk, then the answer, then stops the turn", () => {
  const all = { ...idle, menuOpen: true, browsing: true, answering: true, running: true }
  assert.equal(escapeAction(all), "menu")
  assert.equal(escapeAction({ ...all, menuOpen: false }), "browse")
  assert.equal(escapeAction({ ...all, menuOpen: false, browsing: false }), "answer")
  assert.equal(escapeAction({ ...idle, running: true }), "stop")
})

test("Escape does nothing when a layer above took it, or nothing is open or running", () => {
  assert.isNull(escapeAction({ ...idle, handled: true, menuOpen: true, running: true }))
  assert.isNull(escapeAction(idle))
})

test("a second Escape while the stop is on its way does nothing", () => {
  assert.isNull(escapeAction({ ...idle, running: true, stopping: true }))
})

test("Stop turn shows beside Stop only where it keeps something that Stop ends", () => {
  assert.isTrue(turnStoppable({ running: true, kind: "agent", tasks: [{}] }), "a bot with a background task")
  assert.isTrue(turnStoppable({ running: true, kind: "room", tasks: [] }), "a room or a team")
  assert.isFalse(turnStoppable({ running: true, kind: "agent", tasks: [] }), "a bot with only its turn")
  assert.isFalse(turnStoppable({ running: false, kind: "room", tasks: [{}] }), "no turn runs")
})
