package main

import (
	"testing"
	"time"
)

// formula1.com's season page: one link per race weekend with its round and dates; matched on the last day.
func TestParseF1Pages(t *testing.T) {
	page := `<a href="/en/racing/2026/mexico" class="x"><span>ROUND 19</span><span>30 Oct - 01 Nov</span></a>
<a href="/en/racing/2026/brazil"><p>ROUND 20</p> <p>06 - 08 NOV</p></a>
<a href="/en/racing/2026/brazil"><p>ROUND 20</p> Chequered Flag 06 - 08 Nov</a>
<a href="/en/racing/2025/brazil"><p>ROUND 21</p> 07 - 09 Nov</a>
<a href="/en/racing/2026">Schedule</a>`
	p := parseF1Pages(page, 2026)
	if len(p) != 2 || p["Nov 01"] != "https://www.formula1.com/en/racing/2026/mexico" || p["Nov 08"] != "https://www.formula1.com/en/racing/2026/brazil" {
		t.Fatalf("got %v", p)
	}
	f1Pages.byYear, f1Pages.at = map[int]map[string]string{2026: p}, map[int]time.Time{2026: time.Now()}
	// a race ending late on Saturday in Las Vegas time is Sunday in UTC: a day either way still matches
	if u := f1PageFor(2026, time.Date(2026, 11, 9, 2, 0, 0, 0, time.UTC)); u != p["Nov 08"] {
		t.Fatalf("a day later: %q", u)
	}
	if u := f1PageFor(2026, time.Date(2026, 12, 6, 0, 0, 0, 0, time.UTC)); u != "" {
		t.Fatalf("no race that weekend: %q", u)
	}
}

func TestAllowedF1Page(t *testing.T) {
	for u, want := range map[string]bool{
		"https://www.formula1.com/en/racing/2026/las-vegas":        true,
		"https://www.formula1.com/en/racing/2026/las-vegas?x=1":    false,
		"https://evil.example/en/racing/2026/las-vegas":            false,
		"https://www.formula1.com/en/racing/2026/a/../../../thing": false,
	} {
		if allowedURL(u) != want {
			t.Errorf("%s: want %v", u, want)
		}
	}
}
