//go:build !windows

package main

// toF1Desktop: virtual desktops are only handled on Windows (see desktop_windows.go).
func toF1Desktop() {}

// leaveF1Desktop: nothing to leave without Windows' virtual desktops.
func leaveF1Desktop(remove bool) {}
