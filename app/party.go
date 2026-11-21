package main

// Party session: relay connection, clock sync, host broadcast and guest follow logic.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Feed state as sent by the host. At = relay-clock milliseconds of the moment Time was read.
type FeedState struct {
	T         string  `json:"t"`
	None      bool    `json:"none,omitempty"`
	ContentID string  `json:"contentId,omitempty"`
	Title     string  `json:"title,omitempty"`
	Live      bool    `json:"live,omitempty"`
	Paused    bool    `json:"paused,omitempty"`
	Time      float64 `json:"time,omitempty"`
	At        float64 `json:"at,omitempty"`
	// Session type from the host's timing ("Race", "Qualifying", …): tells a guest's Stream Deck plugin
	// which layout to open.
	SessionType string `json:"sessionType,omitempty"`
	// Track: real-world time (UTC ms) of the frame the host shows at At, from MultiViewer's live timing
	// clock. Live streams are compared on this, since their player positions differ per computer.
	Track float64 `json:"track,omitempty"`
	// CountdownAt: relay time (ms) at which everyone starts playing together ("Start together").
	CountdownAt float64 `json:"countdown,omitempty"`
	// Resync: goes up each time the host asks everyone to sync to them now.
	Resync int `json:"resync,omitempty"`
}

type Member struct {
	Role   string   `json:"role"`
	Name   string   `json:"name"`
	ID     string   `json:"id,omitempty"` // random id per app, sent as "name#id" (picks a new host)
	Drift  *float64 `json:"drift,omitempty"`
	Status string   `json:"status,omitempty"`
}

type Status struct {
	Mode       string    `json:"mode"` // idle | host | guest
	Code       string    `json:"code,omitempty"`
	Name       string    `json:"name"`
	Connected  bool      `json:"connected"`
	HostOnline bool      `json:"hostOnline"`
	Members    []Member  `json:"members"`
	Status     string    `json:"status"`
	Drift      *float64  `json:"drift,omitempty"`
	MVOnline   bool      `json:"mvOnline"`
	Feed       *FeedInfo `json:"feed,omitempty"`
	Note       string    `json:"note,omitempty"`
	Countdown  float64   `json:"countdown,omitempty"` // seconds until "Start together" plays
	Update     *Update   `json:"update,omitempty"`
	Version    string    `json:"version"`
}

type FeedInfo struct {
	Title  string  `json:"title"`
	Time   float64 `json:"time"`
	Paused bool    `json:"paused"`
	Live   bool    `json:"live"`
}

type Party struct {
	mu     sync.Mutex
	st     Status
	cancel context.CancelFunc
	conn   *websocket.Conn

	// clock: relay time ≈ local time + offset (ms); best sample = lowest round trip
	offset   float64
	clockRTT float64
	samples  []clockSample

	host     *FeedState // guest: latest state from the host
	hostWhen time.Time

	setup     string // MultiViewer setup a guest opens on joining ("" = none)
	forceSync bool   // guest: correct the drift now, whatever its size

	id          string    // this app's member id
	countdownAt float64   // host: relay ms at which "Start together" plays
	resync      int       // host: number of "everyone sync to me" requests
	hostOffAt   time.Time // guest: since when the host has been offline
	update      *Update   // a newer release, when there is one
}

type clockSample struct{ offset, rtt float64 }

func nowMs() float64 { return float64(time.Now().UnixNano()) / 1e6 }

func NewParty(name, setup string) *Party {
	return &Party{st: Status{Mode: "idle", Name: name, Version: version}, setup: setup, id: newMemberID()}
}

func newMemberID() string {
	const hex = "0123456789abcdef"
	b := make([]byte, 6)
	for i := range b {
		b[i] = hex[rand.Intn(16)]
	}
	return string(b)
}

