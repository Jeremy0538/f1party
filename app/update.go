package main

// Updates: checks the latest GitHub release now and then. On Windows and Linux the app updates itself: it
// downloads the new F1Party-windows.exe or F1Party-linux, checks it against the release's checksums.txt, puts
// it in place of the running program and starts again (and rejoins the party it was in). On Linux only when
// F1 Party may write its own folder (not for a package install in /usr). Elsewhere the update is a link.

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
	"sync"
	"sync/atomic"
	"time"
)

const releasesAPI = "https://api.github.com/repos/Jeremy0538/f1party/releases/latest"

// ReleasesPage is where the update link points (also allowed by /api/openurl).
const ReleasesPage = "https://github.com/Jeremy0538/f1party/releases/latest"

// The release files the self-update uses.
const updateChecksums = "checksums.txt"

var updateExe = map[string]string{"windows": "F1Party-windows.exe", "linux": "F1Party-linux"}[runtime.GOOS]

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

// UpdateCheck: the answer to Settings → Updates → Check for updates.
type UpdateCheck struct {
	Version string  `json:"version"`          // this app
	Latest  string  `json:"latest,omitempty"` // the newest release
	Update  *Update `json:"update,omitempty"` // a newer one is there
	Checked int64   `json:"checked,omitempty"`
	Error   string  `json:"error,omitempty"`
}

// lastCheck: when updates were last looked up, and what came out (for Settings → Updates).
var lastCheck struct {
	sync.Mutex
	at     time.Time
	latest string
	err    string
}

func (p *Party) watchUpdates() {
	check := func() { _, _ = p.checkUpdate() }
	check()
	t := time.NewTicker(20 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-updateCheckNow:
		}
		check()
	}
}

// CheckUpdateNow: look up the newest release right away and say what came out.
func (p *Party) CheckUpdateNow() UpdateCheck {
	latest, err := p.checkUpdate()
	out := UpdateCheck{Version: version, Latest: latest, Checked: time.Now().Unix()}
	if err != nil {
		out.Error = err.Error()
	}
	p.mu.Lock()
	if p.update != nil {
		u := *p.update
		out.Update = &u
	}
	p.mu.Unlock()
	return out
}

// LastUpdateCheck: what the last check found, without checking again.
func (p *Party) LastUpdateCheck() UpdateCheck {
	lastCheck.Lock()
	out := UpdateCheck{Version: version, Latest: lastCheck.latest, Error: lastCheck.err}
	if !lastCheck.at.IsZero() {
		out.Checked = lastCheck.at.Unix()
	}
	lastCheck.Unlock()
	p.mu.Lock()
	if p.update != nil {
		u := *p.update
		out.Update = &u
	}
	p.mu.Unlock()
	return out
}

// checkUpdate looks up the newest release on GitHub. A newer one becomes p.update (and installs straight away
// when you're not in a party). Returns the newest version.
func (p *Party) checkUpdate() (latest string, err error) {
	defer func() {
		lastCheck.Lock()
		lastCheck.at = time.Now()
		if latest != "" || err != nil {
			lastCheck.latest = latest
		}
		lastCheck.err = ""
		if err != nil {
			lastCheck.err = err.Error()
		}
		lastCheck.Unlock()
	}()
	{
		c := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest("GET", releasesAPI, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "F1Party/"+version)
		res, err := c.Do(req)
		if err != nil {
			return "", errors.New("couldn't reach GitHub: check your internet connection")
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
		if res.StatusCode == 403 || res.StatusCode == 429 {
			return "", errors.New("GitHub asks to wait a bit before checking again: try again in a few minutes")
		}
		if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&r) != nil {
			return "", fmt.Errorf("GitHub didn't answer as expected (%d): try again later", res.StatusCode)
		}
		if r.Prerelease {
			return "", nil
		}
		latest := strings.TrimPrefix(r.Tag, "v")
		if compareVersions(latest, version) <= 0 {
			return latest, nil
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
		u.Auto = updateExe != "" && u.exe != "" && u.sums != "" && selfWritable()
		p.mu.Lock()
		// a new version, or the same one whose files are there now (the release is published before its
		// files are all uploaded)
		if p.update == nil || p.update.Version != u.Version || (u.Auto && !p.update.Auto && p.update.Busy == "") {
			p.update = u
		}
		idle := p.st.Mode == "idle"
		p.mu.Unlock()
		// Not in a party: update straight away (in a party the button waits for a click).
		if idle && u.Auto {
			go p.InstallUpdate()
		}
		return latest, nil
	}
}

// updateCheckNow: check for an update right away (the window came to the front); at most once a minute.
var updateCheckNow = make(chan struct{}, 1)
var updateCheckedAt atomic.Int64

func checkUpdateSoon() {
	if now := time.Now().Unix(); now-updateCheckedAt.Load() >= 60 {
		updateCheckedAt.Store(now)
		select {
		case updateCheckNow <- struct{}{}:
		default:
		}
	}
}

// restartAfterUpdate is set by main: quit this app (the new one is already starting).
var restartAfterUpdate func()

// InstallUpdate (Windows, Linux): download, check, swap and restart. Runs in the background; progress shows on the
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
		self, err := swapExe(u)
		if err != nil {
			set("", "Update failed: "+err.Error())
			return
		}
		set("Restarting…", "")
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

// selfPath: the running program's file (through a symlink such as ~/.local/bin/f1party).
func selfPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if s, err := filepath.EvalSymlinks(self); err == nil {
		self = s
	}
	return self, nil
}

// selfWritable: the program's folder can take the new version (not a package install in /usr).
func selfWritable() bool {
	self, err := selfPath()
	if err != nil {
		return false
	}
	f, err := os.CreateTemp(filepath.Dir(self), ".F1Party.check")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// swapExe downloads the new program next to the running one, checks its SHA-256, and swaps them: the running
// one becomes .old (Windows lets a running exe be renamed, Linux keeps running the old file) and is removed on
// the next start. Returns the path to start the new version from (on Linux os.Executable now names the .old).
func swapExe(u *Update) (string, error) {
	want, err := expectedSum(u.sums, updateExe)
	if err != nil {
		return "", err
	}
	self, err := selfPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(self)
	tmp := filepath.Join(dir, ".F1Party.new.exe")
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("can't write to %s (run the installer from the release page instead)", dir)
	}
	c := &http.Client{Timeout: 5 * time.Minute}
	res, err := c.Get(u.exe)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	defer res.Body.Close()
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, 200<<20))
	f.Close()
	if err != nil || res.StatusCode != 200 {
		os.Remove(tmp)
		return "", fmt.Errorf("download failed (%d)", res.StatusCode)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(tmp)
		return "", errors.New("the download doesn't match the release's checksum")
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return "", err
	}
	old := self + ".old"
	os.Remove(old)
	if err := os.Rename(self, old); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, self); err != nil {
		os.Rename(old, self) // put the running version back
		return "", err
	}
	return self, nil
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
