# η eta

A private filesystem desktop for your computers.

Run Eta on a machine, choose the folders it may expose, then open Eta from another machine on your LAN or Tailnet. Add the other Eta machines you trust and work with their files in the same desktop: browse, preview, copy, move, inspect, and open a terminal in a folder.

## Start

Eta does not open a browser for you. Build and run it, then visit the address it prints.

```sh
npm install
npm run build:web
go run . --root ~/Downloads --root /Volumes/Archive
```

By default Eta listens on port `7080` using `lan` binding: localhost, private-LAN addresses, and Tailnet addresses. To run it only on this machine:

```sh
go run . --ip 127.0.0.1 --port 7080 --root ~/Downloads
```

Open `http://localhost:7080` locally, or use the machine's LAN or Tailnet address from another trusted machine.

## Add another computer

1. Run Eta on each computer whose files you want to reach.
2. Open the Eta desktop on the computer you want to use as the coordinator.
3. Select **Add PC** and enter the other machine's Eta URL, such as `http://laptop:7080`.
4. Open **η → COMPUTERS** and choose a machine.

Each machine has its own glyph and color. Files, inspectors, and terminals keep the identity of the machine that owns them.

## What you can do

- Browse one or more chosen folders on local or enrolled computers.
- Preview images, video, audio, PDFs, Markdown, code, and text files.
- Use image-grid browsing with cached thumbnails.
- Copy or cut and paste files and folders between locations. Eta transfers data directly between computers when needed and verifies file chunks as they arrive.
- Open a terminal in a local or enrolled computer's folder.
- Keep Explorer and file windows where you left them.

## Safety

Eta has **no application login**. Your LAN or Tailnet is the trust boundary.

Only run it on a network you trust, and expose only folders you intend to share. Enrolled computers can browse files and accept the filesystem and terminal actions Eta exposes to them. Do not put Eta directly on the public internet.

## Configuration

```sh
go run . --help
```

Common options:

```sh
--root PATH             # repeat for every folder Eta may expose
--ip lan|127.0.0.1      # network binding; defaults to lan
--port 7080             # HTTP port
--accent NAME           # this machine's identity color
--advertise-url URL     # address peers use when sending files here
```

Eta stores its identity, desktop layout, peer list, caches, and unfinished transfer data in your platform configuration/cache directories unless you provide explicit paths.

## Build

Eta requires Go and Node.js.

```sh
npm install
npm run build:web
go build .
./eta --root ~/Downloads
```

## License

MIT. See [LICENSE](LICENSE).
