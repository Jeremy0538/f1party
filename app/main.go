// F1 Party: watch MultiViewer for F1 in sync with friends. A small desktop app (Wails: the system's own
// web view, no browser needed) that talks to the MultiViewer running on the same computer.
package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var version = "0.3.0"

// Overridable for testing against `wrangler dev`: F1PARTY_RELAY=ws://localhost:8787/f1party/ws
var relayURL = envOr("F1PARTY_RELAY", "wss://jeremyklootwijk.nl/f1party/ws")

//go:embed ui.html
var uiHTML []byte

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	cfg := loadConfig()
	party := NewParty(cfg.Name, cfg.Setup)

	var appCtx context.Context
	err := wails.Run(&options.App{
		Title:            "F1 Party",
		Width:            500,
		Height:           900,
		MinWidth:         420,
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 21, A: 255},
		AssetServer:      &assetserver.Options{Handler: newHandler(party, func() { wruntime.Quit(appCtx) })},
		OnStartup:        func(ctx context.Context) { appCtx = ctx },
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "nl.jeremyklootwijk.f1party",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { // already running: bring it to the front
				wruntime.WindowUnminimise(appCtx)
				wruntime.Show(appCtx)
			},
		},
		Windows: &windows.Options{Theme: windows.Dark},
		Mac:     &mac.Options{Appearance: mac.NSAppearanceNameDarkAqua, About: &mac.AboutInfo{Title: "F1 Party", Message: "Version " + version}},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// newHandler serves the UI and its small JSON API. It is only reachable from the app's own window.
func newHandler(party *Party, quit func()) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, party.Snapshot())
	})
	mux.HandleFunc("POST /api/host", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		json.NewDecoder(r.Body).Decode(&in)
		name := cleanName(in.Name)
		saveConfig(config{Name: name, Setup: party.Setup()})
		code := newCode()
		party.Start("host", code, name)
		writeJSON(w, map[string]string{"code": code})
	})
	mux.HandleFunc("POST /api/join", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code, Name string }
		json.NewDecoder(r.Body).Decode(&in)
		code := strings.ToUpper(strings.Map(func(r rune) rune {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, in.Code))
		if len(code) < 4 || len(code) > 8 {
			http.Error(w, "That doesn't look like a party code", 400)
			return
		}
		name := cleanName(in.Name)
		saveConfig(config{Name: name, Setup: party.Setup()})
		party.Start("guest", code, name)
		writeJSON(w, map[string]string{"code": code})
	})
	mux.HandleFunc("POST /api/leave", func(w http.ResponseWriter, r *http.Request) {
		party.Leave()
		writeJSON(w, map[string]bool{"ok": true})
	})
	// MultiViewer itself: running, setup support (debug port), its setups and the one chosen here.
	mux.HandleFunc("GET /api/mv", func(w http.ResponseWriter, r *http.Request) {
		running := mvRunning()
		writeJSON(w, map[string]any{"installed": mvExe() != "", "running": running, "setupsReady": running && mvDebugReady(),
			"setups": mvSetups(), "setup": party.Setup()})
	})
	mux.HandleFunc("POST /api/mv/start", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, mvStart())
	})
	mux.HandleFunc("POST /api/mv/restart", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, mvRestart())
	})
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		json.NewDecoder(r.Body).Decode(&in)
		party.SetSetup(in.Name)
		saveConfig(config{Name: party.Snapshot().Name, Setup: in.Name})
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/setup/open", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		json.NewDecoder(r.Body).Decode(&in)
		if in.Name == "" {
			http.Error(w, "Choose a setup first", 400)
			return
		}
		contentID := ""
		if s := party.Snapshot(); s.Mode == "guest" {
			contentID = party.HostContent()
		}
		replyErr(w, mvOpenSetup(in.Name, contentID))
	})
	mux.HandleFunc("POST /api/sync", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, party.SyncNow())
	})
	mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
		go func() { time.Sleep(200 * time.Millisecond); quit() }()
	})
	return mux
}

func replyErr(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "Guest"
	}
	if len([]rune(s)) > 24 {
		s = string([]rune(s)[:24])
	}
	return s
}

// Party codes: 5 characters without look-alikes (0/O, 1/I/L).
func newCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 5)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}

type config struct {
	Name  string `json:"name"`
	Setup string `json:"setup,omitempty"` // MultiViewer setup opened on joining
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "f1party", "config.json"), nil
}

func loadConfig() config {
	var c config
	if p, err := configPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			json.Unmarshal(b, &c)
		}
	}
	return c
}

func saveConfig(c config) {
	p, err := configPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return
	}
	b, _ := json.Marshal(c)
	os.WriteFile(p, b, 0o644)
}
