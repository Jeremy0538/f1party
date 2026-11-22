package main

// Notifications from the calendar: a system notification 15 minutes before a session you follow, and before
// a party you planned. You choose which sessions in the calendar: none, races and qualifyings, or every
// session, plus single sessions with their bell. Clicking a notification brings the app to the front.

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// notifyBefore: a notification comes this long before a session starts.
const notifyBefore = 15 * time.Minute

// Notification settings (config.Notify): which sessions notify by themselves.
const (
	notifyOff  = ""
	notifyRace = "race" // the races only (the Grand Prix, not the sprint)
	notifyMain = "main" // races, sprints and qualifyings
	notifyAll  = "all"
)

// sendNotification is set by main once the window is up (nil before, and when notifications aren't available).
var sendNotification func(title, body string) error

var notified sync.Map // session key → true: notified already (this run)

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

// checkNotifications runs with the plans (every 20 s): sessions starting within notifyBefore that notify.
func checkNotifications(c config) {
	if sendNotification == nil || (c.Notify == notifyOff && len(c.NotifySessions) == 0) {
		return
	}
	if loadCalendar() != nil {
		return
	}
	now := time.Now()
	calendar.Lock()
	var due []CalSession
	for _, s := range calendar.sessions {
		left := s.Start.Sub(now)
		if left > 0 && left <= notifyBefore && notifies(c, s) {
			if _, done := notified.LoadOrStore(s.Key, true); !done {
				due = append(due, s)
			}
		}
	}
	meetings := calendar.meetings
	calendar.Unlock()
	for _, s := range due {
		m := meetings[s.Meeting]
		title := fmt.Sprintf("%s starts in %d minutes", s.Name, int(s.Start.Sub(now).Round(time.Minute).Minutes()))
		body := fmt.Sprintf("%s · %s", shortGP(m.Name), s.Start.Local().Format("15:04"))
		if err := sendNotification(title, body); err != nil {
			log.Printf("notification: %v", err)
		}
	}
	// keep the list of single sessions short: drop those that have started
	var keep []int
	for _, k := range c.NotifySessions {
		if s, _, ok := calSession(k); !ok || s.Start.After(now) {
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
