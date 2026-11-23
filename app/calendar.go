package main

// The F1 calendar (OpenF1, public) in the app, and planned watch parties: plan a live session, or a replay at a
// time you pick. Planning posts a Discord announcement with the time (Discord shows it in everyone's own time
// zone) and the party code, a reminder follows 15 minutes before, and at the time the app hosts the party with
// that code, opens the session and (for a replay) starts a minute before the session with the five lights.
// Plans only run while F1 Party runs (the tray is enough).

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CalSession struct {
	Key     int       `json:"key"`
	Meeting int       `json:"meeting"`
	Name    string    `json:"name"` // "Race", "Qualifying", "Practice 1", …
	Type    string    `json:"type"` // "Race", "Qualifying", "Practice"
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
}

type CalMeeting struct {
	Key      int       `json:"key"`
	Name     string    `json:"name"` // "Abu Dhabi Grand Prix"
	Location string    `json:"location"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
}

// plan: a planned watch party (kept in the config).
type plan struct {
	ID        string `json:"id"`
	Session   int    `json:"session"`
	Meeting   int    `json:"meeting"`
	Title     string `json:"title"` // "2026 Abu Dhabi GP · Race"
	Type      string `json:"type"`
	At        int64  `json:"at"` // unix seconds: when the party starts
	Replay    bool   `json:"replay,omitempty"`
	Code      string `json:"code"`
	Announced bool   `json:"announced,omitempty"`
	Reminded  bool   `json:"reminded,omitempty"`
}

var calendar struct {
	sync.Mutex
	sessions []CalSession
	meetings map[int]CalMeeting
	loaded   time.Time
	tried    time.Time
	wait     time.Duration // after a failed update: how long until the next try (doubles, at most an hour)
	lastErr  error
}

// contentTitles: F1 TV content id → "2026 Las Vegas GP · Qualifying" for sessions opened from the calendar
// (MultiViewer only knows the name once its timing has loaded).
var contentTitles sync.Map

// contentTypes: F1 TV content id → the session's name ("Qualifying", "Sprint", "Practice 2") for sessions
// opened from the calendar, so guests get the right layout at once (MultiViewer's timing still shows the
// previous session for a moment after a switch).
var contentTypes sync.Map

var calHTTP = &http.Client{Timeout: 12 * time.Second}

// getJSON with a short back-off when the service says "too many requests" (OpenF1 allows a few per second).
func getJSON(url string, out any) error {
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 F1Party/"+version)
		res, err := calHTTP.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode == 429 && attempt < 5 {
			res.Body.Close()
			time.Sleep(time.Duration(attempt) * 1500 * time.Millisecond)
			continue
		}
		defer res.Body.Close()
		if res.StatusCode == 401 || res.StatusCode == 403 {
			return errors.New("OpenF1 (where the calendar comes from) is closed while a session is live; the calendar is back after it")
		}
		if res.StatusCode != 200 {
			return fmt.Errorf("the F1 calendar can't be loaded right now (%s)", res.Status)
		}
		return json.NewDecoder(res.Body).Decode(out)
	}
}

// loadCalendar keeps the calendar up to date: from OpenF1 at most every 30 minutes, and from the copy saved on
// this computer when OpenF1 can't be reached. OpenF1 shuts out everyone without a paid key while a session is
// live, which is exactly when the calendar is wanted, so the copy matters.
func loadCalendar() error {
	calendar.Lock()
	defer calendar.Unlock()
	if len(calendar.sessions) == 0 {
		readCalendarCache()
	}
	// The calendar hardly changes: an update every 6 hours is plenty. After a failure the next try waits 5
	// minutes, then 10, 20, 40, 60, so a closed OpenF1 costs next to nothing.
	if time.Since(calendar.loaded) < 6*time.Hour || time.Since(calendar.tried) < calendar.wait {
		if len(calendar.sessions) == 0 {
			return calendar.lastErr
		}
		return nil
	}
	calendar.tried = time.Now()
	sessions, meetings, err := fetchCalendar()
	if err == nil && len(sessions) == 0 {
		err = errors.New("the F1 calendar is empty")
	}
	if err != nil {
		calendar.lastErr = err
		calendar.wait = min(max(calendar.wait*2, 5*time.Minute), time.Hour)
		if len(calendar.sessions) > 0 {
			return nil // keep showing the calendar we have
		}
		return err
	}
	calendar.sessions, calendar.meetings, calendar.loaded, calendar.lastErr, calendar.wait = sessions, meetings, time.Now(), nil, 0
	writeCalendarCache() // only the recent seasons; older ones have files of their own
	mergeArchive()
	return nil
}

// ---------------------------------------------------------------- older seasons

// firstSeason: OpenF1's first season (and so the first with meeting keys for F1 TV's replay search).
const firstSeason = 2023

// archive: older seasons, loaded only when you go there, then kept on disk for good (they don't change).
var archive = map[int]calendarCache{}

func archivePath(y int) string {
	p := calendarCachePath()
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), fmt.Sprintf("calendar-%d.json", y))
}

// recentSeason: kept up to date by the normal calendar (last, this and next year).
func recentSeason(y int) bool { return y >= time.Now().UTC().Year()-1 }

// loadSeason makes sure season y is in the calendar: an older one from its file, else once from OpenF1.
// Called with the calendar locked.
func loadSeason(y int) error {
	if recentSeason(y) || y < firstSeason {
		return nil
	}
	if _, ok := archive[y]; ok {
		return nil
	}
	if b, err := os.ReadFile(archivePath(y)); err == nil {
		var c calendarCache
		if json.Unmarshal(b, &c) == nil && len(c.Sessions) > 0 {
			archive[y] = c
			mergeArchive()
			return nil
		}
	}
	ss, ms, err := fetchSeason(y)
	if err == nil && len(ss) == 0 {
		err = fmt.Errorf("OpenF1 has no %d season", y)
	}
	if err != nil {
		return err
	}
	c := calendarCache{Sessions: ss, Saved: time.Now()}
	for _, m := range ms {
		c.Meetings = append(c.Meetings, m)
	}
	archive[y] = c
	if b, err := json.Marshal(c); err == nil && archivePath(y) != "" {
		_ = os.WriteFile(archivePath(y), b, 0o644)
	}
	mergeArchive()
	return nil
}

// mergeArchive adds the loaded older seasons to the calendar (once each) and keeps it in time order.
func mergeArchive() {
	have := map[int]bool{}
	for _, s := range calendar.sessions {
		have[s.Key] = true
	}
	if calendar.meetings == nil {
		calendar.meetings = map[int]CalMeeting{}
	}
	for _, c := range archive {
		for _, m := range c.Meetings {
			calendar.meetings[m.Key] = m
		}
		for _, s := range c.Sessions {
			if !have[s.Key] {
				calendar.sessions = append(calendar.sessions, s)
				have[s.Key] = true
			}
		}
	}
	sort.Slice(calendar.sessions, func(i, j int) bool { return calendar.sessions[i].Start.Before(calendar.sessions[j].Start) })
}

// SeasonRace: one race weekend in the season list.
type SeasonRace struct {
	Key      int       `json:"key"`
	Round    int       `json:"round"`
	Name     string    `json:"name"` // "Las Vegas GP"
	Location string    `json:"location"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Info     string    `json:"info,omitempty"` // its page on formula1.com (see f1info.go)
}

