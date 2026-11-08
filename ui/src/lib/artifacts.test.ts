import { afterEach, assert, test, vi } from "vitest"
import { api } from "./api.ts"
import { ARTIFACT_KINDS, artifactAuthor, artifactsPath } from "./artifacts.ts"

afterEach(() => {
  vi.unstubAllGlobals()
})

test("offers the same kinds as the server, in order", () => {
  // internal/core/artifacts.go keeps the same list.
  assert.deepEqual(ARTIFACT_KINDS, ["plan", "task", "issue", "review", "report", "note"])
})

test("the list path carries only the filters that are set", () => {
  assert.strictEqual(artifactsPath({}), "/api/artifacts")
  assert.strictEqual(artifactsPath({ kind: "", query: "  " }), "/api/artifacts")
  assert.strictEqual(artifactsPath({ kind: "plan" }), "/api/artifacts?kind=plan")
  assert.strictEqual(artifactsPath({ query: " 50% & more " }), "/api/artifacts?q=50%25+%26+more")
  assert.strictEqual(artifactsPath({ kind: "review", query: "dev-3" }), "/api/artifacts?kind=review&q=dev-3")
})

test("an author is the user, a bot by name or a deleted bot", () => {
  const bots = [
    { id: "b1", name: "Planner" },
    { id: "b2", name: "Critic" },
  ]
  assert.strictEqual(artifactAuthor("user", bots), "User")
  assert.strictEqual(artifactAuthor("b1", bots), "Planner")
  assert.strictEqual(artifactAuthor("b2", bots), "Critic")
  assert.strictEqual(artifactAuthor("gone", bots), "Deleted bot")
  assert.strictEqual(artifactAuthor("user", []), "User")
})

test("the artifact calls reach the artifact endpoints", async () => {
  const calls: { url: string; method: string }[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init: RequestInit) => {
      calls.push({ url, method: init.method ?? "" })
      const status = init.method === "DELETE" ? 204 : 200
      return Promise.resolve(new Response(status === 204 ? null : JSON.stringify([]), { status }))
    }),
  )
  await api.artifacts({ kind: "plan" })
  await api.artifact("a 1")
  await api.deleteArtifact("a 1")
  assert.deepEqual(calls, [
    { url: "/api/artifacts?kind=plan", method: "GET" },
    { url: "/api/artifacts/a%201", method: "GET" },
    { url: "/api/artifacts/a%201", method: "DELETE" },
  ])
})
