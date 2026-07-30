interface Window {
  lucide?: any;
  dayjs?: any;
  marked?: any;
  DOMPurify?: any;
  hljs?: any;
  Prism?: any;
  WinBox?: new (options: WinBoxOptions) => WinBoxInstance;
}

type WinBoxOptions = {
  title: string;
  mount: HTMLElement;
  x?: number | "center";
  y?: number | "center";
  width?: number;
  height?: number;
  bottom?: number;
  class?: string;
  close?: boolean;
  min?: boolean;
  max?: boolean;
  onclose?: () => boolean | void;
  onfocus?: () => void;
  onmove?: () => void;
  onresize?: () => void;
  onmaximize?: () => void;
  onrestore?: () => void;
  onminimize?: () => void;
};
type WinBoxInstance = {
  focus: () => void;
  restore: () => void;
  minimize: () => void;
  x: number;
  y: number;
  width: number;
  height: number;
  min: boolean;
  max: boolean;
};

declare const dayjs: any;

type Theme = {
  accent: string;
  accentGlow: string;
  accentDim: string;
  accentBright: string;
};
type Root = { id: number; name: string };
type Peer = { url: string; name: string; id: string; accent: string; glyph: string };
type Entry = {
  name: string;
  path: string;
  kind: "directory" | "file";
  size: number;
  modified: string;
};
type AppState = {
  roots: Root[];
  root: number;
  path: string;
  selected: Entry | null;
  rawText: string;
  view: "list" | "grid";
  peer: Peer | null;
};
type ExplorerView = {
  key: string;
  panel: HTMLElement;
  state: AppState;
  element: (name: string) => HTMLElement;
};
type HostIdentity = {
  id: string;
  hostname: string;
  accent: string;
  glyph: string;
};
type PersistedWindow = {
  kind: "explorer" | "file";
  root: number;
  path?: string;
  x: number;
  y: number;
  width: number;
  height: number;
  minimized: boolean;
  maximized: boolean;
};

// Eta shares Phi's complete accent registry. Keep names and values in sync.
const COLORS: Record<string, Theme> = {
  purple: {
    accent: "#7c6af7",
    accentGlow: "rgba(124, 106, 247, 0.15)",
    accentDim: "#5b4ec2",
    accentBright: "#9a8dfa",
  },
  blue: {
    accent: "#38bdf8",
    accentGlow: "rgba(56, 189, 248, 0.15)",
    accentDim: "#0284c7",
    accentBright: "#7dd3fc",
  },
  green: {
    accent: "#10b981",
    accentGlow: "rgba(16, 185, 129, 0.15)",
    accentDim: "#047857",
    accentBright: "#34d399",
  },
  amber: {
    accent: "#fbbf24",
    accentGlow: "rgba(251, 191, 36, 0.15)",
    accentDim: "#b45309",
    accentBright: "#fcd34d",
  },
  red: {
    accent: "#f87171",
    accentGlow: "rgba(248, 113, 113, 0.15)",
    accentDim: "#b91c1c",
    accentBright: "#fca5a5",
  },
  pink: {
    accent: "#ec4899",
    accentGlow: "rgba(236, 72, 153, 0.15)",
    accentDim: "#be185d",
    accentBright: "#f472b6",
  },
  teal: {
    accent: "#14b8a6",
    accentGlow: "rgba(20, 184, 166, 0.15)",
    accentDim: "#0f766e",
    accentBright: "#5eead4",
  },
  indigo: {
    accent: "#6366f1",
    accentGlow: "rgba(99, 102, 241, 0.15)",
    accentDim: "#4338ca",
    accentBright: "#818cf8",
  },
  orange: {
    accent: "#f97316",
    accentGlow: "rgba(249, 115, 22, 0.15)",
    accentDim: "#c2410c",
    accentBright: "#fdba74",
  },
  cyan: {
    accent: "#06b6d4",
    accentGlow: "rgba(6, 182, 212, 0.15)",
    accentDim: "#0e7490",
    accentBright: "#67e8f9",
  },
  rose: {
    accent: "#f43f5e",
    accentGlow: "rgba(244, 63, 94, 0.15)",
    accentDim: "#be123c",
    accentBright: "#fb7185",
  },
  lime: {
    accent: "#84cc16",
    accentGlow: "rgba(132, 204, 22, 0.15)",
    accentDim: "#4d7c0f",
    accentBright: "#a3e635",
  },
  white: {
    accent: "#ffffff",
    accentGlow: "rgba(255, 255, 255, 0.15)",
    accentDim: "#94a3b8",
    accentBright: "#ffffff",
  },
  gold: {
    accent: "#d4af37",
    accentGlow: "rgba(212, 175, 55, 0.15)",
    accentDim: "#997a15",
    accentBright: "#f3e5ab",
  },
  violet: {
    accent: "#a78bfa",
    accentGlow: "rgba(167, 139, 250, 0.15)",
    accentDim: "#6d28d9",
    accentBright: "#ddd6fe",
  },
  emerald: {
    accent: "#059669",
    accentGlow: "rgba(5, 150, 105, 0.15)",
    accentDim: "#065f46",
    accentBright: "#34d399",
  },
  neon: {
    accent: "#00f0ff",
    accentGlow: "rgba(0, 240, 255, 0.15)",
    accentDim: "#008b99",
    accentBright: "#70f8ff",
  },
  coral: {
    accent: "#e07a5f",
    accentGlow: "rgba(224, 122, 95, 0.15)",
    accentDim: "#9e4731",
    accentBright: "#f4a261",
  },
  fuchsia: {
    accent: "#d946ef",
    accentGlow: "rgba(217, 70, 239, 0.15)",
    accentDim: "#86198f",
    accentBright: "#f0abfc",
  },
  canary: {
    accent: "#ffee10",
    accentGlow: "rgba(255, 238, 16, 0.18)",
    accentDim: "#b8ad00",
    accentBright: "#ffff66",
  },
  copper: {
    accent: "#d35400",
    accentGlow: "rgba(211, 84, 0, 0.18)",
    accentDim: "#8a3700",
    accentBright: "#e59866",
  },
  mint: {
    accent: "#2ed573",
    accentGlow: "rgba(46, 213, 115, 0.18)",
    accentDim: "#1a8a4a",
    accentBright: "#7bed9f",
  },
};

