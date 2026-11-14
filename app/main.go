// F1 Party: watch MultiViewer for F1 in sync with friends. One small program per platform; it opens a
// local page in the browser and talks to the MultiViewer running on the same computer.
package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var version = "0.1.0"

// Overridable for testing against `wrangler dev`: F1PARTY_RELAY=ws://localhost:8787/f1party/ws
var relayURL = envOr("F1PARTY_RELAY", "wss://jeremyklootwijk.nl/f1party/ws")

var uiAddr = "127.0.0.1:" + envOr("F1PARTY_PORT", "47821")

//go:embed ui.html
var uiHTML []byte

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	pageURL := "http://" + uiAddr + "/"
	ln, err := net.Listen("tcp", uiAddr)
	if err != nil { // already running: just show it
		fmt.Println("F1 Party is already running, opening it…")
		openBrowser(pageURL)
		return
	}
	cfg := loadConfig()
	party := NewParty(cfg.Name)

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
		saveConfig(config{Name: name})
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
		saveConfig(config{Name: name})
		party.Start("guest", code, name)
		writeJSON(w, map[string]string{"code": code})
	})
	mux.HandleFunc("POST /api/leave", func(w http.ResponseWriter, r *http.Request) {
		party.Leave()
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
		go func() { time.Sleep(200 * time.Millisecond); os.Exit(0) }()
	})

	// Only this computer's browser may drive it: reject requests whose Host isn't our own address
	// (DNS-rebinding guard) and cross-site posts.
	guard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != uiAddr {
			http.Error(w, "forbidden", 403)
			return
		}
		if o := r.Header.Get("Origin"); r.Method == "POST" && o != "" && o != "http://"+uiAddr {
			http.Error(w, "forbidden", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})

	fmt.Printf("F1 Party %s is running at %s\nKeep this window open while you watch. Close it to stop.\n", version, pageURL)
	if os.Getenv("F1PARTY_NOBROWSER") == "" {
		go func() { time.Sleep(300 * time.Millisecond); openBrowser(pageURL) }()
	}
	log.Fatal(http.Serve(ln, guard))
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

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("Open this address in your browser:", u)
	}
}

type config struct {
	Name string `json:"name"`
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
