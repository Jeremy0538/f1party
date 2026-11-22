package main

// The Settings window (the gear at the top right): what it shows and what saving does. Every option is
// optional; a missing app (Ultimate F1 Viewer) or system feature (desktops, taskbar: Windows only) is
// simply not offered.

import (
	"runtime"
	"slices"
	"strings"
)

// settingsView: everything the Settings tabs show.
func settingsView() map[string]any {
	c := loadConfig()
	name := c.DesktopName
	if name == "" {
		name = f1DesktopName
	}
	setups := []string{}
	for _, s := range mvSetups() {
		setups = append(setups, s.Name)
	}
	v := map[string]any{
		"windows": runtime.GOOS == "windows",
		"desktops": desktopsSupported(),
		// General
		"name": c.Name, "desktop": !c.DesktopOff, "desktopName": name, "taskbar": c.Taskbar, "autoSync": !c.AutoSyncOff,
		// Sessions
		"openWith": openWithOf(c.Setup), "plugin": pluginInfo(),
		"setups": setups, "setupRace": c.SetupRace, "setupQuali": c.SetupQuali, "setupPractice": c.SetupPractice,
		"autoRace": setupForKind("race"), "autoQuali": setupForKind("quali"), "autoPractice": setupForKind("practice"),
		"uf1": uf1Installed(), "uf1On": !c.Uf1Off, "uf1Layouts": uf1Layouts(), "uf1For": c.Uf1For, "uf1CloseFirst": c.Uf1CloseFirst,
		"fitTiming": c.FitTiming,
		// Drivers
		"restOn": c.RestOn, "restLive": c.RestLive, "topTeams": topTeams(), "teams": allTeams(),
		"keepDrivers": keepDrivers(), "drivers": allDrivers(), "keepBy": keepBy(),
		"battleOn": c.BattleOn, "battleLaps": battleLaps(), "battleRight": c.BattleRight,
		// Close F1
		"closeMv": !c.CloseMVOff, "closeUf1": !c.CloseUf1Off, "closeDesktop": c.CloseDesktop,
	}
	if settingsExtra != nil {
		settingsExtra(v)
	}
	return v
}

// settingsExtra / settingsApplyExtra: parts that live in their own files (lights, window places) add theirs.
var (
	settingsExtra      func(v map[string]any)
	settingsApplyExtra func(in map[string]any)
)

// settingsIn: what the Settings window sends; only the fields it sends change.
type settingsIn struct {
	Name          *string
	Desktop       *bool
	DesktopName   *string
	Taskbar       *string
	AutoSync      *bool
	OpenWith      *string
	SetupRace     *string
	SetupQuali    *string
	SetupPractice *string
	Uf1For        map[string]string
	Uf1On         *bool
	Uf1CloseFirst *bool
	FitTiming     *bool
	RestOn        *bool
	RestLive      *bool
	TopTeams      []string
	KeepDrivers   []string
	KeepBy        *string
	BattleOn      *bool
	BattleLaps    *int
	BattleRight   *string
	CloseMv       *bool
	CloseUf1      *bool
	CloseDesktop  *bool
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// applySettings saves what the Settings window sent.
func applySettings(in settingsIn, party *Party) {
	taskbarOff := false
	updateConfig(func(c *config) {
		set := func(dst *bool, v *bool, invert bool) {
			if v != nil {
				*dst = *v != invert
			}
		}
		if in.Name != nil {
			c.Name = cleanName(*in.Name)
		}
		set(&c.DesktopOff, in.Desktop, true)
		if in.DesktopName != nil {
			c.DesktopName = clip(*in.DesktopName, 40)
		}
		if in.Taskbar != nil {
			c.Taskbar = *in.Taskbar
			if c.Taskbar != "main" && c.Taskbar != "autohide" {
				c.Taskbar, taskbarOff = "", true
			}
		}
		set(&c.AutoSyncOff, in.AutoSync, true)
		if in.OpenWith != nil {
			c.Setup = clip(*in.OpenWith, 100)
			if c.Setup == setupAuto {
				c.Setup = ""
			}
		}
		for dst, v := range map[*string]*string{&c.SetupRace: in.SetupRace, &c.SetupQuali: in.SetupQuali, &c.SetupPractice: in.SetupPractice} {
			if v != nil {
				*dst = clip(*v, 100)
			}
		}
		if in.Uf1For != nil {
			c.Uf1For = map[string]string{}
			for k, v := range in.Uf1For {
				if k != "" && v != "" {
					c.Uf1For[clip(k, 100)] = clip(v, 20)
				}
			}
		}
		set(&c.Uf1Off, in.Uf1On, true)
		set(&c.Uf1CloseFirst, in.Uf1CloseFirst, false)
		set(&c.FitTiming, in.FitTiming, false)
		set(&c.RestOn, in.RestOn, false)
		set(&c.RestLive, in.RestLive, false)
		if in.TopTeams != nil {
			c.TopTeams, c.TopTeamsSet = nil, true
			for _, t := range in.TopTeams {
				if t = clip(t, 40); t != "" && len(c.TopTeams) < 10 {
					c.TopTeams = append(c.TopTeams, t)
				}
			}
		}
		if in.KeepBy != nil {
			c.KeepBy = ""
			if *in.KeepBy == "drivers" {
				c.KeepBy = "drivers"
			}
		}
		if in.KeepDrivers != nil {
			c.KeepDrivers = nil
			for _, d := range in.KeepDrivers {
				if d = strings.ToUpper(clip(d, 3)); d != "" && len(c.KeepDrivers) < 30 && !slices.Contains(c.KeepDrivers, d) {
					c.KeepDrivers = append(c.KeepDrivers, d)
				}
			}
		}
		set(&c.BattleOn, in.BattleOn, false)
		if in.BattleLaps != nil {
			c.BattleLaps, c.BattleLive = max(0, min(20, *in.BattleLaps)), false
			if c.BattleLaps == 0 {
				c.BattleLaps = -1 // only when the layout opens
			}
		}
		if in.BattleRight != nil {
			c.BattleRight = ""
			if *in.BattleRight == "p5to8" {
				c.BattleRight = "p5to8"
			}
		}
		set(&c.CloseMVOff, in.CloseMv, true)
		set(&c.CloseUf1Off, in.CloseUf1, true)
		set(&c.CloseDesktop, in.CloseDesktop, false)
	})
	if taskbarOff {
		taskbarRestore() // switched off: put back what F1 Party changed
	}
	if in.AutoSync != nil {
		party.SetAutoSync(*in.AutoSync)
	}
	if in.OpenWith != nil {
		party.SetSetup(setupChoice(loadConfig().Setup))
	}
	if in.FitTiming != nil {
		go fitLiveTiming(*in.FitTiming)
	}
}
