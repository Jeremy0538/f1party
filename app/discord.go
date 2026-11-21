package main

// Party invites to a Discord channel through a webhook (Server settings → Integrations → Webhooks → New
// webhook → pick the channel → Copy webhook URL). The URL is kept in this computer's F1 Party settings only.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var discordHTTP = &http.Client{Timeout: 8 * time.Second}

func validWebhook(u string) bool {
	for _, p := range []string{"https://discord.com/api/webhooks/", "https://discordapp.com/api/webhooks/",
		"https://canary.discord.com/api/webhooks/", "https://ptb.discord.com/api/webhooks/"} {
		if strings.HasPrefix(u, p) && len(u) > len(p)+10 {
			return true
		}
	}
	return false
}

// sendInvite posts the party code (and what the host is watching) to the Discord channel.
func sendInvite(webhook, name, code, watching string) error {
	if webhook == "" {
		return errors.New("set a Discord webhook first")
	}
	if !validWebhook(webhook) {
		return errors.New("that isn't a Discord webhook URL")
	}
	text := fmt.Sprintf("🏁 **%s** is hosting an F1 Party: code **%s**", name, code)
	if watching != "" {
		text += "\nWatching: " + watching
	}
	if link := joinLink(code); link != "" {
		text += "\nJoin with one click: " + link
	}
	text += "\nOpen F1 Party, enter the code and click Join. No F1 Party yet? " +
		"<https://github.com/Jeremy0538/f1party/releases/latest>"
	body, _ := json.Marshal(map[string]any{
		"username": "F1 Party", "content": text,
		"allowed_mentions": map[string]any{"parse": []string{}}, // never ping anyone
	})
	res, err := discordHTTP.Post(webhook+"?wait=true", "application/json", bytes.NewReader(body))
	if err != nil {
		return errors.New("can't reach Discord")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e struct{ Message string }
		json.NewDecoder(res.Body).Decode(&e)
		if e.Message == "" {
			e.Message = res.Status
		}
		return errors.New("Discord: " + e.Message)
	}
	return nil
}

// mvSessionName: the session MultiViewer shows, e.g. "Las Vegas Grand Prix · Qualifying" ("" when unknown).
func mvSessionName() string {
	var out struct {
		State struct {
			SessionInfo struct {
				Name    string `json:"Name"`
				Meeting struct {
					Name string `json:"Name"`
				} `json:"Meeting"`
			} `json:"SessionInfo"`
		} `json:"f1LiveTimingState"`
	}
	if gql(`{ f1LiveTimingState { SessionInfo } }`, nil, &out) != nil {
		return ""
	}
	si := out.State.SessionInfo
	switch {
	case si.Meeting.Name != "" && si.Name != "":
		return si.Meeting.Name + " · " + si.Name
	default:
		return si.Meeting.Name + si.Name
	}
}

// joinLink: an https link that opens F1 Party on this party (the page redirects to f1party://join/CODE), or ""
// when no join page answers. The relay's own page comes first; the GitHub Pages copy is the fallback.
func joinLink(code string) string {
	if base := joinBase(); base != "" {
		return base + code
	}
	return ""
}

// Join pages, best first: the relay's (once deployed), then the static page on GitHub Pages.
const pagesJoin = "https://stanvandaalen.github.io/f1party/join/?c="

var joinBaseCache struct {
	sync.Mutex
	base    string
	checked time.Time
}

func joinBase() string {
	joinBaseCache.Lock()
	defer joinBaseCache.Unlock()
	if time.Since(joinBaseCache.checked) < 10*time.Minute {
		return joinBaseCache.base
	}
	joinBaseCache.checked = time.Now()
	joinBaseCache.base = ""
	var bases []string
	if u, err := url.Parse(relayURL); err == nil && (u.Scheme == "wss" || u.Scheme == "ws") {
		scheme := "https"
		if u.Scheme == "ws" {
			scheme = "http"
		}
		bases = append(bases, scheme+"://"+u.Host+strings.TrimSuffix(u.Path, "/ws")+"/join/")
	}
	bases = append(bases, pagesJoin)
	for _, base := range bases {
		if servesJoinPage(base + "TEST") {
			joinBaseCache.base = base
			break
		}
	}
	return joinBaseCache.base
}

func servesJoinPage(u string) bool {
	res, err := (&http.Client{Timeout: 4 * time.Second}).Get(u)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 16384))
	return res.StatusCode == 200 && strings.Contains(string(b), "f1party://join/")
}
