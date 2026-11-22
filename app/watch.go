package main

// Opening a session the way the F1 Race Control Stream Deck plugin does, without the plugin. Every part is
// optional (Settings):
//   - a MultiViewer setup per session type (Race, Qualifying, Practice), chosen by name or by hand
//   - "best of the rest": the setup's onboards of drivers outside the top teams show the best-placed of
//     them, kept up to date during the session
//   - the Ultimate F1 Viewer layout that belongs to the setup, after the timing has loaded
//   - battle mode in the viewer: left the top 4, right the championship top 3 (or P5-8), kept up to date
//   - Live Timing zoomed to fill its window
//   - Close F1: MultiViewer and the viewer closed, the desktop removed, lights and taskbar back

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// setupAuto: the setup choice "a setup per session type".
const setupAuto = "@auto"

// defaultTopTeams: the teams whose onboards a setup keeps; "best of the rest" fills the others.
var defaultTopTeams = []string{"McLaren", "Ferrari", "Mercedes", "Red Bull Racing"}

func topTeams() []string {
	if t := loadConfig().TopTeams; len(t) > 0 {
		return t
	}
	return defaultTopTeams
}

// kindOf: "race", "quali" or "practice" for a session type or name ("Sprint Qualifying", "Practice 2" …).
func kindOf(sessionType string) string {
	switch k := sessionKind(sessionType); k {
	case "quali", "practice":
		return k
	}
	return "race"
}

// setupForKind: the MultiViewer setup for a session kind: chosen in Settings, else found by its name.
func setupForKind(kind string) string {
	c := loadConfig()
	names := []string{}
	for _, s := range mvSetups() {
		names = append(names, s.Name)
	}
	chosen := map[string]string{"race": c.SetupRace, "quali": c.SetupQuali, "practice": c.SetupPractice}[kind]
	if chosen != "" && slices.Contains(names, chosen) {
		return chosen
	}
	has := func(n, part string) bool { return strings.Contains(strings.ToLower(n), part) }
	for _, n := range names {
		switch {
		case kind == "practice" && (has(n, "practice") || has(n, "training")),
			kind == "quali" && has(n, "qual"),
			kind == "race" && strings.EqualFold(n, "race"):
			return n
		}
	}
	if kind == "race" {
		for _, n := range names {
			if has(n, "race") {
				return n
			}
		}
	}
	return ""
}

// resolveSetup: the MultiViewer setup a choice means for a session ("" = none).
func resolveSetup(choice, sessionType string) string {
	if choice == setupAuto {
		return setupForKind(kindOf(sessionType))
	}
	return choice
}

// openChosenSetup opens what the setup choice means for this session (a setup per session type, or one
// setup), with the best of the rest filled in when that is on; the rest follows in afterSetup.
func openChosenSetup(choice, contentID, sessionType string) error {
	if sessionType == "" {
		sessionType = mvSessionType()
	}
	name := resolveSetup(choice, sessionType)
	if name == "" {
		return fmt.Errorf("no MultiViewer setup for %s sessions: choose one in Settings", kindOf(sessionType))
	}
	kind := kindOf(sessionType)
	watchNow.mu.Lock()
	watchNow.kind, watchNow.setup, watchNow.active, watchNow.lastWanted = kind, name, false, nil
	watchNow.mu.Unlock()
	var rest []int
	c := loadConfig()
	if c.RestOn && kind != "practice" {
		if t, ok := timingNow(); ok {
			rest = t.bestOfRest(topTeams())
		}
	}
	if err := mvOpenSetupRest(name, contentID, rest); err != nil {
		return err
	}
	watchNow.mu.Lock()
	watchNow.active = true
	watchNow.mu.Unlock()
	return nil
}

// watchNow: what F1 Party opened, for the updates during the session.
var watchNow struct {
	mu         sync.Mutex
	kind       string // race | quali | practice
	setup      string
	active     bool  // a setup was opened here: only then are its onboards swapped (never ones placed by hand)
	lastWanted []int // best of the rest at the last check (a change must hold for two checks)
	changeLap  int   // race: lap a new order was first seen (0 = none)
	swapLap    int   // race: lap of the last swap
	loopOnce   sync.Once
}

