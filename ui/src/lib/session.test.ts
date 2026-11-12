import { createElement } from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import { api, type Session } from "./api"
import { needsKey, phoneSession, sessionStore, usePhoneSession, useRights, useSession } from "./session"

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
  vi.stubGlobal("fetch", async (url: string, init?: RequestInit) => {
    asked.push(`${init?.method ?? "GET"} ${url}`)
    return new Response(JSON.stringify({ phone: true }), { status: 200 })
  })
  const Probe = () => String(usePhoneSession())
  assert.equal(renderToStaticMarkup(createElement(Probe)), "false")
  phoneSession.subscribe(() => {})
  await tick()
  assert.deepEqual(asked, ["GET /api/session"])
  assert.isTrue(phoneSession.phone())
})

test("the session keeps the phone's scope and name, and a change of either tells the watchers", async () => {
  const server = answer()
  const store = sessionStore(server.load)
  let calls = 0
  store.subscribe(() => calls++)
  server.send({ phone: true, scope: "chat", device: "Pixel 8" })
  await tick()
  assert.deepEqual(store.session(), { phone: true, scope: "chat", device: "Pixel 8" })
  store.recheck()
  server.send({ phone: true, scope: "chat", device: "Pixel 8" })
  await tick()
  assert.equal(calls, 1, "the same answer again tells no one")
  for (const next of [
    { phone: true, scope: "view" as const, device: "Pixel 8" },
    { phone: true, scope: "view" as const, device: "Pixel 9" },
    { phone: true, scope: "view" as const, device: "Pixel 9", lost: true },
  ]) {
    store.recheck()
    server.send(next)
    await tick()
    assert.deepEqual(store.session(), next)
  }
  assert.equal(calls, 4, "a new scope, name or loss each tell")
})

test("recheck asks again only after the first question", async () => {
  const server = answer()
  const store = sessionStore(server.load)
  store.recheck()
  assert.equal(server.asked(), 0, "no question before a component watches")
  store.subscribe(() => {})
  store.recheck()
  assert.equal(server.asked(), 2)
})

test("a 401 from /api/session is a phone whose session is lost", async () => {
  vi.stubGlobal("fetch", async () => new Response("Pair this phone again.", { status: 401 }))
  assert.deepEqual(await api.session(), { phone: true, lost: true })
  vi.stubGlobal("fetch", async () => new Response("down", { status: 502 }))
  let failed = ""
  await api.session().catch((e: Error) => (failed = e.message))
  assert.equal(failed, "GET /api/session failed with 502")
  vi.stubGlobal("fetch", async () => new Response(JSON.stringify({ phone: true, scope: "view", device: "Pixel 8" })))
  assert.deepEqual(await api.session(), { phone: true, scope: "view", device: "Pixel 8" })
})

test("the rights follow the session's scope", () => {
  const Probe = () => {
    const r = useRights()
    return `${r.computer} ${r.send} ${r.approve}`
  }
  assert.equal(renderToStaticMarkup(createElement(Probe)), "true true true", "a server render is the computer")
  const Whole = () => String(useSession().phone)
  assert.equal(renderToStaticMarkup(createElement(Whole)), "false")
})

test("a change of the key's state or file tells the watchers", async () => {
  const server = answer()
  const store = sessionStore(server.load)
  let calls = 0
  store.subscribe(() => calls++)
  for (const next of [
    { phone: false, key: false, keyFile: "/a/ui-key" },
    { phone: false, key: false, keyFile: "/b/ui-key" },
    { phone: false, key: true },
    { phone: false, key: false },
    { phone: false, key: true },
  ]) {
    store.recheck()
    server.send(next)
    await tick()
    assert.deepEqual(store.session(), next)
  }
  assert.equal(calls, 5, "a key or a key file that changes, alone, each tell")
})

test("only the computer's browser without the key needs the key page", () => {
  assert.isTrue(needsKey({ phone: false, key: false, keyFile: "/a/ui-key" }))
  assert.isFalse(needsKey({ phone: false, key: true }))
  assert.isFalse(needsKey({ phone: false }), "until the server says, no key page")
  assert.isFalse(needsKey({ phone: true, key: false }), "a phone never")
})

test("a write refused for want of the key asks for the session again", async () => {
  const asked: string[] = []
  vi.stubGlobal("fetch", async (url: string, init?: RequestInit) => {
    asked.push(`${init?.method ?? "GET"} ${url}`)
    if (url === "/api/session") return new Response(JSON.stringify({ phone: false, key: false, keyFile: "/a/ui-key" }))
    return new Response(JSON.stringify({ error: "This needs Handful's key." }), { status: 401 })
  })
  phoneSession.subscribe(() => {})
  await tick()
  asked.length = 0
  const err = await api.createBot("Ana", "").catch((e: Error) => e)
  await tick()
  assert.equal((err as Error).message, "This needs Handful's key.")
  assert.deepEqual(asked, ["POST /api/bots", "GET /api/session"])
  assert.deepEqual(phoneSession.session(), { phone: false, key: false, keyFile: "/a/ui-key" })
})
