// Copies every runtime browser dependency out of node_modules and into
// web/vendor/, which is embedded in the Go binary by //go:embed all:web.
//
// Eta is a LAN/Tailnet filesystem browser. Loading its own UI from public
// CDNs meant it could not render without internet access, leaked a request
// to a third party on every page load, and pinned page load time to
// whatever those CDNs were doing (measured 1.5-5.5s per asset, with stalls
// long enough to time out a 30s browser test).
//
// Three of the dependencies also fetched *more* at runtime, which is the
// part that makes "just vendor the script tags" insufficient:
//
//   - Shoelace lazily imports a component chunk per custom element used.
//     Only the closure for the four components Eta uses is copied, found
//     by following relative imports; that is 47 files, not the 15MB CDN
//     tree (8.5MB of which is an icon set Eta never references).
//   - Prism's autoloader fetches a grammar per language on demand, so the
//     whole components directory is copied and languages_path is pointed
//     at it in web/app.ts.
//   - iconify-icon fetched icon data from api.iconify.design. The component
//     is gone entirely: Eta uses 13 fixed file-type icons, so their SVG
//     bodies are emitted as a plain lookup table and rendered inline. That
//     removes a dependency whose whole job was resolving icons at runtime,
//     along with any code path that could reach for the network for one.
//
// Run with: npm run vendor
import {
  cpSync,
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { build } from "esbuild";

const OUT = "web/vendor";
const NM = "node_modules";

function copy(from, to) {
  const dest = join(OUT, to);
  mkdirSync(dirname(dest), { recursive: true });
  cpSync(from, dest, { recursive: true });
}

// --- Shoelace: only the four components Eta uses, plus their closure ----
// The autoloader resolves a component by tag name at runtime, so anything
// not copied here simply never loads. That is intentional: adding an
// <sl-*> element to the markup means adding it to this list.
const SL_COMPONENTS = ["alert", "button", "dialog", "spinner"];
const SL = `${NM}/@shoelace-style/shoelace/cdn`;

function shoelaceClosure() {
  const seen = new Set();
  const walk = (file) => {
    if (seen.has(file) || !existsSync(file)) return;
    seen.add(file);
    const src = readFileSync(file, "utf8");
    const specs = src.matchAll(
      /from\s*["']([^"']+)["']|import\s*\(\s*["']([^"']+)["']\s*\)/g,
    );
    for (const m of specs) {
      const spec = m[1] || m[2];
      if (spec?.startsWith(".")) walk(resolve(dirname(file), spec));
    }
  };
  for (const name of SL_COMPONENTS) walk(`${SL}/components/${name}/${name}.js`);
  walk(`${SL}/shoelace-autoloader.js`);
  return seen;
}

// --- icons: bake only the icons referenced by web/app.ts ----------------
function iconCollection() {
  const app = readFileSync("web/app.ts", "utf8");
  const used = [
    ...new Set(
      [...app.matchAll(/vscode-icons:([a-z0-9-]+)/g)].map((m) => m[1]),
    ),
  ].sort();
  const full = JSON.parse(
    readFileSync(`${NM}/@iconify-json/vscode-icons/icons.json`, "utf8"),
  );
  const icons = {};
  for (const name of used) {
    if (!full.icons[name]) throw new Error(`icon not in collection: ${name}`);
    icons[name] = full.icons[name];
  }
  return {
    used,
    collection: {
      prefix: full.prefix,
      icons,
      width: full.width,
      height: full.height,
    },
  };
}

const FONTS = [
  [`${NM}/@fontsource/inter/files/inter-latin-300-normal.woff2`, "Inter", 300],
  [`${NM}/@fontsource/inter/files/inter-latin-400-normal.woff2`, "Inter", 400],
  [`${NM}/@fontsource/inter/files/inter-latin-500-normal.woff2`, "Inter", 500],
  [`${NM}/@fontsource/inter/files/inter-latin-600-normal.woff2`, "Inter", 600],
  [
    `${NM}/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-400-normal.woff2`,
    "JetBrains Mono",
    400,
  ],
  [
    `${NM}/@fontsource/jetbrains-mono/files/jetbrains-mono-latin-500-normal.woff2`,
    "JetBrains Mono",
    500,
  ],
];

// Everything this script generates. Cleaned before each run so a removed
// dependency cannot linger.
const GENERATED = [
  "shoelace",
  "winbox",
  "xterm",
  "lucide",
  "dayjs",
  "icons",
  "iconify",
  "prism",
  "fonts",
  "marked.min.js",
  "purify.min.js",
  "noble-hashes",
  "fuse.bundle.js",
];

// --- noble-hashes: bundled, not copied ----------------------------------
// Every other dependency here ships a prebuilt global/UMD file that a
// plain <script> tag can load, matching Eta's client bundle (tsconfig
// module:"None", one non-module output file). @noble/hashes ships ESM
// only, so this is the one dependency that needs an actual bundle rather
// than a copy — esbuild folds its handful of relative-import source files
// into one IIFE that assigns window.NobleHashes, run once here at vendor
// time and committed like every other file in this directory.
async function bundleNobleHashes() {
  await build({
    entryPoints: ["scripts/noble-hashes-entry.js"],
    bundle: true,
    minify: true,
    format: "iife",
    globalName: "NobleHashes",
    outfile: join(OUT, "noble-hashes/noble-hashes.bundle.js"),
  });
}

// Fuse.js: same story as noble-hashes — ESM/CJS only, no plain global
// build since v7 dropped it, so it is bundled rather than copied. Used
// for the Explorer search bar (fuzzy filename matching) rather than a
// hand-rolled substring search.
async function bundleFuse() {
  await build({
    entryPoints: ["scripts/fuse-entry.js"],
    bundle: true,
    minify: true,
    format: "iife",
    outfile: join(OUT, "fuse.bundle.js"),
  });
}

async function main() {
  for (const entry of GENERATED) {
    rmSync(join(OUT, entry), { recursive: true, force: true });
  }
  mkdirSync(OUT, { recursive: true });

  for (const file of shoelaceClosure()) {
    copy(file, join("shoelace", relative(resolve(SL), resolve(file))));
  }
  copy(`${SL}/themes/dark.css`, "shoelace/themes/dark.css");

  copy(`${NM}/winbox/dist/winbox.bundle.min.js`, "winbox/winbox.bundle.min.js");
  copy(`${NM}/winbox/dist/css/winbox.min.css`, "winbox/winbox.min.css");

  copy(`${NM}/@xterm/xterm/lib/xterm.js`, "xterm/xterm.js");
  copy(`${NM}/@xterm/xterm/css/xterm.css`, "xterm/xterm.css");
  copy(`${NM}/@xterm/addon-fit/lib/addon-fit.js`, "xterm/addon-fit.js");

  copy(`${NM}/lucide/dist/umd/lucide.min.js`, "lucide/lucide.min.js");
  copy(`${NM}/dayjs/dayjs.min.js`, "dayjs/dayjs.min.js");

  // Prism: core, the two plugins Eta loads, and every grammar so the
  // autoloader can resolve any language offline.
  copy(`${NM}/prismjs/prism.js`, "prism/prism.js");
  copy(
    `${NM}/prismjs/plugins/autoloader/prism-autoloader.min.js`,
    "prism/plugins/autoloader/prism-autoloader.min.js",
  );
  copy(
    `${NM}/prismjs/plugins/line-numbers/prism-line-numbers.min.js`,
    "prism/plugins/line-numbers/prism-line-numbers.min.js",
  );
  copy(
    `${NM}/prismjs/plugins/line-numbers/prism-line-numbers.css`,
    "prism/plugins/line-numbers/prism-line-numbers.css",
  );
  // Only the minified grammars: the autoloader requests prism-<lang>.min.js,
  // and shipping the unminified twins as well doubled the vendored tree.
  mkdirSync(join(OUT, "prism/components"), { recursive: true });
  for (const file of readdirSync(`${NM}/prismjs/components`)) {
    if (file.endsWith(".min.js")) {
      copy(`${NM}/prismjs/components/${file}`, `prism/components/${file}`);
    }
  }

  copy(`${NM}/marked/lib/marked.umd.js`, "marked.min.js");
  copy(`${NM}/dompurify/dist/purify.min.js`, "purify.min.js");

  const { used, collection } = iconCollection();
  // A plain lookup table, not an icon framework: web/app.ts renders these
  // inline as <svg>, keyed by the exact names app.ts already references so
  // this script can keep discovering the set from that one source.
  mkdirSync(join(OUT, "icons"), { recursive: true });
  const table = Object.fromEntries(
    Object.entries(collection.icons).map(([name, icon]) => [
      `${collection.prefix}:${name}`,
      {
        body: icon.body,
        width: icon.width ?? collection.width ?? 24,
        height: icon.height ?? collection.height ?? 24,
      },
    ]),
  );
  writeFileSync(
    join(OUT, "icons/file-icons.js"),
    `// Generated by scripts/vendor.mjs from @iconify-json/vscode-icons.\n` +
      `// The ${used.length} file-type icons web/app.ts references, inlined so\n` +
      `// nothing resolves an icon over the network. Loaded before app.js.\n` +
      `window.ETA_FILE_ICONS = ${JSON.stringify(table)};\n`,
  );

  let fontCss = "/* Generated by scripts/vendor.mjs. */\n";
  for (const [src, family, weight] of FONTS) {
    const file = src.split("/").pop();
    copy(src, `fonts/${file}`);
    fontCss +=
      `@font-face {\n` +
      `  font-family: "${family}";\n` +
      `  font-style: normal;\n` +
      `  font-weight: ${weight};\n` +
      `  font-display: swap;\n` +
      `  src: url("./${file}") format("woff2");\n` +
      `}\n`;
  }
  writeFileSync(join(OUT, "fonts/fonts.css"), fontCss);

  await bundleNobleHashes();
  await bundleFuse();

  console.log(`vendored shoelace components: ${SL_COMPONENTS.join(", ")}`);
  console.log(`vendored icons: ${used.length}`);
}

await main();