// Season: the weekends of one year, for jumping straight to any race.
type Season struct {
	Year  int          `json:"year"`
	Years []int        `json:"years"` // the seasons there are, newest first
	Races []SeasonRace `json:"races"`
}

func season(y int) (*Season, error) {
	if err := loadCalendar(); err != nil {
		return nil, err
	}
	calendar.Lock()
	defer calendar.Unlock()
	now := time.Now().UTC().Year()
	if y == 0 {
		y = now
	}
	err := loadSeason(y)
	out := &Season{Year: y}
	for yy := now + 1; yy >= firstSeason; yy-- {
		out.Years = append(out.Years, yy)
	}
	seen := map[int]bool{}
	for _, s := range calendar.sessions {
		m, ok := calendar.meetings[s.Meeting]
		if !ok || seen[m.Key] || m.Start.Year() != y {
			continue
		}
		seen[m.Key] = true
		out.Races = append(out.Races, SeasonRace{Key: m.Key, Round: len(out.Races) + 1, Name: shortGP(m.Name),
			Location: m.Location, Start: m.Start, End: m.End})
	}
	if len(out.Races) == 0 && err != nil {
		return out, err
	}
	if out.Years[0] == now+1 && y != now+1 && !hasSeason(now+1) {
		out.Years = out.Years[1:] // next year's calendar isn't out yet
	}
	return out, nil
}

