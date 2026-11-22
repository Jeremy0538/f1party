package main

// Updates: checks the latest GitHub release now and then. On Windows the app updates itself: it downloads the
// new F1Party-windows.exe, checks it against the release's checksums.txt, puts it in place of the running
// exe and starts again (and rejoins the party it was in). Elsewhere the update is a link to the release.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

const releasesAPI = "https://api.github.com/repos/Jeremy0538/f1party/releases/latest"

// ReleasesPage is where the update link points (also allowed by /api/openurl).
const ReleasesPage = "https://github.com/Jeremy0538/f1party/releases/latest"

// The release files the Windows self-update uses.
const (
	updateExe       = "F1Party-windows.exe"
	updateChecksums = "checksums.txt"
)

type Update struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Auto    bool   `json:"auto"`            // this app can install it itself
	Busy    string `json:"busy,omitempty"`  // installing: what it is doing
	Error   string `json:"error,omitempty"` // the last install failed
	exe     string // download URL of the new exe
	sums    string // download URL of checksums.txt
}

var updating atomic.Bool

func (p *Party) watchUpdates() {
	first := true
	check := func() {
		c := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest("GET", releasesAPI, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "F1Party/"+version)
		res, err := c.Do(req)
		if err != nil {
			return
		}
		defer res.Body.Close()
		var r struct {
			Tag        string `json:"tag_name"`
			Prerelease bool   `json:"prerelease"`
			Assets     []struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			} `json:"assets"`
		}
		if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&r) != nil || r.Prerelease {
			return
		}
		latest := strings.TrimPrefix(r.Tag, "v")
		if compareVersions(latest, version) <= 0 {
			return
		}
		u := &Update{Version: latest, URL: ReleasesPage}
		for _, a := range r.Assets {
			if !strings.HasPrefix(a.URL, "https://github.com/") {
				continue
			}
			switch a.Name {
			case updateExe:
				u.exe = a.URL
			case updateChecksums:
				u.sums = a.URL
			}
		}
		u.Auto = runtime.GOOS == "windows" && u.exe != "" && u.sums != ""
		p.mu.Lock()
		if p.update == nil || p.update.Version != u.Version {
			p.update = u
		}
		idle := p.st.Mode == "idle"
		p.mu.Unlock()
		// Right after starting and not in a party: update straight away.
		if first && idle && u.Auto {
			go p.InstallUpdate()
		}
	}
	check()
	first = false
	for range time.Tick(6 * time.Hour) {
		check()
	}
}

// restartAfterUpdate is set by main: quit this app (the new one is already starting).
var restartAfterUpdate func()

// InstallUpdate (Windows): download, check, swap and restart. Runs in the background; progress shows on the
// update button.
func (p *Party) InstallUpdate() error {
	p.mu.Lock()
	u := p.update
	p.mu.Unlock()
	if u == nil || !u.Auto {
		return errors.New("no update to install here")
	}
	if !updating.CompareAndSwap(false, true) {
		return nil
	}
	set := func(busy, e string) {
		p.mu.Lock()
		if p.update != nil {
			p.update.Busy, p.update.Error = busy, e
		}
		p.mu.Unlock()
	}
	go func() {
		defer updating.Store(false)
		set("Downloading…", "")
		if err := swapExe(u); err != nil {
			set("", "Update failed: "+err.Error())
			return
		}
		set("Restarting…", "")
		self, _ := os.Executable()
		cmd := exec.Command(self, "--after-update")
		cmd.Dir = filepath.Dir(self)
		detach(cmd)
		if err := cmd.Start(); err != nil {
			set("", "Update installed; start F1 Party again")
			return
		}
		if restartAfterUpdate != nil {
			restartAfterUpdate()
		}
	}()
	return nil
}

// swapExe downloads the new exe next to the running one, checks its SHA-256, and swaps them: the running exe
// becomes .old (Windows lets a running exe be renamed) and is removed on the next start.
func swapExe(u *Update) error {
	want, err := expectedSum(u.sums, updateExe)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if s, err := filepath.EvalSymlinks(self); err == nil {
		self = s
	}
	dir := filepath.Dir(self)
	tmp := filepath.Join(dir, ".F1Party.new.exe")
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("can't write to %s (run the installer from the release page instead)", dir)
	}
	c := &http.Client{Timeout: 5 * time.Minute}
	res, err := c.Get(u.exe)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	defer res.Body.Close()
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, 200<<20))
	f.Close()
	if err != nil || res.StatusCode != 200 {
		os.Remove(tmp)
		return fmt.Errorf("download failed (%d)", res.StatusCode)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(tmp)
		return errors.New("the download doesn't match the release's checksum")
	}
	old := self + ".old"
	os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, self); err != nil {
		os.Rename(old, self) // put the running version back
		return err
	}
	return nil
}

// expectedSum reads the line for name from the release's checksums.txt ("<sha256>  <name>").
func expectedSum(url, name string) (string, error) {
	c := &http.Client{Timeout: 20 * time.Second}
	res, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums.txt: %s", res.Status)
	}
	sc := bufio.NewScanner(io.LimitReader(res.Body, 1<<20))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", errors.New("the release has no checksum for " + name)
}

// afterUpdate runs first in the new exe: wait for the old one to close, and clean up what it left.
func afterUpdate(args []string) {
	self, err := os.Executable()
	if err == nil {
		defer os.Remove(self + ".old")
	}
	for _, a := range args {
		if a == "--after-update" {
			time.Sleep(3 * time.Second) // the old app is quitting and still holds the single-instance lock
			return
		}
	}
}
