// appMenuTemplate returns the template of the app's menu bar. updateItem is
// the updater's item; moveItem, Move to Handful…, goes right below it once
// the bridge found Handful's release, and is null before. openSettingsPage
// and openSettingsFile run the two settings items.
function appMenuTemplate({ appName, updateItem, moveItem = null, openSettingsPage, openSettingsFile }) {
  return [
    {
      label: appName,
      submenu: [
        { role: "about" },
        updateItem,
        ...(moveItem ? [moveItem] : []),
        { type: "separator" },
        { id: "settings", label: "Settings…", accelerator: "Command+,", click: () => void openSettingsPage() },
        { id: "settings-file", label: "Open Settings File", click: () => void openSettingsFile() },
        { type: "separator" },
        { role: "services" },
        { type: "separator" },
        { role: "hide" },
        { role: "hideOthers" },
        { role: "unhide" },
        { type: "separator" },
        { role: "quit" },
      ],
    },
    { role: "editMenu" },
    { role: "viewMenu" },
    { role: "windowMenu" },
  ]
}

module.exports = { appMenuTemplate }
