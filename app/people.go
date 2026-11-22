package main

// People: friends and teams through the relay. Every 30 s (and right after the party changes) the app tells the
// relay who it is (its fixed id plus a secret only this app knows), what it is doing, who its friends are and
// which teams it is in. The relay answers with what its mutual friends and teammates are doing, its teams and
// anything waiting for it: a new friend from a friend link, a party invite, a team invite. Strangers never see
// what you watch.

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

// TeamRef: a team this app is in. The key proves membership; the name is what the relay last said.
type TeamRef struct {
	Key  string `json:"key"`
	Name string `json:"name,omitempty"`
}

// teamID: the team's id is part of its key (f1p_<id8><secret24>).
func teamID(key string) string {
	if len(key) < len(groupKeyPrefix)+8 {
		return ""
	}
	return key[len(groupKeyPrefix) : len(groupKeyPrefix)+8]
}

// inviteTargets: where "Invite" sent the party last time; the Stream Deck key and planned parties reuse it.
type inviteTargets struct {
	Teams   []string `json:"teams,omitempty"` // team ids
	Friends []string `json:"friends,omitempty"`
	Own     bool     `json:"own,omitempty"` // the own Discord webhook
}

// PersonStatus: what someone is doing right now (nil when idle or offline).
type PersonStatus struct {
	Mode  string `json:"mode"` // host | guest
	Code  string `json:"code"`
	Title string `json:"title,omitempty"`
	Live  bool   `json:"live,omitempty"`
}

type Person struct {
	ID     string        `json:"id"`
	Name   string        `json:"name"`
	Seen   int64         `json:"seen"` // unix ms, last time their app checked in
	Status *PersonStatus `json:"status"`
	Friend bool          `json:"friend"`
	Teams  []string      `json:"teams"`
}

type TeamInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Owner   bool   `json:"owner"`
	Members int    `json:"members"`
}

// PartyInvite: someone invited this app to their party (in the app, not through Discord).
type PartyInvite struct {
	From  string `json:"from"`
	Name  string `json:"name"`
	Code  string `json:"code"`
	Title string `json:"title,omitempty"`
	At    int64  `json:"at"`
}

// TeamInvite: a friend invited this app to a team; accepting uses the one-time team link inside.
type TeamInvite struct {
	Token string `json:"token"`
	Team  string `json:"team"`
	Name  string `json:"name"`
	At    int64  `json:"at"`
}

type peopleState struct {
	mu          sync.Mutex
	people      []Person
	teams       []TeamInfo
	invites     []PartyInvite
	teamInvites []TeamInvite
	online      bool  // the last check-in worked
	checked     int64 // unix ms of the last answer
	poke        chan struct{}
}

var peopleNow = &peopleState{poke: make(chan struct{}, 1)}

// pokePeople: check in now instead of at the next 30 s tick (after joining, leaving, a new team...).
func pokePeople() {
	select {
	case peopleNow.poke <- struct{}{}:
	default:
	}
}

// presenceSecret: made once and kept in the settings; it proves to the relay that this app owns its id.
func presenceSecret() string {
	var s string
	updateConfig(func(c *config) {
		if len(c.Secret) < 20 {
			var b [18]byte
			_, _ = cryptorand.Read(b[:])
			c.Secret = hex.EncodeToString(b[:])
		}
		s = c.Secret
	})
	return s
}

// me: the fields every signed-in relay call carries.
func me() map[string]any {
	return map[string]any{"id": persistentID(), "secret": presenceSecret(), "name": cleanName(loadConfig().Name)}
}

