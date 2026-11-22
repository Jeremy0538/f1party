// F1 Party: watch MultiViewer for F1 in sync with friends. A small desktop app (Wails: the system's own
// web view, no browser needed) that talks to the MultiViewer running on the same computer.
package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var version = "1.0.0"

// Overridable for testing against `wrangler dev`: F1PARTY_RELAY=ws://localhost:8787/f1party/ws
var relayURL = envOr("F1PARTY_RELAY", "wss://jeremyklootwijk.nl/f1party/ws")

//go:embed ui.html
var uiHTML []byte

//go:embed logo64.png
var logoPNG []byte

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	afterUpdate(os.Args[1:])
	cfg := loadConfig()
	party := NewParty(cfg.Name, setupChoice(cfg.Setup))
	go party.watchUpdates()
	go party.watchPresence()
	migrateTeams()
	go party.watchPeople()
	go rememberParty(party)
	go party.runPlans()
	go followMainStream()
	go refreshLineup()
	go func() { // friend requests in the party
		for range time.Tick(2 * time.Second) {
			if party.Snapshot().Mode != "idle" {
				party.checkFriends()
			}
		}
	}()

	var appCtx context.Context
	show := func() {
		if appCtx != nil {
			wruntime.WindowShow(appCtx)
			wruntime.WindowUnminimise(appCtx)
			wruntime.Show(appCtx)
		}
	}
	// hostAndInvite: host a party (or keep the one hosted now) and post the invite. With resync (the Stream Deck's
	// Sync key held), a host that already runs a party first has everyone sync to them.
	hostAndInvite := func(resync bool) (string, error) {
		s := party.Snapshot()
		resynced := false
		switch s.Mode {
		case "guest":
			return "", errors.New("you're a guest in a party; leave it first")
		case "idle":
			name := cleanName(loadConfig().Name)
			party.Start("host", newCode(), name)
			s = party.Snapshot()
		case "host":
			if resync {
				// already hosting: everyone syncs to you, then the invite goes out again
				resynced = party.ResyncAll() == nil
				party.SetNote("Everyone synced to you")
			}
		}
		t := defaultTargets()
		if len(t.Teams) > 0 || len(t.Friends) > 0 || t.Own {
			watching := mvSessionName()
			if f := party.Snapshot().Feed; watching != "" && f != nil {
				if f.Live {
					watching += " · LIVE"
				} else {
					watching += " · replay"
				}
			}
			if _, err := sendPartyInvites(t, s.Name, s.Code, watching); err != nil {
				party.SetNote(err.Error())
				return s.Code, err
			}
			if resynced {
				party.SetNote("Everyone synced to you · invites sent again")
			} else {
				party.SetNote("Invites sent")
			}
		}
		return s.Code, nil
	}
	startControl(party, controlHooks{
		host:   func() (string, error) { return hostAndInvite(true) },
		invite: func() (string, error) { return hostAndInvite(false) },
		copy: func(text string) error {
			if appCtx == nil {
				return errors.New("F1 Party is still starting")
			}
			return wruntime.ClipboardSetText(appCtx, text)
		},
		show:   show,
	})
	hostArg := func(args []string) bool {
		for _, a := range args {
			if a == "--host" {
				return true
			}
		}
		return false
	}
	// f1party://join/CODE (an invite link): join that party under your saved name.
	joinArg := func(args []string) {
		for _, a := range args {
			if code := joinCode(a); code != "" {
				if s := party.Snapshot(); s.Code == code {
					return
				}
				party.Start("guest", code, cleanName(loadConfig().Name))
				return
			}
		}
	}

	// f1party://group/TOKEN (a group invite link): set up the group's Discord invites.
	groupArg := func(args []string) bool {
		for _, a := range args {
			if t := groupToken(a); t != "" {
				if err := redeemGroupInvite(t); err != nil {
					party.SetNote("Team link: " + err.Error())
				} else {
					party.SetNote("You joined the team: party invites now go to the team's Discord channel")
				}
				return true
			}
			if t := friendLinkToken(a); t != "" {
				if name, err := redeemFriendLink(t); err != nil {
					party.SetNote("Friend link: " + err.Error())
				} else {
					party.SetNote("You and " + name + " are friends now")
				}
				return true
			}
		}
		return false
	}

	err := wails.Run(&options.App{
		Title:            "F1 Party",
		Width:            880,
		Height:           660,
		MinWidth:         680,
		MinHeight:        480,
		BackgroundColour: &options.RGBA{R: 15, G: 17, B: 21, A: 255},
		AssetServer: &assetserver.Options{Handler: newHandler(party, func() { wruntime.Quit(appCtx) }, func(u string) { wruntime.BrowserOpenURL(appCtx, u) },
			func(text string) error { return wruntime.ClipboardSetText(appCtx, text) }, hostAndInvite)},
		OnStartup: func(ctx context.Context) {
			appCtx = ctx
			restartAfterUpdate = func() { markQuitting(); wruntime.Quit(appCtx) }
			setupNotifications(ctx, show)
			startTray(show, func() { wruntime.Quit(appCtx) })
			switch {
			case hostArg(os.Args[1:]):
				go hostAndInvite(true)
			case hasJoinArg(os.Args[1:]):
				go joinArg(os.Args[1:])
			case hasGroupArg(os.Args[1:]):
				go func() { groupArg(os.Args[1:]); rejoinLast(party) }()
			default:
				go rejoinLast(party)
			}
		},
		// With the tray icon up, closing the window hides it and the party keeps running.
		OnBeforeClose: func(ctx context.Context) bool {
			if keepRunningOnClose() {
				wruntime.WindowHide(ctx)
				return true
			}
			return false
		},
		OnShutdown: func(context.Context) { stopTray(); taskbarRestore() },
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "nl.jeremyklootwijk.f1party",
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) { // already running: bring it to the front
				show()
				if hostArg(d.Args) {
					go hostAndInvite(true)
				}
				go joinArg(d.Args)
				go groupArg(d.Args)
			},
		},
		Windows: &windows.Options{Theme: windows.Dark},
		Mac:     &mac.Options{Appearance: mac.NSAppearanceNameDarkAqua, About: &mac.AboutInfo{Title: "F1 Party", Message: "Version " + version}},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// setupNotifications turns on system notifications (Windows toasts, macOS and Linux notifications) for the