func hasSeason(y int) bool {
	for _, m := range calendar.meetings {
		if m.Start.Year() == y {
			return true
		}
	}
	return false
}

// fetchCalendar: last year's, this year's and next year's sessions and meetings from OpenF1.
func fetchCalendar() ([]CalSession, map[int]CalMeeting, error) {
	year := time.Now().UTC().Year()
	var sessions []CalSession
	meetings := map[int]CalMeeting{}
	for i, y := range []int{year - 1, year, year + 1} {
		if i > 0 {
			time.Sleep(400 * time.Millisecond)
		}
		ss, ms, err := fetchSeason(y)
		if err != nil {
			if y == year+1 {
				break // next year's calendar isn't always out
			}
			return nil, nil, err
		}
		sessions = append(sessions, ss...)
		for k, m := range ms {
			meetings[k] = m
		}
	}
	return sessions, meetings, nil
}

// fetchSeason: one season's sessions and race weekends from OpenF1 (no testing, nothing cancelled).
func fetchSeason(y int) ([]CalSession, map[int]CalMeeting, error) {
	var sessions []CalSession
	meetings := map[int]CalMeeting{}
	{
		var ss []struct {
			Key       int    `json:"session_key"`
			Meeting   int    `json:"meeting_key"`
			Name      string `json:"session_name"`
			Type      string `json:"session_type"`
			Start     string `json:"date_start"`
			End       string `json:"date_end"`
			Cancelled bool   `json:"is_cancelled"`
		}
		if err := getJSON(fmt.Sprintf("https://api.openf1.org/v1/sessions?year=%d", y), &ss); err != nil {
			return nil, nil, err
		}
		var ms []struct {
			Key       int    `json:"meeting_key"`
			Name      string `json:"meeting_name"`
			Location  string `json:"location"`
			Start     string `json:"date_start"`
			End       string `json:"date_end"`
			Cancelled bool   `json:"is_cancelled"`
		}
		time.Sleep(400 * time.Millisecond)
		if err := getJSON(fmt.Sprintf("https://api.openf1.org/v1/meetings?year=%d", y), &ms); err != nil {
			return nil, nil, err
		}
		for _, m := range ms {
			if m.Cancelled || strings.Contains(strings.ToLower(m.Name), "testing") {
				continue
			}
			st, _ := time.Parse(time.RFC3339, m.Start)
			en, _ := time.Parse(time.RFC3339, m.End)
			meetings[m.Key] = CalMeeting{Key: m.Key, Name: m.Name, Location: m.Location, Start: st, End: en}
		}
		for _, s := range ss {
			if _, ok := meetings[s.Meeting]; !ok || s.Cancelled {
				continue
			}
			st, err1 := time.Parse(time.RFC3339, s.Start)
			en, err2 := time.Parse(time.RFC3339, s.End)
			if err1 != nil || err2 != nil {
				continue
			}
			sessions = append(sessions, CalSession{Key: s.Key, Meeting: s.Meeting, Name: s.Name, Type: s.Type, Start: st, End: en})
		}
	}
	return sessions, meetings, nil
}

// The copy on disk (next to the settings): what OpenF1 gave last time.
type calendarCache struct {
	Sessions []CalSession `json:"sessions"`
	Meetings []CalMeeting `json:"meetings"`
	Saved    time.Time    `json:"saved"`
}

func calendarCachePath() string {
	p, err := configPath()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "calendar.json")
}

