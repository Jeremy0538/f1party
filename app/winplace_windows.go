package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var wpUser = syscall.NewLazyDLL("user32.dll")
var wpDwm = syscall.NewLazyDLL("dwmapi.dll")
var wpKernel = syscall.NewLazyDLL("kernel32.dll")
var wpMu sync.Mutex

type wpRect struct{ L, T, R, B int32 }
type wpMonitor struct {
	Size          uint32
	Monitor, Work wpRect
	Flags         uint32
	Device        [32]uint16
}
type wpPlacement struct {
	Length, Flags, Show uint32
	Min, Max            [2]int32
	Normal              wpRect
}
type wpPlace struct {
	X, Y, Width, Height int32
	Maximized           bool
	Monitor             string
}
type wpSettings struct{ PlaceMultiViewer, PlaceUf1 *wpPlace }
type wpWindow struct {
	h              uintptr
	title, exe     string
	outer, visible wpRect
	mon            wpMonitor
}

func wpCall(name string, args ...uintptr) uintptr {
	r, _, _ := wpUser.NewProc(name).Call(args...)
	return r
}
func wpDPI() func() {
	runtime.LockOSThread()
	old := wpCall("SetThreadDpiAwarenessContext", ^uintptr(3))
	return func() { wpCall("SetThreadDpiAwarenessContext", old); runtime.UnlockOSThread() }
}
func wpMon(h uintptr) wpMonitor {
	m := wpMonitor{Size: uint32(unsafe.Sizeof(wpMonitor{}))}
	wpCall("GetMonitorInfoW", h, uintptr(unsafe.Pointer(&m)))
	return m
}

// Windows callbacks are never freed and a process may only make about 2000, so each enumeration callback
// is made once and fills a package variable (every caller holds wpMu).
var (
	wpEnumOut    []wpWindow
	wpEnumMons   []wpMonitor
	wpWindowsCB  = syscall.NewCallback(wpWindowsEach)
	wpMonitorsCB = syscall.NewCallback(func(h, dc, r, d uintptr) uintptr { wpEnumMons = append(wpEnumMons, wpMon(h)); return 1 })
)

