# Testing F1 Party

A checklist to run before or right after a release, by hand, in the real app. Each line says what to do and
what you should see. Look at the screen, not only whether it works: text that is cut off, overlapping or
misaligned parts, wrong colours, a missing icon or a red dot that stays after you dealt with it all count as
failures.

How to use it: copy the list into a new GitHub issue ("Test F1 Party X.Y.Z"), tick what passes, and write
what fails under the item (a screenshot helps). Items marked (Windows) or (Linux) only apply there. Items
marked (2 people) need a second computer or a friend.

Automatic tests (go test ./... in app/) cover the logic. This list is for what only a person can see.

## 1. Start and update

- [ ] Fresh start (no settings yet): the setup guide opens, with a step bar on top and Back, Next and Skip. Every step fits without text running out of its box.
- [ ] Finish the guide: it closes and does not come back on the next start.
- [ ] After an update: "What's new" shows once, with only the new version's lines. A line with a Settings tab opens that tab when clicked.
- [ ] Starting: the bar at the top slides in, then the page fades in. Nothing jumps or flickers.
- [ ] With "reduce motion" on in the system: nothing slides or fades, everything just appears.
- [ ] A new version on GitHub: one notification "F1 Party X is out", a red Update button at the top and a red dot on the gear. Nothing installs by itself.
- [ ] Click Update: it downloads, F1 Party restarts by itself and you are still in your party.
- [ ] (Windows) Close F1 Party to the tray, open it again from the tray: it looks for updates then (Settings, Updates shows "checked just now").
- [ ] Settings, Updates, Check for updates: says "You have the newest version" or shows the install button.

## 2. Party page (not in a party)

- [ ] "Host or join a party" with your name and picture (or initial) under it.
- [ ] Two clear boxes: "Host a party" with a red plus icon, and "Join a party" with an arrow icon, each with a short explanation.
- [ ] Join: typing a code fills the five boxes one letter each; pasting a code fills all five; a wrong code shows a clear error.

## 3. In a party

- [ ] Host: the party code drops in letter by letter, the member list fades in below it.
- [ ] Copy code copies the code. Invite opens the invite choice (friends, teams, your own Discord channel).
- [ ] Member list: the host has a crown and HOST, you have "(you)", everyone has a status line under the name.
- [ ] (2 people) Join from the other computer: they appear in the list within a few seconds, with their picture.
- [ ] (2 people) Host pauses, plays, skips: the guest follows within about a second.
- [ ] (2 people) Guest with Auto-sync on pauses or skips in MultiViewer: the whole party does the same.
- [ ] (2 people) Guest with Auto-sync off pauses or skips: only the guest does; the host and others carry on. The Sync button appears; clicking it jumps back to the host.
- [ ] (2 people) Start together: the five red lights and "Lights out together" show on both screens, the streams start at the same moment.
- [ ] (2 people) Hand over host: the crown moves, the new host's stream is the one everyone follows.
- [ ] End party (the x): the party ends and the start page comes back.

## 4. Calendar

- [ ] This weekend: the race name, place and "Round N of M", the sessions with times in your own time zone.
- [ ] Previous and next weekend arrows work and the dates change.
- [ ] Click the race name: the season list opens (slides down) with the years on top and a tile per race. The arrow next to the name turns.
- [ ] Every race tile has an (i) on the right. Clicking the (i) opens that race's page on formula1.com; clicking the rest of the tile opens that weekend.
- [ ] The (i) is also there for older seasons (pick 2024 or 2025).
- [ ] Notifications (the bell): "Notify me" and "When" can be changed and are kept after a restart.
- [ ] Watch now / Watch on a session: a party starts on it; as a guest the buttons say only the host picks.
- [ ] Plan / Plan replay: the planned party shows up, and starts at its time.

## 5. Friends

- [ ] The Friends tab: a heading, the inbox button (tray icon) and the green add-friend button next to it.
- [ ] Green button: copies a friend link. Someone opening it becomes your friend on both sides; you get a notification.
- [ ] Friends list: online friends first, what they watch under their name, Join when they're in a party.
- [ ] Inbox button: opens a box under the heading (slides down). With nothing waiting it says "Nothing waiting".
- [ ] (2 people) Party invite from a friend: a pop-up at the bottom right on any tab, a number on the inbox button, a red dot on the Friends tab, and a system notification. Join works from the pop-up and from the inbox; Not now removes it.
- [ ] (2 people) Friend request in a party (the add-friend icon next to a name): the other gets a pop-up and sees it in their inbox with Accept and Decline. Accept: friends on both sides. Decline: it goes away and doesn't come back in this party.
- [ ] (2 people) Remove a friend (the red icon): the question says they lose you too. After it, you are gone from their Friends list as well (within about 30 seconds).
- [ ] (2 people) Send a friend request to someone who already is your friend: it says "already friends".
- [ ] Red dots and the number on the inbox button disappear as soon as nothing is waiting anymore.

