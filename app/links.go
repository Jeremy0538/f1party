package main

// The Downloads menu: where to get MultiViewer, Ultimate F1 Viewer, the Stream Deck plugin and F1 Party
// itself, and which of them are installed here.

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	mvDownload  = "https://multiviewer.app/download"
	uf1Download = "https://github.com/MRAJEKO/UF1-Viewer-with-F1MV/releases/latest"
)

// uf1Installed: Ultimate F1 Viewer is on this computer (its usual install places).
func uf1Installed() bool {
	var paths []string
	switch runtime.GOOS {
	case "windows":
		paths = []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "ultimate-f1viewer-with-f1mv", "Ultimate-F1Viewer-With-F1MV.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Ultimate-F1Viewer-With-F1MV", "Ultimate-F1Viewer-With-F1MV.exe"),
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		paths = []string{"/Applications/Ultimate-F1Viewer-With-F1MV.app", filepath.Join(home, "Applications", "Ultimate-F1Viewer-With-F1MV.app")}
	default:
		if _, err := exec.LookPath("ultimate-f1viewer"); err == nil {
			return true
		}
		paths = []string{"/opt/Ultimate-F1Viewer-With-F1MV/ultimate-f1viewer-with-f1mv"}
	}
	for _, p := range paths {
		if fileExists(p) {
			return true
		}
	}
	return false
}

// setupPrompt: a prompt for an AI assistant that builds MultiViewer setups and Ultimate F1 Viewer layouts per
// session type for your own monitors. The same file ships with the F1 Race Control Stream Deck plugin
// (StanvanDaalen/streamdeck-plugins, F1/sdPlugin/setup-prompt.md); keep the two copies the same.
//
//go:embed setup-prompt.md
var setupPromptFile string

// setupPromptText: the part to paste, everything after the first line that is just "---".
func setupPromptText() string {
	s := strings.ReplaceAll(setupPromptFile, "\r\n", "\n")
	if _, after, ok := strings.Cut(s, "\n---\n"); ok {
		s = after
	}
	return strings.TrimSpace(s)
}

// allowedURL: the only pages the window may open in the browser.
func allowedURL(u string) bool {
	switch u {
	case pluginDownload, pluginPage, mvDownload, uf1Download, ReleasesPage:
		return true
	}
	return false
}
