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

// A guest's own pause, play or skip becomes a request to the host that rides on the guest's status.
func TestAskHost(t *testing.T) {
	old := &Party{st: Status{Mode: "guest"}, host: &FeedState{T: "state"}}
	old.askHost("s", "@1234.5")
	if old.req != "" {
		t.Fatalf("seek request %q sent to a host that doesn't understand it", old.req)
	}
	p := &Party{st: Status{Mode: "guest"}, host: &FeedState{T: "state", Seek: true}}
	p.askHost("s", "@1234.5")
	if p.req != "s1@1234.5" || !p.askSeek {
		t.Fatalf("seek request %q askSeek=%v", p.req, p.askSeek)
	}
	p.askHost("p", "")
	if p.req != "p2" || p.askSeek {
		t.Fatalf("pause request %q askSeek=%v", p.req, p.askSeek)
	}
	p.SetAutoSync(false)
	if !p.autoOff || p.st.AutoSync {
		t.Fatal("auto-sync still on")
	}
	p.SetAutoSync(true)
	if p.autoOff || !p.forceSync {
		t.Fatal("auto-sync back on should catch up at once")
	}
}

// The host hands the party to a guest by id; the id rides on the state until that guest takes over.
func TestMakeHost(t *testing.T) {
	p := &Party{st: Status{Mode: "host", Members: []Member{{Role: "host", Name: "Stan"}, {Role: "guest", Name: "Sam", ID: "aaaaaaaaaaaa"}}}}
	if p.MakeHost("bbbbbbbbbbbb") == nil {
		t.Fatal("handed to someone who isn't in the party")
	}
	if err := p.MakeHost("aaaaaaaaaaaa"); err != nil || p.handover != "aaaaaaaaaaaa" {
		t.Fatalf("handover %q err %v", p.handover, err)
	}
	g := &Party{st: Status{Mode: "guest"}}
	if g.MakeHost("aaaaaaaaaaaa") == nil {
		t.Fatal("a guest handed the party over")
	}
}
