# F1 Party

Watch [MultiViewer](https://multiviewer.app) in sync with friends — Windows, macOS and Linux.

## Download
| Your computer | File |
|---|---|
| Windows | [F1Party-windows.exe](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-windows.exe) |
| Mac (M1/M2/M3/M4…) | [F1Party-macos-apple-silicon](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-macos-apple-silicon) |
| Mac (Intel) | [F1Party-macos-intel](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-macos-intel) |
| Linux | [F1Party-linux](https://github.com/Jeremy0538/f1party/releases/latest/download/F1Party-linux) |

## How to use
1. Open MultiViewer and sign in with F1 TV (everyone needs their own subscription).
2. Start F1 Party. A page opens in your browser.
3. The host opens the world feed (International) and clicks **Host a party**, then shares the 5-letter code.
4. Everyone else enters the code and clicks **Join**. Their MultiViewer opens the same session and follows the
   host's play/pause/seek. Onboards you open yourself follow along.

First launch warnings (the app isn't signed):
- **Windows:** "Windows protected your PC" → **More info** → **Run anyway**.
- **macOS:** open Terminal and run `chmod +x ~/Downloads/F1Party-macos-*; xattr -d com.apple.quarantine ~/Downloads/F1Party-macos-*`,
  then right-click the file → **Open**.
- **Linux:** `chmod +x F1Party-linux && ./F1Party-linux`

Keep the F1 Party window open while watching. Live sessions: a guest who is ahead waits for the host;
one who is behind can't be pulled forward (live streams can't skip ahead).

## Developer notes

- `app/` — Go program. One file per platform, no install. It opens a local page (http://127.0.0.1:47821)
  with **Host a party** / **Join**, and talks to the MultiViewer on the same computer through its local
  API (`http://localhost:10101/api/graphql`).
- `relay/` — Cloudflare Worker + Durable Object (one per party code) at `wss://jeremyklootwijk.nl/f1party/ws`.
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
    ./build.sh            # → dist/F1Party-{windows.exe,macos-apple-silicon,macos-intel,linux}

## Test locally
    cd relay && npx wrangler dev --port 8787 --local
    F1PARTY_RELAY=ws://localhost:8787/f1party/ws F1PARTY_NOBROWSER=1 F1PARTY_PORT=47821 ./F1Party-linux

## Deploy relay
    cd relay && npx wrangler login && npx wrangler deploy     # route jeremyklootwijk.nl/f1party/*

