package main

// Notifications from the calendar: a system notification 15 minutes before a session you follow, and before
// a party you planned. You choose which sessions in the calendar: none, races and qualifyings, or every
// session, plus single sessions with their bell. Clicking a notification brings the app to the front.

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"
)

// Notification moments (minutes before the start; 0 = when it starts). config.NotifyLeads picks any of
// them; without a choice it is 15 minutes.
var notifyLeadChoices = []int{1440, 180, 60, 30, 15, 5, 0}

func notifyLeads(c config) []int {
	if c.NotifyLeads == nil {
		return []int{15}
	}
	return c.NotifyLeads
}

// leadText: "in 1 hour", "in 15 minutes", "now".
func leadText(min int) string {
	switch {
	case min == 0:
		return "now"
	case min%1440 == 0:
		if min == 1440 {
			return "tomorrow"
		}
		return fmt.Sprintf("in %d days", min/1440)
	case min%60 == 0:
		if min == 60 {
			return "in 1 hour"
		}
		return fmt.Sprintf("in %d hours", min/60)
	}
	return fmt.Sprintf("in %d minutes", min)
}

// leadWindow: a moment's notification still goes out this long after it (the check runs every 20 s).
const leadWindow = 2 * time.Minute

// SetNotifyLeads: the moments chosen in the calendar.
func SetNotifyLeads(leads []int) error {
	var keep []int
	for _, l := range notifyLeadChoices {
		if slices.Contains(leads, l) {
			keep = append(keep, l)
		}
	}
	if keep == nil {
		keep = []int{} // none chosen: no notifications (distinct from "not set")
	}
	updateConfig(func(c *config) { c.NotifyLeads = keep })
	return nil
}

// Notification settings (config.Notify): which sessions notify by themselves.
const (
	notifyOff  = ""
	notifyRace = "race" // the races only (the Grand Prix, not the sprint)
	notifyMain = "main" // races, sprints and qualifyings
	notifyAll  = "all"
)

// sendNotification is set by main once the window is up (nil before, and when notifications aren't available).
var sendNotification func(title, body string) error

var notified sync.Map // "session key/minutes" → true: notified already (this run)

func isRace(s CalSession) bool { return s.Name == "Race" }

func isMainSession(s CalSession) bool {
	n := strings.ToLower(s.Name)
	return s.Type == "Race" || s.Type == "Qualifying" || strings.Contains(n, "qualifying") || strings.Contains(n, "shootout")
}

// notifies: whether this session notifies with these settings.
func notifies(c config, s CalSession) bool {
	for _, k := range c.NotifySessions {
		if k == s.Key {
			return true
		}
	}
	switch c.Notify {
	case notifyAll:
		return true
	case notifyMain:
		return isMainSession(s)
	case notifyRace:
		return isRace(s)
	}
	return false
}

// notifiesBySetting: on by the calendar's setting alone (not by the session's own bell).
func notifiesBySetting(mode string, s CalSession) bool {
	return notifies(config{Notify: mode}, s)
}

// checkNotifications runs with the plans (every 20 s): each chosen moment before a session that notifies.
func checkNotifications(c config) {
	leads := notifyLeads(c)
	if sendNotification == nil || len(leads) == 0 || (c.Notify == notifyOff && len(c.NotifySessions) == 0) {
		return
	}
	if loadCalendar() != nil {
		return
	}
	now := time.Now()
	calendar.Lock()
	type hit struct {
		s    CalSession
		lead int
	}
	var due []hit
	for _, s := range calendar.sessions {
		if !notifies(c, s) {
			continue
		}
		left := s.Start.Sub(now)
		for _, l := range leads {
			at := time.Duration(l) * time.Minute
			if left <= at && left > at-leadWindow {
				if _, done := notified.LoadOrStore(fmt.Sprintf("%d/%d", s.Key, l), true); !done {
					due = append(due, hit{s, l})
				}
			}
		}
	}
	meetings := calendar.meetings
	calendar.Unlock()
	for _, h := range due {
		s, m := h.s, meetings[h.s.Meeting]
		title := fmt.Sprintf("%s starts %s", s.Name, leadText(h.lead))
		if h.lead == 0 {
			title = s.Name + " is starting"
		}
		body := fmt.Sprintf("%s · %s", shortGP(m.Name), s.Start.Local().Format("Mon 15:04"))
		if err := sendNotification(title, body); err != nil {
			log.Printf("notification: %v", err)
		}
	}
	// keep the list of single sessions short: drop those that have started
	var keep []int
	for _, k := range c.NotifySessions {
		if s, _, ok := calSession(k); !ok || s.Start.After(now.Add(-leadWindow)) {
			keep = append(keep, k)
		}
	}
	if len(keep) != len(c.NotifySessions) {
		updateConfig(func(cc *config) { cc.NotifySessions = keep })
	}
}

// notifyPlan: the system notification that goes with a planned party's reminder.
func notifyPlan(pl *plan) {
	if sendNotification == nil {
		return
	}
	what := "Your F1 Party"
	if pl.Replay {
		what += " (replay)"
	}
	_ = sendNotification(fmt.Sprintf("%s starts in 15 minutes", what), pl.Title+" · code "+pl.Code)
}

// SetNotify changes the calendar's notification setting (all/main/off).
func SetNotify(mode string) error {
	switch mode {
	case notifyOff, notifyRace, notifyMain, notifyAll:
	default:
		return fmt.Errorf("unknown setting %q", mode)
	}
	updateConfig(func(c *config) { c.Notify = mode })
	return nil
}

// ToggleNotify turns a single session's bell on or off.
func ToggleNotify(key int) bool {
	on := false
	updateConfig(func(c *config) {
		var keep []int
		found := false
		for _, k := range c.NotifySessions {
			if k == key {
				found = true
				continue
			}
			keep = append(keep, k)
		}
		if !found {
			keep = append(keep, key)
			on = true
		}
		c.NotifySessions = keep
	})
	return on
}
