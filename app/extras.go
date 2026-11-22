package main

// Connects the optional extras to the Settings window and the opening of a session: the flags on Philips
// Hue lights (hue.go, flags.go) and the window positions (winplace_*.go).

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
)

func init() {
	stopLights = restoreLights
	afterSetupWindows = func() {
		if loadConfig().PlaceOff {
			return
		}
		winPlaceApps()
		winTidy()
		winBattleOnTop()
	}
	settingsExtra = func(v map[string]any) {
		h := loadHue()
		hue := map[string]any{"paired": h.BridgeIP != "" && h.AppKey != "", "bridge": h.BridgeIP, "group": h.Group,
			"enabled": h.Enabled, "showYellow": h.ShowYellow, "showSafetyCar": h.ShowSafetyCar, "vscPulse": h.VscPulse,
			"showRed": h.ShowRed, "showChequered": h.ShowChequered}
		if hue["paired"] == true {
			if g, err := hueGroups(); err == nil {
				hue["groups"] = g
			}
		}
		v["hue"] = hue
		if runtime.GOOS == "windows" {
			mv, uf1 := winPlacesSaved()
			v["places"] = map[string]any{"mv": mv, "uf1": uf1, "on": !loadConfig().PlaceOff}
		}
	}
	settingsApplyExtra = func(in map[string]any) {
		b := func(k string, dst *bool) {
			if x, ok := in[k].(bool); ok {
				*dst = x
			}
		}
		_ = saveHue(func(s *HueSettings) {
			b("hueEnabled", &s.Enabled)
			b("hueYellow", &s.ShowYellow)
			b("hueSafetyCar", &s.ShowSafetyCar)
			b("hueVscPulse", &s.VscPulse)
			b("hueRed", &s.ShowRed)
			b("hueChequered", &s.ShowChequered)
			if g, ok := in["hueGroup"].(string); ok {
				s.Group = clip(g, 64)
			}
		})
		if x, ok := in["placeApps"].(bool); ok {
			updateConfig(func(c *config) { c.PlaceOff = !x })
		}
		if x, ok := in["hueEnabled"].(bool); ok && !x {
			restoreLights()
		}
	}
}

// extraRoutes: the Settings window's buttons for the lights and the windows.
func extraRoutes(mux *http.ServeMux) {
	startFlags() // the flags loop (it does nothing until the lights are paired and switched on)
	// Pair bridge: the bridge from before, else the one Philips' discovery finds on this network.
	mux.HandleFunc("POST /api/hue/pair", func(w http.ResponseWriter, r *http.Request) {
		ip := loadHue().BridgeIP
		if ip == "" {
			ips, err := hueDiscover()
			if err != nil || len(ips) == 0 {
				replyErr(w, errors.New("no Hue bridge found on this network"))
				return
			}
			ip = ips[0]
		}
		replyErr(w, huePair(ip))
	})
	mux.HandleFunc("POST /api/hue/test", func(w http.ResponseWriter, r *http.Request) { replyErr(w, hueTest()) })
	mux.HandleFunc("POST /api/windows/save", func(w http.ResponseWriter, r *http.Request) { replyErr(w, winSavePlaces()) })
	mux.HandleFunc("POST /api/windows/tidy", func(w http.ResponseWriter, r *http.Request) {
		n := winTidy()
		winBattleOnTop()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"moved": n})
	})
}
