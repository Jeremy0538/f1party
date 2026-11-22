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
3. The host opens the session in MultiViewer (F1 Live on the main monitor) and clicks **Host a party**, then
   shares the 5-letter code.
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
- **Own Discord channel**: no team? Paste a Discord webhook URL once (Server settings → Integrations → Webhooks); **Invite on
  Discord** then posts the party code to that channel. The URL stays in this computer's settings.
- **Friends tab**: two parts.
  - **Friends**: everyone you added. You see who is online and what they're watching, with a **Join** button.
    **Invite people** copies a friend link (works 7 days): opening it gets someone F1 Party and makes you friends.
  - **Teams**: a party is one session with a 5-letter code; a team is a group of friends with one Discord channel.
    **Start a team** with a name and the Discord webhook of the team's channel (the relay keeps it secret). Each
    team has **Invite** (tick friends, who get it in F1 Party, or copy a team link), **Leave**, and **Rename** for
    whoever started it. You see what teammates are watching too. Paste a team or friend link in the field below to join.
- **Invite** (Party tab): tick teams (their Discord channel) and friends (an invite in their F1 Party). The choice
  is remembered for the Stream Deck key and planned parties. Only mutual friends and teammates see what you watch.
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
  Quit), so calendar notifications and plans keep working. Starting F1 Party again brings the window back.
- **Party ends by itself** when you're done watching: MultiViewer closed for 3 minutes after it ran in the party
  (a MultiViewer restart is shorter). Or end it yourself with **End party** (host) / **Leave** (guest).
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
- **Choose or change the session** (host): **Choose** / **Change** in the Session block opens the calendar,
  where **Switch** (any weekend, also long ago) takes the whole party to that session; guests can't. The
  old session closes first, everyone included; with a setup or the Stream Deck also its timing windows, and
  each guest opens the new one with the layout for its type (Race, Qualifying, Practice).
- **Calendar**: every race weekend (OpenF1) in your own time. **Watch** a replay now (hosts a party, opens it
  the way you chose under *Open with* and goes to a minute before the start, so nobody sees the end while it
  loads), or **Plan** a party: a live session at its start, or a replay at a time you pick. Planning posts an
  announcement to Discord (with the time in everyone's own time zone and the party code), a reminder 15 minutes
  before, and at the time the app hosts the party with that code and opens the session. F1 Party has to be
  running then (the tray is enough).
- **Notifications** (the bell at the top of the calendar): which sessions (races only, races and qualifyings,
  every session, or single sessions with their own bell) and when (1 day, 3 h, 1 h, 30 min, 15 min, 5 min
  before, at the start; any combination). Planned parties notify too. Clicking one opens F1 Party.
- **Every season**: click the weekend's name in the calendar for a list of all seasons (from 2023, OpenF1's
  first) and every race of the one you pick, to jump straight to an old race. An older season is fetched once
  and then kept on your computer (`calendar-<year>.json`), since it doesn't change.
- **Calendar offline**: the calendar is saved on your computer, so it still shows when OpenF1 can't be reached
  (OpenF1 shuts out everyone without a paid key while a session is live).
- **Friends**: next to someone in your party who isn't a friend yet is an add button; they see **Accept**
  (and a notification). Both asking makes you friends; the **Friends** tab lists them. Requests ride on the
  party's own messages, so the relay needs no change. Each app has a fixed random id to know friends by.
- **Downloads** (bottom right): MultiViewer, Ultimate F1 Viewer, the Stream Deck plugin and F1 Party, with
  what is installed already.
- **Open with: Automatic (Stream Deck)**: the Stream Deck plugin picks your Race, Qualifying or Practice setup
  for the session and opens Ultimate F1 Viewer too.
- **Plans without a webhook** are announced on Discord as soon as one is set.

First launch warnings (the app isn't signed):
- **Windows:** "Windows protected your PC", then **More info**, then **Run anyway**.
- **macOS:** unzip, move **F1 Party.app** to Applications, open Terminal and run
  `xattr -dr com.apple.quarantine "/Applications/F1 Party.app"`, then open it.
- **Linux:** needs WebKitGTK (`libwebkit2gtk-4.1-0`, installed on most desktops), then
  `chmod +x F1Party-linux && ./F1Party-linux`

Keep F1 Party running while watching (the tray is enough). Live sessions: a guest who is ahead waits for the host;
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


## Contributing
Collaborators: clone this repo, work on a branch and open a pull request against master.
Everyone else: fork the repo, push to your fork and open a pull request.
Please test a change with the local relay (see Test locally) before opening the pull request.

## License
MIT, see LICENSE.
