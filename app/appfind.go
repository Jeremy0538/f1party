package main

// Finding the apps F1 Party starts (MultiViewer, Ultimate F1 Viewer) on any system, not only where their
// installers put them:
//   - the usual places per system (the installer's folders on Windows, /Applications on macOS, PATH);
//   - where the app was seen running, remembered in the config: any install, also a portable copy, a zip
//     unpacked somewhere or an AppImage;
//   - on Linux the app launchers' .desktop entries (packages, and AppImage managers such as Gear Lever) and
//     the folders AppImages are kept in.
// An app also counts as installed once it has been used here (its settings folder exists) or while it runs,
// even when its program can't be found: then it has to be started by hand once, and F1 Party remembers it.

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

type appSpec struct {
	name       string         // for messages
	match      *regexp.Regexp // the app in a file name, a launcher entry's name or its Exec line
	known      func() []string
	remembered func(c config) string
	remember   func(c *config, path string)
	running    func() string // the program of the running app ("" = doesn't run)
	knownFirst bool          // known places before the remembered one (MultiViewer on Windows keeps old versions)
	cache      struct {
		sync.Mutex
		exe string
		at  time.Time
	}
}

// exe: the app's program ("" = not found). Looked up at most every 30 seconds.
func (a *appSpec) exe() string {
	a.cache.Lock()
	defer a.cache.Unlock()
	if time.Since(a.cache.at) < 30*time.Second && (a.cache.exe == "" || fileExists(a.cache.exe)) {
		return a.cache.exe
	}
	a.cache.exe, a.cache.at = a.find(), time.Now()
	return a.cache.exe
}

func (a *appSpec) find() string {
	firstOf := func(ps []string) string {
		for _, p := range ps {
			if p != "" && fileExists(p) {
				return p
			}
		}
		return ""
	}
	if a.knownFirst {
		if p := firstOf(a.known()); p != "" {
			return p
		}
	}
	if p := a.remembered(loadConfig()); p != "" && fileExists(p) {
		return p
	}
	if p := a.running(); p != "" && fileExists(p) {
		updateConfig(func(c *config) { a.remember(c, p) })
		return p
	}
	if !a.knownFirst {
		if p := firstOf(a.known()); p != "" {
			return p
		}
	}
	if runtime.GOOS == "linux" {
		home, _ := os.UserHomeDir()
		if p := firstOf(a.fromDesktopFiles(home)); p != "" {
			return p
		}
		return firstOf(a.appImages(home))
	}
	return ""
}

// fromDesktopFiles: the program in an app launcher entry for the app.
func (a *appSpec) fromDesktopFiles(home string) []string {
	dirs := []string{filepath.Join(home, ".local", "share", "applications"), "/usr/share/applications", "/usr/local/share/applications"}
	var out []string
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d, "*.desktop"))
		for _, f := range files {
			name, execLine := desktopEntry(f)
			if !a.match.MatchString(name) && !a.match.MatchString(filepath.Base(f)) && !a.match.MatchString(execLine) {
				continue
			}
			if p := desktopProgram(execLine); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// appImages: AppImages named after the app in the folders they're usually kept in.
func (a *appSpec) appImages(home string) []string {
	var out []string
	for _, dir := range []string{
		filepath.Join(home, "Applications"), filepath.Join(home, "AppImages"), filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".local", "share", "AppImage"), filepath.Join(home, ".local", "opt"), filepath.Join(home, "Downloads"), "/opt",
	} {
		for _, pat := range []string{"*.[Aa]pp[Ii]mage", "*/*.[Aa]pp[Ii]mage"} {
			m, _ := filepath.Glob(filepath.Join(dir, pat))
			for _, p := range m {
				if a.match.MatchString(filepath.Base(p)) {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// usedHere: the app's own settings folder exists (it has run on this computer).
func usedHere(folder string) bool {
	dir, err := os.UserConfigDir()
	return err == nil && fileExists(filepath.Join(dir, folder))
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
		lp, err := exec.LookPath(p)
		if err != nil {
			return ""
		}
		p = lp
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