let dialogView: ExplorerView | null = null;
type ExplorerEntry = { view: ExplorerView; entry: Entry };
let contextEntry: ExplorerEntry | null = null;
// Explorer clipboard intentionally models locations, not PCs. The transfer
// transport is selected only when a paste crosses to an enrolled peer.
let explorerClipboard: ExplorerEntry | null = null;
let explorerSequence = 0;
let localHost: HostIdentity = {
  id: "local",
  hostname: "local",
  accent: "purple",
  glyph: "◆",
};

function createExplorerView(key: string, panel: HTMLElement): ExplorerView {
  return {
    key,
    panel,
    state: {
      roots: [],
      root: 0,
      path: "",
      selected: null,
      rawText: "",
      view:
        localStorage.getItem("eta_directory_view") === "grid" ? "grid" : "list",
      peer: null,
    },
    element: (name: string) => {
      const element = panel.querySelector(`[data-explorer="${name}"]`);
      if (!element) throw new Error(`Missing explorer element: ${name}`);
      return element as HTMLElement;
    },
  };
}
const $: any = (selector: string) => {
  const element = document.querySelector(selector);
  if (!element) throw new Error(`Missing required element: ${selector}`);
  return element;
};
const imageExtensions = new Set([
  "avif",
  "bmp",
  "gif",
  "ico",
  "jpeg",
  "jpg",
  "png",
  "svg",
  "webp",
]);
const thumbnailExtensions = new Set(["gif", "jpeg", "jpg", "png"]);
const audioExtensions = new Set([
  "aac",
  "flac",
  "m4a",
  "mp3",
  "ogg",
  "opus",
  "wav",
]);
const videoExtensions = new Set(["m4v", "mov", "mp4", "ogv", "webm"]);
const markdownExtensions = new Set(["markdown", "md"]);
const htmlExtensions = new Set(["htm", "html"]);
// Prism Autoloader provides the language grammars; this only maps file names
// and extensions to Prism's component names. Unknown text remains readable.
const codeLanguages: Record<string, [string, string]> = {
  asm: ["nasm", "Assembly"],
  bash: ["bash", "Bash"],
  bat: ["batch", "Batch"],
  c: ["c", "C"],
  cc: ["cpp", "C++"],
  cmake: ["cmake", "CMake"],
  cpp: ["cpp", "C++"],
  cs: ["csharp", "C#"],
  css: ["css", "CSS"],
  cue: ["cue", "CUE"],
  dockerfile: ["docker", "Dockerfile"],
  elm: ["elm", "Elm"],
  ex: ["elixir", "Elixir"],
  exs: ["elixir", "Elixir"],
  fs: ["fsharp", "F#"],
  go: ["go", "Go"],
  graphql: ["graphql", "GraphQL"],
  h: ["c", "C"],
  hpp: ["cpp", "C++"],
  htm: ["markup", "HTML"],
  html: ["markup", "HTML"],
  ini: ["ini", "INI"],
  java: ["java", "Java"],
  js: ["javascript", "JavaScript"],
  json: ["json", "JSON"],
  jsonc: ["json", "JSON"],
  jsx: ["jsx", "JSX"],
  kt: ["kotlin", "Kotlin"],
  kts: ["kotlin", "Kotlin"],
  less: ["less", "Less"],
  lua: ["lua", "Lua"],
  m: ["objectivec", "Objective-C"],
  makefile: ["makefile", "Makefile"],
  mdx: ["jsx", "MDX"],
  php: ["php", "PHP"],
  pl: ["perl", "Perl"],
  proto: ["protobuf", "Protocol Buffers"],
  py: ["python", "Python"],
  r: ["r", "R"],
  rb: ["ruby", "Ruby"],
  rs: ["rust", "Rust"],
  sass: ["sass", "Sass"],
  scss: ["scss", "SCSS"],
  sh: ["bash", "Shell"],
  sol: ["solidity", "Solidity"],
  sql: ["sql", "SQL"],
  swift: ["swift", "Swift"],
  toml: ["toml", "TOML"],
  ts: ["typescript", "TypeScript"],
  tsx: ["tsx", "TSX"],
  vb: ["visual-basic", "Visual Basic"],
  vue: ["markup", "Vue"],
  xml: ["markup", "XML"],
  yaml: ["yaml", "YAML"],
  yml: ["yaml", "YAML"],
  zig: ["zig", "Zig"],
};

