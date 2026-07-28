# η eta

A Go-powered, read-only filesystem viewer for a private LAN and Tailnet.

## Current slice

- Phi-compatible `lan` binding: loopback, RFC 1918 LAN addresses, and Tailnet IPv4 addresses.
- No application login; deploy only on a trusted network/Tailnet.
- One or more explicitly exposed filesystem roots.
- Directory navigation (including a visible `..` parent row), downloads, and file metadata.
- Native image, audio (MP3/OGG/WAV and more), video, and PDF previews.
- Markdown rendering with Phi's vendored Marked, DOMPurify, and Highlight.js assets.
- Phi's complete 22-color highlight registry.
- Symlinks are resolved server-side and rejected when they lead outside their configured root.
- Embedded static web UI with a Phi-adjacent obsidian/glass visual system.

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

## UI dependencies

The UI source is `web/app.ts`, compiled by TypeScript to `web/generated/app.js` for Go's embedded static server. It uses CDN-delivered [Shoelace](https://shoelace.style/), [Lucide](https://lucide.dev/), [Day.js](https://day.js.org/), and Prism's Autoloader + Line Numbers for code inspection. Marked, DOMPurify, and Highlight.js are vendored from Phi so Markdown viewing remains available without relying on another CDN. The Go server itself has no third-party dependencies.
