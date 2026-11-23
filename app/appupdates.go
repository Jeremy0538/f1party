package main

// Updates for the apps F1 Party works with (Settings → Downloads): MultiViewer, the F1 Race Control Stream
// Deck plugin and Ultimate F1 Viewer. Each has an installed version (when F1 Party can tell) and a newest
// version (looked up at most every few hours); a newer one shows a red dot on the gear and on Downloads.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// AppVersion: one app's installed and newest version ("" = unknown).
type AppVersion struct {
	Installed string `json:"installed,omitempty"`
	Latest    string `json:"latest,omitempty"`
	Update    bool   `json:"update,omitempty"` // a newer version is out
	URL       string `json:"url,omitempty"`    // where the update comes from
}

const (
	mvLatestAPI    = "https://api.multiviewer.app/api/v1/releases/latest"
	pluginRepo     = "StanvanDaalen/streamdeck-plugins"
	pluginManifest = "F1/sdPlugin/manifest.json"
	uf1Repo        = "MRAJEKO/UF1-Viewer-with-F1MV"
	appCheckEvery  = 6 * time.Hour
)

var appVers struct {
	sync.Mutex
	latest  map[string]string // newest version per app (mv, sd, uf1)
	at      time.Time
	mvSeen  string // the MultiViewer version last seen running (it only says while it runs)
	checker sync.Once
}

// appUpdates: what Downloads shows per app. Never waits on the network: the newest versions come from the
// last lookup (started here when it's due).
func appUpdates() map[string]AppVersion {
	appVers.checker.Do(func() { go watchAppVersions() })
	inst := map[string]string{"mv": mvInstalledVersion(), "sd": pluginInstalledVersion(), "uf1": uf1InstalledVersion()}
	urls := map[string]string{"mv": mvDownload, "sd": pluginDownload, "uf1": uf1Download}
	appVers.Lock()
	defer appVers.Unlock()
	out := map[string]AppVersion{}
	for k, v := range inst {
		l := appVers.latest[k]
		out[k] = AppVersion{Installed: v, Latest: l, URL: urls[k], Update: v != "" && l != "" && compareVersions(l, v) > 0}
	}
	return out
}

// appUpdateWaiting: any of the apps has a newer version (for the red dot).
func appUpdateWaiting() bool {
	for _, v := range appUpdates() {
		if v.Update {
			return true
		}
	}
	return false
}

func watchAppVersions() {
	for {
		latest := map[string]string{}
		var r struct{ Version string }
		if getJSON(mvLatestAPI, &r) == nil {
			latest["mv"] = r.Version
		}
		if tag := githubLatestTag(pluginRepo); tag != "" {
			var m struct{ Version string }
			if getJSON("https://raw.githubusercontent.com/"+pluginRepo+"/"+tag+"/"+pluginManifest, &m) == nil {
				latest["sd"] = shortVersion(m.Version)
			}
		}
		if tag := githubLatestTag(uf1Repo); tag != "" {
			latest["uf1"] = strings.TrimPrefix(tag, "v")
		}
		appVers.Lock()
		if appVers.latest == nil {
			appVers.latest = map[string]string{}
		}
		for k, v := range latest { // a failed lookup keeps what the last one found
			appVers.latest[k] = v
		}
		appVers.at = time.Now()
		appVers.Unlock()
		time.Sleep(appCheckEvery)
	}
}

func githubLatestTag(repo string) string {
	var r struct {
		Tag string `json:"tag_name"`
	}
	if getJSON("https://api.github.com/repos/"+repo+"/releases/latest", &r) != nil {
		return ""
	}
	return r.Tag
}

// shortVersion: "1.29.4.0" → "1.29.4" (Stream Deck manifests have four parts).
func shortVersion(v string) string {
	if p := strings.Split(v, "."); len(p) == 4 && p[3] == "0" {
		return strings.Join(p[:3], ".")
	}
	return v
}

// mvInstalledVersion: on Windows from the app-x.y.z folder; elsewhere MultiViewer says it while it runs, and
// F1 Party remembers the last answer.
func mvInstalledVersion() string {
	if exe := mvExe(); runtime.GOOS == "windows" && exe != "" {
		if d := filepath.Base(filepath.Dir(exe)); strings.HasPrefix(d, "app-") {
			return d[4:]
		}
	}
	appVers.Lock()
	seen := appVers.mvSeen
	appVers.Unlock()
	if mvRunning() {
		if v, err := gqlVersion(); err == nil && v != "" {
			appVers.Lock()
			appVers.mvSeen = v
			appVers.Unlock()
			return v
		}
	}
	return seen
}

// pluginInstalledVersion: from the plugin's manifest in Stream Deck's or OpenDeck's plugin folder.
func pluginInstalledVersion() string {
	for _, d := range pluginDirs() {
		b, err := os.ReadFile(filepath.Join(d, "com.stan.f1.sdPlugin", "manifest.json"))
		if err != nil {
			continue
		}
		var m struct{ Version string }
		if json.Unmarshal(b, &m) == nil && m.Version != "" {
			return shortVersion(m.Version)
		}
	}
	return ""
}

var uf1VersionRe = regexp.MustCompile(`Ultimate-F1Viewer-With-F1MV/(\d+\.\d+\.\d+)`)

// uf1InstalledVersion: Ultimate F1 Viewer says its version (in its browser name) while it runs; F1 Party
// remembers the last answer.
var uf1Seen string

func uf1InstalledVersion() string {
	var r struct {
		UA string `json:"User-Agent"`
	}
	c := http.Client{Timeout: 700 * time.Millisecond}
	if res, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", uf1Port)); err == nil {
		if json.NewDecoder(res.Body).Decode(&r) == nil {
			if m := uf1VersionRe.FindStringSubmatch(r.UA); m != nil {
				appVers.Lock()
				uf1Seen = m[1]
				appVers.Unlock()
			}
		}
		res.Body.Close()
	}
	appVers.Lock()
	defer appVers.Unlock()
	return uf1Seen
}