## 6. Teams

- [ ] The + button opens the form to start a team (slides down); with a Discord webhook the team is made.
- [ ] Paste a team link and Join: you are in the team.
- [ ] Team card: logo or first letter, name, member count, the moderator first, Member or Moderator on each row.
- [ ] Moderator: click the logo square to choose a logo; it shows for everyone in the team.
- [ ] Moderator: rename the team, remove someone (they can only come back with a new link).
- [ ] Invite someone to the team (green button): a friend gets it in the Teams inbox with Accept and Decline.
- [ ] (2 people) Next to a teammate who isn't your friend: the add-friend icon. Click it: "Friend request sent", then "Request sent" next to their name.
- [ ] (2 people) The teammate gets a notification and the request in the Friends inbox ("From your team"), and an Accept button next to your name in the team. Accept: friends on both sides. Decline: gone, and you can ask again.
- [ ] Leave team: the team disappears from your list.

## 7. Settings

- [ ] The gear opens Settings with a menu on the left: You and party, Sync, Sessions, Drivers, Screen, Lights, End session, then Updates and Downloads under a line.
- [ ] The window pops in, switching menu items fades the right side. Nothing is cut off at the bottom; long tabs scroll.
- [ ] You and party: name, profile picture (Choose picture, shows everywhere: you, friends, teams, party), your own Discord channel.
- [ ] Sync: Auto-sync with the host, with an "On:" and an "Off:" line that explain what each does.
- [ ] Sessions: open sessions with (automatic, F1 Live, Stream Deck plugin, a setup), the table of setups and layouts per kind of session.
- [ ] Drivers: fixed onboards per team or driver, battle mode numbers.
- [ ] Screen: desktop or workspace name, taskbar (Windows), window positions (Save current positions, Tidy up now).
- [ ] Lights: says it works with Philips Hue; Philips Hue bridge (Search again, Pair, then press the bridge button); flags on your Hue lights; Test the flags now.
- [ ] End session: what End session closes.
- [ ] Downloads: MultiViewer, Ultimate F1 Viewer, F1 Race Control (Stream Deck), F1 Party. Installed ones are green with their version when known.
- [ ] Downloads, an app with a newer version: a red "Update: X" label, "You have Y" under its name, a red Update button, a red dot on Downloads and on the gear. Update opens the download.
- [ ] Save: Settings closes and "Settings saved" pops up at the bottom.
- [ ] Change something, then Cancel (or press Escape): a bar asks "Close without saving them?" with Keep editing, Don't save and Save; the normal buttons are hidden meanwhile. Each of the three does what it says.
- [ ] Change nothing, then Cancel: Settings closes straight away, no question.

## 8. Apps F1 Party works with

- [ ] MultiViewer not running: the Setup box says Not running with Start. Start opens MultiViewer; the box turns to Running.
- [ ] MultiViewer installed somewhere else than usual (a zip unpacked in another folder, or an AppImage): start it once by hand, then F1 Party finds and starts it by itself next time.
- [ ] Ultimate F1 Viewer: shows as installed in Downloads and under Sessions, also when installed as an AppImage (Linux) or in another folder (Windows). Its layout opens with a setup.
- [ ] (Linux) Ultimate F1 Viewer and MultiViewer added through Gear Lever or another AppImage manager are found.
- [ ] Stream Deck plugin: found in Stream Deck (Windows) and in OpenDeck, also OpenDeck as a Flatpak (Linux). The F1 Race Control keys drive F1 Party (Party, Race, Quali, Start together, End session).
- [ ] End session closes MultiViewer, Ultimate F1 Viewer, the F1 desktop or workspace and puts the lights and taskbar back, as set; the party stays.
- [ ] Discord status shows what you watch while in a party.

## 9. Windows only

- [ ] (Windows) Closing the window keeps F1 Party in the tray; the tray icon opens it again; Quit in the tray menu quits.
- [ ] (Windows) Notifications show as F1 Party with its icon, and clicking one opens F1 Party.
- [ ] (Windows) Its own virtual desktop for F1, named as set, and back to your desktop after End session.
- [ ] (Windows) Taskbar: only on the main monitor or hidden while watching, back afterwards.

## 10. Linux only

- [ ] (Linux) Closing the window quits F1 Party (there is no tray).
- [ ] (Linux, Hyprland) The F1 workspace, saved window places, Tidy up.
- [ ] (Linux) Update: F1 Party replaces itself and starts again (when it may write its own folder).