func readCalendarCache() {
	path := calendarCachePath()
	b, err := os.ReadFile(path)
	if path == "" || err != nil {
		return
	}
	var c calendarCache
	if json.Unmarshal(b, &c) != nil || len(c.Sessions) == 0 {
		return
	}
	calendar.sessions, calendar.meetings = c.Sessions, map[int]CalMeeting{}
	calendar.loaded = c.Saved // a copy from less than 6 hours ago needs no update at start-up
	for _, m := range c.Meetings {
		calendar.meetings[m.Key] = m
	}
	mergeArchive()
}

func writeCalendarCache() {
	path := calendarCachePath()
	if path == "" {
		return
	}
	c := calendarCache{Saved: time.Now()}
	for _, s := range calendar.sessions {
		if recentSeason(s.Start.UTC().Year()) {
			c.Sessions = append(c.Sessions, s)
		}
	}
	for _, m := range calendar.meetings {
		if recentSeason(m.Start.UTC().Year()) {
			c.Meetings = append(c.Meetings, m)
		}
	}
	b, _ := json.Marshal(c)
	if os.WriteFile(path+".tmp", b, 0o644) == nil {
		_ = os.Rename(path+".tmp", path)
	}
}

func calSession(key int) (CalSession, CalMeeting, bool) {
	calendar.Lock()
	defer calendar.Unlock()
	for _, s := range calendar.sessions {
		if s.Key == key {
			return s, calendar.meetings[s.Meeting], true
		}
	}
	return CalSession{}, CalMeeting{}, false
}

// liveCalSession: the session that is on right now, if any.
func liveCalSession() (CalSession, CalMeeting, bool) {
	calendar.Lock()
	defer calendar.Unlock()
	now := time.Now()
	for _, s := range calendar.sessions {
		if now.After(s.Start) && now.Before(s.End) {
			return s, calendar.meetings[s.Meeting], true
		}
	}
	return CalSession{}, CalMeeting{}, false
}

// latestCalSession: the Race ("Race") or the qualifying ("Qualifying") on now, else the last one that ended
// (also when the next weekend has begun already).
func latestCalSession(name string) (CalSession, bool) {
	calendar.Lock()
	defer calendar.Unlock()
	now := time.Now()
	var best CalSession
	found := false
	for _, s := range calendar.sessions {
		if s.Name != name || s.Start.After(now) {
			continue
		}
		if !found || s.Start.After(best.Start) {
			best, found = s, true
		}
	}
	return best, found
}

// shortGP: "Abu Dhabi Grand Prix" → "Abu Dhabi GP".
func shortGP(name string) string { return strings.Replace(name, "Grand Prix", "GP", 1) }

func sessionTitle(s CalSession, m CalMeeting) string {
	return fmt.Sprintf("%d %s · %s", s.Start.Year(), shortGP(m.Name), s.Name)
}

// CalendarWeekend: one race weekend for the app. meeting 0 = the current one (or the next one).
type CalendarWeekend struct {
	Meeting  CalMeeting `json:"meeting"`
	Prev     int        `json:"prev,omitempty"`
	Next     int        `json:"next,omitempty"`
	PrevName string     `json:"prevName,omitempty"` // "Las Vegas GP"
	NextName string     `json:"nextName,omitempty"`
	Round    int        `json:"round,omitempty"`  // the weekend's place in its season
	Rounds   int        `json:"rounds,omitempty"` // weekends in that season
	Sessions []CalRow   `json:"sessions"`
}

type CalRow struct {
	CalSession
	Status string `json:"status"` // "done", "live", "soon"
	Plan   *plan  `json:"plan,omitempty"`
	Bell   bool   `json:"bell,omitempty"` // a notification 15 minutes before (by the setting, or its own bell)
	Auto   bool   `json:"auto,omitempty"` // on by the calendar's setting (the bell itself can't turn it off)
}

