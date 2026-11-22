//go:build linux

package main

// The Linux side of the F1 desktop, window positions, Tidy up, battle mode and the main screen. Only Hyprland
// is supported (see hypr_linux.go); on any other desktop every function here does nothing, like before.
//
// How the Windows features map onto Hyprland:
//   - the F1 desktop is a named workspace ("F1" by default), made by Hyprland when F1 Party first goes there;
//   - a saved position is either floating (monitor, place and size) or tiled (Maximized: Hyprland's layout
//     decides the place, F1 Party only makes sure the window is tiled);
//   - Tidy up closes the seams between floating windows; tiled ones are already tidy;
//   - battle mode on top raises the floating Battle mode window above the Flag Display.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// desktopsSupported / placesSupported: whether the settings for these show on this computer.
func desktopsSupported() bool { return hyprOK() }
func placesSupported() bool   { return hyprOK() }

// hyprApp: which app a window belongs to ("mv", "uf1" or ""), by the program behind it.
func hyprApp(pid int) string {
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ""
	}
	switch strings.ToLower(filepath.Base(exe)) {
	case "multiviewer", "multiviewer-for-f1":
		return "mv"
	case "ultimate-f1viewer-with-f1mv", "ultimate-f1viewer":
		return "uf1"
	}
	return ""
}

type hyprWin struct {
	hyprClient
	app string
}

func (w hyprWin) target() string { return "address:" + w.Address }

func (w hyprWin) rect() wpRect {
	return wpRect{w.At[0], w.At[1], w.At[0] + w.Size[0], w.At[1] + w.Size[1]}
}

// hyprWins: the MultiViewer and Ultimate F1 Viewer windows.
func hyprWins() []hyprWin {
	var out []hyprWin
	for _, c := range hyprClients() {
		if !c.Mapped || c.Hidden {
			continue
		}
		if app := hyprApp(c.Pid); app != "" {
			out = append(out, hyprWin{c, app})
		}
	}
	return out
}

// hyprMains: the main MultiViewer and Ultimate F1 Viewer windows (not the feeds or overlays).
func hyprMains() []hyprWin {
	titles, online := wpTitles()
	var out []hyprWin
	for _, w := range hyprWins() {
		if w.app == "mv" && !online && w.Title != "MultiViewer" {
			continue
		}
		if wpMainTitle(w.app == "mv", w.Title, titles) {
			out = append(out, w)
		}
	}
	return out
}

// ---------------------------------------------------------------- F1 workspace

var hyprBack struct {
	sync.Mutex
	ws string // the workspace F1 Party left for the F1 one, to go back to on Close F1
}

func f1WorkspaceName() string {
	name := strings.TrimSpace(loadConfig().DesktopName)
	if name == "" {
		name = f1DesktopName
	}
	return name
}

// wsSelector: how Hyprland names a workspace in a dispatch ("3" for a plain numbered one, "name:F1" otherwise).
func wsSelector(id int, name string) string {
	if fmt.Sprint(id) == name {
		return name
	}
	return "name:" + name
}

func hyprFocusWorkspace(sel string) {
	hyprDispatch(hyprCmd{"workspace " + sel, "hl.dsp.focus({workspace=" + luaStr(sel) + "})"})
}

// toF1Desktop goes to the F1 workspace, so the windows that open next land there.
func toF1Desktop() {
	if !hyprOK() || loadConfig().DesktopOff {
		return
	}
	name := f1WorkspaceName()
	for _, m := range hyprMonitors() {
		if !m.Focused {
			continue
		}
		if strings.EqualFold(m.ActiveWorkspace.Name, name) {
			return
		}
		hyprBack.Lock()
		hyprBack.ws = wsSelector(m.ActiveWorkspace.ID, m.ActiveWorkspace.Name)
		hyprBack.Unlock()
	}
	hyprFocusWorkspace("name:" + name)
}

// leaveF1Desktop (Close F1): back to the workspace F1 Party came from. With remove, the windows still on the
// F1 workspace move along, so it is empty and Hyprland removes it; nothing closes.
func leaveF1Desktop(remove bool) {
	if !hyprOK() || loadConfig().DesktopOff {
		return
	}
	hyprBack.Lock()
	back := hyprBack.ws
	hyprBack.ws = ""
	hyprBack.Unlock()
	if back == "" {
		return
	}
	if remove {
		name := f1WorkspaceName()
		var cmds []hyprCmd
		for _, c := range hyprClients() {
			if strings.EqualFold(c.Workspace.Name, name) {
				t := "address:" + c.Address
				cmds = append(cmds, hyprCmd{
					"movetoworkspacesilent " + back + "," + t,
					"hl.dsp.window.move({workspace=" + luaStr(back) + ", follow=false, window=" + luaStr(t) + "})",
				})
			}
		}
		hyprDispatch(cmds...)
	}
	hyprFocusWorkspace(back)
}

// ---------------------------------------------------------------- main screen

// primaryScreen: the monitor at 0,0 (else the focused one), in layout pixels.
func primaryScreen() (w, h float64, ok bool) {
	if !hyprOK() {
		return 0, 0, false
	}
	ms := hyprMonitors()
	for _, m := range ms {
		if m.X == 0 && m.Y == 0 {
			a, b := m.size()
			return float64(a), float64(b), true
		}
	}
	for _, m := range ms {
		if m.Focused {
			a, b := m.size()
			return float64(a), float64(b), true
		}
	}
	return 0, 0, false
}

