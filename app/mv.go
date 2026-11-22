package main

// MultiViewer's local GraphQL API (same on Windows, macOS and Linux): http://localhost:10101/api/graphql

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const mvURL = "http://localhost:10101/api/graphql"

var mvClient = &http.Client{Timeout: 4 * time.Second}

type gqlResp struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func gql(query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequest("POST", mvURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := mvClient.Do(req)
	if err != nil {
		return errMVOffline
	}
	defer res.Body.Close()
	var r gqlResp
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return fmt.Errorf("MultiViewer: %w", err)
	}
	if len(r.Errors) > 0 {
		return errors.New(r.Errors[0].Message)
	}
	if out != nil {
		return json.Unmarshal(r.Data, out)
	}
	return nil
}

var errMVOffline = errors.New("MultiViewer is not running")

type Player struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	State *struct {
		Paused                  bool     `json:"paused"`
		Live                    bool     `json:"live"`
		CurrentTime             *float64 `json:"currentTime"`
		InterpolatedCurrentTime *float64 `json:"interpolatedCurrentTime"`
	} `json:"state"`
	StreamData *struct {
		ContentID string `json:"contentId"`
		ChannelID *int   `json:"channelId"`
		Title     string `json:"title"`
	} `json:"streamData"`
	Bounds *struct {
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"bounds"`
}

func (p *Player) Time() (float64, bool) {
	if p.State == nil {
		return 0, false
	}
	if p.State.InterpolatedCurrentTime != nil {
		return *p.State.InterpolatedCurrentTime, true
	}
	if p.State.CurrentTime != nil {
		return *p.State.CurrentTime, true
	}
	return 0, false
}

func players() ([]Player, error) {
	var out struct {
		Players []Player `json:"players"`
	}
	err := gql(`{ players { id type state { paused live currentTime interpolatedCurrentTime } streamData { contentId channelId title } bounds { x y width height } } }`, nil, &out)
	return out.Players, err
}

// mainFeed: the stream everything syncs with. That is the stream on the main monitor (usually F1 LIVE); without
// one there (or where the monitor is unknown) F1 LIVE, then INTERNATIONAL. Optionally only for one content id.
func mainFeed(ps []Player, contentID string) *Player {
	var feeds []*Player
	for i := range ps {
		p := &ps[i]
		if p.StreamData == nil || (contentID != "" && p.StreamData.ContentID != contentID) {
			continue
		}
		if p.Type != "ADDITIONAL" && p.Type != "OBC" {
			continue
		}
		feeds = append(feeds, p)
	}
	if w, h, ok := primaryScreen(); ok {
		var best *Player
		bestArea := 0.0
		for _, p := range feeds {
			if p.Bounds == nil {
				continue
			}
			b := p.Bounds
			cx, cy := b.X+b.Width/2, b.Y+b.Height/2
			if cx < 0 || cy < 0 || cx >= w || cy >= h {
				continue
			}
			if a := b.Width * b.Height; a > bestArea {
				best, bestArea = p, a
			}
		}
		if best != nil {
			return best
		}
	}
	for _, title := range []string{mainTitle, "INTERNATIONAL"} {
		for _, p := range feeds {
			if p.StreamData.Title == title {
				return p
			}
		}
	}
	return nil
}

// mainTitle: the stream a guest opens and the default everywhere (with commentary and sound).
const mainTitle = "F1 LIVE"

func playerCreate(contentID, streamTitle string) (string, error) {
	toF1Desktop() // new windows land on the F1 desktop when there is one
	var out struct {
		ID string `json:"playerCreate"`
	}
	err := gql(`mutation($c: ID!, $t: String) { playerCreate(input: { contentId: $c, streamTitle: $t }) }`,
		map[string]any{"c": contentID, "t": streamTitle}, &out)
	return out.ID, err
}

func playerSeek(id string, abs float64) error {
	return gql(`mutation($id: ID!, $a: Float) { playerSeekTo(id: $id, absolute: $a) }`, map[string]any{"id": id, "a": abs}, nil)
}

func playerPause(id string, paused bool) error {
	return gql(`mutation($id: ID!, $p: Boolean) { playerSetPaused(id: $id, paused: $p) }`, map[string]any{"id": id, "p": paused}, nil)
}

// mvCloseOthers closes every player that shows another session than keep ("" = all of them) and, with
// timing, MultiViewer's live timing windows too (a setup or the Stream Deck opens the new session's own).
// Before switching the party to another session, so the old one doesn't play on behind the new one.
func mvCloseOthers(keep string, timing bool) int {
	ps, err := players()
	if err != nil {
		return 0
	}
	n := 0
	for _, p := range ps {
		if p.StreamData == nil || (keep != "" && p.StreamData.ContentID == keep) {
			continue
		}
		if gql(`mutation($id: ID!) { playerDelete(id: $id) }`, map[string]any{"id": p.ID}, nil) == nil {
			n++
		}
	}
	if timing {
		if pages, err := cdpPages(); err == nil {
			for _, pg := range pages {
				if strings.Contains(pg.URL, "#/live-timing") {
					cdpEval(pg, "window.close(); true", 2*time.Second)
				}
			}
		}
	}
	return n
}

// playerSync: MultiViewer's own "sync other players to this one" (onboards follow the world feed).
func playerSync(id string) error {
	return gql(`mutation($id: ID!) { playerSync(id: $id) }`, map[string]any{"id": id}, nil)
}

// playerSeekBy moves a player relative to where it is (seconds; live streams clamp at the live edge).
func playerSeekBy(id string, rel float64) error {
	return gql(`mutation($id: ID!, $r: Float) { playerSeekTo(id: $id, relative: $r) }`, map[string]any{"id": id, "r": rel}, nil)
}

// trackNow: the real-world time (UTC, ms) of the moment MultiViewer shows, from its live timing clock (the clock
// it keeps in step with the video). The same instant of a session has the same track time on every computer,
// whatever each one's stream delay, so it is the reference for live streams.
func trackNow() (float64, bool) {
	var out struct {
		C struct {
			Paused     bool   `json:"paused"`
			SystemTime string `json:"systemTime"`
			TrackTime  string `json:"trackTime"`
		} `json:"f1LiveTimingClock"`
	}
	if gql(`{ f1LiveTimingClock { paused systemTime trackTime } }`, nil, &out) != nil {
		return 0, false
	}
	sys, err1 := strconv.ParseFloat(out.C.SystemTime, 64)
	track, err2 := strconv.ParseFloat(out.C.TrackTime, 64)
	if err1 != nil || err2 != nil || track <= 0 {
		return 0, false
	}
	if out.C.Paused {
		return track, true
	}
	return track + (nowMs() - sys), true
}
