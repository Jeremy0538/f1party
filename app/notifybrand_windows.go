package main

import "golang.org/x/sys/windows/registry"

// brandNotifications: notifications show "F1 Party" as the sender instead of the exe name.
func brandNotifications() {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\F1Party.exe`, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetStringValue("DisplayName", "F1 Party")
}
