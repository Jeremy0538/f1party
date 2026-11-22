package main

import (
	"strings"
	"testing"
)

// Two apps in one party, each asking the other: both end up friends. Only one asking: a pending request.
func TestFriendRequests(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())         // Windows settings folder
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // Linux
	t.Setenv("HOME", t.TempDir())            // macOS

	me := "aaaaaaaaaaaa"
	other := "bbbbbbbbbbbb"
	p := &Party{id: me, st: Status{Mode: "host", Members: []Member{
		{Role: "host", Name: "Ann", ID: me},
		{Role: "guest", Name: "Bob", ID: other},
	}}}

	// nobody asked yet: an add button
	s := p.Snapshot()
	if g := s.Members[1]; g.Friend || g.Sent || g.Asked {
		t.Fatalf("fresh member: %+v", g)
	}
	if !s.Members[0].Me {
		t.Fatal("own row not marked as me")
	}

	// they ask us (their status carries our key): "Accept"
	p.st.Members[1].Offers = []string{offerKey(me)}
	if g := p.Snapshot().Members[1]; !g.Asked {
		t.Fatalf("their request not seen: %+v", g)
	}

	// we accept: friends, saved in the settings
	p.AskFriend(other)
	if !isFriend(loadFriends(), other) {
		t.Fatal("not friends after both asked")
	}
	if g := p.Snapshot().Members[1]; !g.Friend || g.Asked || g.Sent {
		t.Fatalf("friend row: %+v", g)
	}

	// what we send: our request for them, short enough for the relay
	if w := p.offerWire(3); w != offerKey(other) {
		t.Fatalf("wire = %q", w)
	}
	status := strings.Repeat("x", 30) + "…|p3|" + p.offerWire(3)
	if len([]rune(status)) > 64 {
		t.Fatalf("status too long for the relay: %d", len([]rune(status)))
	}

	RemoveFriend(other)
	if isFriend(loadFriends(), other) {
		t.Fatal("still a friend after remove")
	}
}
