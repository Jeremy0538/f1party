package main

import "testing"

func TestWelcomeFor(t *testing.T) {
	old := version
	defer func() { version = old }()
	version = "1.7.0"
	if w := welcomeFor(config{}); w == nil || !w.Setup {
		t.Fatal("first start should get the setup guide")
	}
	if w := welcomeFor(config{Name: "Jeremy"}); w == nil || w.Setup || len(w.News) == 0 {
		t.Fatal("an existing user updating should get this version's news")
	}
	if w := welcomeFor(config{Name: "Jeremy", SeenVersion: "1.7.0"}); w != nil {
		t.Fatal("seen already")
	}
	version = "1.9.0"
	if w := welcomeFor(config{Name: "Jeremy", SeenVersion: "1.8.0"}); w != nil {
		t.Fatal("no 1.9.0 news: nothing to show")
	}
	if w := welcomeFor(config{SetupDone: true, SeenVersion: "1.7.0"}); w == nil || len(w.News) == 0 {
		t.Fatal("1.7.0 -> 1.9.0 should show 1.8.0's news")
	}
}
