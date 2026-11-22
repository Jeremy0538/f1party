package main

// Windows: an icon by the clock while F1 Party runs, to bring the window forward or quit. Closing the window
// quits too.

import (
	_ "embed"
	"runtime"

	"github.com/energye/systray"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

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
			q.Click(quit)
		}, func() {})
	}()
}

func stopTray() { systray.Quit() }
