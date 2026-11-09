import { assert, test } from "vitest"
import { imageHost, isExternalLink, isLocalImage } from "./links.ts"

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

test("only the app's own images load in a post", () => {
  assert.isTrue(isLocalImage("/api/media/a.png", origin))
  assert.isTrue(isLocalImage("api/media/a.png", origin))
  assert.isTrue(isLocalImage("http://localhost:4317/api/media/a.png", origin))
  assert.isFalse(isLocalImage("https://example.com/a.png", origin))
  assert.isFalse(isLocalImage("//example.com/a.png", origin))
  assert.isFalse(isLocalImage("http://localhost:9999/a.png", origin))
  assert.isFalse(isLocalImage("https://localhost:4317/a.png", origin))
  assert.isFalse(isLocalImage(undefined, origin))
  assert.isFalse(isLocalImage("", origin))
  assert.isFalse(isLocalImage("http://[bad", origin))
  assert.isFalse(isLocalImage("javascript:alert(1)", origin))
  assert.isFalse(isLocalImage("ftp://localhost:4317/a.png", origin))
  // Behind https, the app's own images load too.
  assert.isTrue(isLocalImage("/api/media/a.png", "https://yargpt.local"))
  assert.isFalse(isLocalImage("http://yargpt.local/a.png", "https://yargpt.local"))
})

test("an image that doesn't load is named by its host", () => {
  assert.equal(imageHost("https://example.com/a/b.png?x=1", origin), "example.com")
  assert.equal(imageHost("http://cdn.example.org:8080/c.png", origin), "cdn.example.org:8080")
  assert.equal(imageHost("http://[bad", origin), "http://[bad")
})