// calendar; clicking one brings the window up.
func setupNotifications(ctx context.Context, show func()) {
	if err := wruntime.InitializeNotifications(ctx); err != nil || !wruntime.IsNotificationAvailable(ctx) {
		log.Printf("notifications: not available (%v)", err)
		return
	}
	brandNotifications()
	wruntime.OnNotificationResponse(ctx, func(wruntime.NotificationResult) { show() })
	n := 0
	sendNotification = func(title, body string) error {
		if goruntime.GOOS == "darwin" {
			if ok, _ := wruntime.RequestNotificationAuthorization(ctx); !ok {
				return errors.New("notifications aren't allowed for F1 Party")
			}
		}
		n++
		return wruntime.SendNotification(ctx, wruntime.NotificationOptions{
			ID: fmt.Sprintf("f1party-%d-%d", time.Now().Unix(), n), Title: title, Body: body})
	}
}

// newHandler serves the UI and its small JSON API. It is only reachable from the app's own window.
func newHandler(party *Party, quit func(), openURL func(string), copyText func(string) error, hostAndInvite func(bool) (string, error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	})
	mux.HandleFunc("GET /logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(logoPNG)
	})
	mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("now") == "1" { // Settings → Updates: check now and say what came out
			updateCheckedAt.Store(time.Now().Unix())
			writeJSON(w, party.CheckUpdateNow())
			return
		}
		checkUpdateSoon()
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, party.LastUpdateCheck())
	})
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		s := party.Snapshot()
		cfg := loadConfig()
		s.Welcome = welcomeFor(cfg)
		if s.Mode == "idle" {
			s.Name = cfg.Name // the name from Settings (shown on the start screen)
		}
		// open: a tab the window should show once (the Stream Deck's Start session asks for the calendar)
		open, _ := uiOpen.Swap("").(string)
		writeJSON(w, struct {
			Status
			Open string `json:"open,omitempty"`
		}{s, open})
	})
	// The setup guide or the what's new window was shown: not again (until the next version).
	mux.HandleFunc("POST /api/welcome/done", func(w http.ResponseWriter, r *http.Request) {
		updateConfig(func(c *config) { c.SetupDone, c.SeenVersion = true, version })
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/host", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		json.NewDecoder(r.Body).Decode(&in)
		// the name from Settings, unless one is sent (older windows)
		if strings.TrimSpace(in.Name) == "" {
			in.Name = loadConfig().Name
		}
		name := cleanName(in.Name)
		updateConfig(func(c *config) { c.Name = name })
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
		if strings.TrimSpace(in.Name) == "" {
			in.Name = loadConfig().Name
		}
		name := cleanName(in.Name)
		updateConfig(func(c *config) { c.Name = name })
		party.Start("guest", code, name)
		writeJSON(w, map[string]string{"code": code})
	})
	mux.HandleFunc("POST /api/leave", func(w http.ResponseWriter, r *http.Request) {
		endParty(party)
		writeJSON(w, map[string]bool{"ok": true})
	})
	// MultiViewer itself: running, setup support (debug port), its setups and the one chosen here.
	mux.HandleFunc("GET /api/mv", func(w http.ResponseWriter, r *http.Request) {
		running := mvRunning()
		out := map[string]any{"installed": mvExe() != "", "running": running, "setupsReady": running && mvDebugReady(),
			"setups": mvSetups(), "setup": party.Setup(), "plugin": pluginInfo(), "uf1": uf1Installed() && !loadConfig().Uf1Off}
		// what really runs: Ultimate F1 Viewer only when it's up, and how many streams MultiViewer shows
		_, out["uf1Running"] = uf1Main()
		if running {
			if ps, err := players(); err == nil {
				out["streams"] = len(ps)
			}
		}
		if out["uf1"] == true {
			c := loadConfig()
			out["uf1Layouts"], out["uf1For"], out["uf1Auto"] = uf1Layouts(), c.Uf1For, uf1LayoutFor(party.Setup())
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("POST /api/mv/start", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, mvStart())
	})
	mux.HandleFunc("POST /api/mv/restart", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, mvRestart())
	})
	mux.HandleFunc("POST /api/setup/open", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		json.NewDecoder(r.Body).Decode(&in)
		if in.Name == "" {
			http.Error(w, "Choose a setup first", 400)
			return
		}
		contentID, sessionType := "", ""
		if s := party.Snapshot(); s.Mode == "guest" {
			contentID, sessionType = party.HostSession()
		}
		in.Name = effectiveSetup(in.Name) // no plugin answering: F1 Party opens it itself
		if in.Name == setupStreamDeck {
			if contentID == "" {
				sessionType = mvSessionType()
			}
			replyErr(w, pluginOpen(sessionKind(sessionType), contentID))
			return
		}
		// MultiViewer closed: start it, wait until it can open setups, then open this one
		party.mu.Lock()
		party.setupBusy = true
		party.mu.Unlock()
		err := mvStartReady()
		if err == nil {
			err = openChosenSetup(in.Name, contentID, sessionType)
		}
		party.mu.Lock()
		party.setupBusy = false
		if err == nil && contentID != "" {
			party.setupDone = contentID
		}
		party.mu.Unlock()
		replyErr(w, err)
	})
	mux.HandleFunc("POST /api/openurl", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ URL string }
		json.NewDecoder(r.Body).Decode(&in)
		if !allowedURL(in.URL) {
			http.Error(w, "forbidden", 403)
			return
		}
		openURL(in.URL)
		writeJSON(w, map[string]bool{"ok": true})
	})
	// Friends and teams (people.go): what the Friends and Teams tabs show, and their actions.
	mux.HandleFunc("GET /api/people", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, peopleView()) })
	mux.HandleFunc("GET /api/invitechoice", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, loadConfig().InviteTo) })
	// copyLink copies a made link and answers it (or says what went wrong).
	copyLink := func(w http.ResponseWriter, link string, err error) {
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := copyText(link); err != nil {
			http.Error(w, "Couldn't copy the link: "+link, 500)
			return
		}
		writeJSON(w, map[string]string{"url": link})
	}
	mux.HandleFunc("POST /api/friendlink", func(w http.ResponseWriter, r *http.Request) {
		link, err := newFriendLink()
		copyLink(w, link, err)
	})
	handle := func(path string, f func(w http.ResponseWriter, in *peopleReq)) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			var in peopleReq
			json.NewDecoder(r.Body).Decode(&in)
			f(w, &in)
		})
	}
	handle("/api/team/create", func(w http.ResponseWriter, in *peopleReq) {
		replyErr(w, createTeam(strings.TrimSpace(in.Name), strings.TrimSpace(in.Webhook)))
	})
	handle("/api/team/link", func(w http.ResponseWriter, in *peopleReq) { link, err := teamLink(in.ID); copyLink(w, link, err) })
	handle("/api/team/leave", func(w http.ResponseWriter, in *peopleReq) { replyErr(w, leaveTeam(in.ID)) })
	handle("/api/team/rename", func(w http.ResponseWriter, in *peopleReq) {
		replyErr(w, teamCall("/team/rename", in.ID, "teamName", strings.TrimSpace(in.Name)))
	})
	handle("/api/team/invitefriend", func(w http.ResponseWriter, in *peopleReq) {
		replyErr(w, teamCall("/team/invitefriend", in.ID, "to", in.To))
	})
	handle("/api/team/kick", func(w http.ResponseWriter, in *peopleReq) {
		replyErr(w, teamCall("/team/kick", in.ID, "member", in.To))
	})
	// The join field: a team link, a friend link, a team key or an own Discord webhook ("" removes the webhook).
	handle("/api/addlink", func(w http.ResponseWriter, in *peopleReq) {
		v := strings.TrimSpace(in.Link)
		switch {
		case teamLinkToken(v) != "":
			replyErr(w, redeemGroupInvite(teamLinkToken(v)))
		case friendLinkToken(v) != "":
			name, err := redeemFriendLink(friendLinkToken(v))
			if err == nil {
				party.SetNote("You and " + name + " are friends now")
			}
			replyErr(w, err)
		case isGroupKey(v):
			replyErr(w, joinTeamByKey(v))
		case v == "" || validWebhook(v):
			updateConfig(func(c *config) { c.Webhook = v })
			writeJSON(w, map[string]bool{"ok": true})
		default:
			http.Error(w, "Paste a team link, a friend link or a Discord webhook URL", 400)
		}
	})
	handle("/api/teaminvite/accept", func(w http.ResponseWriter, in *peopleReq) {
		err := redeemGroupInvite(in.Token)
		dropTeamInvite(in.Token)
		replyErr(w, err)
	})
	handle("/api/teaminvite/dismiss", func(w http.ResponseWriter, in *peopleReq) {
		dropTeamInvite(in.Token)
		writeJSON(w, map[string]bool{"ok": true})
	})
	handle("/api/partyinvite/dismiss", func(w http.ResponseWriter, in *peopleReq) {
		dismissInvite(in.From)
		writeJSON(w, map[string]bool{"ok": true})
	})
	// Join a friend's or teammate's party (from their status or an invite).
	handle("/api/joinparty", func(w http.ResponseWriter, in *peopleReq) {
		code := strings.ToUpper(strings.TrimSpace(in.To))
		if joinCode("f1party://join/"+code) == "" {
			http.Error(w, "Bad party code", 400)
			return
		}
		dismissInvite(in.From)
		if party.Snapshot().Code != code {
			party.Start("guest", code, cleanName(loadConfig().Name))
		}
		pokePeople()
		writeJSON(w, map[string]bool{"ok": true})
	})
	// Invite: post the party to teams' Discord channels / the own webhook and invite friends in their app.
	handle("/api/invite", func(w http.ResponseWriter, in *peopleReq) {
		if party.Snapshot().Mode == "idle" {
			http.Error(w, "Host or join a party first", 400)
			return
		}
		t := inviteTargets{Teams: in.Teams, Friends: in.Friends, Own: in.Own}
		if len(t.Teams)+len(t.Friends) == 0 && !t.Own {
			http.Error(w, "Pick a team or a friend", 400)
			return
		}
		updateConfig(func(c *config) { c.InviteTo = &t })
		s := party.Snapshot()
		watching := mvSessionName()
		if s.Feed != nil && watching != "" {
			if s.Feed.Live {
				watching += " · LIVE"
			} else {
				watching += " · replay"
			}
		}
		n, err := sendPartyInvites(t, s.Name, s.Code, watching)
		if err == nil {
			party.SetNote(fmt.Sprintf("Invites sent (%d)", n))
		}
		replyErr(w, err)
	})
	mux.HandleFunc("POST /api/copycode", func(w http.ResponseWriter, r *http.Request) {
		code := party.Snapshot().Code
		if code == "" {
			http.Error(w, "Not in a party", 400)
			return
		}
		replyErr(w, copyText(code))
	})
	mux.HandleFunc("POST /api/start", func(w http.ResponseWriter, r *http.Request) {
		err := party.StartTogether()
		if errors.Is(err, errNoSession) {
			// no session open: one that is live now opens after asking where to start; else pick one in the calendar
			pick := map[string]any{"pick": "session"}
			if s, m, ok := liveCalSession(); ok {
				pick = map[string]any{"pick": "live", "session": s.Key, "name": sessionTitle(s, m)}
			}
			b, _ := json.Marshal(pick)
			http.Error(w, string(b), 409)
			return
		}
		replyErr(w, err)
	})
	// The session MultiViewer shows, and jumping to a moment in it (the host takes the party along).
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"session": party.Session()})
	})
	mux.HandleFunc("POST /api/makehost", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		json.NewDecoder(r.Body).Decode(&in)
		replyErr(w, party.MakeHost(in.ID))
	})
	mux.HandleFunc("POST /api/session/end", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, party.EndSession())
	})
	mux.HandleFunc("POST /api/session/again", func(w http.ResponseWriter, r *http.Request) {
		party.WatchAgain()
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/session/jump", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ To string }
		json.NewDecoder(r.Body).Decode(&in)
		replyErr(w, party.Jump(in.To))
	})
	// The F1 calendar and planned watch parties.
	mux.HandleFunc("GET /api/calendar", func(w http.ResponseWriter, r *http.Request) {
		meeting, _ := strconv.Atoi(r.URL.Query().Get("meeting"))
		wk, err := calendarWeekend(meeting)
		if err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		c := loadConfig()
		writeJSON(w, map[string]any{"weekend": wk, "webhookSet": c.Webhook != "", "notify": c.Notify, "leads": notifyLeads(c),
			"notifications": sendNotification != nil})
	})
	mux.HandleFunc("GET /api/calendar/season", func(w http.ResponseWriter, r *http.Request) {
		y, _ := strconv.Atoi(r.URL.Query().Get("year"))
		s, err := season(y)
		if err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		writeJSON(w, s)
	})
	mux.HandleFunc("POST /api/calendar/watch", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Session int
			Start   string // begin | start | live | ""
		}
		json.NewDecoder(r.Body).Decode(&in)
		switch in.Start {
		case "", "begin", "start", "live":
		default:
			in.Start = ""
		}
		replyErr(w, party.WatchNow(in.Session, cleanName(loadConfig().Name), in.Start))
	})
	mux.HandleFunc("POST /api/calendar/plan", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Session int
			At      int64
		}
		json.NewDecoder(r.Body).Decode(&in)
		pl, err := Plan(in.Session, in.At, cleanName(loadConfig().Name))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, pl)
	})
	mux.HandleFunc("POST /api/calendar/notify", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Mode string }
		json.NewDecoder(r.Body).Decode(&in)
		if err := SetNotify(in.Mode); err != nil {
			replyErr(w, err)
			return
		}
		// one right away, so you see what they look like (and macOS asks for permission now)
		if sendNotification != nil && in.Mode != notifyOff {
			what := map[string]string{notifyRace: "every race", notifyMain: "every race and qualifying", notifyAll: "every session"}[in.Mode]
			go sendNotification("Notifications are on", "F1 Party lets you know before "+what+" starts.")
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/calendar/leads", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Leads []int }
		json.NewDecoder(r.Body).Decode(&in)
		replyErr(w, SetNotifyLeads(in.Leads))
	})
	mux.HandleFunc("POST /api/calendar/bell", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Session int }
		json.NewDecoder(r.Body).Decode(&in)
		writeJSON(w, map[string]bool{"on": ToggleNotify(in.Session)})
	})
	// Friends: ask someone in the party (or accept their request), the list, remove one.
	mux.HandleFunc("POST /api/friend/ask", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		json.NewDecoder(r.Body).Decode(&in)
		if in.ID == "" {
			http.Error(w, "unknown member", 400)
			return
		}
		party.AskFriend(in.ID)
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/friends", func(w http.ResponseWriter, r *http.Request) {
		fs := loadFriends()
		if fs == nil {
			fs = []Friend{}
		}
		writeJSON(w, map[string]any{"friends": fs})
	})
	mux.HandleFunc("POST /api/friend/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		json.NewDecoder(r.Body).Decode(&in)
		RemoveFriend(in.ID)
		pokePeople()
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/calendar/unplan", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ ID string }
		json.NewDecoder(r.Body).Decode(&in)
		Unplan(in.ID)
		writeJSON(w, map[string]bool{"ok": true})
	})
	// Sync: a host has everyone (and their own onboards) sync to their main stream; a guest jumps to the host.
	mux.HandleFunc("POST /api/sync", func(w http.ResponseWriter, r *http.Request) {
		if party.Snapshot().Mode == "host" {
			replyErr(w, party.ResyncAll())
			return
		}
		replyErr(w, party.SyncNow())
	})
	// Settings (the gear at the top right).
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, settingsView()) })
	mux.HandleFunc("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		var in settingsIn
		if json.Unmarshal(body, &in) != nil || json.Unmarshal(body, &raw) != nil {
			http.Error(w, "Bad settings", 400)
			return
		}
		applySettings(in, party)
		if settingsApplyExtra != nil {
			settingsApplyExtra(raw)
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	// Close F1 (older windows; the app now does this with End session): MultiViewer and Ultimate F1 Viewer
	// closed, desktop, lights and taskbar back (as set).
	mux.HandleFunc("POST /api/closef1", func(w http.ResponseWriter, r *http.Request) {
		go closeF1()
		writeJSON(w, map[string]bool{"ok": true})
	})
	extraRoutes(mux)
	// Ultimate F1 Viewer layout with a MultiViewer setup ("-" = none, "" = the one with the same name).
	mux.HandleFunc("POST /api/setup/uf1", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Setup, Layout string }
		json.NewDecoder(r.Body).Decode(&in)
		if in.Setup == "" || in.Setup == setupStreamDeck {
			http.Error(w, "Choose a MultiViewer setup first", 400)
			return
		}
		updateConfig(func(c *config) {
			if c.Uf1For == nil {
				c.Uf1For = map[string]string{}
			}
			if in.Layout == "" {
				delete(c.Uf1For, in.Setup)
			} else {
				c.Uf1For[in.Setup] = in.Layout
			}
		})
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/autosync", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ On bool }
		json.NewDecoder(r.Body).Decode(&in)
		party.SetAutoSync(in.On)
		updateConfig(func(c *config) { c.AutoSyncOff = !in.On })
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/update", func(w http.ResponseWriter, r *http.Request) {
		replyErr(w, party.InstallUpdate())
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

func hasGroupArg(args []string) bool {
	for _, a := range args {
		if groupToken(a) != "" || friendLinkToken(a) != "" {
			return true
		}
	}
	return false
}

func hasJoinArg(args []string) bool {
	for _, a := range args {
		if joinCode(a) != "" {
			return true
		}
	}
	return false
}

// joinCode: the party code in an f1party://join/CODE link, or "".
func joinCode(arg string) string {
	rest, ok := strings.CutPrefix(strings.ToLower(arg), "f1party://join/")
	if !ok {
		return ""
	}
	code := strings.ToUpper(strings.Trim(rest, "/ "))
	for _, r := range code {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	if len(code) < 4 || len(code) > 8 {
		return ""
	}
	return code
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
	Name           string         `json:"name"`
	SetupDone      bool           `json:"setupDone,omitempty"`    // the first-start guide was gone through
	SeenVersion    string         `json:"seenVersion,omitempty"`  // the version whose "what's new" was shown
	Setup          string         `json:"setup,omitempty"`        // how sessions open: "" automatic per session type, "@live" F1 Live only, "@streamdeck", or a setup
	Webhook        string         `json:"webhook,omitempty"`      // own Discord webhook for party invites (a team key here is moved to Teams)
	TeamName       string         `json:"teamName,omitempty"`     // (1.3.0) name of the team key kept in Webhook
	Teams          []TeamRef      `json:"teams,omitempty"`        // teams this app is in
	Secret         string         `json:"secret,omitempty"`       // proves to the relay that MemberID is ours
	InviteTo       *inviteTargets `json:"inviteTo,omitempty"`     // last Invite choice
	DiscordAppID   string         `json:"discordAppId,omitempty"` // Discord application for Rich Presence (overrides the built-in one)
	Last           *lastParty     `json:"last,omitempty"`
	Plans          []plan         `json:"plans,omitempty"`    // planned watch parties
	MemberID       string         `json:"memberId,omitempty"` // this app's fixed id (friends know it by this)
	Friends        []Friend       `json:"friends,omitempty"`
	NotifyLeads    []int          `json:"notifyLeads"`              // minutes before a session to notify (nil = 15)
	Notify         string         `json:"notify,omitempty"`         // calendar notifications: "", "main" or "all"
	NotifySessions []int          `json:"notifySessions,omitempty"` // single sessions with their bell on         // the party you were in, to rejoin after a restart
	AutoSyncOff    bool           `json:"autoSyncOff,omitempty"`    // a guest only syncs on "Sync to host" (auto-sync is on by default)
	// Settings (the gear at the top right)
	DesktopOff         bool              `json:"desktopOff,omitempty"`         // open on the desktop that happens to show
	DesktopName        string            `json:"desktopName,omitempty"`        // the desktop for watching ("" = "F1")
	Taskbar            string            `json:"taskbar,omitempty"`            // while watching: "" leave it, "main" main monitor only, "autohide"
	TaskbarRestore     bool              `json:"taskbarRestore,omitempty"`     // F1 Party turned "on all displays" off: put it back
	TaskbarRestoreHide bool              `json:"taskbarRestoreHide,omitempty"` // F1 Party turned auto-hide on: put it back
	Uf1For             map[string]string `json:"uf1For,omitempty"`             // MultiViewer setup -> Ultimate F1 Viewer layout id ("-" = none)
	// Sessions: a MultiViewer setup per session type (with the setup choice "Automatic per session type")
	SetupRace     string   `json:"setupRace,omitempty"`     // "" = found by its name
	SetupQuali    string   `json:"setupQuali,omitempty"`    //
	SetupPractice string   `json:"setupPractice,omitempty"` //
	Uf1Off        bool     `json:"uf1Off,omitempty"`        // don't use Ultimate F1 Viewer (even when it's installed)
	Uf1CloseFirst bool     `json:"uf1CloseFirst,omitempty"` // close the viewer's open windows before its layout opens
	FitTiming     bool     `json:"fitTiming,omitempty"`     // zoom Live Timing to fill its window
	RestOn        bool     `json:"restOn,omitempty"`        // best of the rest in the onboards outside the top teams
	RestLive      bool     `json:"restLive,omitempty"`      // keep them up to date during the session
	TopTeams      []string `json:"topTeams,omitempty"`      // the teams whose onboards always stay
	TopTeamsSet   bool     `json:"topTeamsSet,omitempty"`   // TopTeams was chosen (also none); else McLaren, Ferrari, Mercedes, Red Bull Racing
	KeepDrivers   []string `json:"keepDrivers,omitempty"`   // drivers (three-letter code) whose onboards always stay
	KeepBy        string   `json:"keepBy,omitempty"`        // "drivers": KeepDrivers stay; else (teams) the TopTeams stay
	BattleOn      bool     `json:"battleOn,omitempty"`      // pick battle mode's drivers in Ultimate F1 Viewer
	BattleLive    bool     `json:"battleLive,omitempty"`    // (1.5) keep them up to date: now BattleEvery
	BattleLaps    int      `json:"battleLaps,omitempty"`    // laps between battle mode updates during the race (-1 = only when it opens, 0 = 5)
	BattleRight   string   `json:"battleRight,omitempty"`   // second battle mode window: "" the championship top, "p5to8" the next places in the race
	BattleMax1    int      `json:"battleMax1,omitempty"`    // at most this many drivers in the first battle mode window, 1-4 (0 = 4)
	BattleMax2    int      `json:"battleMax2,omitempty"`    // and in the next ones (0 = 4); fewer when they don't fit
	// Close F1
	CloseMVOff   bool `json:"closeMvOff,omitempty"`   // leave MultiViewer open
	CloseUf1Off  bool `json:"closeUf1Off,omitempty"`  // leave Ultimate F1 Viewer open
	CloseDesktop bool `json:"closeDesktop,omitempty"` // remove the watching desktop
	PlaceOff     bool `json:"placeOff,omitempty"`     // don't put MultiViewer and Ultimate F1 Viewer at their saved places
}

// f1DesktopName: the desktop for watching unless the settings name another (the F1 Race Control plugin's name).
const f1DesktopName = "F1"

// lastParty: the party this app was in, kept up to date while in it and cleared by Leave.
type lastParty struct {
	Code string `json:"code"`
	Host bool   `json:"host,omitempty"`
	At   int64  `json:"at"` // unix seconds, last seen in it
}

// rejoinWindow: how long after closing the app it still goes back into the party on its own.
const rejoinWindow = 3 * time.Hour

// rememberParty keeps the current party in the config (every 15 s), so a restart can rejoin it.
func rememberParty(party *Party) {
	var end partyEnd
	for range time.Tick(15 * time.Second) {
		s := party.Snapshot()
		if s.Mode == "idle" || s.Code == "" {
			end = partyEnd{}
			continue
		}
		if end.due(s.Code, mvRunning(), time.Now()) {
			endParty(party)
			if sendNotification != nil {
				_ = sendNotification("Party ended", "MultiViewer was closed, so F1 Party left the party.")
			}
			end = partyEnd{}
			continue
		}
		updateConfig(func(c *config) {
			// back as a guest after a restart: still remember that this was our party to host
			wasHost := c.Last != nil && c.Last.Code == s.Code && c.Last.Host
			c.Last = &lastParty{Code: s.Code, Host: s.Mode == "host" || wasHost, At: time.Now().Unix()}
		})
	}
}

// partyEnd: done watching means MultiViewer closed for a few minutes after it ran in this party. The party
// then ends, so the window can stay in the tray for calendar notifications without a finished party open.
type partyEnd struct {
	code string    // the party being watched
	seen bool      // MultiViewer ran during it
	gone time.Time // since when MultiViewer is closed
}

func (e *partyEnd) due(code string, mvUp bool, now time.Time) bool {
	if code != e.code {
		*e = partyEnd{code: code}
	}
	switch {
	case mvUp:
		e.seen, e.gone = true, time.Time{}
	case !e.seen:
	case e.gone.IsZero():
		e.gone = now
	case now.Sub(e.gone) >= partyEndAfter:
		return true
	}
	return false
}

// partyEndAfter: how long MultiViewer may be closed before the party ends (a restart of MultiViewer takes less).
const partyEndAfter = 3 * time.Minute

// endParty: leave on purpose, so a restart doesn't rejoin.
func endParty(party *Party) {
	party.Leave()
	taskbarRestore()
	updateConfig(func(c *config) { c.Last = nil })
}

// rejoinLast: on start-up, back into the last party if the app closed less than rejoinWindow ago.
func rejoinLast(party *Party) {
	c := loadConfig()
	if c.Last == nil || c.Last.Code == "" || time.Since(time.Unix(c.Last.At, 0)) > rejoinWindow {
		return
	}
	if party.Snapshot().Mode != "idle" {
		return
	}
	party.Rejoin(c.Last.Code, cleanName(c.Name), c.Last.Host)
}

// updateConfig changes some settings and keeps the others.
// One change at a time: several parts of the app update the settings (the party every 15 s, the calendar,
// the window), and two at once would each write back what they read and lose the other's change.
func updateConfig(f func(c *config)) {
	configMu.Lock()
	defer configMu.Unlock()
	c, err := readConfig()
	for try := 0; err != nil && try < 5; try++ { // held open for a moment: try again
		time.Sleep(100 * time.Millisecond)
		c, err = readConfig()
	}
	if err != nil {
		// never write over settings that couldn't be read: that would wipe the webhook, setup, plans...
		log.Printf("settings not saved, the file can't be read: %v", err)
		return
	}
	f(&c)
	saveConfig(c)
}

var configMu sync.Mutex

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "f1party", "config.json"), nil
}

func loadConfig() config {
	c, _ := readConfig()
	return c
}

// readConfig: the settings, or an error when the file is there but can't be read (held open, damaged).
// No file yet is no error: that is a fresh start.
func readConfig() (config, error) {
	var c config
	p, err := configPath()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func saveConfig(c config) {
	p, err := configPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return
	}
	// written next to it and then swapped in, so nobody ever reads a half-written file
	b, _ := json.Marshal(c)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) != nil {
		return
	}
	if os.Rename(tmp, p) != nil { // the file held open for a moment (a virus scanner): write it in place
		os.Remove(tmp)
		_ = os.WriteFile(p, b, 0o644)
	}
}
