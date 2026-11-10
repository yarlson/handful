import { assert, test } from "vitest"
import { failed, modelError, saved } from "./save.ts"

test("a save that worked says Saved, or the form's own text", () => {
  assert.deepEqual(saved(), { ok: true, text: "Saved." })
  assert.deepEqual(saved("Saved. Applies from the next turn."), {
    ok: true,
    text: "Saved. Applies from the next turn.",
  })
})

test("a failed save keeps its reason", () => {
  assert.deepEqual(failed("name is taken"), { ok: false, text: "name is taken" })
})

test("only a failure about the model goes to the model field", () => {
  assert.equal(modelError(failed('unknown model "opus-9"')), 'unknown model "opus-9"')
  assert.equal(modelError(failed("Model IDs can't have spaces")), "Model IDs can't have spaces")
  assert.isNull(modelError(failed("name is taken")))
  assert.isNull(modelError(saved("Saved the model.")))
  assert.isNull(modelError(null))
})
