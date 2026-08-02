# Changelog

All notable changes to Eta are recorded here. Format loosely follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); dates are
UTC. There is no tagged release yet — entries accumulate under
"Unreleased" until the first one ships.

## Unreleased

### Added

- Optional access password (Settings → Security). Off by default — an
  instance with no password behaves exactly as before, matching the
  Tailscale/LAN trust boundary Eta otherwise assumes. The browser
  derives a PBKDF2 verifier client-side; the password itself never
  reaches the server, for this instance's own login or for a peer's.
  A peer with a password is authenticated to like any other client —
  its password is entered once at enrollment, and this server logs
  back in on its own afterward, surviving either side restarting.
- "Edit in vim": the file viewer can open a terminal already sitting in
  `vim <file>`, instead of a browser-based editor. Works identically
  for a peer's filesystem.
- Explorer search bar: fuzzy-filters the current folder's own listing
  by name (Fuse.js). Not a recursive search across subfolders.
- Explorer "go to path": type a path directly instead of clicking
  through breadcrumbs; opens a file directly if the path resolves to
  one.
- Add PC now takes a bare hostname ("minerva") — Eta's own default port
  is filled in automatically unless an explicit scheme was given.
- Copy/paste a PC list between instances (only URLs travel; a peer's
  stored credential never does).
- Remove PC from the desktop's own context menu (the endpoint existed
  already; nothing in the UI called it).
- Hidden-files toggle, tmux session list and attach, per-machine
  desktop icons with accent-correct colouring, a taskbar tray (clock,
  volume), and a start menu that no longer drowns every other PC in
  the host's own accent colour.
- This changelog and an in-app version/changelog viewer
  (Settings → About).

### Fixed

- A peer going offline was flipping this server's own header
  "OFFLINE" indicator — wrong, since that indicator means this
  instance's own health, not a peer's. A peer window that fails to
  open, or goes offline mid-session, now says so in the window itself.
- Peer proxy failures returned Go's own transport error text verbatim
  (a raw `dial tcp ...: connection refused`, internal request URL
  included) instead of a message meant to be read.
- Fenced code blocks in a rendered markdown file only highlighted
  correctly for a fixed set of ~35 languages (highlight.js, statically
  bundled, no path to any more); the standalone code viewer next to it
  already had every one of Prism's ~300 grammars vendored. Markdown now
  shares that highlighter; highlight.js is gone.
- The windowed file viewer gave code its padding back explicitly (it
  intentionally fills the window edge to edge) but markdown never had
  any of its own — it rendered flush against the window frame.
- The terminal rendered its own text underneath its scrollbar on
  Chromium's overlay scrollbars, which report zero layout width to the
  fit addon's column-width math.
- A remote terminal's live stream, and a cross-PC directory transfer's
  correctness under interruption, hardened with dedicated coverage.
- Assorted window-chrome bugs: minimize animation, taskbar icon sizing
  (Lucide replaces `<i>` with `<svg>`, so a rule written for the former
  never reached the latter), unfocused-window identity colour loss,
  context-menu items that stayed visible despite `hidden` (a class
  selector's `display` outranking the attribute — hit three times
  across this codebase before it made it into the project's own notes
  as a named trap).

### Security

- Every browser dependency is vendored; a test guard fails any spec
  whose page requests a non-localhost URL.
- A peer's derived login credential (`Verifier`) is never sent to the
  browser — only ever used server-to-server, and only ever persisted
  on the machine that needs it to log back in.
