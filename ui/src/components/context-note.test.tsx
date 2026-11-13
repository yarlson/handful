import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import type { TurnUsage } from "@/lib/usage"
import { ContextNote } from "./bot-panel.tsx"

const last = (cold: boolean): TurnUsage => ({
  inputTokens: 4,
  cacheReadTokens: 9443,
  cacheWriteTokens: 3659,
  outputTokens: 306,
  calls: 3,
  contextTokens: 142_000,
  cold,
})

test("the Settings tab says the context and that the last turn came from cache", () => {
  const html = renderToStaticMarkup(<ContextNote usage={{ windowTokens: 200_000, last: last(false) }} />)
  assert.equal(
    html,
    '<p class="text-muted-foreground text-xs">Context: 142k of 200k · last turn: 3 calls, from cache</p>',
  )
})

test("after a cold turn it says the turn rebuilt the cache", () => {
  const html = renderToStaticMarkup(<ContextNote usage={{ windowTokens: 200_000, last: last(true) }} />)
  assert.include(html, "Context: 142k of 200k · last turn: 3 calls, rebuilt the cache")
})

test("nothing shows before the first recorded turn", () => {
  assert.equal(renderToStaticMarkup(<ContextNote usage={{ windowTokens: 200_000, last: null }} />), "")
  assert.equal(renderToStaticMarkup(<ContextNote usage={undefined} />), "")
})
