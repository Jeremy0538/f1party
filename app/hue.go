package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type HueSettings struct {
	BridgeIP, AppKey, Group                                     string
	Enabled                                                     bool
	ShowYellow, ShowSafetyCar, VscPulse, ShowRed, ShowChequered bool
}

var hueMu sync.Mutex

func hueDefaults() HueSettings {
	return HueSettings{ShowYellow: true, ShowSafetyCar: true, VscPulse: true, ShowRed: true, ShowChequered: true}
}
func huePath() (string, error) {
	d, e := os.UserConfigDir()
	return filepath.Join(d, "f1party", "hue.json"), e
}
func hueRead() (HueSettings, error) {
	s := hueDefaults()
	p, e := huePath()
	if e != nil {
		return s, e
	}
	b, e := os.ReadFile(p)
	if e == nil {
		if e = json.Unmarshal(b, &s); e != nil {
			return hueDefaults(), e
		}
	} else if !os.IsNotExist(e) {
		return s, e
	}
	if s.BridgeIP == "" {
		d, e := os.UserConfigDir()
		if e == nil {
			b, e := os.ReadFile(filepath.Join(d, "StanF1", "settings.json"))
			if e == nil {
				var old struct{ BridgeIp, AppKey, Group string }
				if json.Unmarshal(b, &old) == nil {
					s.BridgeIP, s.AppKey, s.Group = old.BridgeIp, old.AppKey, old.Group
				}
			}
		}
	}
	return s, nil
}
func loadHue() HueSettings { hueMu.Lock(); defer hueMu.Unlock(); s, _ := hueRead(); return s }
func saveHue(f func(*HueSettings)) error {
	hueMu.Lock()
	defer hueMu.Unlock()
	s, e := hueRead()
	if e != nil {
		return e
	}
	f(&s)
	p, e := huePath()
	if e != nil {
		return e
	}
	if e = hueWrite(p, s); e != nil {
		return e
	}
	if s.Enabled {
		hueStopped.Store(false)
	}
	return nil
}
func hueWrite(p string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".settings-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), p)
}

var hueHTTP = &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func hueRequest(s HueSettings, method, path string, body, out any) error {
	if net.ParseIP(s.BridgeIP) == nil {
		return errors.New("invalid Hue bridge IP")
	}
	b, e := json.Marshal(body)
	if e != nil {
		return e
	}
	base := "https://" + net.JoinHostPort(s.BridgeIP, "443") + "/api"
	if s.AppKey != "" {
		base += "/" + url.PathEscape(s.AppKey)
	}
	req, e := http.NewRequest(method, base+path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	r, e := hueHTTP.Do(req)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	b, e = io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if e != nil {
		return e
	}
	if r.StatusCode/100 != 2 {
		return fmt.Errorf("Hue HTTP %d", r.StatusCode)
	}
	var results []struct {
		Error *struct {
			Type        int
			Description string
		}
	}
	if json.Unmarshal(b, &results) == nil {
		for _, v := range results {
			if v.Error != nil {
				if v.Error.Type == 101 {
					return errors.New("press the Hue bridge button, then try pairing again")
				}
				return errors.New(v.Error.Description)
			}
		}
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
func hueDiscover() ([]string, error) {
	c := http.Client{Timeout: 3 * time.Second}
	r, e := c.Get("https://discovery.meethue.com")
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("Hue discovery HTTP %d", r.StatusCode)
	}
	var rows []map[string]string
	if e = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&rows); e != nil {
		return nil, e
	}
	out := []string{}
	for _, r := range rows {
		if net.ParseIP(r["internalipaddress"]) != nil {
			out = append(out, r["internalipaddress"])
		}
	}
	return out, nil
}
func huePair(ip string) error {
	var r []struct{ Success struct{ Username string } }
	if e := hueRequest(HueSettings{BridgeIP: ip}, "POST", "", map[string]string{"devicetype": "f1party#pc"}, &r); e != nil {
		return e
	}
	if len(r) == 0 || r[0].Success.Username == "" {
		return errors.New("Hue pairing returned no app key")
	}
	return saveHue(func(s *HueSettings) { s.BridgeIP = ip; s.AppKey = r[0].Success.Username })
}

type hueGroup struct {
	Name, Type string
	Lights     []string
}

func hueGroups() ([]string, error) {
	var groups map[string]hueGroup
	e := hueRequest(loadHue(), "GET", "/groups", nil, &groups)
	out := []string{}
	seen := map[string]bool{}
	for _, g := range groups {
		if (g.Type == "Room" || g.Type == "Zone") && !seen[g.Name] {
			out = append(out, g.Name)
			seen[g.Name] = true
		}
	}
	sort.Strings(out)
	return out, e
}

type hueScene struct {
	settings HueSettings
	lights   map[string]map[string]any
}

func hueCapture(s HueSettings) (*hueScene, error) {
	var groups map[string]hueGroup
	var lights map[string]struct{ State map[string]any }
	if e := hueRequest(s, "GET", "/groups", nil, &groups); e != nil {
		return nil, e
	}
	if e := hueRequest(s, "GET", "/lights", nil, &lights); e != nil {
		return nil, e
	}
	snap := &hueScene{s, map[string]map[string]any{}}
	for _, g := range groups {
		if g.Name != s.Group || (g.Type != "Room" && g.Type != "Zone") {
			continue
		}
		for _, id := range g.Lights {
			st := lights[id].State
			if st == nil {
				return nil, errors.New("missing Hue light state")
			}
			if st["xy"] == nil {
				continue
			}
			v := map[string]any{"on": st["on"], "bri": st["bri"], "transitiontime": 4}
			switch st["colormode"] {
			case "ct":
				v["ct"] = st["ct"]
			case "hs":
				v["hue"], v["sat"] = st["hue"], st["sat"]
			default:
				v["xy"] = st["xy"]
			}
			snap.lights[id] = v
		}
	}
	if len(snap.lights) == 0 {
		return nil, errors.New("no colour lights in the selected Hue room or zone")
	}
	return snap, nil
}
func (s *hueScene) set(v map[string]any) error {
	var errs []error
	for id := range s.lights {
		errs = append(errs, hueRequest(s.settings, "PUT", "/lights/"+url.PathEscape(id)+"/state", v, nil))
	}
	return errors.Join(errs...)
}
func (s *hueScene) restore() error {
	var errs []error
	for id, st := range s.lights {
		v := map[string]any{}
		for k, x := range st {
			v[k] = x
		}
		v["on"] = true
		e := hueRequest(s.settings, "PUT", "/lights/"+url.PathEscape(id)+"/state", v, nil)
		if e == nil && st["on"] == false {
			time.Sleep(500 * time.Millisecond)
			e = hueRequest(s.settings, "PUT", "/lights/"+url.PathEscape(id)+"/state", map[string]any{"on": false, "transitiontime": 4}, nil)
		}
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}
