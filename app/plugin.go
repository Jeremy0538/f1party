package main

// The F1 Race Control Stream Deck plugin (github.com/StanvanDaalen/streamdeck-plugins), optional. When it is
// installed it can do the whole Race / Quali opening for F1 Party: its own desktop, MultiViewer and Ultimate
// F1 Viewer in place, the setup with the best of the rest and the viewer's layout. It listens on
// 127.0.0.1:9335 while Stream Deck runs.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// setupStreamDeck: the "setup" choice that hands the opening to the Stream Deck plugin.
const setupStreamDeck = "@streamdeck"

const pluginURL = "http://127.0.0.1:9335"

// Release page and direct download of the plugin (double-click the file to install it in Stream Deck).
const (
	pluginPage     = "https://github.com/StanvanDaalen/streamdeck-plugins#f1-race-control"
	pluginDownload = "https://github.com/StanvanDaalen/streamdeck-plugins/releases/latest/download/F1.streamDeckPlugin"
)

var pluginHTTP = &http.Client{Timeout: 1500 * time.Millisecond}

// PluginInfo: whether the plugin is installed and whether it answers now.
type PluginInfo struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	Version   string `json:"version,omitempty"`
	Download  string `json:"download"`
	Page      string `json:"page"`
}

func pluginInfo() PluginInfo {
	info := PluginInfo{Installed: pluginInstalled(), Download: pluginDownload, Page: pluginPage}
	if res, err := pluginHTTP.Get(pluginURL + "/f1/status"); err == nil {
		var s struct{ Version string }
		if res.StatusCode == 200 && json.NewDecoder(res.Body).Decode(&s) == nil {
			info.Running, info.Version, info.Installed = true, s.Version, true
		}
		res.Body.Close()
	}
	return info
}

// pluginInstalled looks for the plugin's folder in Stream Deck (Windows, macOS) or OpenDeck (Linux).
func pluginInstalled() bool {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return false
	}
	var dir string
	switch runtime.GOOS {
	case "windows":
		dir = filepath.Join(cfg, "Elgato", "StreamDeck", "Plugins")
	case "darwin":
		dir = filepath.Join(cfg, "com.elgato.StreamDeck", "Plugins")
	default:
		dir = filepath.Join(cfg, "opendeck", "plugins")
	}
	return fileExists(filepath.Join(dir, "com.stan.f1.sdPlugin"))
}

// pluginBusy: the plugin is still opening (desktop, apps, setup, layout). Unknown counts as not busy.
func pluginBusy() bool {
	res, err := pluginHTTP.Get(pluginURL + "/f1/status")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var s struct{ Busy bool }
	return json.NewDecoder(res.Body).Decode(&s) == nil && s.Busy
}

// pluginOpen asks the plugin for Race / Quali / Practice, for one session (contentID) or, without, for
// the live or latest one. The plugin answers at once and does the opening in the background.
func pluginOpen(kind, contentID string) error {
	body, _ := json.Marshal(map[string]any{"kind": kind, "contentId": nilIfEmpty(contentID)})
	res, err := pluginHTTP.Post(pluginURL+"/f1/open", "application/json", bytes.NewReader(body))
	if err != nil {
		if pluginInstalled() {
			return errors.New("the Stream Deck plugin doesn't answer: start Stream Deck, or update the plugin")
		}
		return errors.New("the F1 Race Control Stream Deck plugin isn't installed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		var e struct{ Error string }
		json.NewDecoder(res.Body).Decode(&e)
		return fmt.Errorf("Stream Deck plugin: %s", e.Error)
	}
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// sessionKind: the plugin's layout for a timing session type ("Race", "Qualifying", "Sprint Qualifying",
// "Practice 2", …).
func sessionKind(sessionType string) string {
	switch {
	case strings.Contains(sessionType, "Qualifying"), strings.Contains(sessionType, "Shootout"):
		return "quali"
	case strings.Contains(sessionType, "Practice"):
		return "practice"
	default:
		return "race"
	}
}

// mvSessionType: the type of the session MultiViewer's timing shows now ("" when unknown).
func mvSessionType() string {
	var out struct {
		State struct {
			SessionInfo struct {
				Type string `json:"Type"`
				Name string `json:"Name"`
			} `json:"SessionInfo"`
		} `json:"f1LiveTimingState"`
	}
	if gql(`{ f1LiveTimingState { SessionInfo } }`, nil, &out) != nil {
		return ""
	}
	if n := out.State.SessionInfo.Name; strings.Contains(n, "Sprint") && out.State.SessionInfo.Type == "Qualifying" {
		return "Sprint Qualifying"
	}
	return out.State.SessionInfo.Type
}
