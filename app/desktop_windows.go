package main

// The "F1" virtual desktop that the F1 Race Control Stream Deck plugin sets up for MultiViewer. When it
// exists, F1 Party goes there before it opens a setup or a stream, so the new windows land on it instead
// of on the desktop F1 Party happens to be on. Without such a desktop nothing changes.
//
// Windows keeps the list of desktops, the current one and their names in the registry; switching uses the
// same Ctrl+Win+Left/Right shortcut a person would press (the same way the plugin does it).

import (
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const vdKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\VirtualDesktops`

// f1DesktopName: the name the plugin gives its desktop (as Task View shows it).
const f1DesktopName = "F1"

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
	if c, _, err := k.GetBinaryValue("CurrentVirtualDesktop"); err == nil && len(c) == 16 {
		for i, id := range ids {
			if id == guidString(c) {
				cur = i
			}
		}
	}
	return ids, cur
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

// toF1Desktop goes to the "F1" desktop when there is one and it isn't the current one already. It returns
// once Windows shows that desktop (or after a few seconds).
func toF1Desktop() {
	ids, cur := desktops()
	target := -1
	for i, id := range ids {
		if strings.EqualFold(strings.TrimSpace(desktopName(id)), f1DesktopName) {
			target = i
			break
		}
	}
	if target < 0 || target == cur {
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
