package main

// What's new: shown once after an update (and a setup guide on the very first start). Add an entry here for
// every release that adds something people notice; Tab names the Settings tab where its options are
// (general, sessions, drivers, lights, windows, close) or is empty.

type newsItem struct {
	Version string `json:"version"`
	Text    string `json:"text"`
	Tab     string `json:"tab,omitempty"`
}

var whatsNew = []newsItem{
	{"1.7.0", "New look for the party and the session: who's in the party, the setup and the session each in their own box.", ""},
	{"1.7.0", "Start together on a live session now works without the Stream Deck plugin.", ""},
	{"1.7.0", "Every way a setup opens (calendar, Start together, the Stream Deck plugin or just F1 Live) now uses your desktop, taskbar and window settings.", "general"},
	{"1.7.0", "Linux with Hyprland: its own F1 workspace, saved window positions and Tidy up.", "windows"},
	{"1.7.0", "Linux: F1 Party now updates itself, like on Windows.", ""},
	{"1.7.0", "Updates are found within 20 minutes and install by themselves when you're not in a party.", ""},
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
