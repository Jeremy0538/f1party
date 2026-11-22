package main

import "testing"

// All routes register without clashing (a clash panics when the app starts).
func TestHandlerRoutes(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("routes clash: %v", r)
		}
	}()
	newHandler(&Party{}, func() {}, func(string) {}, func(string) error { return nil }, func(bool) (string, error) { return "", nil })
}
