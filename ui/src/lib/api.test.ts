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
  await api.extendGoal("g1", 10)
  assert.deepEqual(calls, [
    {
      method: "POST",
      url: "/api/teams",
      body: {
        name: "Ship",
        instructions: "Ship it.",
        memberIds: ["a1"],
        leadId: "a1",
        agreement: "Tests pass.",
      },
    },
    { method: "PATCH", url: "/api/teams/t1", body: { goalBudget: 12 } },
    { method: "GET", url: "/api/teams/t1/board", body: undefined },
    { method: "POST", url: "/api/goals/g1/extend", body: { turns: 10 } },
  ])
})

test("plan and request calls go to the server's routes with their bodies", async () => {
  const calls = stubFetch(200)
  await api.startNextGoal("t1")
  await api.withdrawItem("i1")
  await api.undeferItem("i2")
  await api.updateTeam("t1", { requestSlots: 2 })
  assert.deepEqual(calls, [
    { method: "POST", url: "/api/teams/t1/goal-queue/start", body: {} },
    { method: "POST", url: "/api/items/i1/withdraw", body: {} },
    { method: "POST", url: "/api/items/i2/undefer", body: {} },
    { method: "PATCH", url: "/api/teams/t1", body: { requestSlots: 2 } },
  ])
})

test("task calls go to the server's routes with their bodies", async () => {
  const calls = stubFetch(200)
  await api.userTask("t1", { text: "Ship CSV export.", startNow: true })
  await api.handOver("t2", { fromTeamId: "t1", text: "Spec it." })
  await api.takeTask("g1")
  await api.takeTask("g1", 2)
  await api.pushBack("g2", "Not ours.")
  await api.answerTask("g3", "macOS 15.")
  await api.pinTask("g4", true)
  await api.pinTask("g4", false)
  await api.stopTask("g5")
  await api.reopenTask("g5")
  await api.withdrawTask("g6")
  await api.approveTask("g7")
  await api.changesTask("g7", "Add a test.")
  await api.needsYou()
  await api.updateTeam("t1", { decisionMinutes: 45, inboxMax: 8, inboxPerSender: 2 })
  assert.deepEqual(calls, [
    {
      method: "POST",
      url: "/api/teams/t1/tasks",
      body: { text: "Ship CSV export.", startNow: true },
    },
    { method: "POST", url: "/api/teams/t2/handoffs", body: { fromTeamId: "t1", text: "Spec it." } },
    { method: "POST", url: "/api/tasks/g1/take", body: { position: 0 } },
    { method: "POST", url: "/api/tasks/g1/take", body: { position: 2 } },
    { method: "POST", url: "/api/tasks/g2/push-back", body: { text: "Not ours." } },
    { method: "POST", url: "/api/tasks/g3/answer", body: { text: "macOS 15." } },
    { method: "POST", url: "/api/tasks/g4/pin", body: {} },
    { method: "DELETE", url: "/api/tasks/g4/pin", body: {} },
    { method: "POST", url: "/api/tasks/g5/stop", body: {} },
    { method: "POST", url: "/api/tasks/g5/reopen", body: {} },
    { method: "POST", url: "/api/tasks/g6/withdraw", body: {} },
    { method: "POST", url: "/api/tasks/g7/approve", body: {} },
    { method: "POST", url: "/api/tasks/g7/changes", body: { text: "Add a test." } },
    { method: "GET", url: "/api/needs-you", body: undefined },
    {
      method: "PATCH",
      url: "/api/teams/t1",
      body: { decisionMinutes: 45, inboxMax: 8, inboxPerSender: 2 },
    },
  ])
})

test("ordering the plan posts to its route and returns nothing", async () => {
  const calls = stubFetch(204)
  assert.strictEqual(await api.orderPlan("t1", ["g5", "g4"]), undefined)
  assert.deepEqual(calls, [{ method: "POST", url: "/api/teams/t1/plan", body: { order: ["g5", "g4"] } }])
})

test("a failed request throws the server's error without its invalid input prefix", async () => {
  vi.stubGlobal("fetch", async () =>
    Response.json({ error: "invalid input: Dev's inbox is full (10 waiting)." }, { status: 400 }),
  )
  const err = await api.userTask("t1", { text: "Again." }).catch((e: Error) => e)
  assert.strictEqual((err as Error).message, "Dev's inbox is full (10 waiting).")
})

test("a failed request without a server error throws its status", async () => {
  vi.stubGlobal("fetch", async () => Response.json({}, { status: 502 }))
  const err = await api.board("t1").catch((e: Error) => e)
  assert.strictEqual((err as Error).message, "GET /api/teams/t1/board failed with 502")
})

test("settings and restart calls go to the server's routes with their bodies", async () => {
  const calls = stubFetch(200)
  await api.settings()
  await api.patchSettings({ maxParallel: 4, model: null })
  await api.resetSettingsFile()
  await api.restart("idle")
  await api.restart("now")
  await api.cancelRestart()
  assert.deepEqual(calls, [
    { method: "GET", url: "/api/settings", body: undefined },
    { method: "PATCH", url: "/api/settings", body: { maxParallel: 4, model: null } },
    { method: "PUT", url: "/api/settings/file", body: {} },
    { method: "POST", url: "/api/restart", body: { when: "idle" } },
    { method: "POST", url: "/api/restart", body: { when: "now" } },
    { method: "DELETE", url: "/api/restart", body: undefined },
  ])
})
