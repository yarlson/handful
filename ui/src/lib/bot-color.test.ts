import { assert, test } from "vitest"
import { botColor, colorId, hues } from "./bot-color.ts"

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

test("the palette is five hues in the server's order, without amber or rose", () => {
  assert.deepEqual([...hues], ["sky", "emerald", "violet", "fuchsia", "lime"])
})

test("a stored hue gives that hue's name colour: -700 on light, -400 on dark", () => {
  // "core-id" alone gives another hue; the stored one wins.
  assert.deepEqual(botColor("core-id", "lime"), { text: "text-lime-700 dark:text-lime-400" })
  assert.deepEqual(botColor("core-id", "sky"), { text: "text-sky-700 dark:text-sky-400" })
  assert.deepEqual(botColor("", "emerald"), { text: "text-emerald-700 dark:text-emerald-400" })
  assert.deepEqual(botColor("", "violet"), { text: "text-violet-700 dark:text-violet-400" })
  assert.deepEqual(botColor("", "fuchsia"), { text: "text-fuchsia-700 dark:text-fuchsia-400" })
})

test("a hue the palette does not have, or none, falls back to the id's colour", () => {
  assert.deepEqual(botColor("core-id", "rose"), botColor("core-id"))
  assert.deepEqual(botColor("core-id", ""), botColor("core-id"))
  assert.deepEqual(botColor("bot-ana"), { text: "text-fuchsia-700 dark:text-fuchsia-400" })
  assert.deepEqual(botColor("id-ana"), { text: "text-sky-700 dark:text-sky-400" })
})