function setTheme(name: string, persist = true) {
  const theme = COLORS[name] || COLORS.purple;
  document.documentElement.style.setProperty("--accent", theme.accent);
  document.documentElement.style.setProperty("--accent-glow", theme.accentGlow);
  document.documentElement.style.setProperty("--accent-dim", theme.accentDim);
  document.documentElement.style.setProperty(
    "--accent-bright",
    theme.accentBright,
  );
  if (persist) localStorage.setItem("eta_theme_color", name);
}
function hostWindowTitle(title: string) {
  return `${localHost.glyph} ${title}`;
}
async function loadLocalHost() {
  const identity = (await api("/api/identity")) as HostIdentity;
  localHost = identity;
  setTheme(identity.accent, false);
  $("#host-name").textContent = identity.hostname;
}
function iconify() {
  window.lucide?.createIcons({ attrs: { "stroke-width": 1.65 } });
}
function escapeHTML(value) {
  const node = document.createElement("span");
  node.textContent = value;
  return node.innerHTML;
}
function sourceURL(view: ExplorerView, endpoint: string, params: Record<string, string>) {
  const query = new URLSearchParams(params);
  if (view.state.peer) query.set("peer", view.state.peer.url);
  return `${view.state.peer ? "/api/remote" : "/api"}/${endpoint}?${query}`;
}
function fileURL(view: ExplorerView, path: string, download = false) {
  return sourceURL(view, "file", { root: String(view.state.root), path, ...(download ? { download: "1" } : {}) });
}
function previewURL(view: ExplorerView, path: string) {
  return sourceURL(view, "preview", { root: String(view.state.root), path });
}
function thumbnailURL(view: ExplorerView, path: string, edge = 320) {
  return sourceURL(view, "thumbnail", { root: String(view.state.root), path, size: String(edge) });
}
function extension(name) {
  return name.includes(".")
    ? name.split(".").pop().toLowerCase()
    : name.toLowerCase();
}
function bytes(value) {
  if (!value) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1,
  );
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}
function date(value) {
  return window.dayjs
    ? dayjs(value).format("MMM D, YYYY")
    : new Date(value).toLocaleDateString();
}
function parentPath(view: ExplorerView) {
  return view.state.path.split("/").filter(Boolean).slice(0, -1).join("/");
}

async function api(path: string, init?: RequestInit) {
  const response = await fetch(path, init);
  const body = await response.json().catch(() => ({}));
  if (!response.ok)
    throw new Error(body.error || `Request failed (${response.status})`);
  return body;
}
function showToast(message, variant = "danger") {
  const alert = $("#error-toast");
  alert.variant = variant;
  $("#error-message").textContent = message;
  alert.toast();
}

type DesktopWindow = {
  title: string;
  window: WinBoxInstance;
  state: () => Omit<
    PersistedWindow,
    "x" | "y" | "width" | "height" | "minimized" | "maximized"
  >;
};
const desktopWindows = new Map<string, DesktopWindow>();
let enrolledPeers: Peer[] = [];
const explorerViews = new Map<string, ExplorerView>();
let restoringDesktop = false;
let stateSaveTimer: number | undefined;

function capturedDesktopState(): PersistedWindow[] {
  return [...desktopWindows.values()].map((item) => ({
    ...item.state(),
    x: Math.max(0, Math.round(item.window.x)),
    y: Math.max(0, Math.round(item.window.y)),
    width: Math.max(0, Math.round(item.window.width)),
    height: Math.max(0, Math.round(item.window.height)),
    minimized: item.window.min,
    maximized: item.window.max,
  }));
}
function statePayload() {
  return JSON.stringify({ version: 1, windows: capturedDesktopState() });
}
async function saveDesktopState() {
  try {
    await api("/api/state", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: statePayload(),
    });
  } catch {
    // State persistence must never interrupt normal desktop use.
  }
}
function scheduleDesktopSave() {
  if (!desktopEnabled() || restoringDesktop) return;
  if (stateSaveTimer) window.clearTimeout(stateSaveTimer);
  stateSaveTimer = window.setTimeout(() => void saveDesktopState(), 400);
}

