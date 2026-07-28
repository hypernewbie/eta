# η eta

A Go-powered, read-only filesystem viewer for a private LAN and Tailnet.

## Current slice

- Phi-compatible `lan` binding: loopback, RFC 1918 LAN addresses, and Tailnet IPv4 addresses.
- No application login; deploy only on a trusted network/Tailnet.
- One or more explicitly exposed filesystem roots.
- Directory navigation, text/image previews, and downloads.
- Symlinks are resolved server-side and rejected when they lead outside their configured root.
- Embedded static web UI with a Phi-adjacent obsidian/glass visual system.

## Run

```sh
go run . --root ~/Downloads --root /Volumes/Archive
```

The default root is eta's current working directory. The default port is `7080` and default binding is `lan`.

```sh
# Explicit development binding
go run . --ip 127.0.0.1 --port 7080 --root ~/Downloads
```

## UI dependencies

The browser UI uses CDN-delivered [Shoelace](https://shoelace.style/), [Lucide](https://lucide.dev/), and [Day.js](https://day.js.org/). The Go server itself has no third-party dependencies.
