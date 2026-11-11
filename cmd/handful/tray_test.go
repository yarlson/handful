package main

import (
	"slices"
	"testing"
)

func TestTrayMenuChecksForUpdatesAboveExitServer(t *testing.T) {
	t.Parallel()
	want := []trayItem{
		{title: "Check for updates…", action: "checkForUpdates:"},
		{title: "Exit server", action: "exitServer:"},
	}
	if !slices.Equal(trayItems, want) {
		t.Fatalf("trayItems = %v, want %v", trayItems, want)
	}
	if checkForUpdatesURL != "handful://check-for-updates" {
		t.Fatalf("checkForUpdatesURL = %q; desktop/app.js handles handful://check-for-updates", checkForUpdatesURL)
	}
}
