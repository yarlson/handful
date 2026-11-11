package main

// checkForUpdatesURL is the address that the menu-bar icon's Check for
// updates… opens. The app registers the handful scheme, so macOS hands the
// address to the app, which shows its window and checks.
const checkForUpdatesURL = "handful://check-for-updates"

// trayItem is an item of the menu-bar icon's menu. action names the
// Objective-C selector of the menu target that the item calls.
type trayItem struct {
	title  string
	action string
}

// trayItems lists the menu-bar icon's menu, from top to bottom.
var trayItems = []trayItem{
	{title: "Check for updates…", action: "checkForUpdates:"},
	{title: "Exit server", action: "exitServer:"},
}