func wpWindows() []wpWindow {
	wpEnumOut = nil
	wpCall("EnumWindows", wpWindowsCB, 0)
	out := wpEnumOut
	wpEnumOut = nil
	return out
}
func wpWindowsEach(h, _ uintptr) uintptr {
	if wpCall("IsWindowVisible", h) == 0 {
		return 1
	}
	var pid uint32
	wpCall("GetWindowThreadProcessId", h, uintptr(unsafe.Pointer(&pid)))
	p, _, _ := wpKernel.NewProc("OpenProcess").Call(0x1000, 0, uintptr(pid))
	if p == 0 {
		return 1
	}
	defer wpKernel.NewProc("CloseHandle").Call(p)
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	ok, _, _ := wpKernel.NewProc("QueryFullProcessImageNameW").Call(p, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if ok == 0 {
		return 1
	}
	exe := strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:n])))
	if exe != "multiviewer.exe" && exe != "ultimate-f1viewer-with-f1mv.exe" {
		return 1
	}
	var title [1024]uint16
	wpCall("GetWindowTextW", h, uintptr(unsafe.Pointer(&title[0])), 1024)
	w := wpWindow{h: h, title: syscall.UTF16ToString(title[:]), exe: exe}
	if wpCall("GetWindowRect", h, uintptr(unsafe.Pointer(&w.outer))) == 0 {
		return 1
	}
	w.visible = w.outer
	wpDwm.NewProc("DwmGetWindowAttribute").Call(h, 9, uintptr(unsafe.Pointer(&w.visible)), unsafe.Sizeof(w.visible))
	w.mon = wpMon(wpCall("MonitorFromWindow", h, 2))
	wpEnumOut = append(wpEnumOut, w)
	return 1
}
func wpMain(w wpWindow, titles []string) bool {
	if w.exe != "multiviewer.exe" {
		return w.title == "Ultimate Formula 1 Viewer"
	}
	if w.title == "MultiViewer" {
		return true
	}
	if !strings.HasSuffix(w.title, "— MultiViewer") || strings.Contains(w.title, "Live Timing") {
		return false
	}
	for _, t := range titles {
		if strings.HasPrefix(w.title, t+" —") {
			return false
		}
	}
	return true
}
func wpTitles() ([]string, bool) {
	ps, e := players()
	if e != nil {
		return nil, false
	}
	var titles []string
	for _, p := range ps {
		if p.StreamData != nil {
			titles = append(titles, p.StreamData.Title)
		}
	}
	return titles, true
}
func wpPath() (string, error) {
	d, e := os.UserConfigDir()
	return filepath.Join(d, "f1party", "windows.json"), e
}
func winSavePlaces() error {
	wpMu.Lock()
	defer wpMu.Unlock()
	done := wpDPI()
	defer done()
	p, e := wpPath()
	if e != nil {
		return e
	}
	var s wpSettings
	b, e := os.ReadFile(p)
	if e == nil {
		if e = json.Unmarshal(b, &s); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	titles, online := wpTitles()
	for _, w := range wpWindows() {
		if w.exe == "multiviewer.exe" && !online && w.title != "MultiViewer" {
			continue
		}
		if !wpMain(w, titles) {
			continue
		}
		n := wpPlacement{Length: uint32(unsafe.Sizeof(wpPlacement{}))}
		if wpCall("GetWindowPlacement", w.h, uintptr(unsafe.Pointer(&n))) == 0 {
			continue
		}
		r := w.outer
		if n.Show == 3 || n.Show == 2 {
			r = n.Normal
			dx := w.mon.Work.L - w.mon.Monitor.L
			dy := w.mon.Work.T - w.mon.Monitor.T
			r.L += dx
			r.R += dx
			r.T += dy
			r.B += dy
		}
		place := &wpPlace{r.L - w.mon.Monitor.L, r.T - w.mon.Monitor.T, r.R - r.L, r.B - r.T, n.Show == 3, syscall.UTF16ToString(w.mon.Device[:])}
		if w.exe == "multiviewer.exe" {
			s.PlaceMultiViewer = place
		} else {
			s.PlaceUf1 = place
		}
	}
	return hueWrite(p, s)
}
func wpMonitors() []wpMonitor {
	wpEnumMons = nil
	wpCall("EnumDisplayMonitors", 0, 0, wpMonitorsCB, 0)
	ms := wpEnumMons
	wpEnumMons = nil
	return ms
}
func winPlaceApps() {
	wpMu.Lock()
	defer wpMu.Unlock()
	done := wpDPI()
	defer done()
	p, e := wpPath()
	if e != nil {
		return
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return
	}
	var s wpSettings
	if json.Unmarshal(b, &s) != nil {
		return
	}
	titles, online := wpTitles()
	ms := wpMonitors()
	for _, w := range wpWindows() {
		if w.exe == "multiviewer.exe" && !online && w.title != "MultiViewer" {
			continue
		}
		if !wpMain(w, titles) {
			continue
		}
		p := s.PlaceUf1
		if w.exe == "multiviewer.exe" {
			p = s.PlaceMultiViewer
		}
		if p == nil || p.Width <= 0 || p.Height <= 0 {
			continue
		}
		m := w.mon
		for _, v := range ms {
			if syscall.UTF16ToString(v.Device[:]) == p.Monitor {
				m = v
				break
			}
		}
		x, y := m.Monitor.L+p.X, m.Monitor.T+p.Y
		if x+p.Width <= m.Work.L || x >= m.Work.R || y+p.Height <= m.Work.T || y >= m.Work.B {
			x, y = m.Work.L, m.Work.T
		}
		wpCall("ShowWindow", w.h, 9)
		wpCall("SetWindowPos", w.h, 0, uintptr(x), uintptr(y), uintptr(p.Width), uintptr(p.Height), 0x14)
		if p.Maximized {
			wpCall("ShowWindow", w.h, 3)
		}
	}
}
func wpOverlay(t string) bool {
	for _, s := range []string{"Battle mode", "Flag Display", "Crash Detection", "Sector Statuses"} {
		if strings.HasPrefix(t, s) {
			return true
		}
	}
	return false
}
func winTidy() int {
	wpMu.Lock()
	defer wpMu.Unlock()
	done := wpDPI()
	defer done()
	titles, online := wpTitles()
	moved := map[uintptr]bool{}
	wins := wpWindows()
	sort.Slice(wins, func(i, j int) bool {
		if wins[i].outer.T == wins[j].outer.T {
			return wins[i].outer.L > wins[j].outer.L
		}
		return wins[i].outer.T > wins[j].outer.T
	})
	for _, w := range wins {
		if w.exe == "multiviewer.exe" && online && !wpMain(w, titles) {
			if wpCall("SetWindowPos", w.h, 0, 0, 0, 0, 0, 0x213) != 0 {
				moved[w.h] = true
			}
		}
		square := int32(1)
		none := uint32(0xfffffffe)
		wpDwm.NewProc("DwmSetWindowAttribute").Call(w.h, 33, uintptr(unsafe.Pointer(&square)), 4)
		wpDwm.NewProc("DwmSetWindowAttribute").Call(w.h, 34, uintptr(unsafe.Pointer(&none)), 4)
	}
	for pass := 0; pass < 2; pass++ {
		wins = wpWindows()
		var active []wpWindow
		for _, w := range wins {
			var cloaked uint32
			wpDwm.NewProc("DwmGetWindowAttribute").Call(w.h, 14, uintptr(unsafe.Pointer(&cloaked)), 4)
			if wpOverlay(w.title) || cloaked != 0 || wpCall("IsIconic", w.h) != 0 {
				continue
			}
			if w.title == "Ultimate Formula 1 Viewer" {
				w.visible.L += 2
				w.visible.R -= 2
				w.visible.B -= 2
			}
			active = append(active, w)
		}
		targets := make([]wpRect, len(active))
		for i, w := range active {
			targets[i] = w.visible
		}
		for _, m := range wpMonitors() {
			for axis := 0; axis < 2; axis++ {
				type edge struct {
					pos int32
					i   int
					end bool
				}
				var edges []edge
				for i, w := range active {
					r := w.visible
					cx, cy := (r.L+r.R)/2, (r.T+r.B)/2
					if cx < m.Work.L || cx >= m.Work.R || cy < m.Work.T || cy >= m.Work.B {
						continue
					}
					lo, hi := r.L, r.R
					if axis == 1 {
						lo, hi = r.T, r.B
					}
					edges = append(edges, edge{lo, i, false}, edge{hi, i, true})
				}
				sort.Slice(edges, func(i, j int) bool { return edges[i].pos < edges[j].pos })
				lo, hi := m.Work.L, m.Work.R
				if axis == 1 {
					lo, hi = m.Work.T, m.Work.B
				}
				for i := 0; i < len(edges); {
					j := i
					for j+1 < len(edges) && edges[j+1].pos-edges[j].pos <= 8 {
						j++
					}
					pos := edges[i+(j-i+1)/2].pos
					nearLo, nearHi := false, false
					for k := i; k <= j; k++ {
						d := edges[k].pos - lo
						nearLo = nearLo || (d >= -8 && d <= 8)
						d = edges[k].pos - hi
						nearHi = nearHi || (d >= -8 && d <= 8)
					}
					if nearLo {
						pos = lo
					} else if nearHi {
						pos = hi
					}
					for k := i; k <= j; k++ {
						e := edges[k]
						r := &targets[e.i]
						if axis == 0 {
							if e.end {
								r.R = pos
							} else {
								r.L = pos
							}
						} else {
							if e.end {
								r.B = pos
							} else {
								r.T = pos
							}
						}
					}
					i = j + 1
				}
			}
		}
		changed := false
		for i, w := range active {
			t := targets[i]
			if t == w.visible || t.R <= t.L || t.B <= t.T {
				continue
			}
			x, y := w.outer.L+t.L-w.visible.L, w.outer.T+t.T-w.visible.T
			r, b := w.outer.R+t.R-w.visible.R, w.outer.B+t.B-w.visible.B
			if wpCall("SetWindowPos", w.h, 0, uintptr(x), uintptr(y), uintptr(r-x), uintptr(b-y), 0x14) != 0 {
				moved[w.h] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return len(moved)
}
func winBattleOnTop() {
	wpMu.Lock()
	defer wpMu.Unlock()
	wins := wpWindows()
	flags := map[uintptr]bool{}
	for _, w := range wins {
		if w.exe == "ultimate-f1viewer-with-f1mv.exe" && w.title == "Flag Display" {
			flags[w.h] = true
		}
	}
	for _, w := range wins {
		if w.exe != "ultimate-f1viewer-with-f1mv.exe" || w.title != "Battle mode" {
			continue
		}
		for h := wpCall("GetWindow", w.h, 3); h != 0; h = wpCall("GetWindow", h, 3) {
			if flags[h] {
				wpCall("SetWindowPos", w.h, ^uintptr(0), 0, 0, 0, 0, 0x213)
				break
			}
		}
	}
}

// winPlacesSaved: whether a position is saved for MultiViewer and for Ultimate F1 Viewer.
func winPlacesSaved() (mv, uf1 bool) {
	p, e := wpPath()
	if e != nil {
		return
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return
	}
	var s wpSettings
	if json.Unmarshal(b, &s) != nil {
		return
	}
	return s.PlaceMultiViewer != nil, s.PlaceUf1 != nil
}