// afterSetup runs once a setup is opening: wait for the timing, then the onboards, Live Timing, Ultimate F1
// Viewer and battle mode (in the background; each part only when it is on).
func afterSetup(setup, contentID string) {
	go func() {
		c := loadConfig()
		timing := false
		for i := 0; i < 25 && !timing; i++ {
			if t, ok := timingNow(); ok && len(t.byPosition()) >= 8 {
				timing = true
				break
			}
			time.Sleep(time.Second)
		}
		if timing && c.RestOn {
			updateRestSlots(true)
		}
		if c.FitTiming {
			fitLiveTiming(true)
		}
		if id := uf1LayoutFor(setup); id != "" && uf1Installed() {
			if c.Uf1CloseFirst {
				uf1CloseExtra()
			}
			if err := uf1Open(id, contentID); err != nil {
				log.Printf("ultimate f1 viewer: %v", err)
			} else {
				for i := 0; i < 8; i++ { // battle mode and crash detection open in move mode: switch it off
					time.Sleep(time.Second)
					if uf1LockWindows() == 0 {
						break
					}
				}
				watchNow.mu.Lock()
				race := watchNow.kind == "race"
				watchNow.mu.Unlock()
				if race && c.BattleOn {
					for i := 0; i < 10 && !updateBattle(); i++ {
						if i == 3 {
							uf1ReloadBattle()
							time.Sleep(3 * time.Second)
							uf1LockWindows()
						}
						time.Sleep(1500 * time.Millisecond)
					}
				}
			}
		}
		afterSetupWindows()
		watchNow.loopOnce.Do(func() { go watchLoop() })
	}()
}

// afterSetupWindows: the windows tidied up (window placement, when available on this system).
var afterSetupWindows = func() {}

// watchLoop keeps the best of the rest and battle mode current during the session.
func watchLoop() {
	for n := 0; ; n++ {
		time.Sleep(10 * time.Second)
		c := loadConfig()
		if c.RestOn && c.RestLive {
			updateRestSlots(false)
		}
		watchNow.mu.Lock()
		race, active := watchNow.kind == "race", watchNow.active
		watchNow.mu.Unlock()
		if active && race && c.BattleOn && c.BattleLive && n%3 == 0 {
			updateBattle()
		}
	}
}

// ---------------------------------------------------------------- timing

// timing: the parts of MultiViewer's live timing used here.
type timing struct {
	TimingData struct {
		Lines map[string]struct {
			Position json.RawMessage `json:"Position"`
			Retired  bool            `json:"Retired"`
			Stopped  bool            `json:"Stopped"`
			InPit    bool            `json:"InPit"`
		} `json:"Lines"`
	} `json:"TimingData"`
	DriverList map[string]struct {
		TeamName string `json:"TeamName"`
	} `json:"DriverList"`
	ChampionshipPrediction struct {
		Drivers map[string]struct {
			CurrentPosition json.RawMessage `json:"CurrentPosition"`
		} `json:"Drivers"`
	} `json:"ChampionshipPrediction"`
	LapCount struct {
		CurrentLap int `json:"CurrentLap"`
	} `json:"LapCount"`
	TrackStatus struct {
		Status string `json:"Status"`
	} `json:"TrackStatus"`
}

func timingNow() (*timing, bool) {
	var out struct {
		T *timing `json:"f1LiveTimingState"`
	}
	if err := gql(`{ f1LiveTimingState { TimingData DriverList ChampionshipPrediction LapCount TrackStatus } }`, nil, &out); err != nil || out.T == nil {
		return nil, false
	}
	return out.T, true
}

