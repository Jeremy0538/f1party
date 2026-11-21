package main

// Tells when a newer F1 Party is out: checks the latest GitHub release now and then.

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const releasesAPI = "https://api.github.com/repos/Jeremy0538/f1party/releases/latest"

// ReleasesPage is where the update link points (also allowed by /api/openurl).
const ReleasesPage = "https://github.com/Jeremy0538/f1party/releases/latest"

type Update struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

func (p *Party) watchUpdates() {
	check := func() {
		c := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest("GET", releasesAPI, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "F1Party/"+version)
		res, err := c.Do(req)
		if err != nil {
			return
		}
		defer res.Body.Close()
		var r struct {
			Tag        string `json:"tag_name"`
			Prerelease bool   `json:"prerelease"`
		}
		if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&r) != nil || r.Prerelease {
			return
		}
		latest := strings.TrimPrefix(r.Tag, "v")
		if compareVersions(latest, version) > 0 {
			p.mu.Lock()
			p.update = &Update{Version: latest, URL: ReleasesPage}
			p.mu.Unlock()
		}
	}
	check()
	for range time.Tick(6 * time.Hour) {
		check()
	}
}
