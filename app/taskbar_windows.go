package main

// The Windows taskbar while you watch (setting "taskbar"), as the F1 Race Control plugin does it:
// "main" = only on the main monitor ("Show my taskbar on all displays" off), "autohide" = hide it
// automatically, "" = leave it alone. Switched directly and announced to the shell; when the party ends
// (or F1 Party closes) only what F1 Party changed is put back. What to put back is kept in the settings,
// so it also comes back after a crash.

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const tbKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`

var (
	shell32            = syscall.NewLazyDLL("shell32.dll")
	procAppBarMessage  = shell32.NewProc("SHAppBarMessage")
	procFindWindow     = user32.NewProc("FindWindowW")
	procSendMsgTimeout = user32.NewProc("SendMessageTimeoutW")
)

type appBarData struct {
	cbSize           uint32
	hWnd             uintptr
	uCallbackMessage uint32
	uEdge            uint32
	rc               [4]int32
	lParam           uintptr
}

const (
	abmGetState  = 4
	abmSetState  = 10
	absAutoHide  = 1
	absOnTop     = 2
	hwndBroadcst = 0xffff
	wmSettingChg = 0x001A
	smtoAbortHng = 0x0002
)

func taskbarOnAllDisplays() (bool, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, tbKey, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("MMTaskbarEnabled")
	return v != 0, err == nil
}

func setTaskbarOnAllDisplays(on bool) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, tbKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	v := uint32(0)
	if on {
		v = 1
	}
	_ = k.SetDWordValue("MMTaskbarEnabled", v)
	k.Close()
	// the shell rereads its taskbar settings on this broadcast (what the Settings app does too)
	area, _ := syscall.UTF16PtrFromString("TraySettings")
	var res uintptr
	procSendMsgTimeout.Call(hwndBroadcst, wmSettingChg, 0, uintptr(unsafe.Pointer(area)), smtoAbortHng, 2000, uintptr(unsafe.Pointer(&res)))
}

func taskbarAutoHide() bool {
	d := appBarData{cbSize: uint32(unsafe.Sizeof(appBarData{}))}
	r, _, _ := procAppBarMessage.Call(abmGetState, uintptr(unsafe.Pointer(&d)))
	return r&absAutoHide != 0
}

func setTaskbarAutoHide(on bool) {
	cls, _ := syscall.UTF16PtrFromString("Shell_TrayWnd")
	hwnd, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
	d := appBarData{cbSize: uint32(unsafe.Sizeof(appBarData{})), hWnd: hwnd, lParam: absOnTop}
	if on {
		d.lParam = absAutoHide
	}
	procAppBarMessage.Call(abmSetState, uintptr(unsafe.Pointer(&d)))
}

// taskbarForWatching applies the chosen taskbar setting (once; nothing when it is that way already).
func taskbarForWatching() {
	c := loadConfig()
	switch c.Taskbar {
	case "main":
		if all, ok := taskbarOnAllDisplays(); ok && all {
			setTaskbarOnAllDisplays(false)
			updateConfig(func(c *config) { c.TaskbarRestore = true })
		}
	case "autohide":
		if !taskbarAutoHide() {
			setTaskbarAutoHide(true)
			updateConfig(func(c *config) { c.TaskbarRestoreHide = true })
		}
	}
}

// taskbarRestore puts back what taskbarForWatching changed.
func taskbarRestore() {
	c := loadConfig()
	if c.TaskbarRestore {
		setTaskbarOnAllDisplays(true)
	}
	if c.TaskbarRestoreHide {
		setTaskbarAutoHide(false)
	}
	if c.TaskbarRestore || c.TaskbarRestoreHide {
		updateConfig(func(c *config) { c.TaskbarRestore, c.TaskbarRestoreHide = false, false })
	}
}