func with(m map[string]any, kv ...any) map[string]any {
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

// migrateTeams: a team key kept as the Discord setting (1.1.0 to 1.3.0) moves to the team list.
func migrateTeams() {
	updateConfig(func(c *config) {
		if isGroupKey(c.Webhook) {
			if !slices.ContainsFunc(c.Teams, func(t TeamRef) bool { return t.Key == c.Webhook }) {
				c.Teams = append(c.Teams, TeamRef{Key: c.Webhook, Name: c.TeamName})
			}
			c.Webhook, c.TeamName = "", ""
		}
	})
}

func addTeam(key, name string) {
	updateConfig(func(c *config) {
		for i := range c.Teams {
			if c.Teams[i].Key == key {
				c.Teams[i].Name = name
				return
			}
		}
		c.Teams = append(c.Teams, TeamRef{Key: key, Name: name})
	})
	pokePeople()
}

func teamKeyByID(id string) string {
	for _, t := range loadConfig().Teams {
		if teamID(t.Key) == id {
			return t.Key
		}
	}
	return ""
}

// myStatus: what this app reports: the party it's in and what it shows.
func myStatus(p *Party) map[string]any {
	s := p.Snapshot()
	if s.Mode == "idle" || s.Code == "" {
		return nil
	}
	st := map[string]any{"mode": s.Mode, "code": s.Code}
	if s.Feed != nil {
		st["title"], st["live"] = s.Feed.Title, s.Feed.Live
	}
	return st
}

// watchPeople: the check-in loop. Runs for the whole life of the app.
func (p *Party) watchPeople() {
	last := ""
	tick := time.NewTicker(30 * time.Second)
	statusTick := time.NewTicker(3 * time.Second)
	check := p.checkPeople
	check()
	for {
		select {
		case <-tick.C:
		case <-peopleNow.poke:
		case <-statusTick.C:
			// what we show changed (joined, left, another session): tell friends now, not in 30 s
			s := myStatus(p)
			key := ""
			if s != nil {
				key = s["code"].(string) + "|" + anyString(s["title"]) + "|" + s["mode"].(string)
			}
			if key == last {
				continue
			}
			last = key
		}
		check()
	}
}

// checkPeople: one check-in with the relay (see watchPeople).
func (p *Party) checkPeople() {
	c := loadConfig()
	var friends, teams []string
	for _, f := range c.Friends {
		friends = append(friends, f.ID)
	}
	for _, t := range c.Teams {
		teams = append(teams, t.Key)
	}
	var r struct {
		People []Person
		Teams  []TeamInfo
		Inbox  []map[string]any
		Lost   []string
	}
	err := relayJSON("/people", with(me(), "status", myStatus(p), "friends", friends, "teams", teams), &r)
	peopleNow.mu.Lock()
	peopleNow.online = err == nil
	if err == nil {
		peopleNow.people, peopleNow.teams, peopleNow.checked = r.People, r.Teams, time.Now().UnixMilli()
	}
	peopleNow.mu.Unlock()
	if err != nil {
		return
	}
	// names the relay knows (renamed teams) and keys that stopped working
	updateConfig(func(c *config) {
		c.Teams = slices.DeleteFunc(c.Teams, func(t TeamRef) bool { return slices.Contains(r.Lost, t.Key) })
		for i := range c.Teams {
			for _, ti := range r.Teams {
				if ti.ID == teamID(c.Teams[i].Key) {
					c.Teams[i].Name = ti.Name
				}
			}
		}
	})
	for _, ev := range r.Inbox {
		p.handleInbox(ev)
	}
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}

func (p *Party) handleInbox(ev map[string]any) {
	str := func(k string) string { return anyString(ev[k]) }
	at := int64(0)
	if f, ok := ev["at"].(float64); ok {
		at = int64(f)
	}
	switch str("type") {
	case "friend": // someone used our friend link: friends on both sides
		if id, name := str("id"), str("name"); id != "" {
			addFriend(id, name)
			p.SetNote("You and " + name + " are friends now")
			if sendNotification != nil {
				_ = sendNotification(name+" is your friend now", "They used your friend link in F1 Party.")
			}
		}
	case "party":
		inv := PartyInvite{From: str("from"), Name: str("name"), Code: str("code"), Title: str("title"), At: at}
		peopleNow.mu.Lock()
		peopleNow.invites = append(slices.DeleteFunc(peopleNow.invites, func(i PartyInvite) bool { return i.From == inv.From }), inv)
		peopleNow.mu.Unlock()
		if sendNotification != nil {
			what := "their F1 Party"
			if inv.Title != "" {
				what = inv.Title
			}
			_ = sendNotification(inv.Name+" invites you", "Join "+what+": open F1 Party and click Join.")
		}
	case "team":
		inv := TeamInvite{Token: str("token"), Team: str("team"), Name: str("name"), At: at}
		peopleNow.mu.Lock()
		peopleNow.teamInvites = append(peopleNow.teamInvites, inv)
		peopleNow.mu.Unlock()
		if sendNotification != nil {
			_ = sendNotification(inv.Name+" invites you to team "+inv.Team, "Open F1 Party and click Accept under Friends.")
		}
	}
}

// peopleReq: the fields the Friends tab's actions send (each uses a few).
type peopleReq struct {
	ID, Name, Webhook, To, Token, From, Link string
	Teams, Friends                           []string
	Own                                      bool
}

// PeopleView: what the Friends tab shows.
type PeopleView struct {
	Online      bool          `json:"online"`
	Friends     []FriendView  `json:"friends"`
	Teams       []TeamView    `json:"teams"`
	Invites     []PartyInvite `json:"invites"`
	TeamInvites []TeamInvite  `json:"teamInvites"`
	OwnWebhook  bool          `json:"ownWebhook"`
}

type FriendView struct {
	Friend
	Mutual bool          `json:"mutual"` // the relay knows them as a friend both ways
	Status *PersonStatus `json:"status"`
	Online bool          `json:"online"`
}

type TeamView struct {
	TeamInfo
	People []Person `json:"people"`
}

func peopleView() PeopleView {
	c := loadConfig()
	peopleNow.mu.Lock()
	defer peopleNow.mu.Unlock()
	now := time.Now().UnixMilli()
	byID := map[string]Person{}
	for _, x := range peopleNow.people {
		byID[x.ID] = x
	}
	v := PeopleView{Online: peopleNow.online, OwnWebhook: validWebhook(c.Webhook), Invites: []PartyInvite{}, TeamInvites: []TeamInvite{}}
	for _, f := range c.Friends {
		fv := FriendView{Friend: f}
		if x, ok := byID[f.ID]; ok {
			fv.Mutual, fv.Status, fv.Online = x.Friend, x.Status, now-x.Seen < 90_000
		}
		v.Friends = append(v.Friends, fv)
	}
	for _, t := range c.Teams {
		tv := TeamView{TeamInfo: TeamInfo{ID: teamID(t.Key), Name: t.Name, Members: 1}}
		for _, ti := range peopleNow.teams {
			if ti.ID == tv.ID {
				tv.TeamInfo = ti
			}
		}
		for _, x := range peopleNow.people {
			if slices.Contains(x.Teams, tv.ID) {
				tv.People = append(tv.People, x)
			}
		}
		v.Teams = append(v.Teams, tv)
	}
	// party invites stay for two hours
	peopleNow.invites = slices.DeleteFunc(peopleNow.invites, func(i PartyInvite) bool { return now-i.At > 2*3600_000 })
	v.Invites = append(v.Invites, peopleNow.invites...)
	v.TeamInvites = append(v.TeamInvites, peopleNow.teamInvites...)
	return v
}

func dismissInvite(from string) {
	peopleNow.mu.Lock()
	peopleNow.invites = slices.DeleteFunc(peopleNow.invites, func(i PartyInvite) bool { return i.From == from })
	peopleNow.mu.Unlock()
}

func dropTeamInvite(token string) {
	peopleNow.mu.Lock()
	peopleNow.teamInvites = slices.DeleteFunc(peopleNow.teamInvites, func(i TeamInvite) bool { return i.Token == token })
	peopleNow.mu.Unlock()
}

// newFriendLink: a link that makes whoever opens it your friend (and gets them F1 Party).
func newFriendLink() (string, error) {
	var r struct{ URL string }
	if err := relayJSON("/friend/link", me(), &r); err != nil {
		return "", err
	}
	return r.URL, nil
}

// redeemFriendLink: someone's friend link was opened here: friends on both sides.
func redeemFriendLink(token string) (string, error) {
	var r struct{ ID, Name string }
	if err := relayJSON("/friend/redeem", with(me(), "token", token), &r); err != nil {
		return "", err
	}
	addFriend(r.ID, r.Name)
	pokePeople()
	return r.Name, nil
}

// friendLinkToken: the token in a friend link (https://…/f1party/friend/TOKEN or f1party://friend/TOKEN), or "".
func friendLinkToken(s string) string {
	if rest, ok := strings.CutPrefix(s, "f1party://friend/"); ok {
		return groupToken("f1party://group/" + rest)
	}
	rest, ok := strings.CutPrefix(s, "https://")
	if !ok {
		rest, ok = strings.CutPrefix(s, "http://")
	}
	if ok {
		if i := strings.Index(rest, "/f1party/friend/"); i > 0 && !strings.ContainsAny(rest[:i], "/?#") {
			return groupToken("f1party://group/" + rest[i+len("/f1party/friend/"):])
		}
	}
	return ""
}

// joinTeamByKey: a pasted team key; the relay says which team it is.
func joinTeamByKey(key string) error {
	var r struct{ Name string }
	if err := relayJSON("/team/info", map[string]string{"key": key}, &r); err != nil {
		return err
	}
	addTeam(key, r.Name)
	return nil
}

func teamCall(path, id string, extra ...any) error {
	key := teamKeyByID(id)
	if key == "" {
		return errors.New("you're not in that team")
	}
	var out any
	err := relayJSON(path, with(me(), append([]any{"key", key}, extra...)...), &out)
	pokePeople()
	return err
}

func leaveTeam(id string) error {
	key := teamKeyByID(id)
	err := teamCall("/team/leave", id)
	updateConfig(func(c *config) {
		c.Teams = slices.DeleteFunc(c.Teams, func(t TeamRef) bool { return t.Key == key })
	})
	if err != nil && strings.Contains(err.Error(), "Unknown team key") {
		return nil // the team is gone anyway
	}
	return err
}

func teamLink(id string) (string, error) {
	key := teamKeyByID(id)
	if key == "" {
		return "", errors.New("you're not in that team")
	}
	var r struct{ URL string }
	if err := relayJSON("/group/invite", map[string]string{"key": key}, &r); err != nil {
		return "", err
	}
	return r.URL, nil
}

// sendPartyInvites: the party code to Discord (teams, own webhook) and to friends in their app.
func sendPartyInvites(t inviteTargets, name, code, watching string) (int, error) {
	c := loadConfig()
	var errs []string
	sent := 0
	for _, id := range t.Teams {
		key := teamKeyByID(id)
		if key == "" {
			continue
		}
		if err := sendInvite(key, name, code, watching); err != nil {
			errs = append(errs, err.Error())
		} else {
			sent++
		}
	}
	if t.Own && validWebhook(c.Webhook) {
		if err := sendInvite(c.Webhook, name, code, watching); err != nil {
			errs = append(errs, err.Error())
		} else {
			sent++
		}
	}
	if len(t.Friends) > 0 {
		var r struct{ Sent int }
		if err := relayJSON("/invite/send", with(me(), "to", t.Friends, "code", code, "title", watching), &r); err != nil {
			errs = append(errs, err.Error())
		} else {
			sent += r.Sent
		}
	}
	if len(errs) > 0 {
		return sent, errors.New(strings.Join(errs, " · "))
	}
	return sent, nil
}

// defaultTargets: the last choice, else every team and the own webhook.
func defaultTargets() inviteTargets {
	c := loadConfig()
	if c.InviteTo != nil {
		return *c.InviteTo
	}
	var t inviteTargets
	for _, tr := range c.Teams {
		t.Teams = append(t.Teams, teamID(tr.Key))
	}
	t.Own = validWebhook(c.Webhook)
	return t
}

// discordDests: every Discord destination (teams and the own webhook), for planned parties.
func discordDests() []string {
	c := loadConfig()
	var d []string
	for _, t := range c.Teams {
		d = append(d, t.Key)
	}
	if validWebhook(c.Webhook) {
		d = append(d, c.Webhook)
	}
	return d
}

// toAllDests runs f for every Discord destination; an error only when none of them worked.
func toAllDests(f func(dest string) error) error {
	var last error
	ok := false
	for _, d := range discordDests() {
		if err := f(d); err != nil {
			last = err
		} else {
			ok = true
		}
	}
	if ok {
		return nil
	}
	return last
}