function refreshTaskStrip() {
  const taskStrip = $("#task-strip");
  const peerButtons = enrolledPeers.map((peer) => `<sl-button size="small" class="task-button peer-launcher" style="--window-accent:${escapeHTML(COLORS[peer.accent]?.accent || "#7c6af7")}" data-peer="${escapeHTML(peer.url)}"><span class="peer-glyph">${escapeHTML(peer.glyph)}</span>${escapeHTML(peer.name)}</sl-button>`);
  const windows = [...desktopWindows.entries()].map(([key, item]) => `<sl-button size="small" class="task-button" data-window="${escapeHTML(key)}"><i data-lucide="${key.startsWith("explorer:") ? "folder-open" : "file-text"}"></i>${escapeHTML(item.title)}</sl-button>`);
  taskStrip.innerHTML = [...peerButtons, ...windows].join("");
  iconify();
}
function focusDesktopWindow(key: string) {
  const item = desktopWindows.get(key);
  if (!item) return;
  item.window.restore();
  item.window.focus();
}
function desktopEnabled() {
  return Boolean(window.WinBox) && document.body.classList.contains("windowed");
}
function createExplorerPanel() {
  const template = $("#explorer-template") as HTMLTemplateElement;
  const panel = template.content.firstElementChild?.cloneNode(
    true,
  ) as HTMLElement | null;
  if (!panel) throw new Error("Explorer template is empty");
  $("#explorer-backstore").append(panel);
  return panel;
}
async function openExplorerWindow(restored?: PersistedWindow, peer: Peer | null = null) {
  if (!window.WinBox || window.innerWidth < 700) return;
  document.body.classList.add("windowed");
  const number = ++explorerSequence;
  const key = `explorer:${number}`;
  const explorerName = number === 1 ? "Explorer" : `Explorer ${number}`;
  const title = peer ? `${peer.glyph} ${explorerName}` : hostWindowTitle(explorerName);
  const panel = createExplorerPanel();
  if (peer) panel.style.setProperty("--window-accent", COLORS[peer.accent]?.accent || "#7c6af7");
  const view = createExplorerView(key, panel);
  view.state.peer = peer;
  const windowChanged = () => {
    refreshTaskStrip();
    scheduleDesktopSave();
  };
  const explorer = new window.WinBox({
    title,
    mount: panel,
    class: peer ? "eta-window peer-window" : "eta-window",
    x: restored ? restored.x : "center",
    y: restored?.y ?? 64,
    width:
      restored?.width ??
      Math.min(1240, Math.max(640, Math.floor(window.innerWidth * 0.86))),
    height:
      restored?.height ??
      Math.min(820, Math.max(420, Math.floor(window.innerHeight * 0.76))),
    bottom: 40,
    max: restored?.maximized,
    min: restored?.minimized,
    onclose: () => {
      desktopWindows.delete(key);
      explorerViews.delete(key);
      windowChanged();
      queueMicrotask(() => panel.remove());
    },
    onfocus: windowChanged,
    onmove: windowChanged,
    onresize: windowChanged,
    onmaximize: windowChanged,
    onrestore: windowChanged,
    onminimize: windowChanged,
  });
  desktopWindows.set(key, {
    title,
    window: explorer,
    state: () => ({
      kind: "explorer",
      root: view.state.root,
      path: view.state.path,
    }),
  });
  explorerViews.set(key, view);
  refreshTaskStrip();
  scheduleDesktopSave();
  await initializeExplorer(view, restored);
}

function renderBreadcrumbs(view: ExplorerView) {
  const root = view.state.roots[view.state.root];
  const crumbs = [{ name: root?.name || "Root", path: "" }];
  let current = "";
  for (const part of view.state.path.split("/").filter(Boolean)) {
    current = current ? `${current}/${part}` : part;
    crumbs.push({ name: part, path: current });
  }
  view.element("breadcrumbs").innerHTML = crumbs
    .map(
      (crumb, index) =>
        `${index ? '<i class="crumb-separator" data-lucide="chevron-right"></i>' : ""}<button class="breadcrumb" data-path="${escapeHTML(crumb.path)}">${escapeHTML(crumb.name)}</button>`,
    )
    .join("");
}

function fileIcon(entry: Entry) {
  if (entry.kind === "directory")
    return '<i class="entry-icon" data-lucide="folder"></i>';
  const icons: Record<string, string> = {
    go: "vscode-icons:file-type-go",
    js: "vscode-icons:file-type-js",
    ts: "vscode-icons:file-type-typescript",
    py: "vscode-icons:file-type-python",
    rs: "vscode-icons:file-type-rust",
    md: "vscode-icons:file-type-markdown",
    json: "vscode-icons:file-type-json",
    html: "vscode-icons:file-type-html",
    css: "vscode-icons:file-type-css",
    pdf: "vscode-icons:file-type-pdf2",
    jpg: "vscode-icons:file-type-image",
    png: "vscode-icons:file-type-image",
    mp3: "vscode-icons:file-type-audio",
    mp4: "vscode-icons:file-type-video",
  };
  const icon = icons[extension(entry.name)];
  return icon
    ? `<iconify-icon class="entry-icon file-type-icon" icon="${icon}"></iconify-icon>`
    : '<i class="entry-icon" data-lucide="file"></i>';
}
function entryMarkup(entry: Entry) {
  return `<button class="entry ${entry.kind}" data-path="${escapeHTML(entry.path)}" data-kind="${entry.kind}" data-size="${entry.size}" data-modified="${entry.modified}"><span class="entry-name-col">${fileIcon(entry)}<span class="entry-name">${escapeHTML(entry.name)}</span></span><span class="entry-meta">${date(entry.modified)}</span><span class="entry-meta">${entry.kind === "directory" ? "—" : bytes(entry.size)}</span></button>`;
}
function gridEntryMarkup(view: ExplorerView, entry: Entry) {
  const image =
    entry.kind === "file" && thumbnailExtensions.has(extension(entry.name));
  const visual = image
    ? `<img class="thumbnail" loading="lazy" decoding="async" src="${thumbnailURL(view, entry.path)}" alt="">`
    : `<span class="grid-icon"><i data-lucide="${entry.kind === "directory" ? "folder" : "file"}"></i></span>`;
  return `<button class="entry grid-entry ${entry.kind}" data-path="${escapeHTML(entry.path)}" data-kind="${entry.kind}" data-size="${entry.size}" data-modified="${entry.modified}">${visual}<span class="grid-name">${escapeHTML(entry.name)}</span><span class="grid-meta">${entry.kind === "directory" ? "Folder" : bytes(entry.size)}</span></button>`;
}
function renderEntries(view: ExplorerView, entries: Entry[]) {
  view.element("item-count").textContent =
    `${entries.length} ${entries.length === 1 ? "item" : "items"}`;
  const container = view.element("entries");
  view
    .element("file-table")
    .classList.toggle("grid-view", view.state.view === "grid");
  container.classList.toggle("image-grid", view.state.view === "grid");
  const parent = view.state.path
    ? '<button class="entry parent" data-parent="true"><span class="entry-name-col"><i class="entry-icon" data-lucide="corner-left-up"></i><span class="entry-name">..</span></span><span class="entry-meta">Parent folder</span><span class="entry-meta">—</span></button>'
    : "";
  if (!entries.length) {
    container.innerHTML = `${parent}<div class="empty"><div><i data-lucide="package-open"></i>This folder is empty.</div></div>`;
    iconify();
    return;
  }
  const renderer =
    view.state.view === "grid"
      ? (entry: Entry) => gridEntryMarkup(view, entry)
      : entryMarkup;
  container.innerHTML = parent + entries.map(renderer).join("");
  iconify();
}