// ResyncAll (host): every guest syncs to the host right now (and the host's own onboards follow its world feed).
func (p *Party) ResyncAll() error {
	p.mu.Lock()
	host := p.st.Mode == "host"
	if host {
		p.resync++
	}
	p.mu.Unlock()
	if !host {
		return errors.New("only the host can sync everyone")
	}
	if ps, err := players(); err == nil {
		if feed := mainFeed(ps, ""); feed != nil {
			_ = playerSync(feed.ID)
		}
	}
	return nil
}

// StartTogether (host): pause on the current frame, count down 5 seconds, then everyone plays at once.
func (p *Party) StartTogether() error {
	p.mu.Lock()
	host := p.st.Mode == "host"
	p.mu.Unlock()
	if !host {
		return errors.New("only the host can start the party")
	}
	at, ok := p.relayNow()
	if !ok {
		return errors.New("not connected to the party yet")
	}
	ps, err := players()
	if err != nil {
		return err
	}
	feed := mainFeed(ps, "")
	if feed == nil {
		return errors.New("open the world feed (International) in MultiViewer first")
	}
	_ = playerPause(feed.ID, true)
	_ = playerSync(feed.ID)
	p.mu.Lock()
	p.countdownAt = at + 5000
	p.mu.Unlock()
	return nil
}

// SetNote shows a short message under the party status (e.g. "Invite sent to Discord").
func (p *Party) SetNote(note string) { p.mu.Lock(); p.st.Note = note; p.mu.Unlock() }

func (p *Party) SetSetup(name string) { p.mu.Lock(); p.setup = name; p.mu.Unlock() }

func (p *Party) Setup() string { p.mu.Lock(); defer p.mu.Unlock(); return p.setup }

// SyncNow: a guest jumps to the host right away and takes the onboards along; otherwise (host, or not in
// a party) MultiViewer's own sync pulls the onboards to the world feed.
func (p *Party) SyncNow() error {
	p.mu.Lock()
	guest := p.st.Mode == "guest"
	if guest {
		p.forceSync = true
	}
	p.mu.Unlock()
	if guest {
		return nil
	}
	ps, err := players()
	if err != nil {
		return err
	}
	feed := mainFeed(ps, "")
	if feed == nil {
		return errors.New("open the world feed (International) in MultiViewer first")
	}
	return playerSync(feed.ID)
}

func (p *Party) Snapshot() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.st
	s.Members = append([]Member(nil), p.st.Members...)
	s.Update = p.update
	return s
}

func (p *Party) set(f func(s *Status)) { p.mu.Lock(); f(&p.st); p.mu.Unlock() }

func (p *Party) Start(mode, code, name string) {
	p.Leave()
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancel = cancel
	p.st = Status{Mode: mode, Code: code, Name: name, Status: "Connecting…", Version: version}
	p.samples, p.host = nil, nil
	p.hostOffAt, p.countdownAt = time.Time{}, 0
	p.mu.Unlock()
	go p.connLoop(ctx, mode, code, name)
	if mode == "host" {
		go p.hostLoop(ctx)
	} else {
		go p.guestLoop(ctx)
	}
}

func (p *Party) Leave() {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	name := p.st.Name
	p.st = Status{Mode: "idle", Name: name, Version: version}
	p.mu.Unlock()
}

// ---------------------------------------------------------------- relay connection

