# Working on F1 Party per platform (for Claude and other agents)

F1 Party runs on Windows and Linux (macOS builds compile but nobody tests them). Many features work differently
per platform; PARITY.md has the table of what works where. This file says how to work on the code depending on
the computer you run on.

## Rules for every platform

- Work on a branch, never on master. Every push to master publishes a release (see .github/workflows/build.yml).
- Platform code lives in file pairs: name_windows.go, name_linux.go and name_other.go (build tags
  `windows`, `linux`, `!windows && !linux`). Shared logic goes in a file without a build tag
  (for example winplace_common.go). Every function a pair provides must exist for every platform.
- Before every commit, check all three platforms compile, whichever one you are on:

      cd app
      GOOS=linux go vet ./...
      GOOS=windows go vet ./...
      GOOS=darwin go vet ./...
      go test ./...

- A feature that only works on one platform must do nothing (not fail) elsewhere, and its setting must be hidden
  there. The settings page gets flags from settings.go and extras.go ("windows", "desktops", "places").
- Say in the commit message or to the user which platform a change was actually tested on. "Compiles for Windows"
  is not "tested on Windows".
- PARITY.md is updated in the same commit when a feature starts or stops working on a platform.

## On Linux (the repo owner's desktop, Hyprland)

- Build and run: ./build.sh (uses -tags webkit2_41), or cd app && wails dev.
- Hyprland has a Lua config here: `hyprctl dispatch` takes Lua (`hl.dsp.focus({workspace='name:F1'})`), not the
  classic `workspace name:F1`. Friends' Hyprland may be classic. The code sends both (hypr_linux.go); keep every
  new dispatch in both forms. Find the Lua name of a dispatcher with
  `printf '%s\n' 'local t={} for k in pairs(hl.dsp.window) do t[#t+1]=k end return table.concat(t,",")' | hyprctl repl`.
- Do not move or close the owner's real windows to test. Read-only queries (hyprctl -j clients, monitors) are fine;
  test dispatches against a non-existing address (`address:0xdead`), or ask first.
- MultiViewer here is the AUR package started through ~/scripts/f1/multiviewer_launch.sh (debug port 9334).
- Windows-only code (user32, registry, taskbar, virtual desktops) cannot be run here: only compile it
  (GOOS=windows go vet) and say it is untested.

## On Windows (a laptop or a friend's PC)

- Build: ./build.sh in Git Bash, or wails build -nsis in app. WebView2 is needed (it is on Windows 10 and 11).
- Windows 10 and 11 keep virtual desktops in different registry places (desktop_windows.go handles both); test
  desktop changes on the version you have and say which.
- Window placement uses user32 callbacks: make a syscall.NewCallback once at package level, never per call
  (Windows never frees them and a process gets about 2000).
- Linux-only code (hypr_linux.go, winplace_linux.go) cannot run there: compile it with GOOS=linux go vet.
- The automatic update (update.go) only exists on Windows; test it with a pre-release, never by pushing to master.

## In the cloud or another headless machine

- Nothing with a window can be tested. Run the vet commands for all three platforms and go test, then leave real
  testing to the owner and say so.
- The CI build (Build workflow, workflow_dispatch) builds Windows and Linux without releasing: use it to check a
  branch builds on both.
