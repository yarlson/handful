import { assert, test } from "vitest"
import { handfulName } from "./move-notice.ts"

test("a team named after YarGPT gets Handful in its place; other names stay", () => {
  assert.equal(handfulName("YarGPT Dev"), "Handful Dev")
  assert.equal(handfulName("YarGPT  Product Team"), "Handful  Product Team")
  assert.equal(handfulName("YarGPT"), "Handful")
  assert.isNull(handfulName("YarGPTs"))
  assert.isNull(handfulName("Old YarGPT Dev"))
  assert.isNull(handfulName("yargpt dev"))
  assert.isNull(handfulName("Support"))
})
