import { assert, test } from "vitest"
import type { Connector } from "./api.ts"
import { CONNECTOR_VIEWS, connectorsPhase, connectorsStep, NO_CONNECTORS, type ConnectorsState } from "./connectors.ts"

const github: Connector = { name: "github", url: "https://example.test/mcp", bots: [], status: "connected" }
const loadError: ConnectorsState = { list: [github], error: { from: "load", text: "offline" } }
const actionError: ConnectorsState = { list: [github], error: { from: "action", text: "pop-up blocked" } }

test("each connector status has its badge and buttons", () => {
  assert.deepEqual(CONNECTOR_VIEWS.static, { label: "token in file", ok: true, signIn: null, disconnect: false })
  assert.deepEqual(CONNECTOR_VIEWS["needs-sign-in"], {
    label: "needs sign-in",
    ok: false,
    signIn: "Connect",
    disconnect: false,
  })
  assert.deepEqual(CONNECTOR_VIEWS.connected, { label: "connected", ok: true, signIn: "Reconnect", disconnect: true })
  assert.deepEqual(CONNECTOR_VIEWS.expired, { label: "expired", ok: false, signIn: "Connect", disconnect: true })
})

test("the dialog starts loading", () => {
  assert.deepEqual(NO_CONNECTORS, { list: null, error: null })
  assert.equal(connectorsPhase(NO_CONNECTORS), "loading")
})

test("a load shows the list", () => {
  const s = connectorsStep(NO_CONNECTORS, { type: "loaded", list: [github] })
  assert.deepEqual(s, { list: [github], error: null })
  assert.equal(connectorsPhase(s), "ready")
})

test("a load clears an earlier load's error", () => {
  assert.deepEqual(connectorsStep(loadError, { type: "loaded", list: [] }), { list: [], error: null })
})

test("a load keeps the error of a sign-in or disconnect", () => {
  assert.deepEqual(connectorsStep(actionError, { type: "loaded", list: [] }), { list: [], error: actionError.error })
})

test("a failed load keeps the last list and shows its error", () => {
  const s = connectorsStep({ list: [github], error: null }, { type: "loadFailed", text: "offline" })
  assert.deepEqual(s, loadError)
  assert.equal(connectorsPhase(s), "failed")
  assert.equal(connectorsPhase(connectorsStep(NO_CONNECTORS, { type: "loadFailed", text: "x" })), "failed")
})

test("a done action clears any error", () => {
  assert.deepEqual(connectorsStep(actionError, { type: "actionDone" }), { list: [github], error: null })
  assert.deepEqual(connectorsStep(loadError, { type: "actionDone" }), { list: [github], error: null })
})

test("a failed action shows its error and keeps the list", () => {
  assert.deepEqual(connectorsStep(loadError, { type: "actionFailed", text: "pop-up blocked" }), actionError)
})
