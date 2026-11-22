package main

import (
	"encoding/json"
	"slices"
	"testing"
)

// Positions come as numbers or strings; best of the rest skips the top teams and drivers out of the race.
func TestTimingOrder(t *testing.T) {
	var tm timing
	raw := `{
	  "TimingData": {"Lines": {
	    "1": {"Position": "1"}, "4": {"Position": 2}, "16": {"Position": "3"}, "14": {"Position": "4"},
	    "23": {"Position": "5", "Retired": true}, "31": {"Position": "6"}, "10": {"Position": "7"}, "99": {"Position": ""}}},
	  "DriverList": {"1": {"TeamName": "Red Bull Racing"}, "4": {"TeamName": "McLaren"}, "16": {"TeamName": "Ferrari"},
	    "14": {"TeamName": "Aston Martin"}, "23": {"TeamName": "Williams"}, "31": {"TeamName": "Haas F1 Team"}, "10": {"TeamName": "Alpine"}},
	  "ChampionshipPrediction": {"Drivers": {"4": {"CurrentPosition": 1}, "1": {"CurrentPosition": "2"}, "16": {"CurrentPosition": 3}}}
	}`
	if err := json.Unmarshal([]byte(raw), &tm); err != nil {
		t.Fatal(err)
	}
	if got := tm.byPosition(); !slices.Equal(got, []int{1, 4, 16, 14, 23, 31, 10}) {
		t.Errorf("byPosition = %v", got)
	}
	if got := tm.bestOfRest(defaultTopTeams, nil); !slices.Equal(got, []int{14, 31, 10}) {
		t.Errorf("bestOfRest = %v", got)
	}
	if got := tm.byChampionship(); !slices.Equal(got, []int{4, 1, 16}) {
		t.Errorf("byChampionship = %v", got)
	}
}

func TestKindOf(t *testing.T) {
	for in, want := range map[string]string{"Race": "race", "Sprint": "race", "Qualifying": "quali", "Sprint Qualifying": "quali",
		"Sprint Shootout": "quali", "Practice 2": "practice", "": "race"} {
		if got := kindOf(in); got != want {
			t.Errorf("kindOf(%q) = %q, want %q", in, got, want)
		}
	}
	if resolveSetup("My setup", "Qualifying") != "My setup" {
		t.Error("a chosen setup should stay that setup")
	}
}
