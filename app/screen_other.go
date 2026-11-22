//go:build !windows && !linux

package main

// primaryScreen is only known on Windows for now; elsewhere the main feed is picked by name (F1 LIVE first).
func primaryScreen() (w, h float64, ok bool) { return 0, 0, false }
