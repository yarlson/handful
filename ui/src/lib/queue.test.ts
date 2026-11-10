import { assert, test } from "vitest"
import { moved, placeNote } from "./queue.ts"

test("a planned task moves up or down by one, never past the ends", () => {
  assert.deepEqual(moved(["a", "b", "c"], 1, -1), ["b", "a", "c"])
  assert.deepEqual(moved(["a", "b", "c"], 1, 1), ["a", "c", "b"])
  assert.deepEqual(moved(["a", "b"], 0, 1), ["b", "a"])
  assert.isNull(moved(["a", "b", "c"], 0, -1))
  assert.isNull(moved(["a", "b", "c"], 2, 1))
  assert.isNull(moved(["a"], -1, 1))
  assert.isNull(moved(["a"], 1, -1))
  const ids = ["a", "b"]
  moved(ids, 0, 1)
  assert.deepEqual(ids, ["a", "b"])
})

test("a move is announced with the task's new place", () => {
  assert.equal(placeNote("Ship it", 2, 3), "Ship it is now 2 of 3.")
})