func (p *Party) connLoop(ctx context.Context, mode, code, name string) {
	backoff := time.Second
	for ctx.Err() == nil {
		u := relayURL + "?" + url.Values{"room": {code}, "role": {mode}, "name": {name + "#" + p.id}}.Encode()
		dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
		c, _, err := websocket.Dial(dctx, u, nil)
		dcancel()
		if err != nil {
			p.set(func(s *Status) { s.Connected = false; s.Status = "Can't reach the party server, retrying…" })
			log.Printf("relay dial: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 15*time.Second)
			continue
		}
		backoff = time.Second
		c.SetReadLimit(1 << 16)
		p.mu.Lock()
		p.conn = c
		p.st.Connected = true
		p.mu.Unlock()
		replaced := p.readLoop(ctx, c)
		p.mu.Lock()
		p.conn = nil
		p.st.Connected = false
		p.mu.Unlock()
		c.CloseNow()
		if replaced {
			// another member took over hosting (the host had left, or came back): don't reconnect as host
			// (that would push them out again), carry on as a guest
			s := p.Snapshot()
			go func() {
				p.Start("guest", s.Code, s.Name)
				p.SetNote("Someone else is hosting now; you follow them")
			}()
			return
		}
	}
}

func (p *Party) send(v any) {
	p.mu.Lock()
	c := p.conn
	p.mu.Unlock()
	if c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = wsjson.Write(ctx, c, v)
}

// readLoop handles relay messages until the connection ends. It returns true when this host connection
// was replaced by another host (close code 4001).
func (p *Party) readLoop(ctx context.Context, c *websocket.Conn) bool {
	pctx, pcancel := context.WithCancel(ctx)
	defer pcancel()
	go func() { // clock pings: fast at first, then every 5 s
		for i := 0; pctx.Err() == nil; i++ {
			p.send(map[string]any{"t": "ping", "c": nowMs()})
			d := 5 * time.Second
			if i < 6 {
				d = 300 * time.Millisecond
			}
			select {
			case <-pctx.Done():
				return
			case <-time.After(d):
			}
		}
	}()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return websocket.CloseStatus(err) == 4001
		}
		var head struct {
			T string `json:"t"`
		}
		if json.Unmarshal(data, &head) != nil {
			continue
		}
		switch head.T {
		case "pong":
			var m struct{ C, S float64 }
			if json.Unmarshal(data, &m) == nil {
				p.addClockSample(m.C, m.S, nowMs())
			}
		case "state":
			var fs FeedState
			if json.Unmarshal(data, &fs) == nil {
				p.mu.Lock()
				p.host, p.hostWhen = &fs, time.Now()
				p.mu.Unlock()
			}
		case "host":
			var m struct{ Online bool }
			if json.Unmarshal(data, &m) == nil {
				p.mu.Lock()
				p.st.HostOnline = m.Online
				if m.Online {
					p.hostOffAt = time.Time{}
				} else if p.hostOffAt.IsZero() {
					p.hostOffAt = time.Now()
				}
				p.mu.Unlock()
			}
		case "members":
			var m struct{ Members []Member }
			if json.Unmarshal(data, &m) == nil {
				for i := range m.Members {
					if n, id, ok := strings.Cut(m.Members[i].Name, "#"); ok {
						m.Members[i].Name, m.Members[i].ID = n, id
					}
				}
				p.set(func(s *Status) {
					s.Members = m.Members
					if s.Mode == "host" {
						s.HostOnline = true
					}
				})
			}
		case "error":
			var m struct{ Message string }
			if json.Unmarshal(data, &m) == nil {
				p.set(func(s *Status) { s.Status = m.Message })
			}
		}
	}
}

func (p *Party) addClockSample(sent, server, recv float64) {
	rtt := recv - sent
	if rtt < 0 || rtt > 5000 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples = append(p.samples, clockSample{offset: server - (sent + rtt/2), rtt: rtt})
	if len(p.samples) > 12 {
		p.samples = p.samples[len(p.samples)-12:]
	}
	best := p.samples[0]
	for _, s := range p.samples {
		if s.rtt < best.rtt {
			best = s
		}
	}
	p.offset, p.clockRTT = best.offset, best.rtt
}

func (p *Party) relayNow() (float64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nowMs() + p.offset, len(p.samples) > 0
}

// ---------------------------------------------------------------- host

