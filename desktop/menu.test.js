const assert = require("node:assert/strict")
const { test } = require("node:test")
const { appMenuTemplate } = require("./menu")

function template(moveItem) {
  const calls = []
  const items = appMenuTemplate({
    appName: "Handful",
    updateItem: { id: "updates", label: "Check for Updates…" },
    moveItem,
    openSettingsPage: () => calls.push("page"),
    openSettingsFile: () => calls.push("file"),
  })
  return { items, calls, appMenu: items[0].submenu }
}

test("the app menu has Settings… and Open Settings File right below it", () => {
  const { items, appMenu } = template()
  assert.equal(items[0].label, "Handful")
  assert.deepEqual(
    appMenu.map((item) => item.id ?? item.role ?? item.type),
    ["about", "updates", "separator", "settings", "settings-file", "separator", "services", "separator", "hide", "hideOthers", "unhide", "separator", "quit"],
  )
  const settings = appMenu.find((item) => item.id === "settings")
  assert.equal(settings.label, "Settings…")
  assert.equal(settings.accelerator, "Command+,")
  const file = appMenu.find((item) => item.id === "settings-file")
  assert.equal(file.label, "Open Settings File")
  assert.equal(file.accelerator, undefined)
  assert.deepEqual(
    items.slice(1).map((item) => item.role),
    ["editMenu", "viewMenu", "windowMenu"],
  )
})

test("Settings… opens the Settings page and Open Settings File opens the file", () => {
  const { appMenu, calls } = template()
  appMenu.find((item) => item.id === "settings").click()
  assert.deepEqual(calls, ["page"])
  appMenu.find((item) => item.id === "settings-file").click()
  assert.deepEqual(calls, ["page", "file"])
})

test("Move to Handful… goes right below the update item once Handful is found", () => {
  const moveItem = { id: "handful", label: "Move to Handful…", click: () => {} }
  const { appMenu } = template(moveItem)
  assert.deepEqual(
    appMenu.slice(0, 4).map((item) => item.id ?? item.role ?? item.type),
    ["about", "updates", "handful", "separator"],
  )
  assert.equal(appMenu[2], moveItem)
  assert.ok(!template(null).appMenu.some((item) => item.id === "handful"))
})