// rawInt reads a number that may come as a number or a string ("3").
func rawInt(r json.RawMessage) (int, bool) {
	s := strings.Trim(string(r), `"`)
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// byPosition: driver numbers by session position, best first.
func (t *timing) byPosition() []int {
	type dp struct{ num, pos int }
	var l []dp
	for k, v := range t.TimingData.Lines {
		n, ok1 := strconv.Atoi(k)
		p, ok2 := rawInt(v.Position)
		if ok1 == nil && ok2 && p > 0 {
			l = append(l, dp{n, p})
		}
	}
	sort.Slice(l, func(i, j int) bool { return l[i].pos < l[j].pos })
	out := make([]int, len(l))
	for i, x := range l {
		out[i] = x.num
	}
	return out
}

// byChampionship: driver numbers by championship position (races), best first.
func (t *timing) byChampionship() []int {
	type dp struct{ num, pos int }
	var l []dp
	for k, v := range t.ChampionshipPrediction.Drivers {
		n, err := strconv.Atoi(k)
		p, ok := rawInt(v.CurrentPosition)
		if err == nil && ok && p > 0 {
			l = append(l, dp{n, p})
		}
	}
	sort.Slice(l, func(i, j int) bool { return l[i].pos < l[j].pos })
	out := make([]int, len(l))
	for i, x := range l {
		out[i] = x.num
	}
	return out
}

// bestOfRest: the best-placed drivers outside the top teams (still running), best first.
func (t *timing) bestOfRest(top []string) []int {
	var out []int
	for _, n := range t.byPosition() {
		line := t.TimingData.Lines[strconv.Itoa(n)]
		team := t.DriverList[strconv.Itoa(n)].TeamName
		if line.Retired || line.Stopped || slices.ContainsFunc(top, func(x string) bool { return strings.EqualFold(x, team) }) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ---------------------------------------------------------------- best of the rest during the session

type slotPlayer struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	DriverData *struct {
		DriverNumber *int   `json:"driverNumber"`
		TeamName     string `json:"teamName"`
	} `json:"driverData"`
	StreamData *struct {
		ContentID string `json:"contentId"`
	} `json:"streamData"`
	State *struct {
		Muted  bool    `json:"muted"`
		Volume float64 `json:"volume"`
	} `json:"state"`
	Bounds struct{ X, Y, Width, Height float64 } `json:"bounds"`
}

// updateRestSlots swaps the onboards of the setup's best-of-the-rest slots when the best drivers outside the
// top teams change: at once when force (right after opening), otherwise only when the change holds (race:
// 3 laps, at most one swap per 5 laps, never under a safety car or red flag; qualifying: only a driver back
// in the pits is swapped). Practice keeps the setup's drivers.
func updateRestSlots(force bool) {
	watchNow.mu.Lock()
	defer watchNow.mu.Unlock()
	if !watchNow.active || watchNow.kind == "practice" {
		return
	}
	var out struct {
		Players []slotPlayer `json:"players"`
	}
	if gql(`{ players { id type state { muted volume } driverData { driverNumber teamName } streamData { contentId } bounds { x y width height } } }`, nil, &out) != nil {
		return
	}
	top := topTeams()
	var slots []slotPlayer
	for _, p := range out.Players {
		if p.Type == "OBC" && p.DriverData != nil && p.DriverData.DriverNumber != nil &&
			!slices.ContainsFunc(top, func(x string) bool { return strings.EqualFold(x, p.DriverData.TeamName) }) {
			slots = append(slots, p)
		}
	}
	if len(slots) == 0 {
		return
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].Bounds.Y != slots[j].Bounds.Y {
			return slots[i].Bounds.Y < slots[j].Bounds.Y
		}
		return slots[i].Bounds.X < slots[j].Bounds.X
	})
	t, ok := timingNow()
	if !ok {
		return
	}
	wanted := t.bestOfRest(top)
	if len(wanted) < len(slots) {
		watchNow.lastWanted, watchNow.changeLap = nil, 0
		return
	}
	wanted = wanted[:len(slots)]
	same := func(a, b []int) bool {
		if len(a) != len(b) {
			return false
		}
		for _, x := range a {
			if !slices.Contains(b, x) {
				return false
			}
		}
		return true
	}
	stable := force || (watchNow.lastWanted != nil && same(watchNow.lastWanted, wanted))
	watchNow.lastWanted = wanted
	if !stable {
		watchNow.changeLap = 0
		return
	}
	shown := make([]int, len(slots))
	for i, s := range slots {
		shown[i] = *s.DriverData.DriverNumber
	}
	if same(shown, wanted) {
		watchNow.changeLap = 0
		return
	}
	if !force && watchNow.kind == "race" {
		lap := t.LapCount.CurrentLap
		switch t.TrackStatus.Status {
		case "4", "5", "6", "7":
			return
		}
		if watchNow.changeLap == 0 {
			watchNow.changeLap = lap
			return
		}
		if lap < watchNow.changeLap+3 || (watchNow.swapLap > 0 && lap < watchNow.swapLap+5) {
			return
		}
		watchNow.swapLap = lap
	}
	watchNow.changeLap = 0
	// a driver still wanted keeps their slot; only a slot whose driver dropped out gets a newcomer
	var newcomers []int
	for _, w := range wanted {
		if !slices.Contains(shown, w) {
			newcomers = append(newcomers, w)
		}
	}
	for i, s := range slots {
		if slices.Contains(wanted, shown[i]) || len(newcomers) == 0 {
			continue
		}
		if !force && watchNow.kind == "quali" && !t.TimingData.Lines[strconv.Itoa(shown[i])].InPit {
			continue // qualifying: never during a lap
		}
		swapDriver(s, newcomers[0])
		newcomers = newcomers[1:]
	}
}

