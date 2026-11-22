package main

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type hueFlag int

const (
	hueGreen hueFlag = iota
	hueYellow
	hueSafetyCar
	hueVSC
	hueRed
	hueChequered
)

func hueTrackFlag(status string) hueFlag {
	switch status {
	case "2":
		return hueYellow
	case "4":
		return hueSafetyCar
	case "5":
		return hueRed
	case "6", "7":
		return hueVSC
	}
	return hueGreen
}
func hueWanted(f hueFlag, s HueSettings) hueFlag {
	if f == hueYellow && !s.ShowYellow || (f == hueSafetyCar || f == hueVSC) && !s.ShowSafetyCar || f == hueRed && !s.ShowRed || f == hueChequered && !s.ShowChequered {
		return hueGreen
	}
	return f
}
func hueColour(f hueFlag, dim bool) map[string]any {
	xy := []float64{.17, .70}
	switch f {
	case hueYellow:
		xy = []float64{.48, .47}
	case hueSafetyCar, hueVSC:
		xy = []float64{.59, .39}
	case hueRed:
		xy = []float64{.675, .322}
	case hueChequered:
		xy = []float64{.3227, .329}
	}
	bri := 254
	tt := 0
	if f == hueVSC {
		tt = 5
	}
	if f == hueChequered {
		tt = 10
	}
	if dim {
		bri = 63
	}
	return map[string]any{"on": true, "xy": xy, "bri": bri, "transitiontime": tt}
}

var hueStopped atomic.Bool
var hueFlags struct {
	sync.Mutex
	once                         sync.Once
	scene                        *hueScene
	current                      hueFlag
	until, lastFinish, lastPulse time.Time
	dim, untilIn                 bool
	seen                         int
	session                      string
}

func hueRestore() error {
	if hueFlags.scene == nil {
		return nil
	}
	e := hueFlags.scene.restore()
	if e == nil {
		hueFlags.scene = nil
		hueFlags.current = hueGreen
		hueFlags.until = time.Time{}
	}
	return e
}
// restoreLights: the lights back as they were (Close F1), without turning the flags setting off.
func restoreLights() {
	hueFlags.Lock()
	defer hueFlags.Unlock()
	_ = hueRestore()
	hueFlags.seen = -1
}

func stopFlags() {
	hueStopped.Store(true)
	_ = saveHue(func(s *HueSettings) { s.Enabled = false })
	hueFlags.Lock()
	defer hueFlags.Unlock()
	_ = hueRestore()
}
func startFlags() {
	hueFlags.once.Do(func() {
		hueFlags.seen = -1
		go func() {
			t := time.NewTicker(200 * time.Millisecond)
			defer t.Stop()
			next := time.Time{}
			for now := range t.C {
				hueFlags.Lock()
				s := loadHue()
				if hueStopped.Load() || !s.Enabled || s.BridgeIP == "" || s.AppKey == "" {
					_ = hueRestore()
					hueFlags.seen = -1
					hueFlags.Unlock()
					continue
				}
				if hueFlags.scene != nil && (hueFlags.scene.settings.BridgeIP != s.BridgeIP || hueFlags.scene.settings.AppKey != s.AppKey || hueFlags.scene.settings.Group != s.Group) {
					if hueRestore() != nil {
						hueFlags.Unlock()
						continue
					}
				}
				if !hueFlags.until.IsZero() && !now.Before(hueFlags.until) {
					_ = hueRestore()
				}
				if now.After(next) {
					huePoll(s, now)
					next = now.Add(time.Second)
				}
				if hueFlags.scene != nil {
					f := hueFlags.current
					period := 800 * time.Millisecond
					if f == hueChequered {
						period = 1100 * time.Millisecond
					}
					if (f == hueChequered || f == hueVSC && s.VscPulse) && now.Sub(hueFlags.lastPulse) >= period {
						hueFlags.dim = !hueFlags.dim
						_ = hueFlags.scene.set(hueColour(f, hueFlags.dim))
						hueFlags.lastPulse = now
					}
				}
				hueFlags.Unlock()
			}
		}()
	})
}

type hueTiming struct {
	TrackStatus   struct{ Status string }
	SessionStatus struct{ Status string }
	SessionInfo   struct {
		Name, Type string
		Key        any
	}
	RaceControlMessages struct{ Messages any }
	TimingData          struct {
		Lines map[string]struct{ InPit, Retired, Stopped bool }
	}
}

