package main

import (
	"os/exec"
	"strings"
	"syscall"
)

// detach: helper programs started from the app open no console window.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// processRunning: whether a program with this name (without ".exe") runs.
func processRunning(process string) bool {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq "+process+".exe", "/NH", "/FO", "CSV")
	detach(cmd)
	out, err := cmd.Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), strings.ToLower(process)+".exe")
}

// uf1RunningPath: the program of a running Ultimate F1 Viewer ("" when it doesn't run).
func uf1RunningPath() string {
	if !processRunning(uf1Process) {
		return ""
	}
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "(Get-Process -Name '"+uf1Process+"' | Select-Object -First 1).Path")
	detach(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
