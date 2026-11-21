# F1 Party

Watch [MultiViewer](https://multiviewer.app) in sync with friends on Windows, macOS and Linux.

## Download
| Your computer | File |
|---|---|
| Windows | [F1Party-windows.exe](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-windows.exe) |
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
  Stream Deck plugin (1.15 or newer) installed, choose this instead of a setup. On joining, the plugin opens the
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
- **Sync now**: a guest jumps to the host right away and takes the onboards along; the host lines up their own
  onboards with the world feed.

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
Host: every 250 ms reads its world feed (INTERNATIONAL, else F1 LIVE): content id, time, paused, live. Sends it
once a second and immediately on pause/play/seek/new session, stamped in relay-clock time (ping/pong offset,
lowest round trip of the last 12 samples).
Guest: opens the host's session if it isn't open, mirrors pause/play, and seeks the world feed when it's more
than 0.3 s off (seek target + a learned lead for the stream's seek delay), then MultiViewer's own `playerSync`
pulls the onboards along. Live streams can't be seeked: a guest ahead pauses until the host catches up; a guest
behind is only reported (UNTESTED with a real live session as of 2026-11-14).

## Build
Needs Go and the Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1`).

    ./build.sh            # builds this computer's version into app/build/bin/

All platforms: GitHub Actions (`.github/workflows/build.yml`). Run it by hand from the Actions tab, or push a
tag like `v0.2.0` to also publish a release with `F1Party-windows.exe`, `F1Party-macos.zip` and `F1Party-linux`.

## Test locally
    cd relay && npx wrangler dev --port 8787 --local
    F1PARTY_RELAY=ws://localhost:8787/f1party/ws ./app/build/bin/F1Party

## Deploy relay
    cd relay && npx wrangler login && npx wrangler deploy     # route jeremyklootwijk.nl/f1party/*

