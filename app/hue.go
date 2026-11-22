package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
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
	return hueRequestContext(context.Background(), s, method, path, body, out)
}
func hueRequestContext(ctx context.Context, s HueSettings, method, path string, body, out any) error {
	if net.ParseIP(s.BridgeIP) == nil {
		return errors.New("invalid Hue bridge IP")
	}
	var payload io.Reader // GET goes without a body: the bridge refuses one
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		payload = bytes.NewReader(b)
	}
	base := "https://" + net.JoinHostPort(s.BridgeIP, "443") + "/api"
	if s.AppKey != "" {
		base += "/" + url.PathEscape(s.AppKey)
	}
	req, e := http.NewRequestWithContext(ctx, method, base+path, payload)
	if e != nil {
		return e
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	r, e := hueHTTP.Do(req)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	b, e := io.ReadAll(io.LimitReader(r.Body, 4<<20))
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
					return hueButtonError
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
	return append([]string{hueAllLights}, out...), e
}

// hueAllLights: the lights choice "every light on the bridge" (the F1 Race Control plugin's default).
const hueAllLights = "All lights"

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
	if s.Group == hueAllLights || s.Group == "" { // every light, as the plugin's "All lights"
		all := []string{}
		for id := range lights {
			all = append(all, id)
		}
		groups = map[string]hueGroup{"all": {Name: s.Group, Type: "Room", Lights: all}}
	}
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

var hueButtonError = errors.New("press the Hue bridge button, then try pairing again")

type HueBridge struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
}

func huePairWait(ctx context.Context, ip string) error {
	if net.ParseIP(ip) == nil {
		return errors.New("invalid Hue bridge IP")
	}
	timeout := errors.New("Didn't see the button pressed on the bridge in time; press it and try again")
	for {
		if ctx.Err() != nil {
			return timeout
		}
		var r []struct{ Success struct{ Username string } }
		err := hueRequestContext(ctx, HueSettings{BridgeIP: ip}, "POST", "", map[string]string{"devicetype": "f1party#pc"}, &r)
		if ctx.Err() != nil {
			return timeout
		}
		if err == nil {
			for _, v := range r {
				if v.Success.Username != "" {
					return saveHue(func(s *HueSettings) { s.BridgeIP = ip; s.AppKey = v.Success.Username })
				}
			}
			return errors.New("Hue pairing returned no app key")
		}
		if !errors.Is(err, hueButtonError) {
			return fmt.Errorf("could not reach or pair with Hue bridge: %w", err)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return timeout
		case <-timer.C:
		}
	}
}

// hueMDNSAnswers extracts address records without following compression pointers.
func hueMDNSAnswers(b []byte) []string {
	if len(b) < 12 || b[2]&128 == 0 {
		return nil
	}
	skip := func(i int) int {
		for n := 0; n < 128 && i < len(b); n++ {
			v := int(b[i])
			i++
			if v == 0 {
				return i
			}
			if v&192 == 192 {
				if i < len(b) {
					return i + 1
				}
				return -1
			}
			if v > 63 || i+v > len(b) {
				return -1
			}
			i += v
		}
		return -1
	}
	i := 12
	for q := 0; q < int(binary.BigEndian.Uint16(b[4:6])); q++ {
		i = skip(i)
		if i < 0 || i+4 > len(b) {
			return nil
		}
		i += 4
	}
	var out []string
	count := int(binary.BigEndian.Uint16(b[6:8])) + int(binary.BigEndian.Uint16(b[8:10])) + int(binary.BigEndian.Uint16(b[10:12]))
	for n := 0; n < count; n++ {
		i = skip(i)
		if i < 0 || i+10 > len(b) {
			break
		}
		typ := binary.BigEndian.Uint16(b[i : i+2])
		size := int(binary.BigEndian.Uint16(b[i+8 : i+10]))
		i += 10
		if i+size > len(b) {
			break
		}
		if (typ == 1 && size == 4) || (typ == 28 && size == 16) {
			out = append(out, net.IP(b[i:i+size]).String())
		}
		i += size
	}
	return out
}

func hueBridges(ctx context.Context) []HueBridge {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var mu sync.Mutex
	out := []HueBridge{}
	seen := map[string]bool{}
	var workers sync.WaitGroup
	check := func(ip string) {
		addr := net.ParseIP(ip)
		if addr == nil {
			return
		}
		ip = addr.String()
		mu.Lock()
		if seen[ip] {
			mu.Unlock()
			return
		}
		seen[ip] = true
		mu.Unlock()
		workers.Add(1)
		go func() {
			defer workers.Done()
			probe, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			var config struct{ Name string }
			if hueRequestContext(probe, HueSettings{BridgeIP: ip, AppKey: "0"}, "GET", "/config", nil, &config) == nil && config.Name != "" {
				mu.Lock()
				out = append(out, HueBridge{ip, config.Name})
				mu.Unlock()
			}
		}()
	}
	var sources sync.WaitGroup
	sources.Add(3)
	go func() { defer sources.Done(); check(loadHue().BridgeIP) }()
	go func() {
		defer sources.Done()
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://discovery.meethue.com", nil)
		client := &http.Client{Timeout: 3 * time.Second}
		r, err := client.Do(req)
		if err != nil {
			return
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return
		}
		var rows []struct {
			IP string `json:"internalipaddress"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&rows) == nil {
			for _, row := range rows {
				check(row.IP)
			}
		}
	}()
	go func() {
		defer sources.Done()
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
		if err != nil {
			return
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { conn.Close() })
		defer stop()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		// Request unicast replies on this ephemeral socket.
		query := []byte{0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 4, '_', 'h', 'u', 'e', 4, '_', 't', 'c', 'p', 5, 'l', 'o', 'c', 'a', 'l', 0, 0, 12, 128, 1}
		if _, err = conn.WriteToUDP(query, &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}); err != nil {
			return
		}
		buf := make([]byte, 65535)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			check(from.IP.String()) // a bridge that answers without its address records
			for _, ip := range hueMDNSAnswers(buf[:n]) {
				check(ip)
			}
		}
	}()
	sources.Wait()
	workers.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out
}