func (p *Party) hostLoop(ctx context.Context) {
	var lastSent FeedState
	var lastSentAt time.Time
	var sessionType string
	var typeAt time.Time
	var startedFor float64 // countdown that has been started
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		ps, err := players()
		at, clockOK := p.relayNow()
		if err != nil {
			p.set(func(s *Status) { s.MVOnline = false; s.Feed = nil; s.Status = err.Error() })
			continue
		}
		feed := mainFeed(ps, "")
		fs := FeedState{T: "state", None: true}
		if feed != nil {
			if time.Since(typeAt) > 5*time.Second || (sessionType == "" && time.Since(typeAt) > time.Second) {
				sessionType, typeAt = mvSessionType(), time.Now()
			}
			tm, _ := feed.Time()
			fs = FeedState{T: "state", ContentID: feed.StreamData.ContentID, Title: feed.StreamData.Title,
				Live: feed.State != nil && feed.State.Live, Paused: feed.State != nil && feed.State.Paused, Time: tm, At: at,
				SessionType: sessionType}
			if fs.Live {
				if track, ok := trackNow(); ok {
					fs.Track = track
				}
			}
			// Start together: hold the frame until the moment, then play (guests do the same at that moment)
			p.mu.Lock()
			cd := p.countdownAt
			fs.Resync = p.resync
			p.mu.Unlock()
			if cd > 0 && clockOK {
				fs.CountdownAt = cd
				if at >= cd && startedFor != cd {
					startedFor = cd
					_ = playerPause(feed.ID, false)
					fs.Paused = false
				}
				if at > cd+3000 {
					p.mu.Lock()
					if p.countdownAt == cd {
						p.countdownAt = 0
					}
					p.mu.Unlock()
				}
			}
		}
		p.set(func(s *Status) {
			s.MVOnline = true
			if feed == nil {
				s.Feed = nil
				s.Status = "Open the world feed (International) in MultiViewer"
			} else {
				s.Feed = &FeedInfo{Title: fs.Title, Time: fs.Time, Paused: fs.Paused, Live: fs.Live}
				if s.Connected {
					s.Status = "Hosting: guests follow your world feed"
				}
			}
			s.Countdown = 0
			if fs.CountdownAt > at {
				s.Countdown = (fs.CountdownAt - at) / 1000
				s.Status = "Starting together…"
			}
		})
		if !clockOK {
			continue
		}
		// send on any discontinuity (pause/play, seek, new stream) right away, otherwise once a second
		changed := fs.None != lastSent.None || fs.ContentID != lastSent.ContentID || fs.Paused != lastSent.Paused || fs.Title != lastSent.Title ||
			fs.CountdownAt != lastSent.CountdownAt || fs.Resync != lastSent.Resync
		if !changed && !fs.None && !fs.Paused && lastSent.At > 0 {
			expected := lastSent.Time + (fs.At-lastSent.At)/1000
			changed = math.Abs(fs.Time-expected) > 0.5
		}
		if changed || time.Since(lastSentAt) >= time.Second {
			p.send(fs)
			lastSent, lastSentAt = fs, time.Now()
		}
	}
}

// ---------------------------------------------------------------- guest

const (
	tolerance   = 0.30 // seconds of drift we accept before correcting (replays)
	liveTol     = 0.50 // live: seconds of drift we accept before correcting
	liveAhead   = 0.60 // live without track time: pause when this far ahead of the host
	seekSettle  = 2500 * time.Millisecond
	createRetry = 8 * time.Second
)

