package main

// The grid's line-up for the Settings (Fixed onboards): who drives for which team, and the team colours.
// It comes from the most recent session (OpenF1, a free public F1 data service), so a driver who comes in
// during the season shows up by himself. It is kept in lineup.json and fetched again after 6 hours; without
// it (offline, first start) the built-in list counts. The open session's own timing always goes on top.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const lineupURL = "https://api.openf1.org/v1/drivers?session_key=latest"

type lineup struct {
	Fetched time.Time         `json:"fetched"`
	Drivers []driverInfo      `json:"drivers"`
	Colours map[string]string `json:"colours"` // team → "#rrggbb"
}

var (
	lineupMu   sync.Mutex
	lineupHTTP = &http.Client{Timeout: 6 * time.Second}
)

// teamColourDefault: the team colours when there is no line-up yet (2026).
var teamColourDefault = map[string]string{
	"McLaren": "#F47600", "Ferrari": "#ED1131", "Mercedes": "#00D7B6", "Red Bull Racing": "#4781D7",
	"Aston Martin": "#229971", "Alpine": "#00A1E8", "Williams": "#1868DB", "Racing Bulls": "#6C98FF",
	"Haas F1 Team": "#9C9FA2", "Audi": "#F50537", "Cadillac": "#909090",
}

func lineupPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "f1party", "lineup.json")
}

// savedLineup: the line-up as last fetched (empty when there is none).
func savedLineup() lineup {
	var l lineup
	if p := lineupPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &l)
		}
	}
	return l
}

// refreshLineup fetches the latest session's line-up when the saved one is older than 6 hours (or missing).
// Errors keep the saved one.
func refreshLineup() {
	lineupMu.Lock()
	defer lineupMu.Unlock()
	if l := savedLineup(); len(l.Drivers) > 0 && time.Since(l.Fetched) < 6*time.Hour {
		return
	}
	res, err := lineupHTTP.Get(lineupURL)
	if err != nil {
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return
	}
	var in []struct {
		Code   string `json:"name_acronym"`
		Last   string `json:"last_name"`
		Team   string `json:"team_name"`
		Colour string `json:"team_colour"`
	}
	if json.NewDecoder(res.Body).Decode(&in) != nil || len(in) < 10 {
		return
	}
	l := lineup{Fetched: time.Now(), Colours: map[string]string{}}
	for _, d := range in {
		if d.Code == "" || d.Team == "" {
			continue
		}
		l.Drivers = append(l.Drivers, driverInfo{strings.ToUpper(d.Code), d.Last, d.Team})
		if c := strings.TrimPrefix(d.Colour, "#"); len(c) == 6 {
			l.Colours[d.Team] = "#" + strings.ToUpper(c)
		}
	}
	if b, err := json.Marshal(l); err == nil {
		if p := lineupPath(); p != "" {
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			_ = os.WriteFile(p, b, 0o644)
		}
	}
}

// teamColours: each team's colour: the built-in ones, the latest line-up's and the open session's on top.
func teamColours() map[string]string {
	out := map[string]string{}
	for k, v := range teamColourDefault {
		out[k] = v
	}
	for k, v := range savedLineup().Colours {
		out[k] = v
	}
	if t, ok := timingNow(); ok {
		for _, d := range t.DriverList {
			if c := strings.TrimPrefix(d.TeamColour, "#"); len(c) == 6 && d.TeamName != "" {
				out[d.TeamName] = "#" + strings.ToUpper(c)
			}
		}
	}
	return out
}
