//go:build !windows

package main

// No tray icon elsewhere (it would compete with the window for the main thread).

func startTray(show, quit func()) {}

func stopTray() {}
