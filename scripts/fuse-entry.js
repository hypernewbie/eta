// Bundled by scripts/vendor.mjs into web/vendor/fuse.bundle.js. Fuse.js
// only ships ESM/CJS builds (no plain global script anymore), and Eta's
// client bundle is a single non-module script — same reason
// noble-hashes needs bundling rather than a copy. Assigning the global
// directly here, rather than via esbuild's --global-name, keeps the
// browser-side reference plain window.Fuse instead of window.Fuse.default.
import Fuse from "fuse.js";
globalThis.Fuse = Fuse;
