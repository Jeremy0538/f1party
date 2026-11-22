//go:build !windows

package main

import "os/exec"

func detach(cmd *exec.Cmd) {}

// processRunning is only needed on Windows (killApp asks first and ends what stays there).
func processRunning(string) bool { return false }
