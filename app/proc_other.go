//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func detach(cmd *exec.Cmd) {}

// processRunning is only needed on Windows (killApp asks first and ends what stays there).
func processRunning(string) bool { return false }

// uf1RunningPath: the program of a running Ultimate F1 Viewer ("" when it doesn't run, or on macOS). For an
// AppImage that is the .AppImage file itself (its APPIMAGE variable), not the copy it runs from.
func uf1RunningPath() string {
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	re := regexp.MustCompile(uf1Pattern)
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(d, "cmdline"))
		if err != nil || len(b) == 0 {
			continue
		}
		args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		if strings.Contains(strings.Join(args, " "), "--type=") || !re.MatchString(args[0]+" ") {
			continue // Electron's helper processes, or something else
		}
		if env, err := os.ReadFile(filepath.Join(d, "environ")); err == nil {
			for _, kv := range strings.Split(string(env), "\x00") {
				if v, ok := strings.CutPrefix(kv, "APPIMAGE="); ok && v != "" {
					return v
				}
			}
		}
		if p, err := os.Readlink(filepath.Join(d, "exe")); err == nil && !strings.HasPrefix(p, "/tmp/") {
			return p
		}
	}
	return ""
}