func calendarWeekend(meeting int) (*CalendarWeekend, error) {
	if err := loadCalendar(); err != nil {
		return nil, err
	}
	calendar.Lock()
	var keys []int
	seen := map[int]bool{}
	for _, s := range calendar.sessions {
		if !seen[s.Meeting] {
			seen[s.Meeting] = true
			keys = append(keys, s.Meeting)
		}
	}
	now := time.Now()
	if meeting == 0 {
		// the weekend that isn't over yet (the current one, else the next one), else the last one
		meeting = keys[len(keys)-1]
		for _, k := range keys {
			if m := calendar.meetings[k]; m.End.After(now.Add(-6 * time.Hour)) {
				meeting = k
				break
			}
		}
	}
	w := &CalendarWeekend{Meeting: calendar.meetings[meeting]}
	for i, k := range keys {
		if k == meeting {
			if i > 0 {
				w.Prev, w.PrevName = keys[i-1], shortGP(calendar.meetings[keys[i-1]].Name)
			}
			if i+1 < len(keys) {
				w.Next, w.NextName = keys[i+1], shortGP(calendar.meetings[keys[i+1]].Name)
			}
		}
	}
	// its round: the weekends of the same season in order, as the season list counts them
	year := w.Meeting.Start.Year()
	for _, k := range keys {
		if calendar.meetings[k].Start.Year() != year {
			continue
		}
		w.Rounds++
		if k == meeting {
			w.Round = w.Rounds
		}
	}
	for _, s := range calendar.sessions {
		if s.Meeting != meeting {
			continue
		}
		r := CalRow{CalSession: s, Status: "soon"}
		switch {
		case now.After(s.End):
			r.Status = "done"
		case now.After(s.Start):
			r.Status = "live"
		}
		w.Sessions = append(w.Sessions, r)
	}
	calendar.Unlock()
	cfg := loadConfig()
	plans := cfg.Plans
	for i := range w.Sessions {
		r := &w.Sessions[i]
		r.Bell = notifies(cfg, r.CalSession)
		r.Auto = notifiesBySetting(cfg.Notify, r.CalSession)
		for j := range plans {
			if plans[j].Session == w.Sessions[i].Key {
				p := plans[j]
				w.Sessions[i].Plan = &p
			}
		}
	}
	return w, nil
}

// f1tvReplay: the F1 TV content id of a session's replay (F1 TV's own search on the weekend).
func f1tvReplay(s CalSession) (string, error) {
	var out struct {
		ResultObj struct {
			Containers []struct {
				Metadata struct {
					ContentID      json.Number `json:"contentId"`
					ContentSubtype string      `json:"contentSubtype"`
					Emf            struct {
						Series            string `json:"Series"`
						MeetingSessionKey string `json:"MeetingSessionKey"`
					} `json:"emfAttributes"`
				} `json:"metadata"`
			} `json:"containers"`
		} `json:"resultObj"`
	}
	u := fmt.Sprintf("https://f1tv.formula1.com/2.0/R/ENG/WEB_DASH/ALL/PAGE/SEARCH/VOD/F1_TV_Pro_Annual/2?filter_MeetingKey=%d", s.Meeting)
	if err := getJSON(u, &out); err != nil {
		return "", errors.New("F1 TV can't be reached")
	}
	for _, c := range out.ResultObj.Containers {
		m := c.Metadata
		if m.ContentSubtype == "REPLAY" && m.Emf.Series == "FORMULA 1" && m.Emf.MeetingSessionKey == fmt.Sprint(s.Key) {
			return m.ContentID.String(), nil
		}
	}
	return "", errors.New("F1 TV has no replay of this session yet")
}

// f1tvLive: the F1 TV content id of a session while it is live. F1 TV's home page lists the live session
// (contentSubtype LIVE) under its session key; the replay later keeps the same id.
func f1tvLive(s CalSession) (string, error) {
	var page any
	if err := getJSON("https://f1tv.formula1.com/2.0/R/ENG/WEB_DASH/ALL/PAGE/395/F1_TV_Pro_Annual/2", &page); err != nil {
		return "", errors.New("F1 TV can't be reached")
	}
	if id := f1tvFindLive(page, fmt.Sprint(s.Key)); id != "" {
		return id, nil
	}
	return "", errors.New("F1 TV doesn't show the live session yet: open it in MultiViewer")
}

