# F1 Party

Watch [MultiViewer](https://multiviewer.app) in sync with friends on Windows, macOS and Linux.

## Download
| Your computer | File |
|---|---|
| Windows (installer, adds F1 Party to the Start menu) | [F1Party-windows-setup.exe](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-windows-setup.exe) |
| Windows (single file, no install) | [F1Party-windows.exe](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-windows.exe) |
| Mac (Apple Silicon and Intel) | [F1Party-macos.zip](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-macos.zip) |
| Linux | [F1Party-linux](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-linux) |

## How to use
1. Open MultiViewer and sign in with F1 TV (everyone needs their own subscription).
2. Start F1 Party. Its window opens.
3. The host opens the world feed (International) and clicks **Host a party**, then shares the 5-letter code.
4. Everyone else enters the code and clicks **Join**. Their MultiViewer opens the same session and follows the
   host's play/pause/seek. Onboards you open yourself follow along.

Also in the app:
- **Open MultiViewer**: starts MultiViewer when it isn't running.
- **Your MultiViewer setup**: pick one of your own MultiViewer setups (players and live timing windows). When you
  join a party, F1 Party opens it for the host's session by itself; **Open setup now** opens it any time. This
  needs MultiViewer started by F1 Party (it adds a debug port, the same one the F1 Race Control Stream Deck
  plugin uses); if it runs already, **Restart for setups** restarts it that way.
- **Stream Deck: F1 Race Control**: with the [F1 Race Control](https://github.com/StanvanDaalen/streamdeck-plugins#f1-race-control)
  Stream Deck plugin (1.15 or newer) installed in Stream Deck or OpenDeck, choose this instead of a setup. On joining, the plugin opens the
  host's session the way its Race / Quali key does: its own desktop, MultiViewer and Ultimate F1 Viewer in place,
  the setup and the viewer's layout. F1 Party waits until it is done, then syncs. Without the plugin the app
  shows a download link.
- **Copy code**: puts the party code on the clipboard, ready to paste in a chat.
- **Discord invites**: paste a Discord webhook URL once (Server settings → Integrations → Webhooks); **Invite on
  Discord** then posts the party code to that channel. The URL stays in this computer's settings.
- **Stream Deck Sync key** (F1 Race Control 1.15+): press = sync (a guest goes to the host first); hold 1 s =
  host a party and post the Discord invite. F1 Party listens on `127.0.0.1:47821` for this (`/control/status`,
  `/control/sync`, `/control/host`) and refuses web pages. `F1Party --host` does the same from a shortcut.
- When MultiViewer is closed as you join, F1 Party starts it (or lets the plugin do that).
- **Start together** (host): pauses everyone on the same frame, counts down from 5 and starts everyone at once.
- **Invite links**: Discord invites carry a link that opens F1 Party and joins in one click (`f1party://join/CODE`,
  through the relay's join page). The installer and `app/install-windows.ps1` register the link type.
- **Discord status**: your Discord profile shows the session you watch and, for guests, whose party; with a
  "Join party" button when join links work. Needs the F1 Party Discord application id (`discordAppID`).
- **Keeps running**: on Windows, closing the window keeps the party going in the tray (icon by the clock: Open /
  Quit). Starting F1 Party again brings the window back.
- **Updates**: a "Update to x.y" button appears when a newer release is out. On Windows it installs the new
  version itself (checked against the release's `checksums.txt`) and restarts; right after starting, outside a
  party, it updates on its own. Elsewhere the button opens the release page.
- **Sync now**: a guest jumps to the host right away and takes the onboards along; the host lines up their own
  onboards with their main stream.
- **Main stream**: everything syncs with the stream on your main monitor (usually F1 Live); without one there,
  F1 Live, then International. A guest always opens F1 Live.
- **Back in your party**: after a restart (or a crash) within 3 hours, the app goes back into the party it was
  in. A host comes back as a guest first, so a guest who took over stays host; if nobody hosts, you host again.
  **Leave** forgets the party.
- **Session**: the session MultiViewer shows, and jumps for the whole party (host): **Beginning**, **Start −1
  min** (a minute before the session starts, then the five lights; needs MultiViewer's live timing, so a setup
  with a timing window or the Stream Deck) and **Live** on a live stream.
- **Calendar**: every race weekend (OpenF1) in your own time. **Watch** a replay now (hosts a party, opens it
  the way you chose under *Open with* and goes to a minute before the start, so nobody sees the end while it
  loads), or **Plan** a party: a live session at its start, or a replay at a time you pick. Planning posts an
  announcement to Discord (with the time in everyone's own time zone and the party code), a reminder 15 minutes
  before, and at the time the app hosts the party with that code and opens the session. F1 Party has to be
  running then (the tray is enough).

First launch warnings (the app isn't signed):
- **Windows:** "Windows protected your PC", then **More info**, then **Run anyway**.
- **macOS:** unzip, move **F1 Party.app** to Applications, open Terminal and run
  `xattr -dr com.apple.quarantine "/Applications/F1 Party.app"`, then open it.
- **Linux:** needs WebKitGTK (`libwebkit2gtk-4.1-0`, installed on most desktops), then
  `chmod +x F1Party-linux && ./F1Party-linux`

Keep the F1 Party window open while watching. Live sessions: a guest who is ahead waits for the host;
one who is behind can't be pulled forward (live streams can't skip ahead).

## Developer notes

- `app/`: Go desktop app built with [Wails](https://wails.io) v2: a window using the system's own web view
  (WebView2 on Windows, WebKit on macOS/Linux), no browser and no install. The page (`ui.html`) and its small
  JSON API are served straight into that window, not on a network port. It talks to the MultiViewer on the
  same computer through its local API (`http://localhost:10101/api/graphql`). Starting it twice brings the
  open window to the front.
  Setups: read from MultiViewer's `setups.config.json` and opened through its debug port (9334) with
  `window.f1mv.setupManager.restoreSetup`, like MultiViewer's own "Open setup" dialog (`app/mvapp.go`).
  Stream Deck plugin: `app/plugin.go` talks to it on `127.0.0.1:9335`; the host sends its session type
  (Race / Qualifying / Practice) with its feed state so the guest's plugin knows which layout to open.
- `relay/`: Cloudflare Worker + Durable Object (one per party code) at `wss://jeremyklootwijk.nl/f1party/ws`.
  Relays the host's world-feed position and answers clock pings.

## How sync works
Everyone shares one clock: each app pings the relay and keeps the offset of the lowest round trip of its last
12 samples (NTP-style), so a moment stamped by the host means the same moment on every computer.

- **Host:** every 250 ms reads its main stream (the one on the main monitor, else F1 LIVE, else
  INTERNATIONAL): content id, position, paused,
  live. It sends that once a second and at once on pause/play/seek/new session, stamped with the shared clock.
  For live streams it also sends the real-world time of the frame on screen (`track`, from MultiViewer's live
  timing clock).
- **Guest:** works out where the host is *now* (position + time since the stamp), opens the host's session
  if needed, mirrors pause/play, and seeks when it is more than 0.3 s off (plus a learned lead for the
  stream's own seek delay); then MultiViewer's `playerSync` pulls the onboards along.
- **Replays** are compared on the player position: the same content has the same timeline on every computer.
- **Live** streams are compared on the real-world time on screen, since player positions differ per computer
  (each joined the stream at a different moment). A guest off by more than 0.5 s moves by the difference;
  forward stops at the live edge. Without timing data it falls back to pausing a guest that's ahead.
  (Live is not yet tested with a real session.)
- MultiViewer reports a player's position only every few seconds; between reports the apps extrapolate
  (`interpolatedCurrentTime`), which holds while a stream plays smoothly.
- **Start together:** the host pauses, everyone seeks to that frame, a 5-second countdown runs (five red
  lights), and everyone plays at the same shared-clock moment.
- **Host leaves:** after 20 seconds without a host, the guest with the lowest member id takes over (every app
  picks the same one) and the others follow it. When the old host comes back it takes over again; the
  stand-in carries on as a guest.

## Build
Needs Go and the Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1`).

    ./build.sh            # builds this computer's version into app/build/bin/

All platforms: GitHub Actions (`.github/workflows/build.yml`). Run it by hand from the Actions tab, or push a
tag like `v0.2.0` to also publish a release with `F1Party-windows.exe`, `F1Party-macos.zip` and `F1Party-linux`.

Install your own build on this PC (Start menu entry, can be pinned to the taskbar):

    powershell -ExecutionPolicy Bypass -File app\install-windows.ps1     # -Uninstall removes it again

## Test locally
    cd relay && npx wrangler dev --port 8787 --local
    F1PARTY_RELAY=ws://localhost:8787/f1party/ws ./app/build/bin/F1Party

## Deploy relay
The join page for invite links (`/f1party/join/CODE`) is part of the relay; it needs a redeploy before
invites show the one-click link (the app checks for it and leaves the link out until then).

    cd relay && npx wrangler login && npx wrangler deploy     # route jeremyklootwijk.nl/f1party/*

