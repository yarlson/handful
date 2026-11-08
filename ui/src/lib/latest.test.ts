import { assert, test } from "vitest"
import { latestOnly } from "./latest.ts"

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: Error) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function recorder() {
  const seen: string[] = []
  return { seen, onData: (v: string) => seen.push(`data ${v}`), onError: (e: Error) => seen.push(`error ${e.message}`) }
}

test("applies a single response", async () => {
  const run = latestOnly()
  const r = recorder()
  const a = deferred<string>()
  run(a.promise, r.onData, r.onError)
  a.resolve("a")
  await a.promise
  assert.deepEqual(r.seen, ["data a"])
})

test("ignores a response that arrives after a newer request's", async () => {
  const run = latestOnly()
  const r = recorder()
  const older = deferred<string>()
  const newer = deferred<string>()
  run(older.promise, r.onData, r.onError)
  run(newer.promise, r.onData, r.onError)
  newer.resolve("abc")
  await newer.promise
  older.resolve("ab")
  await older.promise
  assert.deepEqual(r.seen, ["data abc"])
})

test("ignores an older request's error", async () => {
  const run = latestOnly()
  const r = recorder()
  const older = deferred<string>()
  const newer = deferred<string>()
  run(older.promise, r.onData, r.onError)
  run(newer.promise, r.onData, r.onError)
  older.reject(new Error("old"))
  await older.promise.catch(() => {})
  newer.reject(new Error("new"))
  await newer.promise.catch(() => {})
  assert.deepEqual(r.seen, ["error new"])
})

test("separate loaders do not cancel each other", async () => {
  const one = latestOnly()
  const two = latestOnly()
  const r = recorder()
  const a = deferred<string>()
  const b = deferred<string>()
  one(a.promise, r.onData, r.onError)
  two(b.promise, r.onData, r.onError)
  a.resolve("a")
  b.resolve("b")
  await Promise.all([a.promise, b.promise])
  assert.deepEqual(r.seen, ["data a", "data b"])
})
