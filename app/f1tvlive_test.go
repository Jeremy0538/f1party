package main

import (
	"encoding/json"
	"testing"
)

// The live session on F1 TV's home page: found by its session key, Formula 1 only, highlights skipped.
func TestF1tvFindLive(t *testing.T) {
	var page any
	_ = json.Unmarshal([]byte(`{"resultObj":{"containers":[{"retrieveItems":{"resultObj":{"containers":[
		{"metadata":{"contentId":1000079892,"contentSubtype":"RACE HIGHLIGHTS","emfAttributes":{"MeetingSessionKey":"11420","Series":"FORMULA 1"}}},
		{"metadata":{"contentId":1000099,"contentSubtype":"LIVE","emfAttributes":{"MeetingSessionKey":"11420","Series":"F1 ACADEMY"}}},
		{"metadata":{"contentId":1000010442,"contentSubtype":"LIVE","emfAttributes":{"MeetingSessionKey":"11420","Series":"FORMULA 1"}}}
	]}}}]}}`), &page)
	if id := f1tvFindLive(page, "11420"); id != "1000010442" {
		t.Fatalf("live id %q", id)
	}
	if id := f1tvFindLive(page, "11416"); id != "" {
		t.Fatalf("other session gave %q", id)
	}
}
