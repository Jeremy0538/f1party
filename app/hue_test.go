package main

import (
	"os"
	"path/filepath"
	"testing"
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