async function navigate(view: ExplorerView, path = "") {
  view.state.path = path;
  view.element("entries").innerHTML =
    '<div class="empty"><sl-spinner></sl-spinner></div>';
  renderBreadcrumbs(view);
  iconify();
  try {
    const result = await api(sourceURL(view, "list", { root: String(view.state.root), path }));
    if (result.entry && result.entry.kind !== "directory") {
      await preview(view, result.entry);
      return;
    }
    renderBreadcrumbs(view);
    renderEntries(view, result.entries || []);
  } catch (error) {
    showToast(error.message);
    renderEntries(view, []);
  }
}

async function loadText(view: ExplorerView, entry: Entry) {
  const result = await api(previewURL(view, entry.path));
  if (result.binary)
    return { text: "", binary: true, truncated: result.truncated };
  return result;
}
function fileFacts(entry) {
  return `<div class="preview-facts"><span>${escapeHTML(entry.name)}</span><span>${bytes(entry.size)}</span><span>${date(entry.modified)}</span></div>`;
}
function renderMarkdown(raw, truncated) {
  if (!window.marked || !window.DOMPurify)
    return `<pre class="preview-text">${escapeHTML(raw)}</pre>`;
  window.marked.setOptions({ gfm: true, breaks: false });
  const html = window.DOMPurify.sanitize(window.marked.parse(raw), {
    USE_PROFILES: { html: true },
  });
  return `<article class="markdown-preview">${html}</article>${truncated ? '<p class="preview-note">Preview truncated at 512 KB.</p>' : ""}`;
}
function renderText(raw, truncated, ext) {
  const language = codeLanguages[ext];
  if (!language)
    return `<pre class="preview-text">${escapeHTML(raw)}${truncated ? "\n\n… preview truncated at 512 KB" : ""}</pre>`;
  const [prismLanguage, label] = language;
  return `<section class="code-inspector"><header class="code-toolbar"><span>${label}</span><span>${truncated ? "preview truncated at 512 KB" : ""}</span></header><pre class="preview-text line-numbers language-${prismLanguage}"><code class="language-${prismLanguage}">${escapeHTML(raw)}</code></pre></section>`;
}

async function renderPreview(
  view: ExplorerView,
  entry: Entry,
  container: HTMLElement,
) {
  container.innerHTML = '<div class="empty"><sl-spinner></sl-spinner></div>';
  let rawText = "";
  let binary = true;
  try {
    const ext = extension(entry.name);
    const source = fileURL(view, entry.path);
    let content = fileFacts(entry);
    if (imageExtensions.has(ext))
      content += `<img class="preview-image" alt="${escapeHTML(entry.name)}" src="${source}">`;
    else if (audioExtensions.has(ext))
      content += `<audio class="media-preview" controls preload="metadata" src="${source}"></audio>`;
    else if (videoExtensions.has(ext))
      content += `<video class="media-preview video-preview" controls preload="metadata" src="${source}"></video>`;
    else if (ext === "pdf")
      content += `<iframe class="pdf-preview" title="${escapeHTML(entry.name)}" src="${source}"></iframe>`;
    else if (htmlExtensions.has(ext))
      content += `<iframe class="pdf-preview html-preview" sandbox title="${escapeHTML(entry.name)}" src="${source}&embed=1"></iframe>`;
    else {
      const result = await loadText(view, entry);
      rawText = result.text || "";
      binary = result.binary;
      content += result.binary
        ? '<p class="preview-note">This looks like a binary file. Download it to inspect it locally.</p>'
        : markdownExtensions.has(ext)
          ? renderMarkdown(rawText, result.truncated)
          : renderText(rawText, result.truncated, ext);
    }
    container.innerHTML = content;
    container
      .querySelectorAll(".markdown-preview pre code")
      .forEach((block) => window.hljs?.highlightElement(block));
    container
      .querySelectorAll(".code-inspector code")
      .forEach((block) => window.Prism?.highlightElement(block));
    iconify();
  } catch (error) {
    container.innerHTML = `<p class="preview-note">${escapeHTML(error.message)}</p>`;
  }
  return { rawText, binary };
}

