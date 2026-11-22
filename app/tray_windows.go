package main

// Windows: an icon by the clock. Closing the window hides it and the party keeps running; the icon (or
// starting F1 Party again) brings it back, and its menu quits.

import (
	_ "embed"
	"runtime"
	"sync/atomic"

	"github.com/energye/systray"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

var (
	trayOn   atomic.Bool // the tray icon is up, so closing the window only hides it
	quitting atomic.Bool
)

func startTray(show, quit func()) {
	go func() {
		runtime.LockOSThread() // the tray's window and message loop live on this thread
		systray.Run(func() {
			systray.SetIcon(trayIcon)
			systray.SetTitle("F1 Party")
			systray.SetTooltip("F1 Party")
			systray.SetOnClick(func(systray.IMenu) { show() })
			systray.SetOnDClick(func(systray.IMenu) { show() })
			open := systray.AddMenuItem("Open F1 Party", "")
			open.Click(show)
			systray.AddSeparator()
			q := systray.AddMenuItem("Quit", "")
			q.Click(func() { quitting.Store(true); quit() })
			trayOn.Store(true)
		}, func() {})
	}()
}

// keepRunningOnClose: true when the window should only hide (the tray icon keeps the app reachable).
func keepRunningOnClose() bool { return trayOn.Load() && !quitting.Load() }

func stopTray() { systray.Quit() }

// markQuitting: the next close quits instead of hiding to the tray.
func markQuitting() { quitting.Store(true) }
