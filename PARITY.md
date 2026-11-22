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
| F1 virtual desktop | yes (own desktop, named) | no, does nothing | desktop_*.go |
| Taskbar while watching | main screen only or auto-hide | no (no common taskbar) | taskbar_*.go |
| Save and restore window positions | yes | no | winplace_*.go |
| Tidy windows, battle mode on top | yes | no | winplace_*.go |
| Main screen size (pick main feed) | yes | no, picks the feed by name | screen_*.go |
| Tray icon, keep running on close | yes | no, closing quits | tray_*.go |
| Automatic update | download, checksum, replace | only "new version" notice | update.go |
| Settings tabs shown | all | Windows-only parts hidden (settings "windows" flag) | settings.go, ui.html |

## Why Linux is missing the window features

Windows has one window API (user32, virtual desktops, one taskbar). Linux does not: on Wayland an app may not move
other apps' windows at all, and every desktop (GNOME, KDE, Hyprland, Sway) has its own way. So a Linux backend has
to be per desktop, with "do nothing" as the fallback.

## Proposed Linux backend

One file winplace_linux.go (and desktop_linux.go) that picks a backend once at start:

1. Hyprland (HYPRLAND_INSTANCE_SIGNATURE set): talk to the hyprctl socket.
   - F1 desktop = a named workspace "F1" (dispatch workspace name:F1).
   - Save/restore positions = read clients (class multiviewer / ultimate-f1viewer), store workspace, monitor,
     x/y/size; restore with movetoworkspacesilent + movewindowpixel/resizewindowpixel (floating windows).
   - Battle mode on top = pin or alterzorder.
   - Main screen = the focused or first monitor from hyprctl monitors.
2. KDE Plasma (Wayland): KWin scripts over D-Bus can move windows and switch virtual desktops. Possible, more work.
3. X11 (any desktop): wmctrl/xdotool style calls through the X server. Works for older setups.
4. GNOME Wayland and everything else: no window control; the settings stay hidden like now.

The settings page should then show the window options per backend ("windows" flag becomes a list of what works)
instead of Windows only.

## Smaller Linux fixes done on this branch

- mvExe also finds the AUR package (f1multiviewer-bin in /opt/f1multiviewer-bin).
- Closing MultiViewer on Linux matched any command line containing "multiviewer" (also scripts and editors with
  that word). It now only matches the MultiViewer program itself.

## Still open

- Ultimate F1 Viewer on Linux has never been tested.
- Automatic update on Linux: possible for the plain binary (replace the file, restart), not for package installs.
- Tray on Linux: Wails v2 and systray both want the main thread; would need the StatusNotifier D-Bus protocol directly.