async function openTerminal(view: ExplorerView, entry: Entry) {
  const created = await api("/api/terminals", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ root: view.state.root, path: entry.path, columns: 120, rows: 32 }),
  });
  const panel = document.createElement("section");
  panel.className = "terminal-panel";
  const output = document.createElement("pre");
  output.className = "terminal-output";
  const input = document.createElement("input");
  input.className = "terminal-input";
  input.placeholder = "Type a command and press Enter";
  panel.append(output, input);
  let offset = 0;
  let stopped = false;
  const refresh = async () => {
    if (stopped) return;
    try {
      const result = await api(`/api/terminals/${encodeURIComponent(created.id)}?offset=${offset}`);
      if (result.output) { output.textContent += result.output; output.scrollTop = output.scrollHeight; }
      offset = result.offset;
      if (result.closed) { stopped = true; input.disabled = true; return; }
    } catch { stopped = true; input.disabled = true; return; }
    window.setTimeout(refresh, 180);
  };
  input.addEventListener("keydown", async (event) => {
    if (event.key !== "Enter" || input.disabled) return;
    const command = input.value; input.value = "";
    await api(`/api/terminals/${encodeURIComponent(created.id)}/input`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ input: `${command}\\n` }) });
  });
  const terminal = new window.WinBox({ title: hostWindowTitle(`Terminal — ${entry.name}`), mount: panel, class: "eta-window", x: "center", y: "center", width: Math.min(980, window.innerWidth - 64), height: Math.min(640, window.innerHeight - 120), bottom: 40, onclose: () => { stopped = true; void api(`/api/terminals/${encodeURIComponent(created.id)}`, { method: "DELETE" }); return false; } });
  terminal.focus(); input.focus(); void refresh();
}

async function openInspector(
  view: ExplorerView,
  entry: Entry,
  restored?: PersistedWindow,
) {
  const WinBox = window.WinBox;
  if (!WinBox) return;
  const key = `file:${view.state.root}:${entry.path}`;
  if (desktopWindows.has(key)) {
    focusDesktopWindow(key);
    return;
  }
  const panel = document.createElement("section");
  panel.className = "inspector-window";
  const content = document.createElement("div");
  content.className = "inspector-content";
  const actions = document.createElement("footer");
  actions.className = "inspector-actions";
  actions.innerHTML =
    '<sl-button class="inspector-copy" disabled><i data-lucide="copy"></i> Copy text</sl-button><sl-button class="inspector-download" variant="primary"><i data-lucide="download"></i> Download</sl-button>';
  panel.append(content, actions);
  const windowChanged = () => {
    refreshTaskStrip();
    scheduleDesktopSave();
  };
  const inspector = new WinBox({
    title: hostWindowTitle(entry.name),
    mount: panel,
    class: "eta-window",
    x: restored ? restored.x : "center",
    y: restored ? restored.y : "center",
    width: restored?.width ?? Math.min(1180, window.innerWidth - 64),
    height: restored?.height ?? Math.min(820, window.innerHeight - 120),
    bottom: 40,
    max: restored?.maximized,
    min: restored?.minimized,
    onclose: () => {
      desktopWindows.delete(key);
      windowChanged();
    },
    onfocus: windowChanged,
    onmove: windowChanged,
    onresize: windowChanged,
    onmaximize: windowChanged,
    onrestore: windowChanged,
    onminimize: windowChanged,
  });
  desktopWindows.set(key, {
    title: hostWindowTitle(entry.name),
    window: inspector,
    state: () => ({ kind: "file", root: view.state.root, path: entry.path }),
  });
  refreshTaskStrip();
  scheduleDesktopSave();
  const result = await renderPreview(view, entry, content);
  const copy = actions.querySelector(".inspector-copy") as any;
  copy.disabled = result.binary;
  copy.addEventListener("click", async () => {
    if (!result.rawText) return;
    try {
      await navigator.clipboard.writeText(result.rawText);
      showToast("Copied text", "success");
    } catch {
      showToast("Clipboard access was denied");
    }
  });
  actions
    .querySelector(".inspector-download")
    ?.addEventListener("click", () => {
      window.open(
        fileURL(view, entry.path, true),
        "_blank",
        "noopener",
      );
    });
}

async function preview(view: ExplorerView, entry: Entry) {
  view.state.selected = entry;
  view.state.rawText = "";
  if (desktopEnabled()) {
    await openInspector(view, entry);
    return;
  }
  $("#preview-dialog").label = entry.name;
  $("#copy-button").disabled = true;
  $("#preview-dialog").show();
  dialogView = view;
  const result = await renderPreview(view, entry, $("#preview-content"));
  view.state.rawText = result.rawText;
  $("#copy-button").disabled = result.binary;
}

async function copyText() {
  if (!dialogView?.state.rawText) return;
  try {
    await navigator.clipboard.writeText(dialogView.state.rawText);
    showToast("Copied text", "success");
  } catch {
    showToast("Clipboard access was denied");
  }
}

