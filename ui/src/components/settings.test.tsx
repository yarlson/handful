import { renderToStaticMarkup } from "react-dom/server"
import { afterEach, assert, test, vi } from "vitest"
import type { SettingsView, Value } from "../lib/settings.ts"
import { Chips, RestartBanner, SelectField, SwitchField, TextField } from "./settings.tsx"

const v = (x: Partial<Value>): Value => ({
  value: "",
  default: "",
  source: "default",
  running: "",
  restart: true,
  ...x,
})
const save = async () => null
const view = (x: Partial<SettingsView>): SettingsView => ({
  file: { path: "/x/config.json" },
  build: "app",
  restart: "launchd",
  values: {},
  runtime: [],
  ...x,
})

test("a pending value shows the edited dot, the Restart required chip and what the server runs", () => {
  const html = renderToStaticMarkup(
    <TextField
      k="maxParallel"
      v={v({ value: 4, default: 3, running: 3, source: "file" })}
      save={save}
      locked={false}
    />,
  )
  assert.include(html, '<label for="setting-maxParallel"')
  assert.include(html, "changed from default")
  assert.include(html, "Restart required")
  assert.include(html, "Running server uses: 3")
  assert.include(html, "Reset to default (3)")
  assert.include(html, 'aria-describedby="setting-maxParallel-help"')
})

test("a value at its default has no markers", () => {
  const html = renderToStaticMarkup(<Chips v={v({ value: 3, default: 3, running: 3 })} />)
  assert.equal(html, "")
})

test("a flag-set value is disabled, chipped and explains the flag and the file", () => {
  const html = renderToStaticMarkup(
    <SelectField
      k="effort"
      v={v({ value: "high", running: "high", source: "flag", flag: "-effort high", file: "low" })}
      save={save}
      locked={false}
    />,
  )
  assert.include(html, "Set by -effort")
  assert.match(html, /<select[^>]*id="setting-effort"[^>]*disabled/)
  assert.include(html, "<code")
  assert.include(html, "-effort high")
  assert.include(html, "config.json says: Low. Remove the flag to use this setting.")
  assert.notInclude(html, "Restart required")
  assert.notInclude(html, "Reset to default")
})

test("a value the server wouldn't start with shows its error on the field", () => {
  const html = renderToStaticMarkup(
    <TextField
      k="claude"
      v={v({
        value: "/nope",
        default: "claude",
        running: "claude",
        source: "file",
        error: "YarGPT won't start with this program. Fix it before restarting.",
      })}
      save={save}
      locked={false}
    />,
  )
  assert.include(html, 'aria-invalid="true"')
  assert.include(html, 'aria-describedby="setting-claude-help setting-claude-error"')
  assert.include(html, "YarGPT won&#x27;t start with this program.")
})

test("a custom model shows its own field", () => {
  const html = renderToStaticMarkup(
    <SelectField
      k="model"
      v={v({ value: "claude-opus-5-5", running: "", source: "file" })}
      save={save}
      locked={false}
    />,
  )
  assert.include(html, "Custom model ID…")
  assert.include(html, 'aria-label="Custom model ID"')
  assert.include(html, 'value="claude-opus-5-5"')
})

test("a broken file locks every field", () => {
  const html = renderToStaticMarkup(
    <SwitchField k="updates.autoCheck" v={v({ value: true, default: true, restart: false })} save={save} locked />,
  )
  assert.include(html, 'role="switch"')
  assert.match(html, /disabled=""/)
})

afterEach(() => vi.unstubAllGlobals())

test("the banner lists what waits, and is refused while the server wouldn't start", () => {
  vi.stubGlobal("window", {})
  const pendingValues = {
    model: v({ value: "opus", running: "", source: "file" }),
    maxParallel: v({ value: 4, running: 3, default: 3, source: "file" }),
  }
  const ok = renderToStaticMarkup(<RestartBanner view={view({ values: pendingValues })} onRestart={() => {}} />)
  assert.include(ok, "2 changes apply after a restart")
  assert.include(ok, "Default model, Turns at once")
  assert.match(ok, /<button[^>]*id="settings-restart"/)
  assert.notMatch(ok, /id="settings-restart"[^>]*disabled/)
  const bad = renderToStaticMarkup(
    <RestartBanner view={view({ values: pendingValues, startError: "effort is bad" })} onRestart={() => {}} />,
  )
  assert.include(bad, "wouldn&#x27;t start with the file as it is: effort is bad")
  assert.match(bad, /<button[^>]*disabled/)
  const manual = renderToStaticMarkup(
    <RestartBanner view={view({ values: pendingValues, restart: "manual" })} onRestart={() => {}} />,
  )
  assert.include(manual, "./bin/yargpt")
  assert.notInclude(manual, "Restart server…")
  assert.equal(renderToStaticMarkup(<RestartBanner view={view({})} onRestart={() => {}} />), "")
})

test("in the desktop app a source build's banner still offers the restart, through the app", () => {
  vi.stubGlobal("window", { yargptSettings: {} })
  const html = renderToStaticMarkup(
    <RestartBanner
      view={view({ restart: "manual", values: { model: v({ value: "opus", running: "", source: "file" }) } })}
      onRestart={() => {}}
    />,
  )
  assert.include(html, "Restart server…")
  assert.notInclude(html, "./bin/yargpt")
})