// f1tvFindLive walks F1 TV's page for the Formula 1 video of this session that is live (or already a replay).
func f1tvFindLive(o any, sessionKey string) string {
	switch v := o.(type) {
	case map[string]any:
		if m, ok := v["metadata"].(map[string]any); ok {
			emf, _ := m["emfAttributes"].(map[string]any)
			sub, _ := m["contentSubtype"].(string)
			if emf != nil && jsonText(emf["MeetingSessionKey"]) == sessionKey && emf["Series"] == "FORMULA 1" &&
				(sub == "LIVE" || sub == "REPLAY") {
				if id := jsonText(m["contentId"]); id != "" {
					return id
				}
			}
		}
		for _, x := range v {
			if id := f1tvFindLive(x, sessionKey); id != "" {
				return id
			}
		}
	case []any:
		for _, x := range v {
			if id := f1tvFindLive(x, sessionKey); id != "" {
				return id
			}
		}
	}
	return ""
}

// jsonText: a JSON string or number as text ("" for anything else).
func jsonText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// openCalSession opens a session in MultiViewer the way you chose under "Open with": the Stream Deck plugin,
// one of your setups, or just F1 Live. contentID "" = the live session (the plugin finds it itself, otherwise
// F1 TV's home page says which content it is).
func (p *Party) openCalSession(s CalSession, contentID string) error {
	setup := p.OpenSetup()
	if setup == setupStreamDeck {
		return pluginOpen(sessionKind(s.Type+" "+s.Name), contentID)
	}
	if err := mvStart(); err != nil {
		return err
	}
	for i := 0; i < 40 && !mvRunning(); i++ {
		time.Sleep(time.Second)
	}
	if contentID == "" {
		id, err := f1tvLive(s)
		if err != nil {
			return err
		}
		contentID = id
	}
	if ps, err := players(); err == nil && mainFeed(ps, contentID) != nil {
		return nil // open already
	}
	if setup != "" && mvDebugReady() {
		if err := openChosenSetup(setup, contentID, s.Type+" "+s.Name); err == nil {
			return nil
		}
	}
	if _, err := playerCreate(contentID, mainTitle); err != nil {
		if _, err2 := playerCreate(contentID, "INTERNATIONAL"); err2 != nil {
			return err
		}
	}
	placeSoon()
	return nil
}

// WatchNow (calendar): host this session now (a replay, or the live one), open it and, for a replay, go to a
// minute before the start with the lights. Keeps the party you're hosting; a guest can't.
// startAt: where the party starts once the session is open: "begin" (the broadcast's beginning), "start" (a
// minute before the session starts) or "live"; "" = a minute before the start for a replay, live as it comes.
func (p *Party) WatchNow(key int, name, startAt string) error { return p.watch(key, name, startAt, true) }

// watch: WatchNow, or with host false (the Stream Deck's Race, Quali and Session keys) only open the session,
// without a party when you're not in one (a party you host switches to it).
func (p *Party) watch(key int, name, startAt string, host bool) error {
	s, m, ok := calSession(key)
	if !ok {
		return errors.New("unknown session")
	}
	if p.Snapshot().Mode == "guest" {
		return errors.New("you're a guest in a party; leave it first")
	}
	contentID := ""
	live := time.Now().After(s.Start) && time.Now().Before(s.End)
	if !live {
		id, err := f1tvReplay(s)
		if err != nil {
			return err
		}
		contentID = id
	}
	if contentID == "" { // the live session: open already?
		if ps, err := players(); err == nil {
			if f := mainFeed(ps, ""); f != nil && f.State != nil && f.State.Live {
				return errors.New("the live session is open already")
			}
		}
	}
	switching := p.Snapshot().Mode == "host"
	if !switching && host {
		p.Start("host", newCode(), name)
	}
	p.SetNote("Opening " + sessionTitle(s, m) + "…")
	if contentID != "" {
		contentTitles.Store(contentID, sessionTitle(s, m))
		contentTypes.Store(contentID, s.Name)
	}
	go func() {
		if switching {
			// another session for the party: the old one closes first (with a setup or the Stream Deck also its
			// timing windows, as the new layout brings its own); guests do the same when they see the switch
			mvCloseOthers(contentID, p.Setup() != "")
		}
		if err := p.openCalSession(s, contentID); err != nil {
			p.SetNote(err.Error())
			return
		}
		switch {
		case startAt != "":
			p.jumpWhenLoaded(contentID, startAt)
		case contentID != "":
			p.jumpWhenLoaded(contentID, "start")
		}
	}()
	return nil
}

