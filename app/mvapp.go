package main

// MultiViewer as an app: finding and starting it, its own saved setups, and opening a setup through the
// remote-debugging port MultiViewer is started with (the same way its own "Open setup" dialog does:
// window.f1mv.setupManager.restoreSetup in the session window).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Same port as the F1 Race Control Stream Deck plugin, so both can use one MultiViewer.
const mvDebugPortDefault = 9334

var errNoDebug = errors.New("MultiViewer runs without setup support: restart it from F1 Party")

func mvConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "MultiViewer")
}

// MVSetup: one of MultiViewer's own saved setups.
type MVSetup struct {
	Name    string `json:"name"`
	Players int    `json:"players"`
}

func mvSetups() []MVSetup {
	b, err := os.ReadFile(filepath.Join(mvConfigDir(), "setups.config.json"))
	if err != nil {
		return nil
	}
	var f struct {
		Setups []struct {
			Name    string `json:"name"`
			Windows []struct {
				Type string `json:"type"`
			} `json:"windows"`
		} `json:"setups"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	out := make([]MVSetup, 0, len(f.Setups))
	for _, s := range f.Setups {
		n := 0
		for _, w := range s.Windows {
			if w.Type == "f1tv.player" {
				n++
			}
		}
		out = append(out, MVSetup{Name: s.Name, Players: n})
	}
	return out
}

// ---------------------------------------------------------------- finding and starting MultiViewer

// mvExe: the installed MultiViewer itself. On Windows that is the newest app-x.y.z\MultiViewer.exe, not
// the updater stub in front of it, so the debug-port argument reaches the app.
func mvExe() string {
	switch runtime.GOOS {
	case "windows":
		root := filepath.Join(os.Getenv("LOCALAPPDATA"), "multiviewer")
		dirs, _ := filepath.Glob(filepath.Join(root, "app-*"))
		slices.SortFunc(dirs, func(a, b string) int { return compareVersions(filepath.Base(b)[4:], filepath.Base(a)[4:]) })
		for _, d := range dirs {
			if p := filepath.Join(d, "MultiViewer.exe"); fileExists(p) {
				return p
			}
		}
		if p := filepath.Join(root, "MultiViewer.exe"); fileExists(p) {
			return p
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		for _, p := range []string{"/Applications/MultiViewer.app", filepath.Join(home, "Applications", "MultiViewer.app")} {
			if fileExists(p) {
				return p
			}
		}
	default:
		if p, err := exec.LookPath("multiviewer"); err == nil {
			return p
		}
		for _, p := range []string{"/usr/bin/multiviewer-for-f1", "/opt/f1multiviewer-bin/multiviewer", "/opt/MultiViewer/multiviewer", "/opt/multiviewer/multiviewer",
			"/usr/lib/multiviewer/multiviewer", "/usr/lib/multiviewer-for-f1/multiviewer-for-f1"} {
			if fileExists(p) {
				return p
			}
		}
	}
	return ""
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// compareVersions compares "2.17.2"-style versions numerically.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(as), len(bs)); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

var (
	mvStartMu sync.Mutex
	mvStartAt time.Time // the last start; MultiViewer takes a while to answer, so no second start meanwhile
)

// mvStart starts MultiViewer with its debug port. It does nothing when MultiViewer already runs or is starting.
func mvStart() error {
	mvStartMu.Lock()
	defer mvStartMu.Unlock()
	if mvRunning() || time.Since(mvStartAt) < 30*time.Second {
		return nil
	}
	exe := mvExe()
	if exe == "" {
		return errors.New("MultiViewer isn't installed (multiviewer.app/download)")
	}
	arg := fmt.Sprintf("--remote-debugging-port=%d", mvDebugPortDefault)
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-a", exe, "--args", arg)
	} else {
		cmd = exec.Command(exe, arg)
		cmd.Dir = filepath.Dir(exe)
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("can't start MultiViewer: %w", err)
	}
	go cmd.Wait()
	mvStartAt = time.Now()
	return nil
}

// mvStartReady starts MultiViewer when it isn't running and waits (up to a minute) until its main window can
// open setups. A MultiViewer that was already running without its debug port needs "Restart for setups".
func mvStartReady() error {
	wasRunning := mvRunning()
	if !wasRunning {
		if err := mvStart(); err != nil {
			return err
		}
	}
	mainUp := func() bool {
		pages, err := cdpPages()
		return err == nil && slices.ContainsFunc(pages, isMainWindow)
	}
	for i := 0; i < 60; i++ {
		if mvRunning() && mainUp() {
			if !wasRunning {
				time.Sleep(2 * time.Second) // its setup manager loads right after the window
			}
			return nil
		}
		if wasRunning && !mvDebugReady() {
			return errNoDebug
		}
		time.Sleep(time.Second)
	}
	if !mvRunning() {
		return errors.New("MultiViewer didn't start")
	}
	return errNoDebug
}

// mvRestart closes MultiViewer and starts it again with its debug port (needed for setups).
func mvRestart() error {
	if mvExe() == "" {
		return errors.New("MultiViewer isn't installed (multiviewer.app/download)")
	}
	mvQuit(false)
	for i := 0; i < 20 && mvRunning(); i++ {
		time.Sleep(500 * time.Millisecond)
	}
	if mvRunning() {
		mvQuit(true)
		time.Sleep(1500 * time.Millisecond)
	}
	mvStartMu.Lock()
	mvStartAt = time.Time{} // closed on purpose: start again now
	mvStartMu.Unlock()
	return mvStart()
}

func mvQuit(force bool) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		args := []string{"/IM", "MultiViewer.exe"}
		if force {
			args = append(args, "/F")
		}
		cmd = exec.Command("taskkill", args...)
	case "darwin":
		if force {
			cmd = exec.Command("pkill", "-9", "-x", "MultiViewer")
		} else {
			cmd = exec.Command("osascript", "-e", `quit app "MultiViewer"`)
		}
	default:
		sig := "-TERM"
		if force {
			sig = "-KILL"
		}
		// Only the MultiViewer program itself, not every command line that mentions the word.
		cmd = exec.Command("pkill", sig, "-f", `(^|/)multiviewer(-for-f1)?( |$)`)
	}
	detach(cmd)
	_ = cmd.Run()
}

// mvRunning: MultiViewer answers on its local API.
func mvRunning() bool {
	_, err := gqlVersion()
	return err == nil
}

func gqlVersion() (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	err := gql(`{ version }`, nil, &out)
	return out.Version, err
}

// ---------------------------------------------------------------- debug port (Chrome DevTools Protocol)

var cdpHTTP = &http.Client{Timeout: 2 * time.Second}

// mvDebugPort: the port MultiViewer's debug server listens on now (it writes it to DevToolsActivePort).
func mvDebugPort() int {
	if b, err := os.ReadFile(filepath.Join(mvConfigDir(), "DevToolsActivePort")); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])); err == nil && n > 0 {
			return n
		}
	}
	return mvDebugPortDefault
}

type cdpPage struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
	WS    string `json:"webSocketDebuggerUrl"`
}

func cdpPages() ([]cdpPage, error) {
	res, err := cdpHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", mvDebugPort()))
	if err != nil {
		return nil, errNoDebug
	}
	defer res.Body.Close()
	var all []cdpPage
	if err := json.NewDecoder(res.Body).Decode(&all); err != nil {
		return nil, err
	}
	pages := all[:0]
	for _, p := range all {
		if p.Type == "page" && p.WS != "" && strings.HasPrefix(p.URL, "http://localhost:10101/") {
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		return nil, errNoDebug
	}
	return pages, nil
}

// cdpTargets: the pages of a Chromium app listening on a debug port (Ultimate F1 Viewer).
func cdpTargets(port int) ([]cdpPage, error) {
	res, err := cdpHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var all []cdpPage
	if err := json.NewDecoder(res.Body).Decode(&all); err != nil {
		return nil, err
	}
	pages := all[:0]
	for _, p := range all {
		if p.Type == "page" && p.WS != "" {
			pages = append(pages, p)
		}
	}
	return pages, nil
}

// mvDebugReady: MultiViewer runs with its debug port, so setups can be opened.
func mvDebugReady() bool { _, err := cdpPages(); return err == nil }

// cdpEval runs JavaScript in a page and returns its (JSON) result.
func cdpEval(page cdpPage, js string, timeout time.Duration) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c, _, err := websocket.Dial(ctx, page.WS, nil)
	if err != nil {
		return nil, err
	}
	defer c.CloseNow()
	c.SetReadLimit(8 << 20)
	req := map[string]any{"id": 1, "method": "Runtime.evaluate",
		"params": map[string]any{"expression": js, "awaitPromise": true, "returnByValue": true}}
	if err := wsjson.Write(ctx, c, req); err != nil {
		return nil, err
	}
	for {
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Result struct {
					Value json.RawMessage `json:"value"`
				} `json:"result"`
				ExceptionDetails *struct {
					Text string `json:"text"`
				} `json:"exceptionDetails"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := wsjson.Read(ctx, c, &msg); err != nil {
			return nil, err
		}
		if msg.ID != 1 {
			continue
		}
		if msg.Error != nil {
			return nil, errors.New(msg.Error.Message)
		}
		if e := msg.Result.ExceptionDetails; e != nil {
			return nil, errors.New(e.Text)
		}
		return msg.Result.Result.Value, nil
	}
}

