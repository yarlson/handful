import { assert, test } from "vitest"
import { CREATE_SETUP, setupProposal } from "./setup.ts"

test("the card's tool is the server's create_setup", () => {
  // internal/core/setup.go names the tool create_setup.
  assert.strictEqual(CREATE_SETUP, "create_setup")
})

test("a setup card shows its bots, teams with their lead, and rooms, and no turn budget", () => {
  const input = {
    bots: [{ name: "Developer", instructions: "Write the code.", model: "haiku", effort: "low" }],
    teams: [
      {
        name: "Web app",
        purpose: "Build it.",
        members: ["Developer", "Ana"],
        lead: "Ana",
        agreement: "Tests pass.",
        goal_budget: 12,
        default_model: "sonnet",
        default_effort: "medium",
      },
    ],
    rooms: [{ name: "Standup", purpose: "Notes.", members: ["Ana"] }],
  }
  assert.deepEqual(setupProposal(input), {
    bots: [{ name: "Developer", instructions: "Write the code.", model: "haiku", effort: "low" }],
    teams: [
      {
        name: "Web app",
        purpose: "Build it.",
        members: ["Developer", "Ana"],
        lead: "Ana",
        agreement: "Tests pass.",
        defaultModel: "sonnet",
        defaultEffort: "medium",
      },
    ],
    rooms: [{ name: "Standup", purpose: "Notes.", members: ["Ana"] }],
  })
})

test("a team without a lead is led by its first member, and missing parts are empty", () => {
  assert.deepEqual(setupProposal({ teams: [{ name: "Pair", members: ["Bo", "Cy"] }] }), {
    bots: [],
    teams: [
      {
        name: "Pair",
        purpose: "",
        members: ["Bo", "Cy"],
        lead: "Bo",
        agreement: "",
        defaultModel: "",
        defaultEffort: "",
      },
    ],
    rooms: [],
  })
  assert.deepEqual(setupProposal({ teams: [{ name: "Nobody", lead: "", goal_budget: "30" }] }), {
    bots: [],
    teams: [{ name: "Nobody", purpose: "", members: [], lead: "", agreement: "", defaultModel: "", defaultEffort: "" }],
    rooms: [],
  })
})

test("fields of the wrong type read as empty", () => {
  assert.deepEqual(
    setupProposal({
      bots: [{ name: 7, model: 1, effort: null }],
      teams: [{ default_model: [], default_effort: 2 }],
      rooms: [{ name: "R", members: ["Ana", 3, null] }],
    }),
    {
      bots: [{ name: "", instructions: "", model: "", effort: "" }],
      teams: [{ name: "", purpose: "", members: [], lead: "", agreement: "", defaultModel: "", defaultEffort: "" }],
      rooms: [{ name: "R", purpose: "", members: ["Ana"] }],
    },
  )
})

test("input that is no setup gives null", () => {
  for (const input of [
    null,
    undefined,
    "setup",
    3,
    [],
    { bots: "Ana" },
    { teams: [null] },
    { rooms: [["Ana"]] },
    { bots: [{ name: "A" }, "B"] },
  ]) {
    assert.isNull(setupProposal(input), JSON.stringify(input))
  }
})
