package main

// Track info: each race weekend in the season list links to its page on formula1.com (the circuit, the
// schedule, results). formula1.com's season page lists every race with its dates; a weekend is matched on
// the day it ends, so pre-season testing and renumbered rounds don't get in the way.

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const f1RacingPage = "https://www.formula1.com/en/racing/%d"

var f1Pages struct {
	sync.Mutex
	byYear map[int]map[string]string // year → "Jan 02" (the weekend's last day) → page URL
	at     map[int]time.Time
}

var (
	f1LinkRe = regexp.MustCompile(`(?s)<a[^>]+href="(/en/racing/(\d{4})/[a-z0-9-]+)"[^>]*>(.*?)</a>`)
	f1TagRe  = regexp.MustCompile(`<[^>]+>`)
	// "06 - 08 Nov" or "30 Oct - 01 Nov": the second date is the last day
	f1DatesRe = regexp.MustCompile(`(\d{1,2})(?:\s+[A-Za-z]{3})?\s*-\s*(\d{1,2})\s+([A-Za-z]{3})`)
)

// f1PageFor: the formula1.com page of the race weekend that ends on end ("" = not found).
func f1PageFor(year int, end time.Time) string {
	pages := f1PagesOf(year)
	for _, d := range []time.Time{end, end.AddDate(0, 0, -1), end.AddDate(0, 0, 1)} { // time zones
		if u := pages[d.Format("Jan 02")]; u != "" {
			return u
		}
	}
	return ""
}

func f1PagesOf(year int) map[string]string {
	f1Pages.Lock()
	defer f1Pages.Unlock()
	if f1Pages.byYear == nil {
		f1Pages.byYear, f1Pages.at = map[int]map[string]string{}, map[int]time.Time{}
	}
	if p, ok := f1Pages.byYear[year]; ok && time.Since(f1Pages.at[year]) < 12*time.Hour {
		return p
	}
	p, err := fetchF1Pages(year)
	if err != nil && f1Pages.byYear[year] != nil {
		return f1Pages.byYear[year] // keep the last good list
	}
	f1Pages.byYear[year], f1Pages.at[year] = p, time.Now()
	if err != nil { // try again in a few minutes, not every time the list opens
		f1Pages.at[year] = time.Now().Add(-12*time.Hour + 5*time.Minute)
	}
	return p
}

func fetchF1Pages(year int) (map[string]string, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf(f1RacingPage, year), nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 F1Party/"+version)
	c := http.Client{Timeout: 8 * time.Second}
	res, err := c.Do(req)
	if err != nil {
		return map[string]string{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return map[string]string{}, fmt.Errorf("formula1.com: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return map[string]string{}, err
	}
	return parseF1Pages(string(b), year), nil
}

func parseF1Pages(page string, year int) map[string]string {
	out := map[string]string{}
	for _, m := range f1LinkRe.FindAllStringSubmatch(page, -1) {
		if m[2] != fmt.Sprint(year) {
			continue
		}
		text := strings.Join(strings.Fields(html.UnescapeString(f1TagRe.ReplaceAllString(m[3], " "))), " ")
		if !strings.Contains(strings.ToUpper(text), "ROUND") {
			continue
		}
		d := f1DatesRe.FindStringSubmatch(text)
		if d == nil {
			continue
		}
		end, err := time.Parse("2 Jan 2006", d[2]+" "+strings.Title(strings.ToLower(d[3]))+" "+fmt.Sprint(year))
		if err != nil {
			continue
		}
		if k := end.Format("Jan 02"); out[k] == "" {
			out[k] = "https://www.formula1.com" + m[1]
		}
	}
	return out
}