// jumpWhenLoaded waits (up to 2 minutes) for the session's stream (and for "start" its timing), then jumps
// there. For a replay "start" = a minute before the start: nobody sees how it ends while the stream loads.
func (p *Party) jumpWhenLoaded(contentID, to string) {
	for i := 0; i < 60; i++ {
		time.Sleep(2 * time.Second)
		ps, err := players()
		if err != nil || mainFeed(ps, contentID) == nil {
			continue
		}
		if to == "start" {
			if _, ok := sessionStart(); !ok {
				continue
			}
			if _, ok := trackNow(); !ok {
				continue
			}
		}
		if err := p.Jump(to); err != nil {
			p.SetNote(err.Error())
		}
		return
	}
	p.SetNote("Session open. Start −1 min needs a setup with live timing")
}

// Plan a watch party for a session: live at its start, or a replay at a time you pick (at = unix seconds).
func Plan(key int, at int64, name string) (*plan, error) {
	s, m, ok := calSession(key)
	if !ok {
		return nil, errors.New("unknown session")
	}
	pl := &plan{ID: newCode(), Session: s.Key, Meeting: s.Meeting, Title: sessionTitle(s, m), Type: s.Type, Code: newCode()}
	if time.Now().Before(s.Start) && at == 0 {
		pl.At = s.Start.Unix()
	} else {
		if at == 0 {
			return nil, errors.New("pick a time for the replay")
		}
		if time.Unix(at, 0).Before(time.Now().Add(-time.Minute)) {
			return nil, errors.New("that time has passed")
		}
		if time.Unix(at, 0).Before(s.End) {
			return nil, errors.New("pick a time after the session has ended")
		}
		pl.At, pl.Replay = at, true
	}
	if len(discordDests()) > 0 {
		if err := toAllDests(func(d string) error { return announcePlan(d, name, pl) }); err != nil {
			log.Printf("plan announcement: %v", err)
		} else {
			pl.Announced = true
		}
	}
	updateConfig(func(c *config) {
		var keep []plan
		for _, x := range c.Plans {
			if x.Session != pl.Session {
				keep = append(keep, x)
			}
		}
		c.Plans = append(keep, *pl)
	})
	return pl, nil
}

func Unplan(id string) {
	updateConfig(func(c *config) {
		var keep []plan
		for _, x := range c.Plans {
			if x.ID != id {
				keep = append(keep, x)
			}
		}
		c.Plans = keep
	})
}

// announcePlan: "🗓️ Stan plans an F1 Party · 2026 Abu Dhabi GP · Race (replay) · Sunday 20:00 (in 3 days)".
func announcePlan(webhook, name string, pl *plan) error {
	what := pl.Title
	if pl.Replay {
		what += " (replay)"
	}
	if isGroupKey(webhook) {
		return postGroup(webhook, map[string]any{"kind": "plan", "name": name, "code": pl.Code, "title": what, "at": pl.At})
	}
	text := fmt.Sprintf("🗓️ **%s** plans an F1 Party\n📺 **%s**\n⏰ <t:%d:F> (<t:%d:R>)\nCode: **%s**", name, what, pl.At, pl.At, pl.Code)
	if link := joinLink(pl.Code); link != "" {
		text += fmt.Sprintf(" · [Join the party](<%s>) (from the start time)", link)
	}
	text += "\nNo F1 Party yet? [Download it](<https://github.com/Jeremy0538/f1party/releases/latest>)"
	return postDiscord(webhook, text)
}

