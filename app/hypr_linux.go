//go:build linux

package main

// Talking to Hyprland over its IPC socket ($XDG_RUNTIME_DIR/hypr/$HYPRLAND_INSTANCE_SIGNATURE/.socket.sock).
// Used for the F1 workspace, window positions, Tidy up and battle mode on Linux. Other Linux desktops get
// none of these (hyprOK is false and the settings stay hidden).
//
// Hyprland has two dispatch syntaxes: the classic one ("movewindowpixel exact 10 20,address:0x1") and, with a
// Lua config, a Lua expression ("hl.dsp.window.move({x=10, y=20, relative=false, window='address:0x1'})").
// Every command carries both; the first reply that asks for the Lua form switches to it for good.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var hyprLua atomic.Bool
var hyprMu sync.Mutex

func hyprSocket() string {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig == "" {
		return ""
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	p := filepath.Join(dir, "hypr", sig, ".socket.sock")
	if fileExists(p) {
		return p
	}
	p = filepath.Join("/tmp", "hypr", sig, ".socket.sock") // older Hyprland
	if fileExists(p) {
		return p
	}
	return ""
}

// hyprOK: F1 Party runs inside Hyprland.
func hyprOK() bool { return hyprSocket() != "" }

func hyprRequest(cmd string) (string, error) {
	p := hyprSocket()
	if p == "" {
		return "", fmt.Errorf("not running in Hyprland")
	}
	c, err := net.DialTimeout("unix", p, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write([]byte(cmd)); err != nil {
		return "", err
	}
	b, err := io.ReadAll(c)
	return string(b), err
}

// hyprJSON runs a query such as "clients" or "monitors" and decodes its JSON reply.
func hyprJSON(query string, v any) error {
	r, err := hyprRequest("j/" + query)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(r), v)
}

// hyprCmd is one dispatch in both syntaxes.
type hyprCmd struct{ classic, lua string }

func hyprDispatch(cmds ...hyprCmd) {
	hyprMu.Lock()
	defer hyprMu.Unlock()
	for _, c := range cmds {
		if hyprLua.Load() {
			_, _ = hyprRequest("dispatch " + c.lua)
			continue
		}
		r, err := hyprRequest("dispatch " + c.classic)
		if err == nil && strings.Contains(r, "hl.dispatch") {
			hyprLua.Store(true)
			_, _ = hyprRequest("dispatch " + c.lua)
		}
	}
}

// luaStr quotes s as a Lua string.
func luaStr(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`).Replace(s) + "'"
}

type hyprClient struct {
	Address    string   `json:"address"`
	Title      string   `json:"title"`
	Pid        int      `json:"pid"`
	At         [2]int32 `json:"at"`
	Size       [2]int32 `json:"size"`
	Floating   bool     `json:"floating"`
	Hidden     bool     `json:"hidden"`
	Mapped     bool     `json:"mapped"`
	Fullscreen int      `json:"fullscreen"`
	Monitor    int      `json:"monitor"`
	Workspace  struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"workspace"`
}

type hyprMonitor struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	X               int32    `json:"x"`
	Y               int32    `json:"y"`
	Width           int32    `json:"width"`
	Height          int32    `json:"height"`
	Scale           float64  `json:"scale"`
	Transform       int      `json:"transform"`
	Reserved        [4]int32 `json:"reserved"`
	Focused         bool     `json:"focused"`
	ActiveWorkspace struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"activeWorkspace"`
}

// size: the monitor in layout (logical) pixels, the units window positions use.
func (m hyprMonitor) size() (w, h int32) {
	s := m.Scale
	if s <= 0 {
		s = 1
	}
	w, h = int32(float64(m.Width)/s+0.5), int32(float64(m.Height)/s+0.5)
	if m.Transform%2 == 1 { // rotated 90 or 270 degrees
		w, h = h, w
	}
	return
}

func (m hyprMonitor) area() wpRect {
	w, h := m.size()
	return wpRect{m.X, m.Y, m.X + w, m.Y + h}
}

// work: the monitor without the space bars and panels keep for themselves.
func (m hyprMonitor) work() wpRect {
	a := m.area()
	return wpRect{a.L + m.Reserved[0], a.T + m.Reserved[1], a.R - m.Reserved[2], a.B - m.Reserved[3]}
}

func hyprMonitors() []hyprMonitor {
	var ms []hyprMonitor
	_ = hyprJSON("monitors", &ms)
	return ms
}

func hyprClients() []hyprClient {
	var cs []hyprClient
	_ = hyprJSON("clients", &cs)
	return cs
}
