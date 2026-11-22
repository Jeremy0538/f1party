//go:build !windows

package main

import "errors"

func winSavePlaces() error { return errors.New("window placement is Windows only") }
func winPlaceApps()        {}
func winTidy() int         { return 0 }
func winBattleOnTop()      {}

// winPlacesSaved: nothing is saved outside Windows.
func winPlacesSaved() (mv, uf1 bool) { return false, false }
