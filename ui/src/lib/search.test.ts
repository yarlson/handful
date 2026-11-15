import { assert, test } from "vitest"
import type { Entry } from "./api"
import { matchesName, modKey, searchable, snippetOf, speakerOf, textOf } from "./search"

const base = { id: "e", botId: "b", seq: 1, rev: 1, turnId: "", createdAt: 0 }

test("the sidebar filter matches a part of a name with no case and ignores blanks around it", () => {
  assert.isTrue(matchesName("Release Manager", "manag"))
  assert.isTrue(matchesName("Release Manager", "  RELEASE m "))
  assert.isTrue(matchesName("Ревьюер", "РЕВЬ"))
  assert.isTrue(matchesName("Scout", ""), "an empty filter")
  assert.isTrue(matchesName("Scout", "   "), "a blank filter")
  assert.isFalse(matchesName("Scout", "scouts"))
})

test("the chat search starts from 2 characters, counted after trimming", () => {
  assert.isNull(searchable(""))
  assert.isNull(searchable(" a "))
  assert.isNull(searchable("😀"), "one character of two UTF-16 units")
  assert.equal(searchable(" ab "), "ab")
  assert.equal(searchable("я😀"), "я😀")
})

test("⌘F opens the search on a Mac and Ctrl+F elsewhere, by the key's place", () => {
  const key = { code: "KeyF", metaKey: false, ctrlKey: false, altKey: false, shiftKey: false }
  assert.isTrue(modKey({ ...key, metaKey: true }, "KeyF", true))
  assert.isFalse(modKey({ ...key, ctrlKey: true }, "KeyF", true), "Ctrl+F on a Mac moves the cursor")
  assert.isTrue(modKey({ ...key, ctrlKey: true }, "KeyF", false))
  assert.isFalse(modKey({ ...key, metaKey: true }, "KeyF", false))
  assert.isFalse(modKey(key, "KeyF", true), "F alone")
  assert.isFalse(modKey({ ...key, code: "KeyG", metaKey: true }, "KeyF", true))
  assert.isFalse(modKey({ ...key, metaKey: true, ctrlKey: true }, "KeyF", true))
  assert.isFalse(modKey({ ...key, ctrlKey: true, metaKey: true }, "KeyF", false))
  assert.isFalse(modKey({ ...key, metaKey: true, altKey: true }, "KeyF", true))
  assert.isFalse(modKey({ ...key, metaKey: true, shiftKey: true }, "KeyF", true))
})

test("⌘B and Ctrl+B match by the key's place, also on a layout where that key is not B", () => {
  // Russian ЙЦУКЕН: the key at KeyB types "и".
  const russian = { code: "KeyB", key: "и", metaKey: true, ctrlKey: false, altKey: false, shiftKey: false }
  assert.isTrue(modKey(russian, "KeyB", true))
  assert.isTrue(modKey({ ...russian, metaKey: false, ctrlKey: true }, "KeyB", false))
  // Dvorak: the key that types "b" is at KeyN, so it is not the shortcut.
  const dvorak = { ...russian, code: "KeyN", key: "b" }
  assert.isFalse(modKey(dvorak, "KeyB", true))
  assert.isFalse(modKey({ ...russian, altKey: true }, "KeyB", true))
  assert.isFalse(modKey({ ...russian, metaKey: false, ctrlKey: true }, "KeyB", true), "Ctrl+B on a Mac")
})

test("a result's speaker is you, the bot or member, or the other bot of a direct message", () => {
  const user: Entry = { ...base, kind: "user", body: { text: "hi" } }
  const bot: Entry = { ...base, kind: "bot", body: { text: "hi", fallback: false } }
  const member: Entry = { ...base, kind: "bot", body: { text: "hi", fallback: false, speakerName: "Ana" } }
  const agent = (direction: "in" | "out"): Entry => ({
    ...base,
    kind: "agent",
    body: { text: "hi", direction, peerId: "p", peerName: "Lead", priority: false },
  })
  assert.equal(speakerOf(user, "Scout"), "You")
  assert.equal(speakerOf(bot, "Scout"), "Scout")
  assert.equal(speakerOf(member, "Room"), "Ana")
  assert.equal(speakerOf(agent("in"), "Scout"), "Lead")
  assert.equal(speakerOf(agent("out"), "Scout"), "Scout")
  const event: Entry = { ...base, kind: "event", body: { type: "x", text: "y" } }
  assert.equal(speakerOf(event, "Scout"), "Scout")
})

test("a result's text is a message's text, and nothing for other entries", () => {
  assert.equal(textOf({ ...base, kind: "user", body: { text: "one" } }), "one")
  assert.equal(textOf({ ...base, kind: "bot", body: { text: "two", fallback: false } }), "two")
  assert.equal(
    textOf({
      ...base,
      kind: "agent",
      body: { text: "three", direction: "in", peerId: "p", peerName: "P", priority: false },
    }),
    "three",
  )
  assert.equal(textOf({ ...base, kind: "event", body: { type: "x", text: "four" } }), "")
})

test("a snippet marks the first match with the text's own case", () => {
  assert.deepEqual(snippetOf("Deploy the Server now. Server ok.", "server"), {
    before: "Deploy the ",
    match: "Server",
    after: " now. Server ok.",
  })
})

test("a snippet matches special characters literally", () => {
  assert.deepEqual(snippetOf("500 done, 50% done", "0%"), { before: "500 done, 5", match: "0%", after: " done" })
  assert.deepEqual(snippetOf("a.b (c) [d] x*y+z? ^$ {1} | \\", "(c) [d] x*y+z? ^$ {1} | \\"), {
    before: "a.b ",
    match: "(c) [d] x*y+z? ^$ {1} | \\",
    after: "",
  })
  assert.deepEqual(snippetOf("axb a.b", "a.b"), { before: "axb ", match: "a.b", after: "" })
})

test("a snippet keeps 40 characters before the match and 100 after, with … where the text goes on", () => {
  const text = `${"b".repeat(50)}MATCH${"a".repeat(120)}`
  assert.deepEqual(snippetOf(text, "match"), {
    before: `…${"b".repeat(40)}`,
    match: "MATCH",
    after: `${"a".repeat(100)}…`,
  })
  const exact = `${"b".repeat(40)}MATCH${"a".repeat(100)}`
  assert.deepEqual(snippetOf(exact, "match"), { before: "b".repeat(40), match: "MATCH", after: "a".repeat(100) })
})

test("a snippet shows each run of whitespace as one space and trims its ends", () => {
  assert.deepEqual(snippetOf("  first line\n\n  second   line  \n", "second"), {
    before: "first line ",
    match: "second",
    after: " line",
  })
  assert.deepEqual(snippetOf("a\n\nb", "a\n\nb"), { before: "", match: "a b", after: "" })
})

test("a snippet of a text without the match is its start", () => {
  assert.deepEqual(snippetOf("Hello there", "zz"), { before: "", match: "", after: "Hello there" })
})