func (p *Party) guestLoop(ctx context.Context) {
	var (
		lead         = 0.20 // seconds added to seek targets to cover the stream's seek/rebuffer time
		settleUntil  time.Time
		pendingSync  bool
		measureLead  bool
		creatingAt   time.Time
		resumeAt     time.Time
		lastReport   time.Time
		setupFor     string    // content id the chosen setup was opened (or tried) for
		pluginUntil  time.Time // the Stream Deck plugin is opening the host's session until then
		mvStartTried bool      // MultiViewer was closed and has been started (once per party)
		restAt       time.Time // ask the Stream Deck plugin for its best of the rest then (after syncing)
		pluginErr    string    // the Stream Deck plugin couldn't be asked (shown; asked again later)
		pluginRetry  time.Time
		startedFor   float64 // "Start together" countdown this guest has already started playing on
		resyncSeen   = -1    // host's resync number last acted on (-1: none seen yet)
	)
	// askPlugin hands the opening of the host's session to the Stream Deck plugin (at most every 10 s).
	askPlugin := func(host *FeedState) {
		if time.Now().Before(pluginRetry) {
			return
		}
		if err := pluginOpen(sessionKind(host.SessionType), host.ContentID); err != nil {
			log.Printf("stream deck: %v", err)
			pluginErr, pluginRetry = err.Error(), time.Now().Add(10*time.Second)
			return
		}
		setupFor, pluginErr, pluginUntil = host.ContentID, "", time.Now().Add(90*time.Second)
	}
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		status := ""
		var drift *float64
		report := func() {
			p.set(func(s *Status) { s.Status = status; s.Drift = drift })
			if time.Since(lastReport) > 2*time.Second {
				p.send(map[string]any{"t": "report", "drift": drift, "status": status})
				lastReport = time.Now()
			}
		}

		ps, err := players()
		if err != nil {
			p.set(func(s *Status) { s.MVOnline = false; s.Feed = nil })
			status = err.Error()
			p.mu.Lock()
			host, setup := p.host, p.setup
			p.mu.Unlock()
			// MultiViewer is closed: once the host's session is known, the Stream Deck plugin (which starts
			// MultiViewer itself) or F1 Party starts it.
			known := host != nil && !host.None && host.ContentID != ""
			switch {
			case setup == setupStreamDeck:
				// only the plugin starts MultiViewer then, on F1's own desktop (never F1 Party itself)
				if known && setupFor != host.ContentID {
					askPlugin(host)
				}
				if pluginErr != "" {
					status = pluginErr
				} else if time.Now().Before(pluginUntil) {
					status = "Your Stream Deck is starting MultiViewer…"
				}
			case known && !mvStartTried:
				mvStartTried = true
				if err := mvStart(); err != nil {
					status = err.Error()
				}
			}
			if mvStartTried && status == errMVOffline.Error() {
				status = "Starting MultiViewer…"
			}
			report()
			continue
		}
		p.set(func(s *Status) { s.MVOnline = true })

		p.mu.Lock()
		host, hostWhen, online, connected, setup := p.host, p.hostWhen, p.st.HostOnline, p.st.Connected, p.setup
		force := p.forceSync
		p.forceSync = false
		p.mu.Unlock()
		now, clockOK := p.relayNow()
		switch {
		case !connected:
			status = "Connecting to the party…"
		case !online:
			status = "Waiting for the host"
			if p.takeOverHost() {
				return
			}
		case host == nil || !clockOK || time.Since(hostWhen) > 10*time.Second:
			status = "Waiting for the host's stream"
		case host.None:
			status = "The host has no world feed open"
		}
		if status != "" {
			feed := mainFeed(ps, "")
			p.set(func(s *Status) { s.Feed = feedInfo(feed) })
			report()
			continue
		}

		feed := mainFeed(ps, host.ContentID)
		p.set(func(s *Status) { s.Feed = feedInfo(feed) })
		if setup == setupStreamDeck && setupFor != host.ContentID {
			// the Stream Deck plugin opens everything: its desktop, MultiViewer, setup and the viewer's layout
			// (also when the session is open already: it skips what is in place)
			askPlugin(host)
		}
		if setup == setupStreamDeck && pluginErr != "" && feed == nil {
			// no world feed of our own on whatever desktop shows: wait for the plugin (asked again every 10 s)
			status = pluginErr
			report()
			continue
		}
		// While the plugin opens its layout, leave MultiViewer alone (no pause or seek in between): its
		// players are starting and the viewer waits for the session to run. Then sync once.
		if !pluginUntil.IsZero() {
			if time.Now().Before(pluginUntil) && (pluginBusy() || feed == nil) {
				status = "Your Stream Deck is opening the host's session…"
				if feed != nil {
					status = "Your Stream Deck is opening your layout…"
				}
				report()
				continue
			}
			pluginUntil = time.Time{}
			force = true
		}
		if force && setup == setupStreamDeck {
			// the replay's time changes with the sync, and with it who the best of the rest are
			restAt = time.Now().Add(seekSettle + 3*time.Second)
		}
		if !restAt.IsZero() && time.Now().After(restAt) {
			restAt = time.Time{}
			go pluginRefreshRest()
		}
		if feed == nil && setup != "" && setup != setupStreamDeck && setupFor != host.ContentID && mvDebugReady() {
			// the chosen MultiViewer setup opens the host's session with all its windows (tried once)
			setupFor = host.ContentID
			status = "Opening your setup “" + setup + "”…"
			report()
			if err := mvOpenSetup(setup, host.ContentID); err != nil {
				log.Printf("setup %q: %v", setup, err)
				status = "Setup didn't open (" + err.Error() + "), opening the world feed"
				report()
			}
			creatingAt = time.Now() // give the setup's players time to appear before opening one ourselves
			continue
		}
		if feed == nil {
			status = "Opening the host's session…"
			if time.Since(creatingAt) > createRetry {
				creatingAt = time.Now()
				title := host.Title
				if title == "" {
					title = "INTERNATIONAL"
				}
				if _, err := playerCreate(host.ContentID, title); err != nil {
					status = "Can't open the host's session: " + err.Error()
				}
			}
			report()
			continue
		}

		local, ok := feed.Time()
		if !ok || feed.State == nil {
			status = "Loading stream…"
			report()
			continue
		}

		// The host asked everyone to sync to them now.
		if host.Resync != resyncSeen {
			if resyncSeen >= 0 {
				force = true
			}
			resyncSeen = host.Resync
		}

		// Start together: during the countdown stay paused on the host's frame; at the moment, play.
		h := *host
		if h.CountdownAt > 0 {
			switch {
			case now < h.CountdownAt:
				if !feed.State.Paused {
					_ = playerPause(feed.ID, true)
				}
				if math.Abs(local-h.Time) > tolerance && time.Now().After(settleUntil) {
					_ = playerSeek(feed.ID, h.Time)
					settleUntil = time.Now().Add(seekSettle)
				}
				left := (h.CountdownAt - now) / 1000
				p.set(func(s *Status) { s.Countdown = left })
				status = "Starting together…"
				report()
				continue
			case startedFor != h.CountdownAt && now < h.CountdownAt+3000:
				startedFor = h.CountdownAt
				_ = playerPause(feed.ID, false)
				settleUntil, pendingSync, measureLead = time.Now().Add(seekSettle), true, false
			}
			if h.Paused && startedFor == h.CountdownAt && now < h.CountdownAt+3000 {
				// the host's "playing" state may still be on its way: it plays from the countdown moment
				h.Paused, h.At = false, h.CountdownAt
			}
		}
		p.set(func(s *Status) { s.Countdown = 0 })
		host = &h

		target := host.Time
		if !host.Paused {
			target += (now - host.At) / 1000
		}
		d := local - target
		// Live: player positions differ per computer, so compare the real-world time on screen instead.
		liveTrack := false
		if host.Live && host.Track > 0 {
			if own, ok := trackNow(); ok {
				hostTrack := host.Track
				if !host.Paused {
					hostTrack += now - host.At
				}
				d, liveTrack = (own-hostTrack)/1000, true
			}
		}
		drift = &d

		// mirror pause / play (not while a live hold is pausing us on purpose)
		if resumeAt.IsZero() && feed.State.Paused != host.Paused {
			_ = playerPause(feed.ID, host.Paused)
		}

		live := host.Live || feed.State.Live
		switch {
		case (force && live && liveTrack) || (live && liveTrack && math.Abs(d) > liveTol && time.Now().After(settleUntil) && !pendingSync):
			// live: move by the difference in real-world time (forward stops at the live edge)
			rel := -d
			if !host.Paused {
				rel += lead
			}
			if err := playerSeekBy(feed.ID, rel); err != nil {
				status = "Seek failed: " + err.Error()
			} else {
				settleUntil, pendingSync, measureLead = time.Now().Add(seekSettle), true, !force
				status = "Syncing…"
			}
		case force && live:
			// live without timing: can't be compared exactly, just take the onboards along
			_ = playerSync(feed.ID)
			status = "In sync (live)"
		case force:
			seekTo := target
			if !host.Paused {
				seekTo += lead
			}
			resumeAt = time.Time{}
			if err := playerSeek(feed.ID, math.Max(0, seekTo)); err != nil {
				status = "Seek failed: " + err.Error()
			} else {
				settleUntil, pendingSync, measureLead = time.Now().Add(seekSettle), true, false
				status = "Syncing…"
			}
		case time.Now().Before(settleUntil):
			status = "Syncing…"
		case pendingSync:
			// first steady reading after a seek: let the onboards follow, then learn the seek delay
			pendingSync = false
			_ = playerSync(feed.ID)
			if measureLead && !host.Paused {
				lead = math.Max(0, math.Min(2, lead-d*0.7))
			}
			measureLead = false
			status = "In sync"
		case live && liveTrack:
			status = "In sync (live)"
		case live:
			// live without timing data: wait out a lead by pausing; a lag can't be measured exactly
			switch {
			case !resumeAt.IsZero() && time.Now().Before(resumeAt):
				status = "Waiting for the host…"
			case !resumeAt.IsZero():
				resumeAt = time.Time{}
				_ = playerPause(feed.ID, false)
				settleUntil, pendingSync = time.Now().Add(seekSettle), true
				status = "Syncing…"
			case d > liveAhead && !host.Paused:
				_ = playerPause(feed.ID, true)
				resumeAt = time.Now().Add(time.Duration(d * float64(time.Second)))
				status = fmt.Sprintf("Ahead of the host by %.1f s, waiting", d)
			case d < -2:
				status = fmt.Sprintf("Behind the host by %.1f s (live can't skip ahead)", -d)
			default:
				status = "In sync (live)"
			}
		case math.Abs(d) > tolerance:
			seekTo := target
			if !host.Paused {
				seekTo += lead
			}
			if err := playerSeek(feed.ID, math.Max(0, seekTo)); err != nil {
				status = "Seek failed: " + err.Error()
			} else {
				settleUntil, pendingSync, measureLead = time.Now().Add(seekSettle), true, true
				status = "Syncing…"
			}
		default:
			status = "In sync"
		}
		report()
	}
}

