//go:build !windows

package main

// The taskbar setting is only for Windows (see taskbar_windows.go).
func taskbarForWatching() {}
func taskbarRestore()     {}
