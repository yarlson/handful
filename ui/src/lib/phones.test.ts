import { assert, test } from "vitest"
import {
  codeGroups,
  codeOfHash,
  COMPUTER,
  decidedOn,
  pairLink,
  rightsOf,
  SCOPES,
  scopeName,
  splitHosts,
  timeLeft,
} from "./phones"

test("the three scopes have P3's names and words, in its order", () => {
  assert.deepEqual(
    SCOPES.map((s) => `${s.name} — ${s.help}`),
    [
      "View — only read.",
      "Chat — read and send messages. It can't approve a bot's requests.",
      "Chat and approve once — also Allow once or Deny a bot's request.",
    ],
  )
  assert.deepEqual(
    SCOPES.map((s) => s.id),
    ["view", "chat", "chat+approve"],
    "the ids are the server's scopes",
  )
  assert.equal(scopeName("chat+approve"), "Chat and approve once")
  assert.equal(scopeName("view"), "View")
  assert.equal(scopeName("other"), "other", "an unknown scope shows as it is")
})

test("a code shows in groups of 4, and its link carries it in the fragment", () => {
  assert.equal(codeGroups("K7QM2XPD9WTR"), "K7QM-2XPD-9WTR")
  assert.equal(codeGroups("K7QM2"), "K7QM-2")
  assert.equal(codeGroups(""), "")
  assert.equal(pairLink("phone.handful.dev", "K7QM2XPD9WTR"), "https://phone.handful.dev/pair#K7QM2XPD9WTR")
})

test("the pair page reads the code from the fragment, and is empty when the link lost it", () => {
  assert.equal(codeOfHash("#K7QM2XPD9WTR"), "K7QM2XPD9WTR")
  assert.equal(codeOfHash("#k7qm-2xpd-9wtr"), "k7qm-2xpd-9wtr")
  assert.equal(codeOfHash(""), "")
  assert.equal(codeOfHash("#"), "")
  assert.equal(codeOfHash("K7QM2XPD9WTR"), "", "a hash starts with #")
  assert.equal(codeOfHash("x#K7QM2XPD9WTR"), "", "and only with #")
  assert.equal(codeOfHash("#<script>"), "", "only code symbols")
  assert.equal(codeOfHash("#K7QM2XPD9WTR&x"), "", "nothing after the code")
  assert.equal(codeOfHash("#" + "A".repeat(21)), "", "at most 20 symbols")
  assert.equal(codeOfHash("#" + "A".repeat(20)), "A".repeat(20))
})

test("a pair request counts down in minutes and seconds and stops at 0:00", () => {
  assert.equal(timeLeft(120_000, 0), "2:00 left")
  assert.equal(timeLeft(112_000, 0), "1:52 left")
  assert.equal(timeLeft(112_000, 1), "1:52 left", "a part of a second rounds up")
  assert.equal(timeLeft(9_000, 0), "0:09 left")
  assert.equal(timeLeft(1_000, 5_000), "0:00 left")
})

test("a phone decision's note says Approved once or Denied, and the phone (P7)", () => {
  assert.equal(decidedOn("once", "Pixel 8"), "Approved once from Pixel 8.")
  assert.equal(decidedOn("deny", "Pixel 8"), "Denied from Pixel 8.")
})

test("a computer may do everything; a phone's scope decides messages and approvals", () => {
  assert.deepEqual(rightsOf({ phone: false }), COMPUTER)
  assert.deepEqual(COMPUTER, { computer: true, send: true, approve: true })
  assert.deepEqual(rightsOf({ phone: true, scope: "view" }), { computer: false, send: false, approve: false })
  assert.deepEqual(rightsOf({ phone: true, scope: "chat" }), { computer: false, send: true, approve: false })
  assert.deepEqual(rightsOf({ phone: true, scope: "chat+approve" }), { computer: false, send: true, approve: true })
  assert.deepEqual(rightsOf({ phone: true }), { computer: false, send: false, approve: false }, "no scope, no rights")
})

test("phone addresses split on commas and spaces", () => {
  assert.deepEqual(splitHosts(" phone.handful.dev, other.example.com\nthird.example.com "), [
    "phone.handful.dev",
    "other.example.com",
    "third.example.com",
  ])
  assert.deepEqual(splitHosts(" , "), [])
})
