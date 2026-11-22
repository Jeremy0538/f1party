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
	text := fmt.Sprintf("🏁 **%s** is hosting an F1 Party", name)
	if watching != "" {
		text += "\n📺 **" + watching + "**"
	}
	text += fmt.Sprintf("\nCode: **%s**", code)
	if link := joinLink(code); link != "" {
		text += fmt.Sprintf(" · [Join the party](<%s>)", link)
	}
	text += "\nNo F1 Party yet? [Download it](<https://github.com/Jeremy0538/f1party/releases/latest>)"
	return postDiscord(webhook, text)
}

// postDiscord posts one message through the webhook (as "F1 Party", never pinging anyone).
func postDiscord(webhook, text string) error {
	if !validWebhook(webhook) {
		return errors.New("that isn't a Discord webhook URL")
	}
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

// mvSessionName: the session MultiViewer has open, e.g. "2026 Las Vegas Grand Prix · Qualifying" ("" when
// unknown). The session window's title is exact for what is open; the live timing is the fallback.
func mvSessionName() string {
	if pages, err := cdpPages(); err == nil {
		for _, p := range pages {
			t := strings.TrimSpace(strings.TrimSuffix(p.Title, "— MultiViewer"))
			if strings.Contains(p.URL, "#/content/") && t != "" && t != "MultiViewer" {
				return strings.Replace(t, " - ", " · ", 1)
			}
		}
	}
	var out struct {
		State struct {
			SessionInfo struct {
				Name      string `json:"Name"`
				StartDate string `json:"StartDate"`
				Meeting   struct {
					Name string `json:"Name"`
				} `json:"Meeting"`
			} `json:"SessionInfo"`
		} `json:"f1LiveTimingState"`
	}
	if gql(`{ f1LiveTimingState { SessionInfo } }`, nil, &out) != nil {
		return ""
	}
	si := out.State.SessionInfo
	gp := si.Meeting.Name
	if len(si.StartDate) >= 4 && gp != "" {
		gp = si.StartDate[:4] + " " + gp
	}
	switch {
	case gp != "" && si.Name != "":
		return gp + " · " + si.Name
	default:
		return gp + si.Name
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
