package main

// Local control for the F1 Race Control Stream Deck plugin (its Sync key): sync to the party, and start a
// party with a Discord invite. Listens on 127.0.0.1 only and refuses web pages (Origin header) and other
// host names (DNS rebinding).
//   GET  /control/status → { "mode": "idle" | "host" | "guest", "code": "…", "version": "…" }
//   POST /control/sync   → guest: jump to the host and take the onboards along; otherwise sync the onboards
//   POST /control/host   → start a party (or keep the one hosted now) and send the Discord invite

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
)

const controlAddr = "127.0.0.1:47821"

func startControl(party *Party, host func() (string, error), show func()) {
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
		writeJSON(w, map[string]string{"mode": s.Mode, "code": s.Code, "version": version, "discordStatus": ps})
	})
	mux.HandleFunc("POST /control/sync", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, party.SyncNow())
	})
	mux.HandleFunc("POST /control/host", func(w http.ResponseWriter, r *http.Request) {
		code, err := host()
		show()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"code": code})
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
