package main

import (
	"os"
	"testing"
	"time"
)

// The push line, against a local relay (see TestPeopleFlow):
//
//	F1PARTY_TEST_RELAY=ws://localhost:8787/f1party/ws go test -run TestPushLine
//
// B keeps a push line open; what A does for B (a friend request, accepting B's) pokes B within a second.
func TestPushLine(t *testing.T) {
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
	p := &Party{}
	drain := func() {
		for {
			select {
			case <-peopleNow.poke:
			default:
				return
			}
		}
	}
	poked := func(what string) {
		t.Helper()
		select {
		case <-peopleNow.poke:
		case <-time.After(2 * time.Second):
			t.Fatal("no poke within 2 s: " + what)
		}
	}

	// A starts a team, B joins it
	as(dirA, "Anna")
	idA := persistentID()
	if err := createTeam("Pit Wall", "https://discord.com/api/webhooks/1/testtesttest"); err != nil {
		t.Fatal(err)
	}
	p.checkPeople()
	link, err := teamLink(peopleView().Teams[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	as(dirB, "Bram")
	idB := persistentID()
	if err := redeemGroupInvite(teamLinkToken(link)); err != nil {
		t.Fatal(err)
	}
	p.checkPeople()

	// B's push line (its id and secret are read when it connects)
	go pushOnce()
	poked("connecting catches up")
	time.Sleep(200 * time.Millisecond)
	drain()

	as(dirA, "Anna")
	if _, err := askTeammate(idB); err != nil {
		t.Fatal(err)
	}
	poked("A's friend request")

	// B accepts: A's line gets the news (A's own line this time)
	as(dirA, "Anna")
	go pushOnce()
	time.Sleep(300 * time.Millisecond)
	drain()
	as(dirB, "Bram")
	if _, err := answerFriendRequest(idA, true); err != nil {
		t.Fatal(err)
	}
	poked("B accepted")

	// a team change: A renames the team, B (and A's other lines) hear it
	drain()
	as(dirA, "Anna")
	p.checkPeople()
	if err := teamCall("/team/rename", peopleView().Teams[0].ID, "teamName", "Pit Lane"); err != nil {
		t.Fatal(err)
	}
	poked("team renamed")
}
