import { assert, test } from "vitest"
import { REACTIONS, toggled } from "./reactions.ts"

test("offers the same fixed set as the server, in order", () => {
  // internal/core/reactions.go keeps the same list.
  assert.deepEqual(REACTIONS, ["👍", "👎", "❤️", "😂", "👀", "✅"])
  assert.strictEqual(REACTIONS[2], "❤️")
})

test("a click turns an absent reaction on and a present one off", () => {
  assert.strictEqual(toggled(undefined, "👍"), true)
  assert.strictEqual(toggled([], "👍"), true)
  assert.strictEqual(toggled(["👍"], "👍"), false)
  assert.strictEqual(toggled(["❤️"], "👍"), true)
  assert.strictEqual(toggled(["👍", "❤️"], "❤️"), false)
})
