package main

// Ultimate F1 Viewer on any system (see appfind.go).

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
)

var uf1App = &appSpec{
	name:       "Ultimate F1 Viewer",
	match:      regexp.MustCompile(`(?i)ultimate.?f1.?viewer|^uf1[^/]*\.appimage$`),
	known:      uf1Candidates,
	remembered: func(c config) string { return c.Uf1Path },
	remember:   func(c *config, p string) { c.Uf1Path = p },
	running:    func() string { return runningPath(uf1Process, uf1Pattern) },
}

func uf1Exe() string { return uf1App.exe() }

// findUf1: look again now (tests).
func findUf1() string { return uf1App.find() }

// uf1Installed: the program is found, it has been used on this computer, or it runs.
func uf1Installed() bool {
	if uf1Exe() != "" || usedHere(uf1Process) {
		return true
	}
	_, running := uf1Main()
	return running
}

var uf1NameRe = regexp.MustCompile(`(?i)ultimate.?f1.?viewer`)

func uf1Candidates() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, base := range []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if base == "" {
				continue
			}
			out = append(out, filepath.Join(base, "ultimate-f1viewer-with-f1mv", uf1Process+".exe"), filepath.Join(base, uf1Process, uf1Process+".exe"))
			// any folder named after the viewer (another installer, or a renamed one)
			m, _ := filepath.Glob(filepath.Join(base, "*ltimate*1*iewer*", "*.exe"))
			for _, p := range m {
				if uf1NameRe.MatchString(filepath.Base(p)) {
					out = append(out, p)
				}
			}
		}
		return out
	case "darwin":
		return []string{"/Applications/" + uf1Process + ".app", filepath.Join(home, "Applications", uf1Process+".app")}
	}
	var out []string
	for _, name := range []string{"ultimate-f1viewer", "ultimate-f1viewer-with-f1mv", "uf1"} {
		if p, err := exec.LookPath(name); err == nil {
			out = append(out, p)
		}
	}
	return append(out, "/opt/"+uf1Process+"/ultimate-f1viewer-with-f1mv", filepath.Join(home, ".local", "opt", "uf1", "UF1.AppImage"))
}