function bindExplorer(view: ExplorerView) {
  view.element("root-select").addEventListener("change", (event) => {
    view.state.root = Number((event.target as HTMLSelectElement).value);
    navigate(view);
  });
  view
    .element("refresh-button")
    .addEventListener("click", () => navigate(view, view.state.path));
  view.element("view-toggle").addEventListener("click", () => {
    view.state.view = view.state.view === "list" ? "grid" : "list";
    localStorage.setItem("eta_directory_view", view.state.view);
    view.element("view-toggle").title =
      view.state.view === "grid" ? "Use detailed list" : "Use image grid";
    navigate(view, view.state.path);
  });
  view
    .element("up-button")
    .addEventListener("click", () => navigate(view, parentPath(view)));
  view.element("breadcrumbs").addEventListener("click", (event) => {
    const button = (event.target as HTMLElement).closest(
      "[data-path]",
    ) as HTMLElement | null;
    if (button) navigate(view, button.dataset.path);
  });
  view.element("entries").addEventListener("contextmenu", (event) => {
    const row = (event.target as HTMLElement).closest(
      ".entry",
    ) as HTMLElement | null;
    if (!row || row.dataset.parent) return;
    event.preventDefault();
    contextEntry = {
      view,
      entry: {
        path: row.dataset.path || "",
        name: row.querySelector(".entry-name, .grid-name")?.textContent || "",
        kind: row.dataset.kind as Entry["kind"],
        size: Number(row.dataset.size),
        modified: row.dataset.modified || "",
      },
    };
    const menu = $("#file-context-menu");
    (
      menu.querySelector('[data-file-action="trusted-html"]') as HTMLElement
    ).hidden = !htmlExtensions.has(extension(contextEntry.entry.name));
    (menu.querySelector('[data-file-action="copy"]') as HTMLElement).hidden =
      contextEntry.entry.kind !== "file" || !!view.state.peer;
    (menu.querySelector('[data-file-action="paste"]') as HTMLElement).hidden =
      contextEntry.entry.kind !== "directory" || !explorerClipboard || !!explorerClipboard.view.state.peer;
    (menu.querySelector('[data-file-action="terminal"]') as HTMLElement).hidden = !!view.state.peer;
    menu.style.left = `${event.clientX}px`;
    menu.style.top = `${event.clientY}px`;
    menu.hidden = false;
    iconify();
  });
  view.element("entries").addEventListener("dblclick", (event) => {
    const row = (event.target as HTMLElement).closest(
      ".entry",
    ) as HTMLElement | null;
    if (!row) return;
    if (row.dataset.parent) {
      navigate(view, parentPath(view));
      return;
    }
    const item: Entry = {
      path: row.dataset.path || "",
      name: row.querySelector(".entry-name, .grid-name")?.textContent || "",
      kind: row.dataset.kind as Entry["kind"],
      size: Number(row.dataset.size),
      modified: row.dataset.modified || "",
    };
    item.kind === "directory" ? navigate(view, item.path) : preview(view, item);
  });
}

async function initializeExplorer(
  view: ExplorerView,
  restored?: PersistedWindow,
) {
  bindExplorer(view);
  view.element("view-toggle").title =
    view.state.view === "grid" ? "Use detailed list" : "Use image grid";
  try {
    view.state.roots = await api(sourceURL(view, "roots", {}));
    view.element("root-select").innerHTML = view.state.roots
      .map(
        (root) =>
          `<option value="${root.id}">${escapeHTML(root.name)}</option>`,
      )
      .join("");
    if (restored && restored.root < view.state.roots.length) {
      view.state.root = restored.root;
      (view.element("root-select") as HTMLSelectElement).value = String(
        restored.root,
      );
    }
    await navigate(view, restored?.path || "");
  } catch (error) {
    $("#server-status").textContent = "OFFLINE";
    showToast((error as Error).message);
  }
}

async function loadDesktopState(): Promise<PersistedWindow[]> {
  try {
    const state = (await api("/api/state")) as { windows?: PersistedWindow[] };
    return Array.isArray(state.windows) ? state.windows : [];
  } catch {
    return [];
  }
}
async function restoreFileWindow(restored: PersistedWindow) {
  try {
    const result = await api(
      `/api/list?${new URLSearchParams({ root: String(restored.root), path: restored.path || "" })}`,
    );
    if (result.entry?.kind !== "file") return;
    const view = { state: { root: restored.root } } as ExplorerView;
    await openInspector(view, result.entry, restored);
  } catch {
    // Missing roots and files are intentionally skipped during restore.
  }
}

async function boot() {
  setTheme(localStorage.getItem("eta_theme_color") || "purple");
  try {
    await loadLocalHost();
  } catch {
    // Explorer initialization reports an offline server through the normal UI.
  }
  if (window.WinBox && window.innerWidth >= 700) {
    try { enrolledPeers = await api("/api/peers"); } catch { enrolledPeers = []; }
    const restored = await loadDesktopState();
    restoringDesktop = true;
    const explorers = restored.filter((window) => window.kind === "explorer");
    if (explorers.length) {
      for (const window of explorers) await openExplorerWindow(window);
    } else {
      await openExplorerWindow();
    }
    for (const window of restored.filter((window) => window.kind === "file")) {
      await restoreFileWindow(window);
    }
    restoringDesktop = false;
  } else {
    const view = createExplorerView("fallback", createExplorerPanel());
    await initializeExplorer(view);
  }
  iconify();
}

