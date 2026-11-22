package main

// The "F1" virtual desktop that the F1 Race Control Stream Deck plugin sets up for MultiViewer. When it
// exists, F1 Party goes there before it opens a setup or a stream, so the new windows land on it instead
// of on the desktop F1 Party happens to be on. Without such a desktop nothing changes.
//
// Windows keeps the list of desktops, the current one and their names in the registry; switching uses the
// same Ctrl+Win+Left/Right shortcut a person would press (the same way the plugin does it).

import (
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const vdKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\VirtualDesktops`

// desktops: every desktop id in order, and the index of the current one.
func desktops() ([]string, int) {
	k, err := registry.OpenKey(registry.CURRENT_USER, vdKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, 0
	}
	defer k.Close()
	raw, _, err := k.GetBinaryValue("VirtualDesktopIDs")
	if err != nil {
		return nil, 0
	}
	var ids []string
	for i := 0; i+16 <= len(raw); i += 16 {
		ids = append(ids, guidString(raw[i:i+16]))
	}
	cur := 0
	c, _, err := k.GetBinaryValue("CurrentVirtualDesktop") // Windows 11
	if err != nil || len(c) != 16 {
		c = currentDesktopWin10()
	}
	if len(c) == 16 {
		for i, id := range ids {
			if id == guidString(c) {
				cur = i
			}
		}
	}
	return ids, cur
}

// currentDesktopWin10: Windows 10 keeps the current desktop per sign-in session, under SessionInfo.
func currentDesktopWin10() []byte {
	var sid uint32
	if windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &sid) != nil {
		return nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\SessionInfo\`+
		strconv.FormatUint(uint64(sid), 10)+`\VirtualDesktops`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	c, _, err := k.GetBinaryValue("CurrentVirtualDesktop")
	if err != nil {
		return nil
	}
	return c
}

// guidString writes 16 raw GUID bytes the way the registry names them: {xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx}.
func guidString(b []byte) string {
	const hex = "0123456789ABCDEF"
	order := []int{3, 2, 1, 0, -1, 5, 4, -1, 7, 6, -1, 8, 9, -1, 10, 11, 12, 13, 14, 15}
	var sb strings.Builder
	sb.WriteByte('{')
	for _, i := range order {
		if i < 0 {
			sb.WriteByte('-')
			continue
		}
		sb.WriteByte(hex[b[i]>>4])
		sb.WriteByte(hex[b[i]&15])
	}
	sb.WriteByte('}')
	return sb.String()
}

func desktopName(id string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, vdKey+`\Desktops\`+id, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	name, _, _ := k.GetStringValue("Name")
	return name
}

var procKeybdEvt = user32.NewProc("keybd_event") // user32: screen_windows.go

const (
	vkLControl  = 0xA2
	vkLWin      = 0x5B
	vkLeft      = 0x25
	vkRight     = 0x27
	vkD         = 0x44
	vkF4        = 0x73
	keyExtended = 0x1
	keyUp       = 0x2
)

func keyEvent(vk uintptr, flags uintptr) { procKeybdEvt.Call(vk, 0, flags, 0) }

// ctrlWin presses Ctrl+Win+arrow: one desktop to the left or right.
func ctrlWin(arrow uintptr) {
	keyEvent(vkLControl, 0)
	keyEvent(vkLWin, keyExtended)
	keyEvent(arrow, keyExtended)
	keyEvent(arrow, keyExtended|keyUp)
	keyEvent(vkLWin, keyExtended|keyUp)
	keyEvent(vkLControl, keyUp)
}

// toF1Desktop goes to the desktop for watching (setting, "F1" by default) and sets the taskbar the way the
// settings say. Without that desktop a new one is made and named (Task View may show the name only after the
// next sign-in; F1 Party finds it by it right away). Returns once Windows shows that desktop.
func toF1Desktop() {
	c := loadConfig()
	taskbarForWatching()
	if c.DesktopOff {
		return
	}
	name := strings.TrimSpace(c.DesktopName)
	if name == "" {
		name = f1DesktopName
	}
	ids, cur := desktops()
	target := -1
	for i, id := range ids {
		if strings.EqualFold(strings.TrimSpace(desktopName(id)), name) {
			target = i
			break
		}
	}
	if target < 0 {
		newDesktop(name, len(ids))
		return
	}
	if target == cur {
		return
	}
	arrow, steps := uintptr(vkRight), target-cur
	if steps < 0 {
		arrow, steps = vkLeft, -steps
	}
	for i := 0; i < steps; i++ {
		ctrlWin(arrow)
		time.Sleep(350 * time.Millisecond)
	}
	for i := 0; i < 20; i++ {
		if _, now := desktops(); now == target {
			time.Sleep(300 * time.Millisecond) // the switch animation
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// newDesktop makes a desktop at the end (Windows switches to it) and writes its name where Windows keeps it.
func newDesktop(name string, had int) {
	if had == 0 {
		return // no desktop list to read: leave the desktops alone
	}
	keyEvent(vkLControl, 0)
	keyEvent(vkLWin, keyExtended)
	keyEvent(vkD, 0)
	keyEvent(vkD, keyUp)
	keyEvent(vkLWin, keyExtended|keyUp)
	keyEvent(vkLControl, keyUp)
	for i := 0; i < 15; i++ {
		time.Sleep(200 * time.Millisecond)
		if ids, _ := desktops(); len(ids) > had {
			// the new desktop is the last one (never name another desktop when the current one can't be read)
			if k, _, err := registry.CreateKey(registry.CURRENT_USER, vdKey+`\Desktops\`+ids[len(ids)-1], registry.SET_VALUE); err == nil {
				_ = k.SetStringValue("Name", name)
				k.Close()
			}
			time.Sleep(300 * time.Millisecond)
			return
		}
	}
}

// leaveF1Desktop (Close F1): back to the first desktop; with remove, the watching desktop is removed when it
// shows (Ctrl+Win+F4, as a person would: anything still on it moves to the desktop next to it, nothing closes).
func leaveF1Desktop(remove bool) {
	c := loadConfig()
	if c.DesktopOff {
		return
	}
	name := strings.TrimSpace(c.DesktopName)
	if name == "" {
		name = f1DesktopName
	}
	ids, cur := desktops()
	if len(ids) < 2 || cur >= len(ids) {
		return
	}
	if remove && strings.EqualFold(strings.TrimSpace(desktopName(ids[cur])), name) {
		keyEvent(vkLControl, 0)
		keyEvent(vkLWin, keyExtended)
		keyEvent(vkF4, 0)
		keyEvent(vkF4, keyUp)
		keyEvent(vkLWin, keyExtended|keyUp)
		keyEvent(vkLControl, keyUp)
		time.Sleep(500 * time.Millisecond)
		ids, cur = desktops()
	}
	for i := 0; i < cur; i++ {
		ctrlWin(vkLeft)
		time.Sleep(350 * time.Millisecond)
	}
}

// desktopsSupported / placesSupported: whether the settings for these show on this computer.
func desktopsSupported() bool { return true }
func placesSupported() bool   { return true }
