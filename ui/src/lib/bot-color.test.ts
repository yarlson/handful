import { assert, test } from "vitest"
import { botColor, colorId } from "./bot-color.ts"

const room = {
  id: "room-1",
  name: "Dev",
  members: [
    { id: "core-id", name: "Core Dev" },
    { id: "ui-id", name: "UI Dev" },
  ],
}

test("a row with a bot id is colored by it", () => {
  assert.equal(colorId({ botId: "ui-id", bot: "Core Dev" }, room), "ui-id")
})

test("a row without a bot id is colored by the member with its name", () => {
  assert.equal(colorId({ bot: "Core Dev" }, room), "core-id")
  assert.equal(botColor(colorId({ bot: "Core Dev" }, room)), botColor("core-id"))
})

test("a name that matches no member is colored by the name", () => {
  assert.equal(colorId({ bot: "Old Name" }, room), "Old Name")
  assert.equal(colorId({ bot: "Old Name" }, { id: "b", name: "Solo" }), "Old Name")
})

test("a row in a bot's own chat, or without a name, belongs to the chat's bot", () => {
  const solo = { id: "solo-id", name: "Solo" }
  assert.equal(colorId({ bot: "Solo" }, solo), "solo-id")
  assert.equal(colorId({}, solo), "solo-id")
  assert.equal(colorId({ bot: "" }, room), "room-1")
})

test("the same input always gets the same color, and different ids can differ", () => {
  assert.deepEqual(botColor("core-id"), botColor("core-id"))
  const colors = new Set(["a", "b", "c", "d", "e", "f", "g", "h"].map((id) => botColor(id).text))
  assert.isAbove(colors.size, 1)
})
