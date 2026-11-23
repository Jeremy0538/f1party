package main

import (
	"os"
	"testing"
)

// Friend requests between teammates, against a local relay (see TestPeopleFlow):
//
//	F1PARTY_TEST_RELAY=ws://localhost:8787/f1party/ws go test -run TestTeammateFriendRequests
func TestTeammateFriendRequests(t *testing.T) {
	relay := os.Getenv("F1PARTY_TEST_RELAY")
	if relay == "" {
		t.Skip("set F1PARTY_TEST_RELAY to a local relay")
	}
	old := relayURL
	relayURL = relay
	defer func() { relayURL = old }()
	dirA, dirB, dirC := t.TempDir(), t.TempDir(), t.TempDir()
	as := func(dir, name string) {
		t.Setenv("XDG_CONFIG_HOME", dir)
		updateConfig(func(c *config) { c.Name = name })
		peopleNow.mu.Lock()
		peopleNow.people, peopleNow.teams, peopleNow.requests, peopleNow.requestSeen = nil, nil, nil, nil
		peopleNow.mu.Unlock()
	}
	p := &Party{}
	idOf := func(dir, name string) string { as(dir, name); return persistentID() }
	idA, idB, idC := idOf(dirA, "Anna"), idOf(dirB, "Bram"), idOf(dirC, "Cas")

	// A starts a team, B joins it with the team link; C is in no team
	as(dirA, "Anna")
	if err := createTeam("Pit Wall", "https://discord.com/api/webhooks/1/testtesttest"); err != nil {
		t.Fatal("create team:", err)
	}
	p.checkPeople()
	link, err := teamLink(peopleView().Teams[0].ID)
	if err != nil {
		t.Fatal("team link:", err)
	}
	as(dirB, "Bram")
	if err := redeemGroupInvite(teamLinkToken(link)); err != nil {
		t.Fatal("join team:", err)
	}
	p.checkPeople()
	as(dirC, "Cas")
	p.checkPeople()

	// C can't ask A (no shared team); A asks B
	if _, err := askTeammate(idA); err == nil {
		t.Fatal("a stranger could send a friend request")
	}
	as(dirA, "Anna")
	p.checkPeople()
	if a, err := askTeammate(idB); err != nil || a.Friends {
		t.Fatal("ask:", a, err)
	}
	p.checkPeople()
	asked := false
	for _, x := range peopleView().Teams[0].People {
		asked = asked || (x.ID == idB && x.Asked)
	}
	if !asked {
		t.Fatal("A doesn't see the request as sent")
	}

	// B sees it, declines; A asks again, B accepts: friends on both sides
	as(dirB, "Bram")
	p.checkPeople()
	if v := peopleView(); len(v.Requests) != 1 || v.Requests[0].ID != idA || v.Requests[0].Name != "Anna" {
		t.Fatalf("B has no request: %+v", v.Requests)
	}
	if _, err := answerFriendRequest(idA, false); err != nil {
		t.Fatal("decline:", err)
	}
	p.checkPeople()
	if v := peopleView(); len(v.Requests) != 0 {
		t.Fatalf("declined request still there: %+v", v.Requests)
	}
	if _, err := answerFriendRequest(idA, true); err == nil {
		t.Fatal("accepted a declined request")
	}
	as(dirA, "Anna")
	if _, err := askTeammate(idB); err != nil {
		t.Fatal("ask again:", err)
	}
	as(dirB, "Bram")
	p.checkPeople()
	if a, err := answerFriendRequest(idA, true); err != nil || a.Already {
		t.Fatal("accept:", a, err)
	}
	if !isFriend(loadConfig().Friends, idA) {
		t.Fatal("B doesn't have A as friend")
	}
	as(dirA, "Anna")
	p.checkPeople()
	if !isFriend(loadConfig().Friends, idB) {
		t.Fatal("A doesn't have B as friend")
	}

	// asking a friend again says you already are
	if a, err := askTeammate(idB); err != nil || !a.Friends || !a.Already {
		t.Fatal("ask a friend:", a, err)
	}

	// A removes B: B loses A too
	RemoveFriend(idB)
	unfriendOnRelay(idB)
	p.checkPeople()
	as(dirB, "Bram")
	p.checkPeople()
	if isFriend(loadConfig().Friends, idA) {
		t.Fatal("B still has A as friend after A removed B")
	}
	p.checkPeople()
	as(dirA, "Anna")
	p.checkPeople()
	if isFriend(loadConfig().Friends, idB) {
		t.Fatal("B came back as A's friend")
	}
	_ = idC
}