func cdpEvalString(page cdpPage, js string, timeout time.Duration) string {
	v, err := cdpEval(page, js, timeout)
	if err != nil {
		return ""
	}
	var s string
	_ = json.Unmarshal(v, &s)
	return s
}

// The session page's props (contentId, streams, sessionInfo), found through React. Shared by the probe and
// the restore script.
const findSessionProps = `
const findSessionProps = () => {
  const seen = new Set();
  for (const el of document.querySelectorAll('button, [role=button], a')) {
    const k = Object.keys(el).find(k => k.startsWith('__reactFiber$'));
    for (let f = k && el[k]; f; f = f.return) {
      if (seen.has(f)) break; seen.add(f);
      const p = f.memoizedProps;
      if (p && p.contentId && Array.isArray(p.streams)) return p;
    }
  }
  return null;
};`

const probeJS = `(() => {` + findSessionProps + `
  const p = findSessionProps(); return p ? String(p.contentId) : '';
})()`

// restoreJS: opens setup __NAME__ for the session page's content. Returns "ok", "no-setup", "no-session",
// "no-api" or "error: …".
const restoreJS = `(async () => {` + findSessionProps + `
  const name = __NAME__, rest = __REST__, topTeams = __TOP__.map(t => t.toLowerCase()), keep = __KEEP__.map(t => t.toLowerCase());
  if (!window.f1mv || !window.f1mv.setupManager) return 'no-api';
  const setups = await window.f1mv.setupManager.getSetups();
  const setup = (setups || []).find(s => (s.name || '').toLowerCase() === name.toLowerCase());
  if (!setup) return 'no-setup';
  const props = findSessionProps();
  if (!props) return 'no-session';
  // MultiViewer 2.17+ checks this payload and requires f1.isLive (a boolean).
  const payload = { f1: { sessionInfo: props.sessionInfo, isLive: props.isLive === true },
                    f1tv: { contentId: props.contentId, streams: props.streams } };
  const copy = JSON.parse(JSON.stringify(setup));
  // MultiViewer saves video quality as { bitrate, width, height } but 2.17 refuses that when a player
  // starts (it wants a string); the player then never registers. Leave it out.
  for (const w of copy.windows) {
    const ps = w.state && w.state.playerState;
    if (ps && ps.videoQuality !== undefined && typeof ps.videoQuality !== 'string') delete ps.videoQuality;
  }
  // best of the rest: the onboards of drivers outside the top teams show the best-placed of them
  if (rest.length) {
    const slots = copy.windows
      .filter(w => w.type === 'f1tv.player' && w.state && w.state.driverData &&
                   !topTeams.includes(String(w.state.driverData.teamName || '').toLowerCase()) &&
                   !keep.includes(String(w.state.driverData.tla || '').toLowerCase()))
      .sort((a, b) => (a.windowState.bounds.y - b.windowState.bounds.y) || (a.windowState.bounds.x - b.windowState.bounds.x));
    slots.forEach((w, i) => {
      const s = props.streams.find(s => String(s.racingNumber) === String(rest[i]));
      if (!s) return;
      w.state.driverData = { ...w.state.driverData, driverNumber: s.racingNumber, tla: s.title, firstName: undefined, lastName: undefined };
      w.state.streamData = { ...(w.state.streamData || {}), title: s.title, channelId: s.channelId };
    });
  }
  // a setup replaces its own windows but leaves other timing windows open: close the ones it doesn't have
  try {
    const keep = new Set(copy.windows.map(w => w.type));
    const open = new Set((await window.f1mv.setupManager.getWindows()).map(w => w.type));
    for (const t of open)
      if (t.startsWith('f1.live-timing.') && !keep.has(t)) await window.f1mv.setupManager.closeWindowsOfType(t);
  } catch (e) { /* older MultiViewer: just open the setup */ }
  try { await window.f1mv.setupManager.restoreSetup(copy, payload); }
  catch (e) { return 'error: ' + ((e && e.message) || e); }
  return 'ok';
})()`

