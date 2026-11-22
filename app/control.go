package main

// Local control for the F1 Race Control Stream Deck plugin: its keys tell F1 Party what to do. Listens on
// 127.0.0.1 only and refuses web pages (Origin header) and other host names (DNS rebinding).
//   GET  /control/status → { "mode": "idle" | "host" | "guest", "code": "…", "version": "…",
//                            "members": 3, "sync": "ok" | "warn" | "bad" } (members and sync in a party)
//   POST /control/sync   → guest: jump to the host; host: your streams and everyone's to your main stream;
//                          otherwise your streams to your main stream
//   POST /control/pause  {paused, everyone} → pause the whole party, or just you
//   POST /control/host   → start a party (or keep the one hosted now), everyone syncs, send the Discord invite
//   POST /control/party  → the Party key: not in a party → host one; in one → sync (as /control/sync)
//   POST /control/invite → send the Discord invite (hosts a party first when you're not in one)
//   POST /control/copy   → the party code to the clipboard (hosts a party first when you're not in one)
//   POST /control/leave  → leave the party (a host ends it)
//   POST /control/watch  {which: "race" | "quali" | "live"} → open the race or qualifying on now, else the
//                          last one; "live": the session on track now. A party you host switches along.
//   POST /control/close  → End session: streams and F1 closed (a host ends it for everyone; the party stays)
//   POST /control/calendar → the window comes up on the calendar, to pick a session (Start session key)
//   POST /control/start  → Start together: the five lights for everyone, then the session plays (hosts a
//                          party first when you're not in one)
//   status also has "session": true while a session is open (End session), and "countdown": seconds until the lights go out (Start together), when counting

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const controlAddr = "127.0.0.1:47821"

// uiOpen: a tab the window shows on its next status poll ("cal"), then forgets.
var uiOpen atomic.Value

// controlHooks: what the control needs from the app window.
type controlHooks struct {
	host, invite func() (string, error)
	copy         func(text string) error
	show         func()
}

