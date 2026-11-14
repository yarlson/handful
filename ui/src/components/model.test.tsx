import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import { FALLBACK_ALIASES, ModelChip, ModelEffortFields } from "./model.tsx"

const none = { value: "", source: "claude-code" as const }

test("a chip is a labelled button, attention-coloured with a warning icon on a mismatch", () => {
  const ok = renderToStaticMarkup(
    <ModelChip
      effective={{ model: "opus", modelSource: "bot", effort: "max", effortSource: "bot" }}
      aliases={FALLBACK_ALIASES}
      onClick={() => {}}
    />,
  )
  assert.include(ok, "<button")
  assert.include(ok, 'aria-label="Model Opus, set on this bot. Requested effort max, set on this bot. Open settings."')
  assert.include(ok, "Opus · max")
  assert.notInclude(ok, "attention")
  const bad = renderToStaticMarkup(
    <ModelChip
      effective={{
        model: "opus",
        modelSource: "bot",
        modelId: "claude-sonnet-5-5",
        modelMismatch: true,
        effort: "",
        effortSource: "claude-code",
      }}
      aliases={FALLBACK_ALIASES}
    />,
  )
  assert.include(bad, "attention")
  assert.include(bad, "<svg")
  assert.include(bad, 'aria-label="Asked for Opus, ran claude-sonnet-5-5.')
})

test("a team chip says when members override the team's model", () => {
  const html = renderToStaticMarkup(
    <ModelChip
      effective={{ model: "sonnet", modelSource: "team", effort: "medium", effortSource: "team" }}
      aliases={FALLBACK_ALIASES}
      prefix="Team: "
      suffix="· mixed"
      action="Open members."
      onClick={() => {}}
    />,
  )
  assert.include(html, "Team: ")
  assert.include(html, "Sonnet · medium")
  assert.include(html, "· mixed")
  assert.include(html, "Open members.")
})

test("the fields are labelled selects whose first option names what they inherit", () => {
  const html = renderToStaticMarkup(
    <ModelEffortFields
      id="bot"
      model=""
      effort=""
      onModel={() => {}}
      onEffort={() => {}}
      inherited={{ model: { value: "sonnet", source: "config" }, effort: none }}
      defaults={null}
    />,
  )
  assert.include(html, '<label data-slot="label" class')
  assert.match(html, /for="bot-model"/)
  assert.match(html, /<select id="bot-model"/)
  assert.match(html, /for="bot-effort"/)
  assert.include(html, "Default — Sonnet (from config)")
  assert.include(html, "Default (model&#x27;s own)")
  assert.include(html, "Custom model ID…")
  assert.include(html, "Extra high (xhigh)")
  assert.notInclude(html, "nearest lower")
})

test("a custom model shows its labelled field, and an error is tied to it", () => {
  const html = renderToStaticMarkup(
    <ModelEffortFields
      id="bot"
      model="claude opus"
      effort="max"
      onModel={() => {}}
      onEffort={() => {}}
      inherited={{ model: none, effort: none }}
      defaults={null}
      invalid="A model ID can't contain spaces or start with '-'."
    />,
  )
  assert.match(html, /for="bot-model-custom"/)
  assert.include(html, 'aria-invalid="true"')
  assert.include(html, 'aria-describedby="bot-model-error"')
  assert.include(html, 'id="bot-model-error"')
  assert.include(html, "Default — Claude Code&#x27;s default")
  assert.include(html, "nearest lower")
})

test("a member's chip names the member in its label only", () => {
  const html = renderToStaticMarkup(
    <ModelChip
      effective={{ model: "haiku", modelSource: "bot", effort: "low", effortSource: "team" }}
      aliases={FALLBACK_ALIASES}
      who="Triage"
    />,
  )
  assert.include(
    html,
    'aria-label="Triage: Model Haiku, set on this bot. Requested effort low, the team&#x27;s default."',
  )
  assert.notInclude(html, ">Triage")
})
