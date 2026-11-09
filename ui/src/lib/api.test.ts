import { afterEach, assert, test, vi } from "vitest"
import { api } from "./api.ts"

type Call = { method: string; url: string; body: unknown }

// stubFetch records each request and answers with status.
function stubFetch(status: number) {
  const calls: Call[] = []
  vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
    calls.push({
      method: init.method ?? "",
      url,
      body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
    })
    return new Response(status === 204 ? null : JSON.stringify({ id: "x" }), { status })
  })
  return calls
}

afterEach(() => {
  vi.unstubAllGlobals()
})

test("team and goal calls go to the server's routes with their bodies", async () => {
  const calls = stubFetch(200)
  await api.createTeam({
    name: "Ship",
    instructions: "Ship it.",
    memberIds: ["a1"],
    leadId: "a1",
    agreement: "Tests pass.",
  })
  await api.updateTeam("t1", { goalBudget: 12 })
  await api.board("t1")
  await api.startGoal("t1", "Ship CSV export.")
  await api.extendGoal("g1", 10)
  await api.acceptGoal("g1")
  assert.deepEqual(calls, [
    {
      method: "POST",
      url: "/api/teams",
      body: { name: "Ship", instructions: "Ship it.", memberIds: ["a1"], leadId: "a1", agreement: "Tests pass." },
    },
    { method: "PATCH", url: "/api/teams/t1", body: { goalBudget: 12 } },
    { method: "GET", url: "/api/teams/t1/board", body: undefined },
    { method: "POST", url: "/api/teams/t1/goals", body: { text: "Ship CSV export." } },
    { method: "POST", url: "/api/goals/g1/extend", body: { turns: 10 } },
    { method: "POST", url: "/api/goals/g1/accept", body: { startNext: true } },
  ])
})

test("queue and request calls go to the server's routes with their bodies", async () => {
  const calls = stubFetch(200)
  await api.acceptGoal("g1", false)
  await api.endGoal("g2", false)
  await api.cancelGoal("g3")
  await api.reorderGoals("t1", ["g5", "g4"])
  await api.startNextGoal("t1")
  await api.withdrawItem("i1")
  await api.undeferItem("i2")
  await api.updateTeam("t1", { requestSlots: 2 })
  assert.deepEqual(calls, [
    { method: "POST", url: "/api/goals/g1/accept", body: { startNext: false } },
    { method: "POST", url: "/api/goals/g2/end", body: { startNext: false } },
    { method: "POST", url: "/api/goals/g3/cancel", body: {} },
    { method: "POST", url: "/api/teams/t1/goal-queue", body: { order: ["g5", "g4"] } },
    { method: "POST", url: "/api/teams/t1/goal-queue/start", body: {} },
    { method: "POST", url: "/api/items/i1/withdraw", body: {} },
    { method: "POST", url: "/api/items/i2/undefer", body: {} },
    { method: "PATCH", url: "/api/teams/t1", body: { requestSlots: 2 } },
  ])
})

test("ending a goal posts to its route and returns nothing", async () => {
  const calls = stubFetch(204)
  assert.strictEqual(await api.endGoal("g1"), undefined)
  assert.deepEqual(calls, [{ method: "POST", url: "/api/goals/g1/end", body: { startNext: true } }])
})

test("a failed request throws the server's error without its invalid input prefix", async () => {
  vi.stubGlobal("fetch", async () =>
    Response.json({ error: "invalid input: Ship already works on a goal; end it first" }, { status: 400 }),
  )
  const err = await api.startGoal("t1", "Again.").catch((e: Error) => e)
  assert.strictEqual((err as Error).message, "Ship already works on a goal; end it first")
})

test("a failed request without a server error throws its status", async () => {
  vi.stubGlobal("fetch", async () => Response.json({}, { status: 502 }))
  const err = await api.board("t1").catch((e: Error) => e)
  assert.strictEqual((err as Error).message, "GET /api/teams/t1/board failed with 502")
})