async function pasteIntoFolder(destination: ExplorerEntry) {
  const source = explorerClipboard;
  if (!source || source.entry.kind !== "file" || source.view.state.peer) return;
  const destinationPath = destination.entry.path
    ? `${destination.entry.path}/${source.entry.name}`
    : source.entry.name;
  if (!destination.view.state.peer) {
    await api("/api/copy", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ sourceRoot: source.view.state.root, sourcePath: source.entry.path, destinationRoot: destination.view.state.root, destinationPath }) });
    showToast(`Copied ${source.entry.name}`, "success");
    await navigate(destination.view, destination.view.state.path);
    return;
  }
  const peer = destination.view.state.peer;
  const job = await api("/api/transfers/send", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ peer: peer.url, sourceRoot: source.view.state.root, sourcePath: source.entry.path, destinationRoot: destination.view.state.root, destinationPath }) });
  showToast(`Copying ${source.entry.name}…`);
  const poll = async () => { const status = await api(`/api/transfer-jobs/${encodeURIComponent(job.id)}`); if (!status.done) { window.setTimeout(() => void poll(), 300); return; }; if (status.error) { showToast(`Copy failed: ${status.error}`); return; }; showToast(`Copied ${source.entry.name}`, "success"); await navigate(destination.view, destination.view.state.path); };
  void poll();
}

$("#file-context-menu").addEventListener("click", async (event) => {
  const action = (event.target as HTMLElement).closest(
    "[data-file-action]",
  ) as HTMLElement | null;
  const target = contextEntry;
  $("#file-context-menu").hidden = true;
  contextEntry = null;
  if (!action || !target) return;
  try {
    if (action.dataset.fileAction === "terminal") {
      await openTerminal(target.view, target.entry);
      return;
    }
    if (action.dataset.fileAction === "copy") {
      explorerClipboard = target;
      showToast(`Copied ${target.entry.name} to clipboard`);
      return;
    }
    if (action.dataset.fileAction === "paste") {
      await pasteIntoFolder(target);
      return;
    }
    if (action.dataset.fileAction === "trusted-html") {
      window.open(
        fileURL(target.view, target.entry.path),
        "_blank",
        "noopener",
      );
      return;
    }
    if (action.dataset.fileAction === "rename") {
      const next = window.prompt("Rename to:", target.entry.name);
      if (!next || next === target.entry.name) return;
      await api("/api/rename", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          root: target.view.state.root,
          path: target.entry.path,
          target: next,
        }),
      });
    } else if (action.dataset.fileAction === "delete") {
      if (
        !window.confirm(`Delete ${target.entry.name}? This cannot be undone.`)
      )
        return;
      await api("/api/delete", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          root: target.view.state.root,
          path: target.entry.path,
        }),
      });
    }
    await navigate(target.view, target.view.state.path);
  } catch (error) {
    showToast((error as Error).message);
  }
});
document.addEventListener("pointerdown", (event) => {
  if (!(event.target as HTMLElement).closest("#file-context-menu"))
    $("#file-context-menu").hidden = true;
});
$("#eta-launcher").addEventListener("click", () => void openExplorerWindow());
$("#task-strip").addEventListener("click", (event) => {
  const button = (event.target as HTMLElement).closest(
    "[data-window]",
  ) as HTMLElement | null;
  if (button) { focusDesktopWindow(button.dataset.window || ""); return; }
  const peerButton = (event.target as HTMLElement).closest("[data-peer]") as HTMLElement | null;
  const peer = enrolledPeers.find((candidate) => candidate.url === peerButton?.dataset.peer);
  if (peer) void openExplorerWindow(undefined, peer);
});
$("#download-button").addEventListener("click", () => {
  if (dialogView?.state.selected)
    window.open(
      fileURL(dialogView, dialogView.state.selected.path, true),
      "_blank",
      "noopener",
    );
});
$("#copy-button").addEventListener("click", copyText);
$("#close-dialog").addEventListener("click", () => $("#preview-dialog").hide());
$("#add-peer-button").addEventListener("click", async () => {
  const url = window.prompt("Eta peer URL (for example http://pc-b:7080):");
  if (!url) return;
  try {
    const peer = await api("/api/peers", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url }),
    });
    showToast(`Added ${peer.name}`, "success");
  } catch (error) {
    showToast((error as Error).message);
  }
});
$("#theme-button").addEventListener("click", () => $("#theme-dialog").show());
$("#swatches").innerHTML = Object.entries(COLORS)
  .map(
    ([name, theme]) =>
      `<button class="swatch" style="--swatch:${theme.accent}" data-theme="${name}"><span class="swatch-dot"></span>${name}</button>`,
  )
  .join("");
$("#swatches").addEventListener("click", (event) => {
  const button = (event.target as HTMLElement).closest(
    "[data-theme]",
  ) as HTMLElement | null;
  if (!button) return;
  setTheme(button.dataset.theme || "purple");
  $("#theme-dialog").hide();
});
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && $("#preview-dialog").open)
    $("#preview-dialog").hide();
});
window.addEventListener("pagehide", () => {
  if (!desktopEnabled()) return;
  navigator.sendBeacon(
    "/api/state",
    new Blob([statePayload()], { type: "application/json" }),
  );
});
void boot();