// swapDriver replaces an onboard with another driver's in exactly the same place, at the same moment and
// with the same sound.
func swapDriver(old slotPlayer, driver int) {
	if old.StreamData == nil {
		return
	}
	var out struct {
		ID string `json:"playerCreate"`
	}
	err := gql(`mutation($c: ID!, $d: Int, $x: Int, $y: Int, $w: Int, $h: Int) { playerCreate(input: { contentId: $c, driverNumber: $d,
		bounds: { x: $x, y: $y, width: $w, height: $h }, maintainAspectRatio: true }) }`,
		map[string]any{"c": old.StreamData.ContentID, "d": driver, "x": int(old.Bounds.X), "y": int(old.Bounds.Y),
			"w": int(old.Bounds.Width), "h": int(old.Bounds.Height)}, &out)
	if err != nil || out.ID == "" {
		log.Printf("best of the rest: new onboard not made: %v", err)
		return
	}
	time.Sleep(1500 * time.Millisecond)
	if ps, err := players(); err == nil {
		for _, p := range ps {
			if p.ID == old.ID {
				if at, ok := p.Time(); ok {
					_ = playerSeek(out.ID, at)
				}
			}
		}
	}
	if old.State != nil {
		_ = gql(`mutation($id: ID!, $m: Boolean) { playerSetMuted(id: $id, muted: $m) }`, map[string]any{"id": out.ID, "m": old.State.Muted}, nil)
		_ = gql(`mutation($id: ID!, $v: Float) { playerSetVolume(id: $id, volume: $v) }`, map[string]any{"id": out.ID, "v": old.State.Volume}, nil)
	}
	_ = gql(`mutation($id: ID!) { playerDelete(id: $id) }`, map[string]any{"id": old.ID}, nil)
}

// ---------------------------------------------------------------- Live Timing, battle mode

const fitTimingJS = `(() => {
  const root = document.documentElement;
  root.style.zoom = '1';
  const h = innerHeight;
  for (const bar of document.querySelectorAll('.MuiToolbar-root'))
    if (bar.getBoundingClientRect().top > h / 2) bar.style.display = 'none';
  let bottom = 0;
  for (const el of document.body.querySelectorAll('*')) {
    if (el.children.length) continue;
    const r = el.getBoundingClientRect();
    if (r.width > 0 && r.height > 0 && r.height < h / 2 && (el.textContent || '').trim()) bottom = Math.max(bottom, r.bottom);
  }
  if (bottom < h / 2) return 'empty';
  const z = Math.min(1.4, Math.max(1, (h - 4) / bottom));
  root.style.zoom = String(z);
  return 'zoom ' + z.toFixed(3);
})()`

const unfitTimingJS = `(() => { document.documentElement.style.zoom = '1';
  for (const bar of document.querySelectorAll('.MuiToolbar-root')) bar.style.display = ''; return 'normal'; })()`

// fitLiveTiming zooms MultiViewer's Live Timing windows until the rows reach the bottom (at most 40% larger);
// fit false undoes it.
func fitLiveTiming(fit bool) {
	pages, err := cdpPages()
	if err != nil {
		return
	}
	js := fitTimingJS
	if !fit {
		js = unfitTimingJS
	}
	for _, p := range pages {
		if strings.HasPrefix(p.Title, "Live Timing") || strings.HasPrefix(p.Title, "Replay Live Timing") {
			_, _ = cdpEval(p, js, 5*time.Second)
		}
	}
}

// updateBattle: battle mode left = the session's top 4, right = the championship top 3 (or P5-8). False while
// its windows haven't loaded their driver list yet.
func updateBattle() bool {
	t, ok := timingNow()
	if !ok {
		return false
	}
	pos := t.byPosition()
	if len(pos) < 8 {
		return false
	}
	right := pos[4:8]
	if champ := t.byChampionship(); loadConfig().BattleRight != "p5to8" && len(champ) >= 3 {
		right = champ[:3]
	}
	return uf1SetBattle(pos[:4], right)
}

// ---------------------------------------------------------------- Close F1

// closeF1: MultiViewer and Ultimate F1 Viewer closed (as set), the watching desktop removed or left, the
// lights and the taskbar back.
func closeF1() {
	c := loadConfig()
	stopLights()
	if !c.CloseUf1Off && !c.Uf1Off {
		killApp(uf1Process)
	}
	if !c.CloseMVOff {
		mvQuit(false)
	}
	time.Sleep(500 * time.Millisecond)
	leaveF1Desktop(c.CloseDesktop)
	taskbarRestore()
	watchNow.mu.Lock()
	watchNow.active = false
	watchNow.mu.Unlock()
}

// stopLights: the Hue lights back as they were (when lights are available).
var stopLights = func() {}

func killApp(process string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("taskkill", "/IM", process+".exe")
	case "darwin":
		cmd = exec.Command("osascript", "-e", `quit app "`+process+`"`)
	default:
		cmd = exec.Command("pkill", "-f", strings.ToLower(process))
	}
	detach(cmd)
	_ = cmd.Run()
}
