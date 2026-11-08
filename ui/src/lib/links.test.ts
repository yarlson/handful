import { assert, test } from "vitest"
import { isExternalLink } from "./links.ts"

const origin = "http://localhost:4317"

test("a web link to another origin is external", () => {
  assert.strictEqual(isExternalLink("https://example.com/a", origin), true)
  assert.strictEqual(isExternalLink("http://example.com", origin), true)
  // Another port is another origin.
  assert.strictEqual(isExternalLink("http://localhost:9999/", origin), true)
})

test("a link to the app itself is not external", () => {
  assert.strictEqual(isExternalLink("http://localhost:4317/api/media/x", origin), false)
  assert.strictEqual(isExternalLink("/api/x", origin), false)
  assert.strictEqual(isExternalLink("#top", origin), false)
})

test("a missing, unparsable or non-web link is not external", () => {
  assert.strictEqual(isExternalLink(undefined, origin), false)
  assert.strictEqual(isExternalLink("", origin), false)
  assert.strictEqual(isExternalLink("http://[bad", origin), false)
  assert.strictEqual(isExternalLink("mailto:a@b.c", origin), false)
  assert.strictEqual(isExternalLink("ftp://example.com/f", origin), false)
})
