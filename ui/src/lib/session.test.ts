import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { Session } from "./api"
import { phoneSession, sessionStore, usePhoneSession } from "./session"

afterEach(() => {
  vi.unstubAllGlobals()
})

// answer is a server whose answer the test sends when it wants, and counts the questions.
function answer() {
  let asked = 0
  let send: (s: Session) => void = () => {}
  let fail: (e: Error) => void = () => {}
  const load = () => {
    asked++
    return new Promise<Session>((resolve, reject) => {
      send = resolve
      fail = reject
    })
  }
  return { load, asked: () => asked, send: (s: Session) => send(s), fail: (e: Error) => fail(e) }
}
const tick = () => new Promise((r) => setTimeout(r, 0))

test("the session is the computer's until the server says it is a phone, and asks once", async () => {
  const server = answer()
  const store = sessionStore(server.load)
  assert.equal(server.asked(), 0, "nothing is asked before a component watches")
  let calls = 0
  store.subscribe(() => calls++)
  store.subscribe(() => calls++)
  assert.equal(server.asked(), 1)
  assert.isFalse(store.phone())
  server.send({ phone: true })
  await tick()
  assert.isTrue(store.phone())
  assert.equal(calls, 2, "each watcher hears the change")
})

test("an answer that changes nothing tells no one", async () => {
  const server = answer()
  const store = sessionStore(server.load)
  let calls = 0
  store.subscribe(() => calls++)
  server.send({ phone: false })
  await tick()
  assert.isFalse(store.phone())
  assert.equal(calls, 0)
})

test("a failed question leaves the computer's session", async () => {
  const server = answer()
  const store = sessionStore(server.load)
  store.subscribe(() => {})
  server.fail(new Error("offline"))
  await tick()
  assert.isFalse(store.phone())
})

test("a watcher that stopped hears nothing", async () => {
  const server = answer()
  const store = sessionStore(server.load)
  let calls = 0
  const stop = store.subscribe(() => calls++)
  stop()
  server.send({ phone: true })
  await tick()
  assert.isTrue(store.phone())
  assert.equal(calls, 0)
})

test("the page asks the server's /api/session, and a server render is no phone", async () => {
  const asked: string[] = []
  vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
    asked.push(`${init.method} ${url}`)
    return new Response(JSON.stringify({ phone: true }), { status: 200 })
  })
  const Probe = () => String(usePhoneSession())
  assert.equal(renderToStaticMarkup(createElement(Probe)), "false")
  phoneSession.subscribe(() => {})
  await tick()
  assert.deepEqual(asked, ["GET /api/session"])
  assert.isTrue(phoneSession.phone())
})