func hueFinishCount(v any) int {
	n := 0
	add := func(v any) {
		if m, ok := v.(map[string]any); ok {
			if f, ok := m["Flag"].(string); ok && strings.EqualFold(f, "CHEQUERED") {
				n++
			}
		}
	}
	switch v := v.(type) {
	case []any:
		for _, x := range v {
			add(x)
		}
	case map[string]any:
		for _, x := range v {
			add(x)
		}
	}
	return n
}
func huePoll(s HueSettings, now time.Time) {
	var out struct{ F1LiveTimingState *hueTiming }
	if gql("{ f1LiveTimingState { TrackStatus SessionStatus SessionInfo RaceControlMessages TimingData } }", nil, &out) != nil {
		_ = hueRestore() // MultiViewer closed: the lights back as they were
		return
	}
	t := out.F1LiveTimingState
	if t == nil {
		hueFlags.seen = -1
		_ = hueRestore()
		return
	}
	session := t.SessionInfo.Name
	if hueFlags.session != session {
		hueFlags.session = session
		hueFlags.seen = -1
		_ = hueRestore()
	}
	n := hueFinishCount(t.RaceControlMessages.Messages)
	if hueFlags.seen < 0 || n < hueFlags.seen {
		hueFlags.seen = n
		hueFlags.lastFinish = time.Time{}
	}
	f := hueWanted(hueTrackFlag(t.TrackStatus.Status), s)
	if n > hueFlags.seen {
		hueFlags.seen = n
		if s.ShowChequered && now.Sub(hueFlags.lastFinish) >= 5*time.Minute {
			f = hueChequered
			hueFlags.lastFinish = now
			hueFlags.untilIn = t.SessionInfo.Type != "Race"
			duration := 12 * time.Second
			if hueFlags.untilIn {
				duration = 5 * time.Minute
			}
			hueFlags.until = now.Add(duration)
		}
	}
	if t.SessionStatus.Status == "Finalised" {
		_ = hueRestore()
		return
	}
	if hueFlags.current == hueChequered {
		all := len(t.TimingData.Lines) > 0
		for _, l := range t.TimingData.Lines {
			all = all && (l.InPit || l.Retired || l.Stopped)
		}
		if hueFlags.untilIn && all {
			_ = hueRestore()
		}
		return
	}
	if t.SessionStatus.Status == "Finalised" || t.SessionStatus.Status == "Ends" {
		_ = hueRestore()
		return
	}
	if f == hueFlags.current {
		return
	}
	if f == hueGreen {
		if hueFlags.scene != nil {
			_ = hueFlags.scene.set(hueColour(hueGreen, false))
			hueFlags.current = hueGreen
			hueFlags.until = now.Add(3 * time.Second)
		}
		return
	}
	if hueFlags.scene == nil {
		snap, e := hueCapture(s)
		if e != nil {
			return
		}
		hueFlags.scene = snap
	}
	if f != hueChequered {
		hueFlags.until = time.Time{}
	}
	if hueFlags.scene.set(hueColour(f, false)) == nil {
		hueFlags.current = f
		hueFlags.dim = false
		hueFlags.lastPulse = now
	}
}
func hueTest() (err error) {
	hueFlags.Lock()
	defer hueFlags.Unlock()
	if e := hueRestore(); e != nil {
		return e
	}
	s := loadHue()
	snap, e := hueCapture(s)
	if e != nil {
		return e
	}
	defer func() {
		e := snap.restore()
		err = errors.Join(err, e)
		if e != nil {
			hueFlags.scene = snap
		}
	}()
	for _, f := range []hueFlag{hueYellow, hueSafetyCar, hueVSC, hueRed, hueChequered} {
		if hueWanted(f, s) == hueGreen {
			continue
		}
		if e = snap.set(hueColour(f, false)); e != nil {
			return e
		}
		time.Sleep(800 * time.Millisecond)
		if f == hueChequered || f == hueVSC && s.VscPulse {
			if e = snap.set(hueColour(f, true)); e != nil {
				return e
			}
			time.Sleep(800 * time.Millisecond)
		}
	}
	return nil
}
