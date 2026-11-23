package main

// Ultimate F1 Viewer next to a MultiViewer setup. Each setup can have one of the viewer's layouts with it
// (by default the layout with the same name, as the F1 Race Control plugin pairs them): after the setup
// opens, F1 Party opens that layout too. The viewer is driven through Chromium's remote-debugging port
// (9333, the same one the plugin uses), with the same restoreLayout function as its own LOAD button.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	uf1Process = "Ultimate-F1Viewer-With-F1MV"
	uf1Port    = 9333
	// uf1Pattern: the viewer's program in a process list on Linux (a package's name, or "uf1" in the AppImage)
	uf1Pattern = `(^|/)(ultimate-f1viewer-with-f1mv|ultimate-f1viewer|uf1)( |$)`
)

// uf1Exe: where Ultimate F1 Viewer is installed ("" = not found).
func uf1Exe() string {
	var paths []string
	switch runtime.GOOS {
	case "windows":
		paths = []string{
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "ultimate-f1viewer-with-f1mv", uf1Process+".exe"),
			filepath.Join(os.Getenv("ProgramFiles"), uf1Process, uf1Process+".exe"),
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		paths = []string{"/Applications/" + uf1Process + ".app", filepath.Join(home, "Applications", uf1Process+".app")}
	default:
		// a package puts it on PATH or in /opt; the AppImage (the only Linux build of 1.4.8) can be anywhere,
		// so the usual AppImage folders are searched by name too. Its program inside is called "uf1".
		for _, name := range []string{"ultimate-f1viewer", "ultimate-f1viewer-with-f1mv", "uf1"} {
			if p, err := exec.LookPath(name); err == nil {
				return p
			}
		}
		home, _ := os.UserHomeDir()
		paths = []string{"/opt/" + uf1Process + "/ultimate-f1viewer-with-f1mv"}
		for _, dir := range []string{
			filepath.Join(home, ".local", "opt", "uf1"), filepath.Join(home, "Applications"),
			filepath.Join(home, ".local", "bin"), filepath.Join(home, "Downloads"), "/opt",
		} {
			for _, pat := range []string{"UF1*.AppImage", "*[Uu]ltimate*[Ff]1*[Vv]iewer*.AppImage"} {
				m, _ := filepath.Glob(filepath.Join(dir, pat))
				paths = append(paths, m...)
			}
		}
	}
	for _, p := range paths {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func uf1Installed() bool { return uf1Exe() != "" }

// UF1Layout: one of the viewer's saved layouts.
type UF1Layout struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// uf1Layouts: the layouts saved in the viewer (its config.json), in id order.
func uf1Layouts() []UF1Layout {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, uf1Process, "config.json"))
	if err != nil {
		return nil
	}
	var cfg struct {
		Layouts map[string]struct {
			Name string `json:"name"`
		} `json:"layouts"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return nil
	}
	var out []UF1Layout
	for id, l := range cfg.Layouts {
		name := l.Name
		if name == "" {
			name = "Layout " + id
		}
		out = append(out, UF1Layout{ID: id, Name: name})
	}
	slices.SortFunc(out, func(a, b UF1Layout) int {
		x, _ := strconv.Atoi(a.ID)
		y, _ := strconv.Atoi(b.ID)
		return x - y
	})
	return out
}

// uf1LayoutFor: the layout chosen for a MultiViewer setup: the saved choice ("-" = none), else the layout
// with the same name, else none.
func uf1LayoutFor(setup string) string {
	c := loadConfig()
	if setup == "" || setup == setupStreamDeck || c.Uf1Off {
		return ""
	}
	if id, ok := c.Uf1For[setup]; ok {
		if id == "-" {
			return ""
		}
		return id
	}
	for _, l := range uf1Layouts() {
		if strings.EqualFold(strings.TrimSpace(l.Name), strings.TrimSpace(setup)) {
			return l.ID
		}
	}
	return ""
}

func uf1Main() (cdpPage, bool) {
	pages, err := cdpTargets(uf1Port)
	if err != nil {
		return cdpPage{}, false
	}
	for _, p := range pages {
		if strings.Contains(p.URL, "/main/index.html") {
			return p, true
		}
	}
	return cdpPage{}, false
}

// uf1Ensure: the viewer runs with its debug port. Running without it, it is restarted with it.
func uf1Ensure() error {
	for i := 0; i < 3; i++ {
		if _, ok := uf1Main(); ok {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	exe := uf1Exe()
	if exe == "" {
		return errors.New("Ultimate F1 Viewer isn't installed")
	}
	// Running without the port (opened by hand): close it and start it again with the port. A second copy
	// would only hand over to the running one and quit.
	var stop *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		stop = exec.Command("taskkill", "/IM", uf1Process+".exe")
	case "darwin":
		if exec.Command("pgrep", "-x", uf1Process).Run() == nil {
			stop = exec.Command("osascript", "-e", `quit app "`+uf1Process+`"`)
		}
	default:
		stop = exec.Command("pkill", "-f", uf1Pattern)
	}
	if stop != nil {
		detach(stop)
		if stop.Run() == nil {
			time.Sleep(1500 * time.Millisecond)
		}
	}
	arg := fmt.Sprintf("--remote-debugging-port=%d", uf1Port)
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-a", exe, "--args", arg)
	} else {
		cmd = exec.Command(exe, arg)
		cmd.Dir = filepath.Dir(exe)
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("can't start Ultimate F1 Viewer: %w", err)
	}
	go cmd.Wait()
	for i := 0; i < 40; i++ {
		time.Sleep(750 * time.Millisecond)
		if _, ok := uf1Main(); ok {
			time.Sleep(1500 * time.Millisecond) // its session lookup runs once
			return nil
		}
	}
	return errors.New("Ultimate F1 Viewer started but can't be reached")
}

// uf1Open opens a layout of the viewer for a session (contentID "" = the live one, as in its own field).
func uf1Open(layoutID, contentID string) error {
	if _, err := strconv.Atoi(layoutID); err != nil {
		return errors.New("unknown Ultimate F1 Viewer layout")
	}
	if err := uf1Ensure(); err != nil {
		return err
	}
	toF1Desktop()
	main, ok := uf1Main()
	if !ok {
		return errors.New("Ultimate F1 Viewer can't be reached")
	}
	id, _ := json.Marshal(contentID)
	js := fmt.Sprintf(`(async () => { const f = document.getElementById('content-id-field'); if (f) f.value = %s; await restoreLayout(%s); return true; })()`, id, layoutID)
	_, err := cdpEval(main, js, 60*time.Second)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil // before a session starts the viewer keeps waiting for it, and carries on by itself
	}
	return err
}
