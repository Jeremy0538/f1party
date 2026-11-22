package main

import (
	"os"
	"strings"
	"testing"
)

// Two users against a local relay (cd relay && npx wrangler dev --port 8787 --local --var SKIP_WEBHOOK_CHECK:1):
//
//	F1PARTY_TEST_RELAY=ws://localhost:8787/f1party/ws go test -run TestPeopleFlow
//
// Each user is its own settings folder; switching XDG_CONFIG_HOME switches who "this app" is.
func TestPeopleFlow(t *testing.T) {
	relay := os.Getenv("F1PARTY_TEST_RELAY")
	if relay == "" {
		t.Skip("set F1PARTY_TEST_RELAY to a local relay")
	}
	old := relayURL
	relayURL = relay
	defer func() { relayURL = old }()
	dirA, dirB := t.TempDir(), t.TempDir()
	as := func(dir, name string) {
		t.Setenv("XDG_CONFIG_HOME", dir)
		updateConfig(func(c *config) { c.Name = name })
	}
	reset := func() {
		peopleNow.mu.Lock()
		peopleNow.people, peopleNow.teams, peopleNow.invites, peopleNow.teamInvites = nil, nil, nil, nil
		peopleNow.mu.Unlock()
	}
	p := &Party{}

	// A starts a team and makes a friend link
	as(dirA, "Anna")
	if err := createTeam("Pit Wall", "https://discord.com/api/webhooks/1/testtesttest"); err != nil {
		t.Fatal("create team:", err)
	}
	if c := loadConfig(); len(c.Teams) != 1 || c.Teams[0].Name != "Pit Wall" {
		t.Fatalf("team not saved: %+v", c.Teams)
	}
	link, err := newFriendLink()
	if err != nil || !strings.Contains(link, "/f1party/friend/") {
		t.Fatal("friend link:", link, err)
	}

	// B opens it (pasted, the https form)
	as(dirB, "Bram")
	name, err := redeemFriendLink(friendLinkToken(link))
	if err != nil || name != "Anna" {
		t.Fatal("redeem friend link:", name, err)
	}
	reset()
	p.checkPeople()

	// A checks in: B is a friend now (from the inbox), and A owns the team
	as(dirA, "Anna")
	reset()
	p.checkPeople()
	if c := loadConfig(); len(c.Friends) != 1 || c.Friends[0].Name != "Bram" {
		t.Fatalf("A didn't get B as friend: %+v", c.Friends)
	}
	v := peopleView()
	if len(v.Teams) != 1 || !v.Teams[0].Owner {
		t.Fatalf("A should own the team: %+v", v.Teams)
	}

	// A invites B to the team (lands in B's app), B accepts
	if err := teamCall("/team/invitefriend", v.Teams[0].ID, "to", loadConfig().Friends[0].ID); err != nil {
		t.Fatal("team invite:", err)
	}
	as(dirB, "Bram")
	reset()
	p.checkPeople()
	v = peopleView()
	if len(v.TeamInvites) != 1 || v.TeamInvites[0].Team != "Pit Wall" {
		t.Fatalf("B has no team invite: %+v", v.TeamInvites)
	}
	if err := redeemGroupInvite(v.TeamInvites[0].Token); err != nil {
		t.Fatal("accept team invite:", err)
	}
	p.checkPeople()
	v = peopleView()
	if len(v.Teams) != 1 || v.Teams[0].Owner || len(v.Friends) != 1 || !v.Friends[0].Mutual {
		t.Fatalf("B: team %+v friends %+v", v.Teams, v.Friends)
	}

	// B invites A to a party in the app
	if _, err := sendPartyInvites(inviteTargets{Friends: []string{v.Friends[0].ID}}, "Bram", "QWERT", "Abu Dhabi GP · Race"); err != nil {
		t.Fatal("party invite:", err)
	}
	as(dirA, "Anna")
	reset()
	p.checkPeople()
	v = peopleView()
	if len(v.Invites) != 1 || v.Invites[0].Code != "QWERT" || v.Invites[0].Name != "Bram" {
		t.Fatalf("A has no party invite: %+v", v.Invites)
	}
	if len(v.Teams) != 1 || v.Teams[0].Members != 2 {
		t.Fatalf("team should have 2 members: %+v", v.Teams)
	}

	// A renames, B leaves
	if err := teamCall("/team/rename", v.Teams[0].ID, "teamName", "Pit Crew"); err != nil {
		t.Fatal("rename:", err)
	}
	as(dirB, "Bram")
	if err := leaveTeam(teamID(loadConfig().Teams[0].Key)); err != nil {
		t.Fatal("leave:", err)
	}
	if len(loadConfig().Teams) != 0 {
		t.Fatal("B still has the team")
	}
	as(dirA, "Anna")
	reset()
	p.checkPeople()
	if v = peopleView(); v.Teams[0].Name != "Pit Crew" || v.Teams[0].Members != 1 {
		t.Fatalf("after rename/leave: %+v", v.Teams)
	}
}
