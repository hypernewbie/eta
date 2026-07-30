# η eta

A Go-powered filesystem desktop for a private LAN and Tailnet.

## Current slice

- Phi-compatible `lan` binding: loopback, RFC 1918 LAN addresses, and Tailnet IPv4 addresses.
- No application login; deploy only on a trusted network/Tailnet.
- One or more explicitly exposed filesystem roots.
- Directory navigation (including a visible `..` parent row), downloads, and file metadata.
- Native image, audio (MP3/OGG/WAV and more), video, and PDF previews.
- Markdown rendering with Phi's vendored Marked, DOMPurify, and Highlight.js assets.
- Phi's complete 22-color highlight registry.
- Persistent per-host identity: stable random ID, deterministic Egyptian glyph, hostname, and accent exposed at `/api/identity`. The identity file defaults to the platform user config directory; use `--identity-file` for an explicit location and `--accent` to override its accent.
- Symlinks are resolved server-side and rejected when they lead outside their configured root.
- Embedded static web UI with a Phi-adjacent obsidian/glass visual system.
- Explicit peer enrollment and host-identified remote Explorer windows through coordinator allowlisted proxies.
- Persistent disk cache plus a conservative 64MB RAM hot-range cache for remote reads; large/media reads remain streamed.
- Direct peer-to-peer resumable transfers with SHA-256 chunk verification and atomic root-contained finalization.
- Local PTY terminal sessions, opened from Explorer; terminal execution has the same Tailnet/LAN trust-boundary requirement as file access.

## Run

The browser source is TypeScript. Build it after changing `web/app.ts`:

```sh
npm install
npm run build:web
```

Then run eta:

```sh
go run . --root ~/Downloads --root /Volumes/Archive
```

The default root is eta's current working directory. The default port is `7080` and default binding is `lan`.

```sh
# Explicit development binding
go run . --ip 127.0.0.1 --port 7080 --root ~/Downloads
```

## CI

GitHub Actions validates Go formatting/build behavior (`go vet`, `go test`), TypeScript generation, and the Chromium Playwright desktop suite on every push and pull request.

## License

MIT. Eta includes vendored copies of Marked, DOMPurify, and Highlight.js from Phi; their upstream licenses continue to apply to those components.

## UI dependencies

The UI source is `web/app.ts`, compiled by TypeScript to `web/generated/app.js` for Go's embedded static server. It uses CDN-delivered [Shoelace](https://shoelace.style/), [Lucide](https://lucide.dev/), [Day.js](https://day.js.org/), and Prism's Autoloader + Line Numbers for code inspection. Marked, DOMPurify, and Highlight.js are vendored from Phi so Markdown viewing remains available without relying on another CDN. The Go server uses `golang.org/x/image`, `golang.org/x/sync`, and `github.com/creack/pty`; their module licenses apply to those dependencies.
