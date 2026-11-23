package main

import (
	"io"
	"net/http"
	"os"
	"testing"
)

// Profile pictures and team logos against a local relay (see people_integration_test.go):
//
//	F1PARTY_TEST_RELAY=ws://localhost:8787/f1party/ws go test -run TestPics
func TestPics(t *testing.T) {
	relay := os.Getenv("F1PARTY_TEST_RELAY")
	if relay == "" {
		t.Skip("set F1PARTY_TEST_RELAY to a local relay")
	}
	old := relayURL
	relayURL = relay
	defer func() { relayURL = old }()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir) // Windows
	updateConfig(func(c *config) { c.Name = "Pia" })

	// a 1x1 PNG
	png := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	if err := setMyPic(png); err != nil {
		t.Fatal("set picture:", err)
	}
	if loadConfig().Pic != png {
		t.Fatal("picture not kept in the settings")
	}
	// the relay hands it out by id
	res, err := http.Get(relayBase() + "/pic/u/" + persistentID() + "?v=" + picVersion(png))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" || len(b) < 60 {
		t.Fatalf("relay picture: %d %s %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(b))
	}
	// a bad picture is refused, by the app and by the relay
	if err := setMyPic("data:text/html;base64,PGI+"); err == nil {
		t.Fatal("a non-image was taken")
	}
	var out any
	if err := relayJSON("/me/pic", with(me(), "pic", "data:image/svg+xml;base64,PHN2Zz4="), &out); err == nil {
		t.Fatal("the relay took an SVG")
	}

	// a team's logo: the owner sets it, the check-in reports its version
	if err := createTeam("Logo Team", "https://discord.com/api/webhooks/1/testtesttest"); err != nil {
		t.Fatal("create team:", err)
	}
	id := teamID(loadConfig().Teams[0].Key)
	if err := setTeamLogo(id, png); err != nil {
		t.Fatal("team logo:", err)
	}
	p := &Party{}
	p.checkPeople()
	v := peopleView()
	if len(v.Teams) != 1 || v.Teams[0].Logo != picVersion(png) {
		t.Fatalf("team logo version not reported: %+v", v.Teams)
	}
	res, err = http.Get(relayBase() + "/pic/t/" + id)
	if err != nil || res.StatusCode != 200 {
		t.Fatal("relay team logo:", err, res.StatusCode)
	}
	res.Body.Close()

	// removing the picture
	if err := setMyPic(""); err != nil {
		t.Fatal("remove picture:", err)
	}
	res, _ = http.Get(relayBase() + "/pic/u/" + persistentID())
	if res.StatusCode != 404 {
		t.Fatal("removed picture still there:", res.StatusCode)
	}
	res.Body.Close()
}
