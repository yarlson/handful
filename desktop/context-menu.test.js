const assert = require("node:assert/strict")
const { test } = require("node:test")
const { contextMenuTemplate } = require("./context-menu")

const allFlags = { canCut: true, canCopy: true, canPaste: true, canSelectAll: true }

function fakeActions() {
  const calls = []
  return {
    calls,
    replaceMisspelling: (word) => calls.push(["replace", word]),
    learnWord: (word) => calls.push(["learn", word]),
    copyText: (text) => calls.push(["copy", text]),
  }
}

function labels(template) {
  return template.map((item) => item.label ?? item.role ?? item.type)
}

test("a click on plain page text with no selection shows no menu", () => {
  assert.deepEqual(contextMenuTemplate({ isEditable: false, selectionText: "", linkURL: "", editFlags: allFlags }, fakeActions()), [])
  assert.deepEqual(contextMenuTemplate({ selectionText: "  \n" }, fakeActions()), [])
})

test("selected page text offers Copy", () => {
  const template = contextMenuTemplate({ isEditable: false, selectionText: "hello", editFlags: { canCopy: true } }, fakeActions())
  assert.deepEqual(template, [{ role: "copy", enabled: true }])
})

test("selected text without edit flags still offers Copy", () => {
  assert.deepEqual(contextMenuTemplate({ selectionText: "hello" }, fakeActions()), [{ role: "copy", enabled: true }])
})

test("an editable field offers the edit commands, enabled by its flags", () => {
  const template = contextMenuTemplate(
    { isEditable: true, selectionText: "", editFlags: { canCut: false, canCopy: false, canPaste: true, canSelectAll: true } },
    fakeActions(),
  )
  assert.deepEqual(template, [
    { role: "cut", enabled: false },
    { role: "copy", enabled: false },
    { role: "paste", enabled: true },
    { type: "separator" },
    { role: "selectAll", enabled: true },
  ])
})

test("an editable field without edit flags disables the commands", () => {
  const template = contextMenuTemplate({ isEditable: true }, fakeActions())
  assert.deepEqual(
    template.filter((item) => item.role).map((item) => item.enabled),
    [false, false, false, false],
  )
})

test("a selection in an editable field does not add a second Copy", () => {
  const template = contextMenuTemplate({ isEditable: true, selectionText: "hi", editFlags: allFlags }, fakeActions())
  assert.equal(template.filter((item) => item.role === "copy").length, 1)
})

test("a link offers Copy Link, which copies its address", () => {
  const actions = fakeActions()
  const template = contextMenuTemplate({ linkURL: "https://example.com/a", selectionText: "" }, actions)
  assert.deepEqual(labels(template), ["Copy Link"])
  template[0].click()
  assert.deepEqual(actions.calls, [["copy", "https://example.com/a"]])
})

test("a selected link offers Copy Link and Copy", () => {
  const template = contextMenuTemplate({ linkURL: "https://example.com/", selectionText: "docs", editFlags: allFlags }, fakeActions())
  assert.deepEqual(labels(template), ["Copy Link", "separator", "copy"])
})

test("a misspelled word offers its suggestions first", () => {
  const actions = fakeActions()
  const template = contextMenuTemplate(
    { isEditable: true, misspelledWord: "helo", dictionarySuggestions: ["hello", "help"], editFlags: allFlags },
    actions,
  )
  assert.deepEqual(labels(template), ["hello", "help", "Learn Spelling", "separator", "cut", "copy", "paste", "separator", "selectAll"])
  template[1].click()
  template[2].click()
  assert.deepEqual(actions.calls, [
    ["replace", "help"],
    ["learn", "helo"],
  ])
})

test("a misspelled word without suggestions says so", () => {
  const template = contextMenuTemplate({ isEditable: true, misspelledWord: "zzxq", dictionarySuggestions: [] }, fakeActions())
  assert.deepEqual(template[0], { label: "No Guesses Found", enabled: false })
  assert.equal(template[1].label, "Learn Spelling")
})

test("a misspelled word outside an editable field offers no suggestions", () => {
  const template = contextMenuTemplate({ isEditable: false, misspelledWord: "helo", dictionarySuggestions: ["hello"] }, fakeActions())
  assert.deepEqual(template, [])
})