// ---------------------------------------------------------------- window positions

func hyprFloat(t string, on bool) hyprCmd {
	if on {
		return hyprCmd{"setfloating " + t, "hl.dsp.window.float({action='enable', window=" + luaStr(t) + "})"}
	}
	return hyprCmd{"settiled " + t, "hl.dsp.window.float({action='disable', window=" + luaStr(t) + "})"}
}

func hyprMoveResize(t string, r wpRect) []hyprCmd {
	return []hyprCmd{
		{fmt.Sprintf("resizewindowpixel exact %d %d,%s", r.R-r.L, r.B-r.T, t),
			fmt.Sprintf("hl.dsp.window.resize({x=%d, y=%d, relative=false, window=%s})", r.R-r.L, r.B-r.T, luaStr(t))},
		{fmt.Sprintf("movewindowpixel exact %d %d,%s", r.L, r.T, t),
			fmt.Sprintf("hl.dsp.window.move({x=%d, y=%d, relative=false, window=%s})", r.L, r.T, luaStr(t))},
	}
}

func winSavePlaces() error {
	if !hyprOK() {
		return fmt.Errorf("window positions need Windows or Hyprland")
	}
	wpMu.Lock()
	defer wpMu.Unlock()
	s, err := wpLoad()
	if err != nil {
		return err
	}
	ms := hyprMonitors()
	for _, w := range hyprMains() {
		var mon *hyprMonitor
		for i := range ms {
			if ms[i].ID == w.Monitor {
				mon = &ms[i]
			}
		}
		if mon == nil {
			continue
		}
		r := w.rect()
		place := &wpPlace{r.L - mon.X, r.T - mon.Y, r.R - r.L, r.B - r.T, !w.Floating, mon.Name}
		if w.app == "mv" {
			s.PlaceMultiViewer = place
		} else {
			s.PlaceUf1 = place
		}
	}
	p, err := wpPath()
	if err != nil {
		return err
	}
	return hueWrite(p, s)
}

func winPlaceApps() {
	if !hyprOK() {
		return
	}
	wpMu.Lock()
	defer wpMu.Unlock()
	s, err := wpLoad()
	if err != nil {
		return
	}
	ms := hyprMonitors()
	var cmds []hyprCmd
	for _, w := range hyprMains() {
		p := s.PlaceUf1
		if w.app == "mv" {
			p = s.PlaceMultiViewer
		}
		if p == nil || p.Width <= 0 || p.Height <= 0 {
			continue
		}
		if p.Maximized {
			if w.Floating {
				cmds = append(cmds, hyprFloat(w.target(), false))
			}
			continue
		}
		var mon *hyprMonitor
		for i := range ms {
			if ms[i].Name == p.Monitor || (mon == nil && ms[i].ID == w.Monitor) {
				mon = &ms[i]
			}
		}
		if mon == nil {
			continue
		}
		wk := mon.work()
		x, y := mon.X+p.X, mon.Y+p.Y
		if x+p.Width <= wk.L || x >= wk.R || y+p.Height <= wk.T || y >= wk.B {
			x, y = wk.L, wk.T // the saved place is off this monitor now
		}
		if !w.Floating {
			cmds = append(cmds, hyprFloat(w.target(), true))
		}
		cmds = append(cmds, hyprMoveResize(w.target(), wpRect{x, y, x + p.Width, y + p.Height})...)
	}
	hyprDispatch(cmds...)
}

// winTidy closes the seams between the floating MultiViewer and Ultimate F1 Viewer windows on the visible
// workspaces. Returns how many windows moved.
func winTidy() int {
	if !hyprOK() {
		return 0
	}
	wpMu.Lock()
	defer wpMu.Unlock()
	ms := hyprMonitors()
	shown := map[int]bool{}
	for _, m := range ms {
		shown[m.ActiveWorkspace.ID] = true
	}
	var active []hyprWin
	for _, w := range hyprWins() {
		if w.Floating && w.Fullscreen == 0 && shown[w.Workspace.ID] && !wpOverlay(w.Title) {
			active = append(active, w)
		}
	}
	cur := make([]wpRect, len(active))
	for i, w := range active {
		cur[i] = w.rect()
	}
	targets := append([]wpRect(nil), cur...)
	for _, m := range ms {
		wpSnap(cur, targets, m.work())
	}
	var cmds []hyprCmd
	moved := 0
	for i, w := range active {
		t := targets[i]
		if t == cur[i] || t.R <= t.L || t.B <= t.T {
			continue
		}
		cmds = append(cmds, hyprMoveResize(w.target(), t)...)
		moved++
	}
	hyprDispatch(cmds...)
	return moved
}

// winBattleOnTop raises the floating Battle mode window above the Flag Display.
func winBattleOnTop() {
	if !hyprOK() {
		return
	}
	for _, w := range hyprWins() {
		if w.app == "uf1" && w.Floating && w.Title == "Battle mode" {
			hyprDispatch(hyprCmd{"alterzorder top," + w.target(),
				"hl.dsp.window.alter_zorder({mode='top', window=" + luaStr(w.target()) + "})"})
		}
	}
}
