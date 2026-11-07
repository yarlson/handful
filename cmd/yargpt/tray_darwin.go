//go:build darwin

package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// tray.png is 32×32 pixels at 144 dpi, so AppKit draws it at 16×16 points.
//
//go:embed tray.png
var trayIcon []byte

// AppKit's run loop must run on the main thread, where main starts.
func init() { runtime.LockOSThread() }

const (
	accessoryActivationPolicy = 1                       // NSApplicationActivationPolicyAccessory: no Dock icon
	variableStatusItemLength  = -1.0                    // NSVariableStatusItemLength
	userInitiatedActivity     = 0x00FFFFFF &^ (1 << 20) // NSActivityUserInitiatedAllowingIdleSystemSleep
)

// runWithTray shows the menu-bar icon and runs serve. AppKit's run loop keeps
// the main thread and does not return, so the process exits when serve does.
func runWithTray(ctx context.Context, serve func(context.Context) error) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	app, err := addTrayIcon(stop)
	if err != nil {
		return err
	}
	go func() { exit(serve(ctx)) }()
	app.Send(objc.RegisterName("run"))
	return errors.New("the macOS run loop stopped")
}

// addTrayIcon adds the menu-bar icon. Its one item, Exit server, calls onExit.
// It calls AppKit through the Objective-C runtime, so the server needs no cgo.
func addTrayIcon(onExit func()) (objc.ID, error) {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return 0, fmt.Errorf("load AppKit: %w", err)
	}
	sel := objc.RegisterName
	class := func(name string) objc.ID { return objc.ID(objc.GetClass(name)) }

	app := class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("setActivationPolicy:"), accessoryActivationPolicy)
	// macOS can give App Nap to an app without windows, which slows its timers
	// and lowers its priority. The server must run bots at full speed.
	class("NSProcessInfo").Send(sel("processInfo")).
		Send(sel("beginActivityWithOptions:reason:"), userInitiatedActivity, nsString("YarGPT runs bots")).
		Send(sel("retain"))

	target, err := objc.RegisterClass("YarGPTTrayTarget", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{{
		Cmd: sel("exitServer:"),
		Fn:  func(objc.ID, objc.SEL, objc.ID) { onExit() },
	}})
	if err != nil {
		return 0, fmt.Errorf("register the menu target: %w", err)
	}
	item := class("NSMenuItem").Send(sel("alloc")).Send(sel("initWithTitle:action:keyEquivalent:"), nsString("Exit server"), sel("exitServer:"), nsString(""))
	item.Send(sel("setTarget:"), objc.ID(target).Send(sel("new")))
	menu := class("NSMenu").Send(sel("new"))
	menu.Send(sel("addItem:"), item)

	data := class("NSData").Send(sel("dataWithBytes:length:"), unsafe.Pointer(&trayIcon[0]), len(trayIcon))
	image := class("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
	image.Send(sel("setTemplate:"), true)

	status := class("NSStatusBar").Send(sel("systemStatusBar")).Send(sel("statusItemWithLength:"), variableStatusItemLength)
	status.Send(sel("retain")) // the status bar does not keep its items
	button := status.Send(sel("button"))
	button.Send(sel("setImage:"), image)
	button.Send(sel("setToolTip:"), nsString("YarGPT"))
	status.Send(sel("setMenu:"), menu)
	return app, nil
}

func nsString(s string) objc.ID {
	b := append([]byte(s), 0)
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), unsafe.Pointer(&b[0]))
}
