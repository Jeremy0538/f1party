//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// MultiViewer unpacked from its zip somewhere: found through its launcher entry, then remembered.
func TestFindMultiViewerAnywhere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", t.TempDir())
	known := mvApp.known
	mvApp.known = func() []string { return nil } // not the MultiViewer installed on this computer
	defer func() { mvApp.known = known }()
	exe := filepath.Join(home, "Games", "MultiViewer-linux-x64", "multiviewer")
	os.MkdirAll(filepath.Dir(exe), 0o755)
	os.WriteFile(exe, []byte("x"), 0o755)
	apps := filepath.Join(home, ".local", "share", "applications")
	os.MkdirAll(apps, 0o755)
	os.WriteFile(filepath.Join(apps, "mv.desktop"), []byte("[Desktop Entry]\nName=MultiViewer\nExec=\""+exe+"\" %U\n"), 0o644)
	if p := mvApp.find(); p != exe {
		t.Fatalf("found %q, want %q", p, exe)
	}
	updateConfig(func(c *config) { c.MvPath = exe })
	os.RemoveAll(apps)
	if p := mvApp.find(); p != exe {
		t.Fatalf("remembered: %q", p)
	}
	// used here but the program is gone: still installed, and starting it explains what to do
	os.RemoveAll(filepath.Dir(exe))
	os.MkdirAll(filepath.Join(home, ".config", "MultiViewer"), 0o755)
	mvApp.cache.at = mvApp.cache.at.AddDate(-1, 0, 0)
	if mvExe() != "" || !usedHere("MultiViewer") {
		t.Fatal("gone program still found, or settings folder not seen")
	}
}

// OpenDeck as a Flatpak: the plugin is found in its sandbox folder.
func TestPluginInFlatpakOpenDeck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir := filepath.Join(home, ".var", "app", "me.amankhanna.opendeck", "config", "opendeck", "plugins", "com.stan.f1.sdPlugin")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"Version":"1.29.4.0"}`), 0o644)
	if !pluginInstalled() || pluginInstalledVersion() != "1.29.4" {
		t.Fatalf("installed=%v version=%q", pluginInstalled(), pluginInstalledVersion())
	}
}
