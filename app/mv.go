package main

// MultiViewer's local GraphQL API (same on Windows, macOS and Linux): http://localhost:10101/api/graphql

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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
	err := gql(`{ players { id type state { paused live currentTime interpolatedCurrentTime } streamData { contentId channelId title } } }`, nil, &out)
	return out.Players, err
}

// mainFeed: the world feed (INTERNATIONAL, else F1 LIVE), optionally for one content id.
func mainFeed(ps []Player, contentID string) *Player {
	var best *Player
	for i := range ps {
		p := &ps[i]
		if p.Type != "ADDITIONAL" || p.StreamData == nil {
			continue
		}
		if contentID != "" && p.StreamData.ContentID != contentID {
			continue
		}
		switch p.StreamData.Title {
		case "INTERNATIONAL":
			return p
		case "F1 LIVE":
			if best == nil {
				best = p
			}
		}
	}
	return best
}

func playerCreate(contentID, streamTitle string) (string, error) {
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
