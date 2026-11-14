# F1 Party

Watch [MultiViewer](https://multiviewer.app) in sync with friends — Windows, macOS and Linux.

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

Unsigned binaries: Windows SmartScreen → "More info" → "Run anyway"; macOS → right-click → Open
(or `xattr -d com.apple.quarantine F1Party-macos-*`), and `chmod +x` on macOS/Linux.