func feedInfo(f *Player) *FeedInfo {
	if f == nil || f.State == nil || f.StreamData == nil {
		return nil
	}
	tm, _ := f.Time()
	return &FeedInfo{Title: f.StreamData.Title, Time: tm, Paused: f.State.Paused, Live: f.State.Live}
}

// takeOverHost: the host has been gone for 20 seconds. The guest with the lowest member id becomes the host
// (every guest picks the same one), so the party carries on. Returns true when this app took over.
func (p *Party) takeOverHost() bool {
	p.mu.Lock()
	off, code, name, me := p.hostOffAt, p.st.Code, p.st.Name, p.id
	var lowest string
	for _, m := range p.st.Members {
		if m.Role == "guest" && m.ID != "" && (lowest == "" || m.ID < lowest) {
			lowest = m.ID
		}
	}
	p.mu.Unlock()
	if off.IsZero() || time.Since(off) < 20*time.Second || lowest != me {
		return false
	}
	go func() {
		p.Start("host", code, name)
		p.SetNote("The host left, so you're hosting now")
	}()
	return true
}

// HostSession: the content id and session type the host is watching (guest), or "".
func (p *Party) HostSession() (contentID, sessionType string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.host == nil {
		return "", ""
	}
	return p.host.ContentID, p.host.SessionType
}
