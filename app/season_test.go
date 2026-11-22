package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An older season comes from its own file (no OpenF1), joins the calendar and lists its races by round.
func TestSeasonFromArchive(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	p, _ := configPath()
	os.MkdirAll(filepath.Dir(p), 0o755)

	now := time.Now().UTC()
	this := CalMeeting{Key: 900, Name: "Las Vegas Grand Prix", Location: "Las Vegas", Start: now, End: now.Add(48 * time.Hour)}
	calendar.Lock()
	calendar.sessions = []CalSession{{Key: 9001, Meeting: 900, Name: "Race", Start: now, End: now.Add(2 * time.Hour)}}
	calendar.meetings = map[int]CalMeeting{900: this}
	calendar.loaded = time.Now()
	archive = map[int]calendarCache{}
	calendar.Unlock()

	d := func(m, day int) time.Time { return time.Date(2023, time.Month(m), day, 12, 0, 0, 0, time.UTC) }
	old := calendarCache{
		Meetings: []CalMeeting{
			{Key: 2, Name: "Saudi Arabian Grand Prix", Location: "Jeddah", Start: d(3, 17), End: d(3, 19)},
			{Key: 1, Name: "Bahrain Grand Prix", Location: "Sakhir", Start: d(3, 3), End: d(3, 5)},
		},
		Sessions: []CalSession{
			{Key: 21, Meeting: 2, Name: "Race", Start: d(3, 19), End: d(3, 19).Add(2 * time.Hour)},
			{Key: 11, Meeting: 1, Name: "Race", Start: d(3, 5), End: d(3, 5).Add(2 * time.Hour)},
		},
	}
	b, _ := json.Marshal(old)
	if err := os.WriteFile(archivePath(2023), b, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := season(2023)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Races) != 2 || s.Races[0].Name != "Bahrain GP" || s.Races[0].Round != 1 || s.Races[1].Round != 2 {
		t.Fatalf("races: %+v", s.Races)
	}
	if s.Years[len(s.Years)-1] != firstSeason || s.Years[0] < now.Year() {
		t.Fatalf("years: %v", s.Years)
	}
	// the weekend opens like any other, and the recent copy on disk stays without the old season
	if w, err := calendarWeekend(1); err != nil || w.Meeting.Name != "Bahrain Grand Prix" || len(w.Sessions) != 1 {
		t.Fatalf("weekend: %+v %v", w, err)
	}
	calendar.Lock()
	writeCalendarCache()
	calendar.Unlock()
	var c calendarCache
	cb, _ := os.ReadFile(calendarCachePath())
	json.Unmarshal(cb, &c)
	for _, x := range c.Sessions {
		if x.Start.Year() == 2023 {
			t.Fatal("old season written into the recent calendar copy")
		}
	}
}
