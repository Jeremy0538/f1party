package main

import (
	"testing"
	"time"
)

// The party ends only after MultiViewer ran in it and then stayed closed for partyEndAfter.
func TestPartyEnd(t *testing.T) {
	t0 := time.Now()
	var e partyEnd
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	if e.due("A", false, at(0)) || e.due("A", false, at(time.Hour)) {
		t.Fatal("ended before MultiViewer ever ran")
	}
	e.due("A", true, at(time.Hour))
	if e.due("A", false, at(time.Hour+time.Minute)) || e.due("A", false, at(time.Hour+2*time.Minute)) {
		t.Fatal("ended too soon")
	}
	e.due("A", true, at(time.Hour+2*time.Minute)) // MultiViewer restarted: the count starts over
	e.due("A", false, at(time.Hour+3*time.Minute))
	if e.due("A", false, at(time.Hour+5*time.Minute)) {
		t.Fatal("restart didn't reset the count")
	}
	if !e.due("A", false, at(time.Hour+6*time.Minute)) {
		t.Fatal("not ended after MultiViewer stayed closed")
	}
	if e.due("B", false, at(2*time.Hour)) {
		t.Fatal("a new party carried over the old one's state")
	}
}
