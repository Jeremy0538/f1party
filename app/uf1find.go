package main

// Finding Ultimate F1 Viewer on any system, not only where its Windows installer puts it:
//   - where it was seen running (remembered in the config): works for any install, also a portable copy or
//     an AppImage somewhere else;
//   - the usual places per system: the Windows installer's folders, /Applications on macOS; on Linux the
//     PATH, app launchers' .desktop files (AppImage managers add one) and the folders AppImages are kept in;
//   - and it counts as installed once it has been used here (its settings folder exists) or while it runs,
//     even when its program can't be found (then starting it is left to you, once).

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

var uf1Found struct {
	sync.Mutex
	exe string
	at  time.Time
}

// uf1Exe: the viewer's program ("" = not found). Looked up at most every 30 seconds.
func uf1Exe() string {
	uf1Found.Lock()
	defer uf1Found.Unlock()
	if time.Since(uf1Found.at) < 30*time.Second && (uf1Found.exe == "" || fileExists(uf1Found.exe)) {
		return uf1Found.exe
	}
	uf1Found.exe, uf1Found.at = findUf1(), time.Now()
	return uf1Found.exe
}

// uf1Installed: the program is found, it runs, or it has been used on this computer.
func uf1Installed() bool {
	if uf1Exe() != "" {
		return true
	}
	if dir, err := os.UserConfigDir(); err == nil && fileExists(filepath.Join(dir, uf1Process)) {
		return true
	}
	_, running := uf1Main()
	return running
}

func findUf1() string {
	if p := loadConfig().Uf1Path; p != "" && fileExists(p) {
		return p
	}
	if p := uf1RunningPath(); p != "" {
		updateConfig(func(c *config) { c.Uf1Path = p })
		return p
	}
	for _, p := range uf1Candidates() {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

var uf1NameRe = regexp.MustCompile(`(?i)ultimate.?f1.?viewer|(^|[/\\\\])uf1[^/\\\\]*\.appimage$`)

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
	out = append(out, uf1FromDesktopFiles(home)...)
	out = append(out, "/opt/"+uf1Process+"/ultimate-f1viewer-with-f1mv")
	for _, dir := range []string{
		filepath.Join(home, "Applications"), filepath.Join(home, ".local", "opt", "uf1"), filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "AppImages"), filepath.Join(home, ".local", "share", "AppImage"), filepath.Join(home, "Downloads"), "/opt",
	} {
		m, _ := filepath.Glob(filepath.Join(dir, "*.[Aa]pp[Ii]mage"))
		for _, p := range m {
			if uf1NameRe.MatchString(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// uf1FromDesktopFiles: the program in an app launcher entry for the viewer (installed packages, and the
// entries AppImage managers such as Gear Lever or AppImageLauncher make).
func uf1FromDesktopFiles(home string) []string {
	dirs := []string{filepath.Join(home, ".local", "share", "applications"), "/usr/share/applications", "/usr/local/share/applications",
		filepath.Join(home, ".local", "share", "flatpak", "exports", "share", "applications")}
	var out []string
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d, "*.desktop"))
		for _, f := range files {
			name, execLine := desktopEntry(f)
			if !uf1NameRe.MatchString(name) && !uf1NameRe.MatchString(filepath.Base(f)) && !uf1NameRe.MatchString(execLine) {
				continue
			}
			if p := desktopProgram(execLine); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func desktopEntry(path string) (name, execLine string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	main := false
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			main = l == "[Desktop Entry]"
			continue
		}
		if !main {
			continue
		}
		if v, ok := strings.CutPrefix(l, "Name="); ok && name == "" {
			name = v
		}
		if v, ok := strings.CutPrefix(l, "Exec="); ok && execLine == "" {
			execLine = v
		}
	}
	return name, execLine
}

// desktopProgram: the program of an Exec= line ("" for one that runs through something else, like flatpak).
func desktopProgram(execLine string) string {
	f := execFields(execLine)
	for len(f) > 0 && (f[0] == "env" || strings.Contains(f[0], "=")) {
		f = f[1:]
	}
	if len(f) == 0 {
		return ""
	}
	p := f[0]
	if !filepath.IsAbs(p) {
		if lp, err := exec.LookPath(p); err == nil {
			p = lp
		} else {
			return ""
		}
	}
	if strings.Contains(filepath.Base(p), "flatpak") {
		return ""
	}
	return p
}

// execFields splits an Exec= line into words; "double quotes" keep spaces in a word.
func execFields(s string) []string {
	var out []string
	var cur strings.Builder
	quoted, any := false, false
	for _, r := range s {
		switch {
		case r == '"':
			quoted, any = !quoted, true
		case (r == ' ' || r == '\t') && !quoted:
			if any {
				out = append(out, cur.String())
				cur.Reset()
				any = false
			}
		default:
			cur.WriteRune(r)
			any = true
		}
	}
	if any {
		out = append(out, cur.String())
	}
	return out
}
