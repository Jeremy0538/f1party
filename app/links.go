package main

// The Downloads menu: where to get MultiViewer, Ultimate F1 Viewer, the Stream Deck plugin and F1 Party
// itself (which of them are installed: uf1.go, mvapp.go, plugin.go).

import "regexp"

const (
	mvDownload  = "https://multiviewer.app/download"
	uf1Download = "https://github.com/MRAJEKO/UF1-Viewer-with-F1MV/releases/latest"
)

// allowedURL: the only pages the window may open in the browser.
func allowedURL(u string) bool {
	switch u {
	case pluginDownload, pluginPage, mvDownload, uf1Download, ReleasesPage:
		return true
	}
	// a race weekend's page on formula1.com (the season list's info buttons)
	return f1PageRe.MatchString(u)
}

var f1PageRe = regexp.MustCompile(`^https://www\.formula1\.com/en/racing/\d{4}/[a-z0-9-]+$`)
