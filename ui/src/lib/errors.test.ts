import { assert, test } from "vitest"
import { errorMessage } from "./errors.ts"

test("drops the invalid input prefix of a server error", () => {
  assert.strictEqual(
    errorMessage("invalid input: Test Team already works on a goal; end it first"),
    "Test Team already works on a goal; end it first",
  )
})

test("keeps other errors as they are", () => {
  assert.strictEqual(errorMessage('not found: bot "x"'), 'not found: bot "x"')
  assert.strictEqual(errorMessage("the goal is invalid input: try again"), "the goal is invalid input: try again")
  assert.strictEqual(errorMessage("invalid input"), "invalid input")
  assert.strictEqual(errorMessage(""), "")
})
