//go:build darwin

package main

import (
	"context"
	_ "embed"

	"fyne.io/systray"
)

//go:embed tray.png
var trayIcon []byte

// runWithTray shows the menu-bar icon and runs serve until serve returns.
// Exit server ends serve's context, as SIGTERM does. systray locks the main
// goroutine to the main thread, which the macOS menu bar needs.
func runWithTray(ctx context.Context, serve func(context.Context) error) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	systray.Run(func() {
		systray.SetTemplateIcon(trayIcon, trayIcon)
		systray.SetTooltip("YarGPT")
		exit := systray.AddMenuItem("Exit server", "Stop the YarGPT server")
		go func() {
			select {
			case <-exit.ClickedCh:
				stop()
			case <-ctx.Done():
			}
		}()
		go func() {
			done <- serve(ctx)
			systray.Quit()
		}()
	}, nil)
	return <-done
}
