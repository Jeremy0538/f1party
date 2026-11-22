package main

// The session that MultiViewer shows, and jumping to a moment in it: the beginning of the broadcast, a minute
// before the session starts, or the live edge. The host's jump takes the whole party along.

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// SessionView: what the app's Session block shows.
type SessionView struct {
	Name     string  `json:"name"`              // "2025 Abu Dhabi GP · Race"
	Title    string  `json:"title"`             // the stream: "F1 LIVE"
	Time     float64 `json:"time"`              // position in the stream (s)
	Live     bool    `json:"live"`              // a live stream
	Paused   bool    `json:"paused"`            //
	ToStart  float64 `json:"toStart,omitempty"` // seconds from the shown moment to the session start (negative: started)
	CanStart bool    `json:"canStart"`          // the start time is known (live timing loaded)
	CanJump  bool    `json:"canJump"`           // not a guest (the host moves the party)
	Busy     string  `json:"busy,omitempty"`    // a jump in progress
	Stopped  bool    `json:"stopped,omitempty"` // guest: stopped watching this session ("End session")
}

var sessionNameCache struct {
	contentID, name string
	at              time.Time
}

// sessionName caches mvSessionName per content id for a while (it asks MultiViewer's windows).
func sessionName(contentID string) string {
	c := &sessionNameCache
	if c.contentID == contentID && c.name != "" && time.Since(c.at) < 30*time.Second {
		return c.name
	}
	c.contentID, c.name, c.at = contentID, mvSessionName(), time.Now()
	if t, ok := contentTitles.Load(contentID); ok && c.name == "" {
		c.name = t.(string)
	}
	return c.name
}

// sessionStart: the real-world time (UTC ms) the session starts: the scheduled start, or the moment the live
// timing reports it started when that was earlier.
func sessionStart() (float64, bool) {
	var out struct {
		S struct {
			SessionInfo struct {
				StartDate string `json:"StartDate"`
				GmtOffset string `json:"GmtOffset"`
			} `json:"SessionInfo"`
			SessionData struct {
				StatusSeries json.RawMessage `json:"StatusSeries"`
			} `json:"SessionData"`
		} `json:"f1LiveTimingState"`
	}
	if gql(`{ f1LiveTimingState { SessionInfo SessionData } }`, nil, &out) != nil {
		return 0, false
	}
	var start float64
	si := out.S.SessionInfo
	if t, err := time.Parse("2006-01-02T15:04:05", si.StartDate); err == nil {
		t = t.Add(-parseOffset(si.GmtOffset))
		start = float64(t.UnixMilli())
	}
	// StatusSeries is an array, or an object keyed "0", "1", … (live timing sends both)
	type status struct{ Utc, SessionStatus string }
	var list []status
	if json.Unmarshal(out.S.SessionData.StatusSeries, &list) != nil {
		var m map[string]status
		if json.Unmarshal(out.S.SessionData.StatusSeries, &m) == nil {
			for _, v := range m {
				list = append(list, v)
			}
		}
	}
	for _, st := range list {
		if st.SessionStatus != "Started" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, st.Utc); err == nil {
			if ms := float64(t.UnixMilli()); start == 0 || ms < start {
				start = ms
			}
		}
	}
	return start, start > 0
}

// parseOffset: "04:00:00" or "-05:00:00" → duration.
func parseOffset(s string) time.Duration {
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	parts := strings.Split(s, ":")
	var d time.Duration
	units := []time.Duration{time.Hour, time.Minute, time.Second}
	for i, p := range parts {
		if i >= len(units) {
			break
		}
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				return 0
			}
			n = n*10 + int(r-'0')
		}
		d += time.Duration(n) * units[i]
	}
	if neg {
		return -d
	}
	return d
}

// Session: the Session block's data, or nil without a stream.
func (p *Party) Session() *SessionView {
	p.mu.Lock()
	skip := ""
	if p.st.Mode == "guest" && p.host != nil && p.skip != "" && p.skip == p.host.ContentID {
		skip = p.skip
	}
	p.mu.Unlock()
	if skip != "" {
		return &SessionView{Name: sessionName(skip), Stopped: true}
	}
	ps, err := players()
	if err != nil {
		return nil
	}
	feed := mainFeed(ps, "")
	if feed == nil || feed.State == nil || feed.StreamData == nil {
		return nil
	}
	tm, _ := feed.Time()
	v := &SessionView{Name: sessionName(feed.StreamData.ContentID), Title: feed.StreamData.Title, Time: tm,
		Live: feed.State.Live, Paused: feed.State.Paused}
	p.mu.Lock()
	v.CanJump = p.st.Mode != "guest"
	v.Busy = p.jumping
	p.mu.Unlock()
	if start, ok := sessionStart(); ok {
		if track, ok := trackNow(); ok {
			v.CanStart = true
			v.ToStart = (start - track) / 1000
		}
	}
	return v
}

// startLead: "Start −1 min" lands this long before the session starts.
const startLead = 60 * time.Second

// Jump moves your stream (and, as host, the whole party) to "begin", "start" (a minute before the session
// starts, then everyone starts together) or "live". It returns at once; the jump runs in the background.
func (p *Party) Jump(to string) error {
	p.mu.Lock()
	mode, busy := p.st.Mode, p.jumping
	p.mu.Unlock()
	if mode == "guest" {
		return errors.New("the host moves the party")
	}
	if busy != "" {
		return errors.New("still busy: " + busy)
	}
	ps, err := players()
	if err != nil {
		return err
	}
	feed := mainFeed(ps, "")
	if feed == nil || feed.State == nil {
		return errors.New("open F1 Live in MultiViewer first")
	}
	var seek func() error
	label := ""
	switch to {
	case "begin":
		label = "Back to the beginning…"
		seek = func() error { return playerSeek(feed.ID, 0) }
	case "live":
		if !feed.State.Live {
			return errors.New("this isn't a live stream")
		}
		label = "Going live…"
		seek = func() error { return playerSeekBy(feed.ID, 24*3600) } // clamps at the live edge
	case "start":
		start, ok1 := sessionStart()
		track, ok2 := trackNow()
		local, ok3 := feed.Time()
		if !ok1 || !ok2 || !ok3 {
			return errors.New("the session's start time isn't known yet (MultiViewer's live timing hasn't loaded)")
		}
		target := local + (start-float64(startLead.Milliseconds())-track)/1000
		if target < 0 {
			target = 0
		}
		label = "To 1 minute before the start…"
		if feed.State.Live {
			seek = func() error { return playerSeekBy(feed.ID, target-local) }
		} else {
			seek = func() error { return playerSeek(feed.ID, target) }
		}
	default:
		return errors.New("unknown jump")
	}
	p.mu.Lock()
	p.jumping = label
	p.mu.Unlock()
	go func() {
		defer func() { p.mu.Lock(); p.jumping = ""; p.mu.Unlock() }()
		if err := seek(); err != nil {
			p.SetNote("Couldn't jump: " + err.Error())
			return
		}
		time.Sleep(seekSettle)
		_ = playerSync(feed.ID) // onboards follow
		host := mode == "host"
		switch {
		case to == "start" && host:
			if err := p.StartTogether(); err != nil {
				p.SetNote(err.Error())
				return
			}
			p.SetNote("1 minute before the start: lights out together")
		case to == "start":
			_ = playerPause(feed.ID, false)
			p.SetNote("1 minute before the start")
		case host:
			_ = p.ResyncAll()
			p.SetNote(map[string]string{"begin": "Everyone is back at the beginning", "live": "Everyone is live"}[to])
		}
	}()
	return nil
}