// isMainWindow: MultiViewer's main window (home or a session page), not a player or live timing window.
func isMainWindow(p cdpPage) bool {
	return !strings.Contains(p.URL, "setupWindowState") && !strings.Contains(p.URL, "#/player") &&
		!strings.Contains(p.URL, "#/live-timing")
}

// mvOpenSetup opens one of MultiViewer's setups for a session. With contentID it first opens that session
// in MultiViewer's main window when it isn't open yet; without, it uses the session that is open.
func mvOpenSetup(name, contentID string) error { return mvOpenSetupRest(name, contentID, nil) }

// mvOpenSetupRest: mvOpenSetup with the best-of-the-rest drivers (best first) for the setup's other onboards.
func mvOpenSetupRest(name, contentID string, rest []int) error {
	toF1Desktop() // the setup's windows land on the F1 desktop when there is one
	pages, err := cdpPages()
	if err != nil {
		return err
	}
	session := func() (cdpPage, bool) {
		pages, _ := cdpPages()
		for _, p := range pages {
			if !isMainWindow(p) {
				continue
			}
			if id := cdpEvalString(p, probeJS, 3*time.Second); id != "" && (contentID == "" || id == contentID) {
				return p, true
			}
		}
		return cdpPage{}, false
	}
	page, ok := session()
	if !ok && contentID == "" {
		return errors.New("open a session in MultiViewer first")
	}
	if !ok {
		var main *cdpPage
		for i := range pages {
			if isMainWindow(pages[i]) {
				main = &pages[i]
				break
			}
		}
		if main == nil {
			return errors.New("MultiViewer's main window isn't open")
		}
		if _, err := cdpEval(*main, fmt.Sprintf("location.hash = '#/content/%s'; true", jsString(contentID)), 5*time.Second); err != nil {
			return fmt.Errorf("can't open the session: %w", err)
		}
		for i := 0; i < 30 && !ok; i++ {
			time.Sleep(500 * time.Millisecond)
			page, ok = session()
		}
		if !ok {
			return errors.New("the session didn't open in MultiViewer")
		}
	}
	nameJSON, _ := json.Marshal(name)
	if rest == nil {
		rest = []int{}
	}
	restJSON, _ := json.Marshal(rest)
	top, keep := keptNow()
	if top == nil {
		top = []string{}
	}
	if keep == nil {
		keep = []string{}
	}
	topJSON, _ := json.Marshal(top)
	keepJSON, _ := json.Marshal(keep)
	js := strings.NewReplacer("__NAME__", string(nameJSON), "__REST__", string(restJSON), "__TOP__", string(topJSON),
		"__KEEP__", string(keepJSON)).Replace(restoreJS)
	v, err := cdpEval(page, js, 60*time.Second)
	if errors.Is(err, context.DeadlineExceeded) {
		afterSetup(name, contentID)
		return nil // still opening windows; that carries on by itself
	}
	if err != nil {
		return err
	}
	var r string
	_ = json.Unmarshal(v, &r)
	switch {
	case r == "ok":
		afterSetup(name, contentID)
		return nil
	case r == "no-setup":
		return fmt.Errorf("MultiViewer has no setup called %q", name)
	case r == "no-session":
		return errors.New("open a session in MultiViewer first")
	case r == "no-api":
		return errors.New("this MultiViewer version can't open setups from outside")
	default:
		return errors.New("MultiViewer refused the setup: " + strings.TrimPrefix(r, "error: "))
	}
}

// jsString keeps a content id safe inside a single-quoted JavaScript string.
func jsString(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, s)
}
