package main

// What's new: shown once after an update (and a setup guide on the very first start). Add an entry here for
// every release that adds something people notice; Tab names the Settings tab where its options are
// (party, sync, sessions, drivers, screen, lights, end, updates, downloads) or is empty. The entries for a version are also the
// "What's new" at the top of its GitHub release notes (build.yml). Older entries may still name general,
// windows or close: the settings map those to party, screen and end.

type newsItem struct {
	Version string `json:"version"`
	Text    string `json:"text"`
	Tab     string `json:"tab,omitempty"`
}

var whatsNew = []newsItem{
	{"1.16.0", "Removing a friend now removes you from their friends too.", ""},
	{"1.16.0", "A friend request to someone who already is your friend says so.", ""},
	{"1.16.0", "Closing Settings with changes you didn't save asks first: keep editing, don't save, or save.", ""},
	{"1.16.0", "F1 Party looks for updates when it starts and when you open it again from the tray, not all the time.", "updates"},
	{"1.16.0", "MultiViewer and Ultimate F1 Viewer are found wherever they're installed, also on Linux; F1 Party remembers where it saw them running.", "sessions"},
	{"1.16.0", "The Stream Deck plugin is also found in OpenDeck installed as a Flatpak.", "downloads"},
	{"1.15.0", "Updates no longer install by themselves: you get a notification, and F1 Party updates when you click Update at the top.", "updates"},
	{"1.15.0", "Friends and Teams have an inbox button: party invites, friend requests (Accept or Decline) and team invites, with a count. New ones also pop up at the bottom right.", ""},
	{"1.15.0", "Downloads shows when MultiViewer, the Stream Deck plugin or Ultimate F1 Viewer has an update, with a red dot on the gear.", "downloads"},
	{"1.15.0", "Calendar: every race in the season list has an info button to its page on formula1.com.", ""},
	{"1.15.0", "Auto-sync has its own Sync tab in Settings. With it off, your own pause and skip are only yours: the party carries on.", "sync"},
	{"1.15.0", "Send a friend request to anyone in your teams, with the add-friend button next to their name; they accept it in their inbox.", ""},
	{"1.15.0", "Smoother: the app, menus, views and the party code now slide and fade in.", ""},
	{"1.14.0", "Profile pictures: choose yours under Settings → You and party; your friends, teams and party see it instead of your initial.", "party"},
	{"1.14.0", "Team logos: the one who started a team clicks the square next to its name (Teams) to give it a logo.", ""},
	{"1.13.0", "A slimmer bar at the top, and a calendar that shows the next session's countdown and the sessions by day.", ""},
	{"1.13.0", "Downloads moved into Settings, next to Updates.", "downloads"},
	{"1.13.0", "Fixed onboards: choose whole teams or single drivers, in a card per team; the line-up follows the latest session, so a new driver shows up by himself.", "drivers"},
	{"1.13.0", "Battle mode: each window gets as many drivers as fit, up to the most you set (1 to 4), and the second window carries on where the first stopped.", "drivers"},
	{"1.13.0", "Settings has a menu on the left: You and party, Sessions, Drivers, Screen, Lights and End session, with Updates at the bottom.", "party"},
	{"1.12.0", "One End session button: it closes the streams, MultiViewer and Ultimate F1 Viewer, the watching desktop, lights and taskbar (as set). The party stays; End party leaves it.", "close"},
	{"1.12.0", "Your other streams follow the main stream: pause, skip or rewind it and they do the same, and they sync again when you play.", ""},
	{"1.12.0", "The F1 Race Control Stream Deck plugin now drives F1 Party: Party, Race, Quali, Start together and End session keys.", ""},
	{"1.8.0", "A setup guide on the first start, and this window after every update.", ""},
	{"1.8.0", "New look for the party and the session: who's in the party, the setup and the session each in their own box.", ""},
	{"1.7.0", "Start together on a live session now works without the Stream Deck plugin.", ""},
	{"1.8.0", "Every way a setup opens (calendar, Start together, the Stream Deck plugin or just F1 Live) now uses your desktop, taskbar and window settings.", "general"},
	{"1.7.0", "Linux with Hyprland: its own F1 workspace, saved window positions and Tidy up.", "windows"},
	{"1.8.0", "Linux: F1 Party now updates itself, like on Windows.", ""},
	{"1.8.0", "Updates are found within 20 minutes and install by themselves when you're not in a party.", ""},
}

// Welcome: what the app shows once: the setup guide (first start) or what's new since the last version seen.
type Welcome struct {
	Setup bool       `json:"setup,omitempty"`
	News  []newsItem `json:"news,omitempty"`
}

func welcomeFor(c config) *Welcome {
	if !c.SetupDone && c.Name == "" && c.SeenVersion == "" {
		return &Welcome{Setup: true}
	}
	if compareVersions(version, c.SeenVersion) <= 0 {
		return nil
	}
	var news []newsItem
	for _, n := range whatsNew {
		// never seen a version (updated from before this existed): only this version's news
		if compareVersions(n.Version, version) <= 0 && (c.SeenVersion == "" && n.Version == version || c.SeenVersion != "" && compareVersions(n.Version, c.SeenVersion) > 0) {
			news = append(news, n)
		}
	}
	if len(news) == 0 {
		return nil
	}
	return &Welcome{News: news}
}
