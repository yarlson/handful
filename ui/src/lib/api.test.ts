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
    { method: "POST", url: "/api/goals/g1/accept", body: {} },
  ])
})

test("ending a goal posts to its route and returns nothing", async () => {
  const calls = stubFetch(204)
  assert.strictEqual(await api.endGoal("g1"), undefined)
  assert.deepEqual(calls, [{ method: "POST", url: "/api/goals/g1/end", body: {} }])
})
