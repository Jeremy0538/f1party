package main

// The push line: a WebSocket to the relay that stays open, also outside a party. It carries nothing but
// {"t":"poke"}: the relay sends it when something changed for this app (a friend request or its answer, a
// party or team invite, being unfriended, a team's name, logo or members, a friend's or teammate's picture,
// status or coming online). F1 Party then checks in right away, so the Friends and Teams tabs are up to date
// within a second. The 30-second check-in stays as it is: without the push line (an older relay, a network
// that blocks it, while reconnecting) everything still arrives, just later.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// peopleVer goes up whenever what the Friends and Teams tabs show changed; the window looks at it in every
// status and fetches /api/people only then (no faster polling).
var peopleVer atomic.Int64
var peopleSum atomic.Value // [32]byte of the last view

func markPeopleChanged() {
	b, _ := json.Marshal(peopleView())
	sum := sha256.Sum256(b)
	if old, _ := peopleSum.Load().([32]byte); old != sum {
		peopleSum.Store(sum)
		peopleVer.Add(1)
	}
}

func pushURL() string {
	if !strings.HasSuffix(relayURL, "/ws") {
		return ""
	}
	return strings.TrimSuffix(relayURL, "/ws") + "/people/ws"
}

func watchPush() {
	backoff := 2 * time.Second
	for {
		up, wait := pushOnce()
		switch {
		case wait > 0: // a relay without push lines (or this app unknown there yet): try again much later
			time.Sleep(wait)
			continue
		case up > 2*time.Minute: // it was up a good while: this is a fresh drop
			backoff = 2 * time.Second
		}
		time.Sleep(backoff)
		backoff = min(backoff*2, time.Minute)
	}
}

// pushOnce keeps one push line open until it drops. Returns how long it was up, and how long to wait before
// trying again when the relay can't do it (0: retry with the normal back-off).
func pushOnce() (up, wait time.Duration) {
	u := pushURL()
	if u == "" {
		return 0, 10 * time.Minute
	}
	h := http.Header{}
	h.Set("X-F1P-Id", persistentID())
	h.Set("X-F1P-Secret", presenceSecret())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	c, res, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: h})
	cancel()
	if err != nil {
		if res != nil && (res.StatusCode == 404 || res.StatusCode == 426) {
			return 0, 10 * time.Minute // a relay from before push lines
		}
		if res != nil && res.StatusCode == 403 {
			pokePeople() // the first check-in makes this app known to the relay
			return 0, 30 * time.Second
		}
		return 0, 0
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 12)
	start := time.Now()
	pokePeople() // catch up on what changed while the line was down

	// "ping" every 45 s (the relay answers "pong" without waking up); no answer for 100 s: the line is dead
	// (sleep, another network), so reconnect.
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(45 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				wctx, wc := context.WithTimeout(context.Background(), 10*time.Second)
				err := c.Write(wctx, websocket.MessageText, []byte("ping"))
				wc()
				if err != nil {
					c.CloseNow()
					return
				}
			}
		}
	}()
	for {
		rctx, rc := context.WithTimeout(context.Background(), 100*time.Second)
		_, msg, err := c.Read(rctx)
		rc()
		if err != nil {
			return time.Since(start), 0
		}
		if strings.Contains(string(msg), `"poke"`) {
			pokePeople()
		}
	}
}
