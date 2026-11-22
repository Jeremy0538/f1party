package main

import "syscall"

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	getSystemMetrics = user32.NewProc("GetSystemMetrics")
	getDpiForSystem  = user32.NewProc("GetDpiForSystem")
)

// primaryScreen: the size of the main monitor in the same units MultiViewer uses for its windows (Electron's
// device-independent pixels); the main monitor always starts at 0,0.
func primaryScreen() (w, h float64, ok bool) {
	cx, _, _ := getSystemMetrics.Call(0) // SM_CXSCREEN
	cy, _, _ := getSystemMetrics.Call(1) // SM_CYSCREEN
	if cx == 0 || cy == 0 {
		return 0, 0, false
	}
	scale := 1.0
	if getDpiForSystem.Find() == nil {
		if dpi, _, _ := getDpiForSystem.Call(); dpi > 0 {
			scale = float64(dpi) / 96
		}
	}
	return float64(cx) / scale, float64(cy) / scale, true
}
