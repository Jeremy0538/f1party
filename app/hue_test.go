package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHueTrackFlag(t *testing.T) {
	for _, v := range []struct {
		s string
		f hueFlag
	}{{"1", hueGreen}, {"2", hueYellow}, {"3", hueGreen}, {"4", hueSafetyCar}, {"5", hueRed}, {"6", hueVSC}, {"7", hueVSC}, {"", hueGreen}, {"unknown", hueGreen}} {
		if got := hueTrackFlag(v.s); got != v.f {
			t.Errorf("%s: %v", v.s, got)
		}
	}
	s := hueDefaults()
	s.ShowSafetyCar = false
	if hueWanted(hueVSC, s) != hueGreen || hueWanted(hueSafetyCar, s) != hueGreen {
		t.Fatal("disabled SC")
	}
}
func TestHueSettings(t *testing.T) {
	d := t.TempDir()
	t.Setenv("APPDATA", d)
	t.Setenv("XDG_CONFIG_HOME", d)
	s := loadHue()
	if s.Enabled || !s.ShowYellow || !s.ShowSafetyCar || !s.ShowRed || !s.ShowChequered || !s.VscPulse {
		t.Fatal(s)
	}
	p := filepath.Join(d, "StanF1")
	if e := os.MkdirAll(p, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(p, "settings.json"), []byte(`{"BridgeIp":"192.0.2.1","AppKey":"test","Group":"Room","FlagsEnabled":true}`), 0600); e != nil {
		t.Fatal(e)
	}
	s = loadHue()
	if s.BridgeIP != "192.0.2.1" || s.AppKey != "test" || s.Group != "Room" || s.Enabled {
		t.Fatal(s)
	}
	for i := 0; i < 2; i++ {
		if e := saveHue(func(s *HueSettings) { s.ShowYellow = false; s.BridgeIP = "192.0.2.2" }); e != nil {
			t.Fatal(e)
		}
	}
	s = loadHue()
	if s.ShowYellow || !s.ShowRed || s.BridgeIP != "192.0.2.2" {
		t.Fatal(s)
	}
}
func TestHueFinishCount(t *testing.T) {
	m := map[string]any{"Flag": "CHEQUERED"}
	if hueFinishCount([]any{m}) != 1 || hueFinishCount(map[string]any{"0": m}) != 1 || hueFinishCount(nil) != 0 {
		t.Fatal("message formats")
	}
}

func TestHueMDNSAnswers(t *testing.T) {
	packet := []byte{0, 0, 128, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 1}
	got := hueMDNSAnswers(packet)
	if len(got) != 1 || got[0] != "192.0.2.1" {
		t.Fatal(got)
	}
	// A compressed owner name uses the root label at offset 12.
	packet[7] = 2
	packet = append(packet, 192, 12, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 2)
	if got = hueMDNSAnswers(packet); len(got) != 2 || got[1] != "192.0.2.2" {
		t.Fatal(got)
	}
	for i := 0; i < len(packet); i++ {
		hueMDNSAnswers(packet[:i])
	}
}

func TestHuePairWait(t *testing.T) {
	d := t.TempDir()
	t.Setenv("APPDATA", d)
	t.Setenv("XDG_CONFIG_HOME", d)
	var attempts atomic.Int32
	var succeed atomic.Bool
	called := make(chan struct{}, 10)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		ready := succeed.Load()
		called <- struct{}{}
		if ready {
			w.Write([]byte(`[{"success":{"username":"test-key"}}]`))
		} else {
			w.Write([]byte(`[{"error":{"type":101,"description":"button"}}]`))
		}
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	client.Transport = transport
	old := hueHTTP
	hueHTTP = client
	defer func() { hueHTTP = old }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- huePairWait(ctx, "127.0.0.1") }()
	<-called
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "Didn't see the button") {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop pairing")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	go func() { done <- huePairWait(ctx, "127.0.0.1") }()
	<-called
	succeed.Store(true)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if attempts.Load() < 3 {
		t.Fatal("pairing did not retry")
	}
	if s := loadHue(); s.AppKey != "test-key" || s.BridgeIP != "127.0.0.1" {
		t.Fatal(s)
	}
}
