import { renderToStaticMarkup } from "react-dom/server"
import { assert, test } from "vitest"
import { BotDot } from "./bot-dot.tsx"
import { TeamDots } from "./team-dots.tsx"

const ids = (html: string) => [...html.matchAll(/<mask id="([^"]+)"/g)].map((m) => m[1])
const uses = (html: string) => [...html.matchAll(/mask="url\(#([^)]+)\)"/g)].map((m) => m[1])

test("a bot is a round dot with two eye cut-outs, hidden from screen readers, in the text colour", () => {
  const html = renderToStaticMarkup(<BotDot className="size-4 text-pink-600" />)
  assert.match(html, /^<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true" class="size-4 text-pink-600">/)
  assert.include(html, '<circle cx="8" cy="8" r="6.5"')
  assert.equal((html.match(/<rect [^>]*fill="#000"/g) ?? []).length, 2, "two eyes cut out")
  assert.deepEqual(uses(html), ids(html), "the dot uses its own mask")
})

test("several bots on one page each use their own mask id", () => {
  const html = renderToStaticMarkup(
    <>
      <BotDot />
      <BotDot />
      <BotDot />
    </>,
  )
  const masks = ids(html)
  assert.equal(masks.length, 3)
  assert.equal(new Set(masks).size, 3, `mask ids ${masks.join(", ")}`)
  assert.deepEqual(uses(html), masks, "each dot uses the mask beside it")
})

test("a team is three outlined bots with eyes, hidden from screen readers, with no ids to clash", () => {
  const html = renderToStaticMarkup(<TeamDots className="size-4" />)
  assert.match(
    html,
    /^<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1" aria-hidden="true" class="size-4">/,
  )
  assert.equal((html.match(/<circle /g) ?? []).length, 3, "three bots")
  assert.equal((html.match(/<rect [^>]*fill="currentColor"/g) ?? []).length, 6, "two eyes each")
  assert.notInclude(html, " id=")
})
