# Linux and Windows feature parity

Status of every feature that works differently per platform (checked 2026-11-22, code on branch parity/linux-windows).
Platform code lives in pairs: name_windows.go (Windows) and name_other.go (Linux and macOS), plus runtime.GOOS
switches in mvapp.go, uf1.go, watch.go, plugin.go, update.go, settings.go and extras.go.

## Overview

| Feature | Windows | Linux | Where |
|---|---|---|---|
| Party, sync, chat, friends, calendar | yes | yes | shared |
| Discord status | named pipe | unix socket discord-ipc-N (also Flatpak and Snap Discord) | ipc_*.go |
| System notifications | toasts, branded as F1 Party | yes, not branded | notify.go, notifybrand_*.go |
| Philips Hue flags | yes | yes | hue.go |
| Find and start MultiViewer | newest app-x.y.z in LOCALAPPDATA | PATH (multiviewer) or a few fixed paths | mvapp.go mvExe |
| Close MultiViewer | taskkill | pkill on the command line | mvapp.go mvQuit, watch.go killApp |
| Ultimate F1 Viewer | yes | PATH or /opt, untested | uf1.go |
| Stream Deck plugin found | Elgato and OpenDeck folders | OpenDeck folder only | plugin.go |
| F1 virtual desktop | yes (own desktop, named) | Hyprland: named workspace "F1"; other desktops: no | desktop_*.go, winplace_linux.go |
| Taskbar while watching | main screen only or auto-hide | no (no common taskbar) | taskbar_*.go |
| Save and restore window positions | yes | Hyprland: floating place and size, or tiled; other desktops: no | winplace_*.go |
| Tidy windows, battle mode on top | yes | Hyprland: floating windows only; other desktops: no | winplace_*.go |
| Main screen size (pick main feed) | yes | Hyprland: the monitor at 0,0; other desktops: picks the feed by name | screen_*.go, winplace_linux.go |
| Tray icon, keep running on close | yes | no, closing quits | tray_*.go |
| Automatic update | download, checksum, replace | same, when F1 Party may write its own folder (not a /usr package install) | update.go |
| Settings tabs shown | all | desktop and window parts only on Hyprland ("desktops" flag, places), taskbar never | settings.go, extras.go, ui.html |

## Why Linux is missing the window features

Windows has one window API (user32, virtual desktops, one taskbar). Linux does not: on Wayland an app may not move
other apps' windows at all, and every desktop (GNOME, KDE, Hyprland, Sway) has its own way. So a Linux backend has
to be per desktop, with "do nothing" as the fallback.

## Linux backend (Hyprland done, others open)

winplace_linux.go and hypr_linux.go talk to Hyprland's IPC socket. Every dispatch is sent in the classic syntax
first; when Hyprland answers that it wants the Lua form (Lua config, Hyprland 0.56+), that form is used from then
on. Windows are recognised by the program behind them (/proc/PID/exe), not by window class.

- F1 desktop = workspace "name:F1" (the Desktop name setting). Close F1 goes back to the workspace F1 Party came
  from; with "The watching workspace" ticked, windows left on it move along and Hyprland drops the empty workspace.
- Saved position = monitor name, place and size for a floating window; for a tiled window only "tiled" is kept
  (wpPlace.Maximized), because the layout decides where it goes.
- Tidy up = the same edge snapping as Windows (wpSnap in winplace_common.go), for floating windows only.
- Battle mode on top = alterzorder top on the floating Battle mode window.

Still to do: KDE Plasma (KWin scripts over D-Bus), X11 (EWMH), GNOME Wayland stays without window control.

## Smaller Linux fixes done on this branch

- mvExe also finds the AUR package (f1multiviewer-bin in /opt/f1multiviewer-bin).
- Closing MultiViewer on Linux matched any command line containing "multiviewer" (also scripts and editors with
  that word). It now only matches the MultiViewer program itself.

## Still open

- Ultimate F1 Viewer on Linux has never been tested. When it was opened by hand (without the debug port) F1 Party now closes and restarts it on every platform, as it did on Windows.

- Tray on Linux: Wails v2 and systray both want the main thread; would need the StatusNotifier D-Bus protocol directly.
