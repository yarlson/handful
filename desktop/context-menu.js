// contextMenuTemplate returns the menu template for a right-click, built from
// the params of the webContents "context-menu" event. It returns an empty list
// when the menu would have no items. actions holds the effects that need
// Electron: replaceMisspelling(word), learnWord(word) and copyText(text).
function contextMenuTemplate(params, actions) {
  const flags = params.editFlags ?? {}
  const sections = []

  if (params.isEditable && params.misspelledWord) {
    const suggestions = (params.dictionarySuggestions ?? []).map((word) => ({
      label: word,
      click: () => actions.replaceMisspelling(word),
    }))
    if (suggestions.length === 0) suggestions.push({ label: "No Guesses Found", enabled: false })
    suggestions.push({ label: "Learn Spelling", click: () => actions.learnWord(params.misspelledWord) })
    sections.push(suggestions)
  }

  if (params.linkURL) {
    sections.push([{ label: "Copy Link", click: () => actions.copyText(params.linkURL) }])
  }

  if (params.isEditable) {
    sections.push([
      { role: "cut", enabled: Boolean(flags.canCut) },
      { role: "copy", enabled: Boolean(flags.canCopy) },
      { role: "paste", enabled: Boolean(flags.canPaste) },
      { type: "separator" },
      { role: "selectAll", enabled: Boolean(flags.canSelectAll) },
    ])
  } else if (params.selectionText && params.selectionText.trim() !== "") {
    sections.push([{ role: "copy", enabled: flags.canCopy !== false }])
  }

  return sections.flatMap((section, i) => (i === 0 ? section : [{ type: "separator" }, ...section]))
}

module.exports = { contextMenuTemplate }
