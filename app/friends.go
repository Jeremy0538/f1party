package main

// Friends: people you watched with and added. Someone in your party who isn't a friend yet gets an "add friend"
// button; that sends a request, and the other one sees "Accept". Both sides asking each other makes you
// friends, on both computers. The requests ride on the party's own messages (a guest's status, the host's
// state), so the relay needs no change. Each app has a fixed id (saved in its settings) to know a friend by,
// whatever name they use.

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"
)

type Friend struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Since int64  `json:"since"` // unix seconds
	Seen  int64  `json:"seen,omitempty"`
}

// offerLen: requests carry the first characters of the other one's id (the status the relay passes on is short).
const offerLen = 8

func offerKey(id string) string {
	if len(id) > offerLen {
		return id[:offerLen]
	}
	return id
}

var friendsMu sync.Mutex

func loadFriends() []Friend {
	friendsMu.Lock()
	defer friendsMu.Unlock()
	return loadConfig().Friends
}

func isFriend(fs []Friend, id string) bool {
	return id != "" && slices.ContainsFunc(fs, func(f Friend) bool { return f.ID == id })
}

func addFriend(id, name string) {
	updateConfig(func(c *config) {
		if !isFriend(c.Friends, id) {
			now := time.Now().Unix()
			c.Friends = append(c.Friends, Friend{ID: id, Name: name, Since: now, Seen: now})
		}
	})
}

func RemoveFriend(id string) {
	updateConfig(func(c *config) {
		c.Friends = slices.DeleteFunc(c.Friends, func(f Friend) bool { return f.ID == id })
	})
}

// persistentID: this app's member id, made once and kept in the settings.
func persistentID() string {
	var id string
	updateConfig(func(c *config) {
		if len(c.MemberID) < 12 {
			var b [6]byte
			_, _ = cryptorand.Read(b[:])
			c.MemberID = hex.EncodeToString(b[:]) // 12 characters
		}
		id = c.MemberID
	})
	return id
}

// AskFriend: send a friend request to someone in the party (or accept theirs).
func (p *Party) AskFriend(id string) {
	p.mu.Lock()
	if p.offers == nil {
		p.offers = map[string]bool{}
	}
	p.offers[id] = true
	p.mu.Unlock()
	p.checkFriends()
}

// DeclineFriend: turn down someone's friend request (for this party; they can't ask again until the next one).
func (p *Party) DeclineFriend(id string) {
	p.mu.Lock()
	if p.declined == nil {
		p.declined = map[string]bool{}
	}
	p.declined[id] = true
	p.mu.Unlock()
}

// offerWire: the requests this app has out, for the party messages ("ab12cd34,ef56…"; a guest sends at most 3).
func (p *Party) offerWire(max int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var keys []string
	for id := range p.offers {
		keys = append(keys, offerKey(id))
	}
	slices.Sort(keys)
	if len(keys) > max {
		keys = keys[:max]
	}
	return strings.Join(keys, ",")
}

// offersOf: who a member asked (their status for a guest, the state for the host).
func (p *Party) offersOf(m Member) []string {
	if m.Role == "host" && p.st.Mode == "guest" && p.host != nil {
		return p.host.Friends
	}
	return m.Offers
}

// checkFriends: a member asked us and we asked them → friends. A new request from someone shows a notification
// once. Runs with the status (every few seconds is plenty).
func (p *Party) checkFriends() {
	fs := loadFriends()
	p.mu.Lock()
	me := offerKey(p.id)
	type pair struct{ id, name string }
	var made, asked []pair
	for _, m := range p.st.Members {
		if m.ID == "" || m.ID == p.id || isFriend(fs, m.ID) {
			continue
		}
		if !slices.Contains(p.offersOf(m), me) {
			continue
		}
		if p.offers[m.ID] {
			made = append(made, pair{m.ID, m.Name})
		} else if !p.askedSeen[m.ID] {
			if p.askedSeen == nil {
				p.askedSeen = map[string]bool{}
			}
			p.askedSeen[m.ID] = true
			asked = append(asked, pair{m.ID, m.Name})
		}
	}
	p.mu.Unlock()
	for _, f := range made {
		addFriend(f.id, f.name)
		p.SetNote("You and " + f.name + " are friends now")
	}
	for _, f := range asked {
		if sendNotification != nil {
			_ = sendNotification(f.name+" wants to be friends", "Open F1 Party and click Accept next to their name.")
		}
	}
}

// markFriends fills in the friend fields of the members (for the window), and remembers when friends were seen.
func (p *Party) markFriends(s *Status) {
	fs := loadFriends()
	me := offerKey(p.id)
	seen := false
	for i := range s.Members {
		m := &s.Members[i]
		m.Me = m.ID != "" && m.ID == p.id
		if m.ID == "" || m.Me {
			continue
		}
		m.Friend = isFriend(fs, m.ID)
		m.Sent = p.offers[m.ID] && !m.Friend
		m.Asked = !m.Friend && !m.Sent && !p.declined[m.ID] && slices.Contains(p.offersOf(*m), me)
		if m.Friend {
			seen = true
		}
	}
	if seen && time.Since(p.friendsSeenAt) > time.Minute {
		p.friendsSeenAt = time.Now()
		ids := map[string]string{}
		for _, m := range s.Members {
			if m.Friend {
				ids[m.ID] = m.Name
			}
		}
		go updateConfig(func(c *config) {
			for i := range c.Friends {
				if name, ok := ids[c.Friends[i].ID]; ok {
					c.Friends[i].Seen, c.Friends[i].Name = time.Now().Unix(), name
				}
			}
		})
	}
}
