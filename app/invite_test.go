package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The Discord invite: who hosts, what is on, the code and a join link, posted as "F1 Party" without pings.
func TestSendInvite(t *testing.T) {
	var body map[string]any
	var url string
	old := discordHTTP
	defer func() { discordHTTP = old }()
	discordHTTP = &http.Client{Transport: rt(func(r *http.Request) (*http.Response, error) {
		url = r.URL.String()
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
	})}
	hook := "https://discord.com/api/webhooks/123/abcdefghijkl"
	if err := sendInvite(hook, "Ann", "RUN3B", "2026 Las Vegas GP · Race · replay"); err != nil {
		t.Fatal(err)
	}
	text, _ := body["content"].(string)
	for _, want := range []string{"**Ann** is hosting", "2026 Las Vegas GP · Race · replay", "**RUN3B**", "Join the party"} {
		if !strings.Contains(text, want) {
			t.Errorf("invite misses %q:\n%s", want, text)
		}
	}
	if body["username"] != "F1 Party" || !strings.HasSuffix(url, "?wait=true") {
		t.Errorf("username %v, url %s", body["username"], url)
	}
	if m, _ := body["allowed_mentions"].(map[string]any); m == nil {
		t.Error("pings not switched off")
	}
	// without a session open: no empty line for it
	sendInvite(hook, "Ann", "RUN3B", "")
	if text, _ := body["content"].(string); strings.Contains(text, "📺") {
		t.Errorf("empty session line:\n%s", text)
	}
	if err := sendInvite("", "Ann", "RUN3B", ""); err == nil {
		t.Error("no error without a webhook")
	}
}

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
