package main

// Ultimate F1 Viewer's own windows: closing them before a layout, switching off "move mode", and battle mode's
// drivers (see watch.go for when these run).

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// uf1Pages: the viewer's windows other than its main window.
func uf1Pages() []cdpPage {
	pages, err := cdpTargets(uf1Port)
	if err != nil {
		return nil
	}
	var out []cdpPage
	for _, p := range pages {
		if !strings.Contains(p.URL, "/main/index.html") {
			out = append(out, p)
		}
	}
	return out
}

// uf1CloseExtra closes the viewer's open windows (not its main window, not auto camera switching), so a
// layout doesn't open on top of another one.
func uf1CloseExtra() {
	for _, p := range uf1Pages() {
		if p.Title != "Auto Onboard Camera Switching" && p.ID != "" {
			if res, err := cdpHTTP.Get(fmt.Sprintf("http://127.0.0.1:%d/json/close/%s", uf1Port, p.ID)); err == nil {
				res.Body.Close()
			}
		}
	}
}

// uf1LockWindows: battle mode and crash detection open in "move mode"; this switches it off (as Escape
// does). Returns how many windows were still loading.
func uf1LockWindows() int {
	const js = `(() => { const b = document.getElementById('background');
  if (!b || typeof toggleMoveMode !== 'function') return document.readyState === 'complete' ? 'none' : 'loading';
  if (b.classList.contains('drag')) { toggleMoveMode(); return 'locked'; } return 'ok'; })()`
	loading := 0
	for _, p := range uf1Pages() {
		if r := cdpEvalString(p, js, 5*time.Second); r == "loading" || r == "" {
			loading++
		}
	}
	return loading
}

func uf1Battle() []cdpPage {
	var out []cdpPage
	for _, p := range uf1Pages() {
		if strings.Contains(p.URL, "/battlemode/") {
			out = append(out, p)
		}
	}
	return out
}

// uf1ReloadBattle: a battle mode window that loaded before the timing existed gives up; a reload starts it over.
func uf1ReloadBattle() {
	for _, p := range uf1Battle() {
		_, _ = cdpEval(p, "location.reload(); true", 5*time.Second)
	}
}

// uf1SetBattle shows exactly these drivers in the battle mode windows (the leftmost window gets left, the
// rightmost right), by clicking the windows' own driver buttons. False while one isn't ready.
// want(i, shown) gives window i (from the left) its drivers, best first; shown is how many drivers the
// windows before it ended up with, so a next window can carry on from there. A window takes as many as
// fit: it adds them one by one and stops at the first that doesn't fit (it remembers that for its width).
func uf1SetBattle(want func(i, shown int) []int) bool {
	pages := uf1Battle()
	if len(pages) == 0 {
		return true
	}
	type placed struct {
		p cdpPage
		x float64
	}
	var ps []placed
	for _, p := range pages {
		v, err := cdpEval(p, "window.screenX", 5*time.Second)
		if err != nil {
			continue
		}
		x, _ := strconv.ParseFloat(string(v), 64)
		ps = append(ps, placed{p, x})
	}
	slices.SortFunc(ps, func(a, b placed) int { return int(a.x - b.x) })
	ok, shown := true, 0
	for i, pl := range ps {
		w, _ := json.Marshal(want(i, shown))
		r := cdpEvalString(pl.p, battleFitJS+"("+string(w)+")", 10*time.Second)
		n, err := strconv.Atoi(strings.TrimPrefix(r, "ok:"))
		if !strings.HasPrefix(r, "ok:") || err != nil {
			ok = false
			continue
		}
		shown += n
	}
	return ok
}

// battleFitJS shows the wanted drivers in one battle mode window, as many as fit, and answers "ok:<count>".
// The first time (and after the window changed size) it adds them one by one and checks after each whether
// everything still fits inside the window; it keeps that number for the next updates, so those only swap
// drivers without trying again.
const battleFitJS = `(async want => {
  if (typeof toggleDriver !== 'function' || typeof addButtons !== 'function' || typeof driverList === 'undefined' || !driverList) return 'not-ready';
  const missing = () => want.some(n => !document.getElementById('button' + n));
  if (missing()) { document.querySelectorAll('#buttons .driver-button').forEach(b => b.remove()); addButtons(); }
  if (missing()) return 'no-buttons';
  const shownNow = () => [...document.querySelectorAll('.driver')].map(e => parseInt(e.id.replace('driver', '')));
  const frame = () => new Promise(r => requestAnimationFrame(() => setTimeout(r, 30)));
  const fits = () => {
    const W = innerWidth + 1, H = innerHeight + 1;
    if (document.documentElement.scrollWidth > W || document.documentElement.scrollHeight > H) return false;
    return [...document.querySelectorAll('.driver')].every(e => { const r = e.getBoundingClientRect(); return r.width > 0 && r.right <= W && r.bottom <= H; });
  };
  const size = innerWidth + 'x' + innerHeight;
  let cap = window.__f1pCap && window.__f1pCap.size === size ? window.__f1pCap.n : 0;
  if (!cap) {
    // try: start empty, add one at a time, stop at the first that doesn't fit (at least one always shows)
    shownNow().forEach(n => toggleDriver(n));
    await frame();
    cap = 0;
    for (const n of want) {
      toggleDriver(n); await frame();
      if (cap > 0 && !fits()) { toggleDriver(n); await frame(); break; }
      cap++;
    }
    window.__f1pCap = { size, n: cap };
    return 'ok:' + cap;
  }
  const keep = want.slice(0, cap), shown = shownNow();
  shown.filter(n => !keep.includes(n)).forEach(n => toggleDriver(n));
  keep.filter(n => !shown.includes(n)).forEach(n => toggleDriver(n));
  return 'ok:' + keep.length;
})`
