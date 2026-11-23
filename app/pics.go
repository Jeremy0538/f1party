package main

// Pictures: your own profile picture and your teams' logos. The window makes them small and square (a
// data URL, 128 px); your own is kept in the settings and sent to the relay, which hands it to your friends,
// teammates and party (by your id). A team's logo lives only on the relay; its owner sets it. The window
// loads every picture through /api/pic/..., which asks the relay once per version and remembers it.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

var picRE = regexp.MustCompile(`^data:image/(png|jpeg|webp);base64,[A-Za-z0-9+/]+={0,2}$`)

// picOK: "" (no picture) or a small PNG, JPEG or WebP data URL.
func picOK(v string) bool { return v == "" || (len(v) <= 64_000 && picRE.MatchString(v)) }

// picVersion: the start of the picture's sha256, as the relay counts it.
func picVersion(data string) string {
	if data == "" {
		return ""
	}
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])[:10]
}

// setMyPic keeps the picture and sends it to the relay. Kept even when the relay can't take it yet: the
// next check-in sends it again (see syncMyPic).
func setMyPic(data string) error {
	if !picOK(data) {
		return errors.New("that picture isn't a small PNG, JPEG or WebP")
	}
	updateConfig(func(c *config) { c.Pic = data })
	err := uploadMyPic(data)
	pokePeople()
	return err
}

func uploadMyPic(data string) error {
	var out struct{ Pic string }
	if err := relayJSON("/me/pic", with(me(), "pic", data), &out); err != nil {
		if strings.Contains(err.Error(), "Not found") || strings.Contains(err.Error(), "404") {
			return errors.New("saved; others see it once the F1 Party relay has been updated")
		}
		return err
	}
	return nil
}

// syncMyPic: after a check-in, when the relay has another picture of you than the one kept here, send it
// again (a relay without pictures doesn't answer "me": nothing to do then).
func syncMyPic(relayHas *string) {
	if relayHas == nil {
		return
	}
	if want := loadConfig().Pic; picVersion(want) != *relayHas {
		go func() { _ = uploadMyPic(want) }()
	}
}

func setTeamLogo(id, data string) error {
	if !picOK(data) {
		return errors.New("that logo isn't a small PNG, JPEG or WebP")
	}
	err := teamCall("/team/logo", id, "logo", data)
	if err != nil && (strings.Contains(err.Error(), "Not found") || strings.Contains(err.Error(), "404")) {
		return errors.New("team logos work once the F1 Party relay has been updated")
	}
	return err
}

// ---------------------------------------------------------------- loading pictures for the window

type picEntry struct {
	data  []byte
	ctype string
	at    time.Time
}

var picCache = struct {
	sync.Mutex
	m map[string]picEntry
}{m: map[string]picEntry{}}

var picHTTP = &http.Client{Timeout: 8 * time.Second}

var picIDRE = regexp.MustCompile(`^[A-Za-z0-9]{8,12}$`)

// servePic answers /api/pic/me and /api/pic/{u|t}/{id}?v=..: the picture, or 404 when there's none (the
// window then shows the letter). A versioned picture is kept for good, an unversioned one for 5 minutes.
func servePic(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/pic/")
	if rest == "me" {
		data := loadConfig().Pic
		if b, ctype, ok := decodePic(data); ok {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
		return
	}
	kind, id, _ := strings.Cut(rest, "/")
	if (kind != "u" && kind != "t") || !picIDRE.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	v := r.URL.Query().Get("v")
	key := kind + "/" + id + "?" + v
	picCache.Lock()
	e, ok := picCache.m[key]
	picCache.Unlock()
	if !ok || (v == "" && time.Since(e.at) > 5*time.Minute) {
		e = picEntry{at: time.Now()}
		if base := relayBase(); base != "" {
			u := base + "/pic/" + kind + "/" + id
			if v != "" {
				u += "?v=" + v
			}
			if res, err := picHTTP.Get(u); err == nil {
				if res.StatusCode == http.StatusOK {
					e.data, _ = io.ReadAll(io.LimitReader(res.Body, 128<<10))
					e.ctype = res.Header.Get("Content-Type")
				}
				res.Body.Close()
			}
		}
		picCache.Lock()
		picCache.m[key] = e
		picCache.Unlock()
	}
	if len(e.data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", e.ctype)
	w.Header().Set("Cache-Control", "max-age=300")
	_, _ = w.Write(e.data)
}

func decodePic(data string) ([]byte, string, bool) {
	if !picOK(data) || data == "" {
		return nil, "", false
	}
	head, b64, _ := strings.Cut(data, ";base64,")
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "", false
	}
	return b, strings.TrimPrefix(head, "data:"), true
}
