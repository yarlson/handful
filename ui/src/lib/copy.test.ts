import { assert, expect, test } from "vitest"
import { copyText, execCopy, type CopyEnv } from "./copy"

// env records what each step of the copy path received.
function env(clipboard: CopyEnv["clipboard"] | "none", textarea: () => boolean) {
  const calls = { clipboard: [] as string[], textarea: [] as string[], selected: 0 }
  const e: CopyEnv = {
    clipboard:
      clipboard === "none"
        ? undefined
        : {
            writeText: (text) => {
              calls.clipboard.push(text)
              return clipboard!.writeText(text)
            },
          },
    execCopy: (text) => {
      calls.textarea.push(text)
      return textarea()
    },
  }
  const run = () =>
    copyText("ls -la", e, () => {
      calls.selected++
    })
  return { calls, run }
}

const writes = { writeText: () => Promise.resolve() }
const refuses = { writeText: () => Promise.reject(new DOMException("denied", "NotAllowedError")) }

test("copy uses the Clipboard API when it works and touches nothing else", async () => {
  const { calls, run } = env(writes, () => true)
  assert.equal(await run(), "copied")
  assert.deepEqual(calls, { clipboard: ["ls -la"], textarea: [], selected: 0 })
})

test("without the Clipboard API, as over plain HTTP, copy goes through the textarea", async () => {
  const { calls, run } = env("none", () => true)
  assert.equal(await run(), "copied")
  assert.deepEqual(calls, { clipboard: [], textarea: ["ls -la"], selected: 0 })
})

test("when the Clipboard API refuses the write, copy goes through the textarea", async () => {
  const { calls, run } = env(refuses, () => true)
  assert.equal(await run(), "copied")
  assert.deepEqual(calls, { clipboard: ["ls -la"], textarea: ["ls -la"], selected: 0 })
})

test("when every step fails, copy fails and selects the shown text", async () => {
  const { calls, run } = env(refuses, () => false)
  assert.equal(await run(), "failed")
  assert.deepEqual(calls, { clipboard: ["ls -la"], textarea: ["ls -la"], selected: 1 })
})

test("a textarea step that the browser refuses with an error counts as a failed copy", async () => {
  const { calls, run } = env("none", () => {
    throw new DOMException("not allowed", "SecurityError")
  })
  assert.equal(await run(), "failed")
  assert.equal(calls.selected, 1)
})

test("an error that is not a browser refusal propagates from either step", async () => {
  const broken = { writeText: () => Promise.reject(new TypeError("writeText is not a function")) }
  await expect(env(broken, () => true).run()).rejects.toThrow("writeText is not a function")
  const bug = env("none", () => {
    throw new TypeError("body is null")
  })
  await expect(bug.run()).rejects.toThrow("body is null")
  assert.equal(bug.calls.selected, 0)
})

// fakeDocument records the hidden textarea and the state of the page when execCommand runs.
function fakeDocument(copies: boolean, hasFocus = true) {
  const log: string[] = []
  const body: unknown[] = []
  const area = {
    value: "",
    readonly: false,
    style: {} as Record<string, string>,
    setAttribute(name: string, value: string) {
      if (name === "readonly" && value === "") this.readonly = true
    },
    select: () => log.push("select"),
    setSelectionRange: (start: number, end: number) => log.push(`range ${start}-${end}`),
    remove: () => {
      body.splice(body.indexOf(area), 1)
      log.push("remove")
    },
  }
  const focused = { focus: () => log.push("focus back") }
  const doc = {
    body: { append: (el: unknown) => body.push(el) },
    activeElement: hasFocus ? focused : null,
    createElement: (tag: string) => {
      log.push(`create ${tag}`)
      return area
    },
    execCommand: (command: string) => {
      log.push(`exec ${command} with ${body.includes(area) ? "area on page" : "no area"}`)
      return copies
    },
  }
  return { doc: doc as unknown as Document, area, log, body }
}

test("the textarea step copies from a selected, readonly, hidden textarea and cleans up", () => {
  const { doc, area, log, body } = fakeDocument(true)
  assert.isTrue(execCopy(doc, "héllo\nworld"))
  assert.equal(area.value, "héllo\nworld")
  assert.isTrue(area.readonly)
  assert.deepEqual(area.style, { position: "fixed", top: "0", left: "0", opacity: "0", fontSize: "16px" })
  assert.deepEqual(log, [
    "create textarea",
    "select",
    "range 0-11",
    "exec copy with area on page",
    "remove",
    "focus back",
  ])
  assert.deepEqual(body, [])
})

test("the textarea step reports a refused copy and still cleans up", () => {
  const { doc, log, body } = fakeDocument(false)
  assert.isFalse(execCopy(doc, "x"))
  assert.deepEqual(log.slice(-2), ["remove", "focus back"])
  assert.deepEqual(body, [])
})

test("the textarea step copies when no element has the focus", () => {
  const { doc, log } = fakeDocument(true, false)
  assert.isTrue(execCopy(doc, "x"))
  assert.equal(log.at(-1), "remove")
})