func startControl(party *Party, h controlHooks) {
	ln, err := net.Listen("tcp", controlAddr)
	if err != nil {
		log.Printf("control: %v", err)
		return
	}
	writeAppInfo()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /control/status", func(w http.ResponseWriter, r *http.Request) {
		s := party.Snapshot()
		ps, _ := presenceState.Load().(string)
		out := map[string]any{"mode": s.Mode, "code": s.Code, "version": version, "discordStatus": ps}
		if s.Mode != "idle" {
			out["members"], out["sync"] = len(s.Members), syncHealth(s)
		}
		if s.Countdown > 0 {
			out["countdown"] = s.Countdown
		}
		if v := party.Session(); v != nil && !v.Stopped {
			out["session"] = true
		}
		writeJSON(w, out)
	})
	sync := func() error {
		if party.Snapshot().Mode == "host" {
			return party.ResyncAll() // your onboards follow your main stream and every guest syncs to you
		}
		return party.SyncNow()
	}
	hostOnly := func() string {
		if s := party.Snapshot(); s.Mode != "idle" {
			return s.Code
		}
		party.Start("host", newCode(), cleanName(loadConfig().Name))
		return party.Snapshot().Code
	}
	mux.HandleFunc("POST /control/sync", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, sync())
	})
	mux.HandleFunc("POST /control/party", func(w http.ResponseWriter, r *http.Request) {
		if party.Snapshot().Mode == "idle" {
			code := hostOnly()
			h.show()
			writeJSON(w, map[string]string{"code": code, "did": "host"})
			return
		}
		if err := sync(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"did": "sync"})
	})
	mux.HandleFunc("POST /control/pause", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Paused   bool `json:"paused"`
			Everyone bool `json:"everyone"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		replyErr(w, party.Pause(in.Paused, in.Everyone))
	})
	codeReply := func(w http.ResponseWriter, code string, err error) {
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"code": code})
	}
	mux.HandleFunc("POST /control/host", func(w http.ResponseWriter, r *http.Request) {
		code, err := h.host()
		h.show()
		codeReply(w, code, err)
	})
	mux.HandleFunc("POST /control/invite", func(w http.ResponseWriter, r *http.Request) {
		code, err := h.invite()
		codeReply(w, code, err)
	})
	mux.HandleFunc("POST /control/copy", func(w http.ResponseWriter, r *http.Request) {
		code := hostOnly()
		err := h.copy(code)
		if err == nil {
			party.SetNote("Party code " + code + " copied")
		}
		codeReply(w, code, err)
	})
	mux.HandleFunc("POST /control/leave", func(w http.ResponseWriter, r *http.Request) {
		if party.Snapshot().Mode == "idle" {
			http.Error(w, "not in a party", 400)
			return
		}
		party.Leave()
		writeJSON(w, map[string]string{})
	})
	mux.HandleFunc("POST /control/watch", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Which string `json:"which"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		title, err := watchKey(party, in.Which)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"title": title})
	})
	mux.HandleFunc("POST /control/close", func(w http.ResponseWriter, r *http.Request) {
		go party.EndSession()
		writeJSON(w, map[string]string{})
	})
	mux.HandleFunc("POST /control/calendar", func(w http.ResponseWriter, r *http.Request) {
		uiOpen.Store("cal")
		h.show()
		writeJSON(w, map[string]string{})
	})
	mux.HandleFunc("POST /control/start", func(w http.ResponseWriter, r *http.Request) {
		if party.Snapshot().Mode == "idle" {
			hostOnly()
			h.show()
		}
		err := party.StartTogether()
		for i := 0; i < 20 && err != nil && err.Error() == errNotConnected.Error(); i++ {
			time.Sleep(250 * time.Millisecond) // a party that just started: wait for the connection
			err = party.StartTogether()
		}
		replyErr(w, err)
	})
	guard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != controlAddr || r.Header.Get("Origin") != "" {
			http.Error(w, "forbidden", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})
	go http.Serve(ln, guard)
}

// watchKey: the Stream Deck's Race, Quali or Session key. Opens the session the way "Open with" says.
func watchKey(party *Party, which string) (string, error) {
	if err := loadCalendar(); err != nil {
		return "", err
	}
	var s CalSession
	ok := false
	switch which {
	case "race":
		s, ok = latestCalSession("Race")
	case "quali":
		s, ok = latestCalSession("Qualifying")
	case "live":
		s, _, ok = liveCalSession()
	default:
		return "", errors.New("unknown session")
	}
	if !ok {
		return "", errors.New("no such session in the calendar")
	}
	_, m, _ := calSession(s.Key)
	return sessionTitle(s, m), party.watch(s.Key, cleanName(loadConfig().Name), "", false)
}

// syncHealth for the Stream Deck's Sync key: "ok" (everyone within a second), "warn" (someone isn't in sync
// yet) or "bad" (no connection to the party or to MultiViewer).
func syncHealth(s Status) string {
	if !s.Connected || !s.MVOnline || (s.Mode == "guest" && !s.HostOnline) {
		return "bad"
	}
	if s.Mode == "guest" {
		if s.Drift == nil || math.Abs(*s.Drift) > 1 {
			return "warn"
		}
		return "ok"
	}
	for _, m := range s.Members {
		if m.Role != "guest" {
			continue
		}
		if !strings.HasPrefix(m.Status, "In sync") || (m.Drift != nil && math.Abs(*m.Drift) > 1) {
			return "warn"
		}
	}
	return "ok"
}

// writeAppInfo tells the Stream Deck plugin where this app is, so it can start it when it isn't running.
func writeAppInfo() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	b, _ := json.Marshal(map[string]string{"exe": exe, "control": "http://" + controlAddr, "version": version})
	_ = os.MkdirAll(filepath.Join(dir, "f1party"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "f1party", "app.json"), b, 0o644)
}
