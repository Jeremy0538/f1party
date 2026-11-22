package main

// The Downloads menu: where to get MultiViewer, Ultimate F1 Viewer, the Stream Deck plugin and F1 Party
// itself (which of them are installed: uf1.go, mvapp.go, plugin.go).

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
	return false
}
