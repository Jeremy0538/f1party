//go:build !windows && !linux

package main

import "errors"

func winSavePlaces() error { return errors.New("window placement is Windows only") }
func winPlaceApps()        {}
func winTidy() int         { return 0 }
func winBattleOnTop()      {}
