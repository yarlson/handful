import { assert, test } from "vitest"
import { moveOldKeys } from "./stored-keys.ts"

function memoryStore(entries: Record<string, string>) {
  const map = new Map(Object.entries(entries))
  return {
    map,
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => void map.set(k, v),
    removeItem: (k: string) => void map.delete(k),
  }
}

test("a draft and the other keys stored before the rename move to their new names", () => {
  const store = memoryStore({ "yargpt.draft.b1": "Half a message", "yargpt.sidebarWidth": "300", other: "x" })
  moveOldKeys(store, [...store.map.keys()])
  assert.deepEqual(Object.fromEntries(store.map), {
    other: "x",
    "handful.draft.b1": "Half a message",
    "handful.sidebarWidth": "300",
  })
})

test("a key already stored under the new name wins, and the old one goes", () => {
  const store = memoryStore({ "yargpt.draft.b1": "Old draft", "handful.draft.b1": "New draft" })
  moveOldKeys(store, [...store.map.keys()])
  assert.deepEqual(Object.fromEntries(store.map), { "handful.draft.b1": "New draft" })
})

test("with no old keys nothing changes", () => {
  const store = memoryStore({ "handful.draft.b1": "Draft" })
  moveOldKeys(store, [...store.map.keys()])
  assert.deepEqual(Object.fromEntries(store.map), { "handful.draft.b1": "Draft" })
})

test("a listed key that is gone by the time it moves creates nothing", () => {
  const store = memoryStore({})
  moveOldKeys(store, ["yargpt.draft.b1"])
  assert.deepEqual(Object.fromEntries(store.map), {})
})
