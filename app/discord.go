package main

// Party invites to a Discord channel through a webhook (Server settings → Integrations → Webhooks → New
// webhook → pick the channel → Copy webhook URL). The URL is kept in this computer's F1 Party settings only.
//
// Instead of a webhook URL the setting can hold a group key (f1p_…): then the app sends only the facts
// (name, code, session, time) to the relay's /post, which checks the key and writes the message to the
// group's channel itself. The group's webhook URL stays a secret on the relay.

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

const groupKeyPrefix = "f1p_"

func isGroupKey(s string) bool {
	if !strings.HasPrefix(s, groupKeyPrefix) || len(s) < len(groupKeyPrefix)+16 || len(s) > 200 {
		return false
	}
	for _, r := range s[len(groupKeyPrefix):] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// validDiscordDest: a webhook URL or a group key.
func validDiscordDest(s string) bool { return validWebhook(s) || isGroupKey(s) }

// relayBase: the relay's https base ("https://host/f1party"), from relayURL.
func relayBase() string {
	u, err := url.Parse(relayURL)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") {
		return ""
	}
	scheme := "https"
	if u.Scheme == "ws" {
		scheme = "http"
	}
	return scheme + "://" + u.Host + strings.TrimSuffix(u.Path, "/ws")
}

// postGroup sends one group post (kind invite, plan or remind with its facts) through the relay.
func postGroup(key string, facts map[string]any) error {
	base := relayBase()
	if base == "" {
		return errors.New("no relay for team invites")
	}
	facts["key"] = key
	body, _ := json.Marshal(facts)
	res, err := discordHTTP.Post(base+"/post", "application/json", bytes.NewReader(body))
	if err != nil {
		return errors.New("can't reach the F1 Party relay")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = res.Status
		}
		return errors.New("Discord: " + msg)
	}
	return nil
}

// groupToken: the token in an f1party://group/TOKEN link (a group invite), or "".
func groupToken(arg string) string {
	rest, ok := strings.CutPrefix(arg, "f1party://group/")
	if !ok {
		return ""
	}
	t := strings.Trim(rest, "/ ")
	if len(t) < 12 || len(t) > 32 {
		return ""
	}
	for _, r := range t {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return t
}

// teamLinkToken: the token in a pasted team link (https://…/f1party/group/TOKEN or f1party://group/TOKEN), or "".
func teamLinkToken(s string) string {
	if t := groupToken(s); t != "" {
		return t
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	if rest, ok := strings.CutPrefix(u.Path, "/f1party/group/"); ok {
		return groupToken("f1party://group/" + rest)
	}
	return ""
}

// relayJSON posts in to the relay path and decodes the answer into out; the relay's text is the error.
func relayJSON(path string, in, out any) error {
	base := relayBase()
	if base == "" {
		return errors.New("no relay")
	}
	body, _ := json.Marshal(in)
	res, err := discordHTTP.Post(base+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return errors.New("can't reach the F1 Party relay")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		if msg := strings.TrimSpace(string(b)); msg != "" {
			return errors.New(msg)
		}
		return errors.New(res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if t := bytes.TrimSpace(b); len(t) == 0 || (t[0] != '{' && t[0] != '[') {
		return nil // a plain "ok"
	}
	return json.Unmarshal(b, out)
}

// redeemGroupInvite trades a team link token for the team key and joins the team.
func redeemGroupInvite(token string) error {
	var r struct{ Key, Name string }
	// our id lets the relay take us back in when the team's owner had removed us before
	if err := relayJSON("/group/redeem", map[string]string{"token": token, "id": persistentID()}, &r); err != nil {
		return err
	}
	if !isGroupKey(r.Key) {
		return errors.New("the relay sent no team key")
	}
	addTeam(r.Key, r.Name)
	return nil
}

// createTeam makes a new team at the relay (name + the team's Discord webhook), with this app as its owner.
func createTeam(name, webhook string) error {
	if name == "" {
		return errors.New("give your team a name")
	}
	if !validWebhook(webhook) {
		return errors.New("that isn't a Discord webhook URL (https://discord.com/api/webhooks/…)")
	}
	var r struct{ Key, Name string }
	if err := relayJSON("/team/create", with(me(), "teamName", name, "webhook", webhook), &r); err != nil {
		return err
	}
	if !isGroupKey(r.Key) {
		return errors.New("the relay sent no team key")
	}
	addTeam(r.Key, r.Name)
	return nil
}

// sendInvite posts the party code (and what the host is watching) to the Discord channel.
func sendInvite(webhook, name, code, watching string) error {
	if webhook == "" {
		return errors.New("join a team or set a Discord channel under Teams first")
	}
	if isGroupKey(webhook) {
		return postGroup(webhook, map[string]any{"kind": "invite", "name": name, "code": code, "title": watching})
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
const pagesJoin = "https://jeremy0538.github.io/f1party/join/?c="

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
