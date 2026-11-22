package main

// Your own streams stay together: whatever you do with the main stream (the one on the main monitor), the
// other streams of the same session on this computer do too. Pause it and they pause; play it and they play
// and sync to it again; skip ahead, rewind or drag it to another time and they jump along. In a party, the
// host's pauses and skips reach the guest's main stream first and from there their onboards the same way.

import (
	"math"
	"time"
)

const (
	followTick   = 500 * time.Millisecond
	followJump   = 2.0 // seconds the main stream may differ from where it should be before it counts as a skip
	followSettle = 3 * time.Second
)

// followMainStream runs for as long as the app does.
func followMainStream() {
	var (
		prevID     string
		prevTime   float64
		prevPaused bool
		prevAt     time.Time
		quietUntil time.Time // after our own sync: give the streams time to settle
		resyncAt   time.Time // after play: sync once more when the streams have started again
	)
	for {
		time.Sleep(followTick)
		ps, err := players()
		if err != nil {
			prevID = ""
			time.Sleep(3 * time.Second)
			continue
		}
		main := mainFeed(ps, "")
		if main == nil || main.State == nil {
			prevID = ""
			continue
		}
		t, ok := main.Time()
		if !ok {
			continue
		}
		now := time.Now()
		paused := main.State.Paused
		same := prevID == main.ID
		elapsed := now.Sub(prevAt).Seconds()
		wasPaused, lastTime := prevPaused, prevTime
		prevID, prevTime, prevPaused, prevAt = main.ID, t, paused, now
		if !same {
			continue
		}
		others := followers(ps, main)
		if len(others) == 0 {
			continue
		}
		if !resyncAt.IsZero() && now.After(resyncAt) {
			resyncAt = time.Time{}
			_ = playerSync(main.ID)
			quietUntil = now.Add(followSettle)
			continue
		}
		switch {
		case paused && !wasPaused:
			if pluginBusy() {
				prevPaused = wasPaused // not now: try again next tick, don't lose the pause
				continue
			}
			// only pause: MultiViewer's sync plays the others again, so that waits until you play
			for _, o := range others {
				if !o.State.Paused {
					_ = playerPause(o.ID, true)
				}
			}
			quietUntil = now.Add(time.Second)
		case !paused && wasPaused:
			if pluginBusy() {
				prevPaused = wasPaused
				continue
			}
			for _, o := range others {
				if o.State.Paused {
					_ = playerPause(o.ID, false)
				}
			}
			// once they play, they sync to the main stream
			resyncAt = now.Add(800 * time.Millisecond)
			quietUntil = now.Add(followSettle)
		case now.Before(quietUntil):
		case paused:
			// moved while paused: a replay's other streams go to the same moment and stay paused
			if math.Abs(t-lastTime) > followJump && !main.State.Live {
				for _, o := range others {
					_ = playerSeek(o.ID, t)
					if !o.State.Paused {
						_ = playerPause(o.ID, true)
					}
				}
			}
		default:
			expected := lastTime + elapsed
			if math.Abs(t-expected) > followJump && !pluginBusy() {
				// a skip, a rewind or another time: the others jump along, and once more when it has loaded
				_ = playerSync(main.ID)
				resyncAt = now.Add(1500 * time.Millisecond)
				quietUntil = now.Add(followSettle)
			}
		}
	}
}

// followers: the other streams of the main stream's session.
func followers(ps []Player, main *Player) []Player {
	var out []Player
	for _, o := range ps {
		if o.ID == main.ID || o.State == nil || o.StreamData == nil || main.StreamData == nil ||
			o.StreamData.ContentID != main.StreamData.ContentID {
			continue
		}
		out = append(out, o)
	}
	return out
}
