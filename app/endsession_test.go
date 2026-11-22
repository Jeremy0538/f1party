package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// "End session" as a guest: the party stays, this guest stops watching the host's session until the host
// opens another one or Watch again. The host's end travels to guests in the state message.
func TestEndSession(t *testing.T) {
	closed := 0
	old := closeSession
	closeSession = func() { closed++ } // never the real MultiViewer
	defer func() { closeSession = old }()

	p := &Party{st: Status{Mode: "guest"}, host: &FeedState{ContentID: "1000001"}}
	if err := p.EndSession(); err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatalf("streams closed %d times, want 1", closed)
	}
	if v := p.Session(); v == nil || !v.Stopped {
		t.Fatalf("guest after End session: %+v, want stopped", v)
	}
	p.host.ContentID = "1000002" // the host opened another session: watching again by itself
	if v := p.Session(); v != nil && v.Stopped {
		t.Fatal("still stopped after the host switched sessions")
	}
	p.host.ContentID = "1000001"
	p.WatchAgain()
	if v := p.Session(); v != nil && v.Stopped {
		t.Fatal("still stopped after Watch again")
	}

	none := &Party{st: Status{Mode: "guest"}}
	if none.EndSession() == nil {
		t.Fatal("End session without a host session should say so")
	}

	b, _ := json.Marshal(FeedState{T: "state", None: true, Ended: true})
	if !strings.Contains(string(b), `"ended":true`) {
		t.Fatalf("host's end missing in %s", b)
	}
}
