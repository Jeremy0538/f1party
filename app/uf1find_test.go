//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Linux: an AppImage anywhere, found through the launcher entry an AppImage manager made for it.
func TestFindUf1DesktopEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", "/usr/bin:/bin")
	app := filepath.Join(home, "Games", "Ultimate-F1Viewer-With-F1MV-1.4.8.AppImage")
	os.MkdirAll(filepath.Dir(app), 0o755)
	os.WriteFile(app, []byte("x"), 0o755)
	apps := filepath.Join(home, ".local", "share", "applications")
	os.MkdirAll(apps, 0o755)
	os.WriteFile(filepath.Join(apps, "appimagekit_1234-uf1.desktop"), []byte("[Desktop Entry]\nName=Ultimate F1 Viewer\nExec=env DESKTOPINTEGRATION=1 \""+app+"\" %U\n[Desktop Action x]\nExec=/bin/false\n"), 0o644)
	if p := findUf1(); p != app {
		t.Fatalf("found %q, want %q", p, app)
	}
	// remembered: found again after the launcher entry is gone
	updateConfig(func(c *config) { c.Uf1Path = app })
	os.RemoveAll(apps)
	if p := findUf1(); p != app {
		t.Fatalf("remembered path: %q", p)
	}
}

func TestDesktopProgram(t *testing.T) {
	for in, want := range map[string]string{
		`"/opt/a b/uf1" %U`:                    "/opt/a b/uf1",
		`env FOO=1 /opt/uf1/uf1 --x`:           "/opt/uf1/uf1",
		`/usr/bin/flatpak run com.example.app`: "",
	} {
		if got := desktopProgram(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}
