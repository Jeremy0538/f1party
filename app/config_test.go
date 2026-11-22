package main

import (
	"os"
	"testing"
)

// Saving over an existing settings file (temp file + rename) keeps the newest settings, also on Windows.
func TestSaveConfigOverwrites(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())         // Windows settings folder
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // Linux
	t.Setenv("HOME", t.TempDir())            // macOS
	for _, name := range []string{"first", "second", "third"} {
		updateConfig(func(c *config) { c.Name = name })
		if got := loadConfig().Name; got != name {
			t.Fatalf("after saving %q the file says %q", name, got)
		}
	}
}

// Settings that can't be read are never written over (that once wiped the webhook and setup).
func TestDamagedConfigIsKept(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	updateConfig(func(c *config) { c.Name = "kept" })
	p, _ := configPath()
	damaged := []byte(`{"name":"kept","webhook":"x"`) // cut off
	if err := os.WriteFile(p, damaged, 0o644); err != nil {
		t.Fatal(err)
	}
	updateConfig(func(c *config) { c.Name = "other" })
	if b, _ := os.ReadFile(p); string(b) != string(damaged) {
		t.Fatalf("damaged settings were written over: %s", b)
	}
}
