package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Window placement shared by the platform backends (winplace_windows.go, winplace_linux.go): the saved
// positions file, which windows count as the main MultiViewer or Ultimate F1 Viewer window, and the edge
// snapping of Tidy up.

// wpMu: one placement action at a time.
var wpMu sync.Mutex

type wpRect struct{ L, T, R, B int32 }
type wpPlace struct {
	X, Y, Width, Height int32
	Maximized           bool // Windows: maximized. Hyprland: tiled instead of floating.
	Monitor             string
}
type wpSettings struct{ PlaceMultiViewer, PlaceUf1 *wpPlace }

func wpPath() (string, error) {
	d, e := os.UserConfigDir()
	return filepath.Join(d, "f1party", "windows.json"), e
}

// wpLoad reads the saved positions (an empty set when there is no file yet).
func wpLoad() (wpSettings, error) {
	var s wpSettings
	p, e := wpPath()
	if e != nil {
		return s, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		if os.IsNotExist(e) {
			return s, nil
		}
		return s, e
	}
	return s, json.Unmarshal(b, &s)
}

// wpMainTitle: whether a window is the main MultiViewer window (mv) or the main Ultimate F1 Viewer window,
// and not one of the feeds (titles = the titles of the open feeds), Live Timing or an overlay.
func wpMainTitle(mv bool, title string, titles []string) bool {
	if !mv {
		return title == "Ultimate Formula 1 Viewer"
	}
	if title == "MultiViewer" {
		return true
	}
	if !strings.HasSuffix(title, "— MultiViewer") || strings.Contains(title, "Live Timing") {
		return false
	}
	for _, t := range titles {
		if strings.HasPrefix(title, t+" —") {
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
func wpOverlay(t string) bool {
	for _, s := range []string{"Battle mode", "Flag Display", "Crash Detection", "Sector Statuses"} {
		if strings.HasPrefix(t, s) {
			return true
		}
	}
	return false
}
func winPlacesSaved() (mv, uf1 bool) {
	s, e := wpLoad()
	if e != nil {
		return
	}
	return s.PlaceMultiViewer != nil, s.PlaceUf1 != nil
}

// wpSnap closes the small seams on one monitor (work area): edges of the windows whose centre is on it (cur)
// that are within 8 px of each other, or of the work area's edge, move to one line. The result goes in targets.
func wpSnap(cur, targets []wpRect, work wpRect) {
	for axis := 0; axis < 2; axis++ {
		type edge struct {
			pos int32
			i   int
			end bool
		}
		var edges []edge
		for i := range cur {
			r := cur[i]
			cx, cy := (r.L+r.R)/2, (r.T+r.B)/2
			if cx < work.L || cx >= work.R || cy < work.T || cy >= work.B {
				continue
			}
			lo, hi := r.L, r.R
			if axis == 1 {
				lo, hi = r.T, r.B
			}
			edges = append(edges, edge{lo, i, false}, edge{hi, i, true})
		}
		sort.Slice(edges, func(i, j int) bool { return edges[i].pos < edges[j].pos })
		lo, hi := work.L, work.R
		if axis == 1 {
			lo, hi = work.T, work.B
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
