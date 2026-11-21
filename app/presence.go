package main

// Discord Rich Presence: while you're in a party, your Discord profile shows what you watch and, when the
// relay serves join pages, a "Join party" button. It talks to the Discord app on this computer over its
// local IPC pipe; nothing is sent when Discord isn't running or no application id is set.

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// discordAppID: the Discord application "F1 Party" (developer portal), with an art asset named "f1party".
// Override per computer with "discordAppId" in the settings file; empty switches presence off.
const discordAppID = "1573782077885653193"

type presence struct {
	conn    io.ReadWriteCloser
	shown   bool // an activity is set
	started int64
	mode    string
}

func (p *Party) watchPresence() {
	pr := &presence{}
	for ; ; time.Sleep(15 * time.Second) {
		id := loadConfig().DiscordAppID
		if id == "" {
			id = discordAppID
		}
		if id == "" {
			continue
		}
		s := p.Snapshot()
		if s.Mode != pr.mode {
			pr.mode, pr.started = s.Mode, time.Now().Unix()
		}
		var activity any
		if s.Mode != "idle" {
			activity = pr.activity(s)
		} else if !pr.shown {
			continue
		}
		if err := pr.set(id, activity); err != nil {
			pr.close()
			continue
		}
		pr.shown = activity != nil
	}
}

func (pr *presence) activity(s Status) map[string]any {
	details := mvSessionName()
	if details == "" {
		details = "Watching F1"
	}
	watching := 0
	host := ""
	for _, m := range s.Members {
		watching++
		if m.Role == "host" {
			host = m.Name
		}
	}
	state := fmt.Sprintf("Hosting an F1 Party · %d watching", watching)
	if s.Mode == "guest" {
		state = "In an F1 Party"
		if host != "" {
			state = "In " + host + "'s F1 Party"
		}
	}
	a := map[string]any{
		"details":    details,
		"state":      state,
		"timestamps": map[string]any{"start": pr.started},
		"assets":     map[string]any{"large_image": "f1party", "large_text": "F1 Party"},
	}
	if link := joinLink(s.Code); link != "" {
		a["buttons"] = []map[string]string{{"label": "Join party", "url": link}}
	}
	return a
}

// set sends SET_ACTIVITY (nil clears it), connecting and saying hello first when needed.
func (pr *presence) set(appID string, activity any) error {
	if pr.conn == nil {
		c, err := dialDiscord()
		if err != nil {
			return err
		}
		pr.conn = c
		if err := pr.send(0, map[string]any{"v": 1, "client_id": appID}); err != nil {
			return err
		}
		if _, err := pr.read(); err != nil {
			return err
		}
	}
	err := pr.send(1, map[string]any{
		"cmd":   "SET_ACTIVITY",
		"args":  map[string]any{"pid": os.Getpid(), "activity": activity},
		"nonce": fmt.Sprintf("%d", time.Now().UnixNano()),
	})
	if err != nil {
		return err
	}
	reply, err := pr.read()
	if err != nil {
		return err
	}
	var r struct {
		Evt  string `json:"evt"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(reply, &r) == nil && r.Evt == "ERROR" {
		return errors.New("discord: " + r.Data.Message)
	}
	return nil
}

func (pr *presence) send(op uint32, v any) error {
	body, _ := json.Marshal(v)
	frame := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:], op)
	binary.LittleEndian.PutUint32(frame[4:], uint32(len(body)))
	copy(frame[8:], body)
	_, err := pr.conn.Write(frame)
	return err
}

func (pr *presence) read() ([]byte, error) {
	head := make([]byte, 8)
	if _, err := io.ReadFull(pr.conn, head); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(head[4:])
	if n > 1<<20 {
		return nil, errors.New("discord: reply too large")
	}
	body := make([]byte, n)
	_, err := io.ReadFull(pr.conn, body)
	return body, err
}

func (pr *presence) close() {
	if pr.conn != nil {
		pr.conn.Close()
		pr.conn = nil
	}
	pr.shown = false
}