func remindPlan(webhook, name string, pl *plan) error {
	if isGroupKey(webhook) {
		return postGroup(webhook, map[string]any{"kind": "remind", "name": name, "code": pl.Code, "title": pl.Title, "at": pl.At})
	}
	text := fmt.Sprintf("⏰ **%s** starts <t:%d:R>: %s's F1 Party\nCode: **%s**", pl.Title, pl.At, name, pl.Code)
	if link := joinLink(pl.Code); link != "" {
		text += fmt.Sprintf(" · [Join the party](<%s>)", link)
	}
	return postDiscord(webhook, text)
}

// planLead: a planned party starts this long before its time: a replay a minute early (it then starts a
// minute before the session, with the lights), a live session five minutes early so everyone is in.
func planLead(pl plan) time.Duration {
	if pl.Replay {
		return time.Minute
	}
	return 5 * time.Minute
}

// remindBefore: the reminder goes out this long before a planned party.
const remindBefore = 15 * time.Minute

// runPlans checks the plans every 20 s: reminders, and starting a party at its time. A plan more than 30
// minutes past its time (the app was closed) is dropped.
func (p *Party) runPlans() {
	for range time.Tick(20 * time.Second) {
		c := loadConfig()
		checkNotifications(c)
		if len(c.Plans) == 0 {
			continue
		}
		now := time.Now()
		var due *plan
		changed := false
		var keep []plan
		for _, pl := range c.Plans {
			at := time.Unix(pl.At, 0)
			switch {
			case now.After(at.Add(30 * time.Minute)):
				changed = true
				continue // missed
			case !now.Before(at.Add(-planLead(pl))):
				if due == nil {
					x := pl
					due = &x
				}
				changed = true
				continue
			case !pl.Announced && len(discordDests()) > 0 && now.Before(at.Add(-remindBefore)):
				// planned without a team (or Discord didn't answer): announce it now
				if err := toAllDests(func(d string) error { return announcePlan(d, cleanName(c.Name), &pl) }); err != nil {
					log.Printf("plan announcement: %v", err)
				} else {
					pl.Announced, changed = true, true
				}
			case !pl.Reminded && now.After(at.Add(-remindBefore)):
				if err := toAllDests(func(d string) error { return remindPlan(d, cleanName(c.Name), &pl) }); err != nil {
					log.Printf("plan reminder: %v", err)
				}
				notifyPlan(&pl)
				pl.Reminded, changed = true, true
			}
			keep = append(keep, pl)
		}
		if changed {
			updateConfig(func(cc *config) { cc.Plans = keep })
		}
		if due != nil {
			p.startPlan(*due, c)
		}
	}
}

// startPlan hosts the planned party with its code, posts "starting now" and opens the session.
func (p *Party) startPlan(pl plan, c config) {
	if s := p.Snapshot(); s.Mode != "idle" && s.Code != pl.Code {
		p.SetNote("Planned party " + pl.Title + " skipped: you're in another party")
		return
	}
	s, _, ok := calSession(pl.Session)
	if !ok {
		_ = loadCalendar()
		if s, _, ok = calSession(pl.Session); !ok {
			return
		}
	}
	name := cleanName(c.Name)
	if p.Snapshot().Mode == "idle" {
		p.Start("host", pl.Code, name)
	}
	p.SetNote("Your planned party: " + pl.Title)
	if len(discordDests()) > 0 {
		what := pl.Title
		if pl.Replay {
			what += " · replay"
		} else {
			what += " · LIVE"
		}
		if err := toAllDests(func(d string) error { return sendInvite(d, name, pl.Code, what) }); err != nil {
			log.Printf("plan start: %v", err)
		}
	}
	go func() {
		contentID := ""
		if pl.Replay {
			id, err := f1tvReplay(s)
			if err != nil {
				p.SetNote(err.Error())
				return
			}
			contentID = id
			contentTitles.Store(id, pl.Title)
		}
		if err := p.openCalSession(s, contentID); err != nil {
			p.SetNote(err.Error())
			return
		}
		if contentID != "" {
			p.jumpWhenLoaded(contentID, "start")
		}
	}()
}
