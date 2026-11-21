//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

// dialDiscord opens the Discord app's local socket (discord-ipc-0 … -9 in the runtime or temp folder,
// also for the Flatpak and Snap versions).
func dialDiscord() (io.ReadWriteCloser, error) {
	var dirs []string
	for _, k := range []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"} {
		if d := os.Getenv(k); d != "" {
			dirs = append(dirs, d, filepath.Join(d, "app", "com.discordapp.Discord"), filepath.Join(d, "snap.discord"))
		}
	}
	dirs = append(dirs, "/tmp")
	for _, d := range dirs {
		for i := 0; i < 10; i++ {
			if c, err := net.Dial("unix", filepath.Join(d, fmt.Sprintf("discord-ipc-%d", i))); err == nil {
				return c, nil
			}
		}
	}
	return nil, errors.New("discord isn't running")
}
