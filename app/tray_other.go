//go:build !windows

package main

// Elsewhere closing the window quits, as before (a tray icon there would compete with the window for the
// main thread).

func startTray(show, quit func()) {}

func keepRunningOnClose() bool { return false }

func stopTray() {}

func markQuitting() {}
