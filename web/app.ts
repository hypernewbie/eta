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
  onclose?: () => boolean | void;
  onfocus?: () => void;
  onrestore?: () => void;
  onminimize?: () => void;
};
type WinBoxInstance = {
  focus: () => void;
  restore: () => void;
  minimize: () => void;
};

declare const dayjs: any;

type Theme = {
  accent: string;
  accentGlow: string;
  accentDim: string;
  accentBright: string;
};
type Root = { id: number; name: string };
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
};
type ExplorerView = {
  key: string;
  panel: HTMLElement;
  state: AppState;
  element: (name: string) => HTMLElement;
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
let explorerSequence = 0;

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

function setTheme(name) {
  const theme = COLORS[name] || COLORS.purple;
  document.documentElement.style.setProperty("--accent", theme.accent);
  document.documentElement.style.setProperty("--accent-glow", theme.accentGlow);
  document.documentElement.style.setProperty("--accent-dim", theme.accentDim);
  document.documentElement.style.setProperty(
    "--accent-bright",
    theme.accentBright,
  );
  localStorage.setItem("eta_theme_color", name);
}
function iconify() {
  window.lucide?.createIcons({ attrs: { "stroke-width": 1.65 } });
}
function escapeHTML(value) {
  const node = document.createElement("span");
  node.textContent = value;
  return node.innerHTML;
}
function fileURL(root: number, path: string, download = false) {
  const params = new URLSearchParams({ root: String(root), path });
  if (download) params.set("download", "1");
  return `/api/file?${params}`;
}
function previewURL(root: number, path: string) {
  return `/api/preview?${new URLSearchParams({ root: String(root), path })}`;
}
function thumbnailURL(root: number, path: string, edge = 320) {
  return `/api/thumbnail?${new URLSearchParams({ root: String(root), path, size: String(edge) })}`;
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

async function api(path) {
  const response = await fetch(path);
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

type DesktopWindow = { title: string; window: WinBoxInstance };
const desktopWindows = new Map<string, DesktopWindow>();

function refreshTaskStrip() {
  const taskStrip = $("#task-strip");
  taskStrip.innerHTML = [...desktopWindows.entries()]
    .map(
      ([key, item]) =>
        `<sl-button size="small" class="task-button" data-window="${escapeHTML(key)}"><i data-lucide="${key.startsWith("explorer:") ? "folder-open" : "file-text"}"></i>${escapeHTML(item.title)}</sl-button>`,
    )
    .join("");
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
async function openExplorerWindow() {
  if (!window.WinBox || window.innerWidth < 700) return;
  document.body.classList.add("windowed");
  const number = ++explorerSequence;
  const key = `explorer:${number}`;
  const title = number === 1 ? "Explorer" : `Explorer ${number}`;
  const panel = createExplorerPanel();
  const view = createExplorerView(key, panel);
  const explorer = new window.WinBox({
    title,
    mount: panel,
    class: "eta-window",
    x: "center",
    y: 64,
    width: Math.min(1240, Math.max(640, Math.floor(window.innerWidth * 0.86))),
    height: Math.min(820, Math.max(420, Math.floor(window.innerHeight * 0.76))),
    bottom: 40,
    onclose: () => {
      desktopWindows.delete(key);
      refreshTaskStrip();
      queueMicrotask(() => panel.remove());
    },
    onfocus: refreshTaskStrip,
    onrestore: refreshTaskStrip,
    onminimize: refreshTaskStrip,
  });
  desktopWindows.set(key, { title, window: explorer });
  refreshTaskStrip();
  await initializeExplorer(view);
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

function entryMarkup(entry: Entry) {
  const icon = entry.kind === "directory" ? "folder" : "file";
  return `<button class="entry ${entry.kind}" data-path="${escapeHTML(entry.path)}" data-kind="${entry.kind}" data-size="${entry.size}" data-modified="${entry.modified}"><span class="entry-name-col"><i class="entry-icon" data-lucide="${icon}"></i><span class="entry-name">${escapeHTML(entry.name)}</span></span><span class="entry-meta">${date(entry.modified)}</span><span class="entry-meta">${entry.kind === "directory" ? "—" : bytes(entry.size)}</span></button>`;
}
function gridEntryMarkup(view: ExplorerView, entry: Entry) {
  const image =
    entry.kind === "file" && thumbnailExtensions.has(extension(entry.name));
  const visual = image
    ? `<img class="thumbnail" loading="lazy" decoding="async" src="${thumbnailURL(view.state.root, entry.path)}" alt="">`
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
    const result = await api(
      `/api/list?${new URLSearchParams({ root: String(view.state.root), path })}`,
    );
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
  const result = await api(previewURL(view.state.root, entry.path));
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
    const source = fileURL(view.state.root, entry.path);
    let content = fileFacts(entry);
    if (imageExtensions.has(ext))
      content += `<img class="preview-image" alt="${escapeHTML(entry.name)}" src="${source}">`;
    else if (audioExtensions.has(ext))
      content += `<audio class="media-preview" controls preload="metadata" src="${source}"></audio>`;
    else if (videoExtensions.has(ext))
      content += `<video class="media-preview video-preview" controls preload="metadata" src="${source}"></video>`;
    else if (ext === "pdf")
      content += `<iframe class="pdf-preview" title="${escapeHTML(entry.name)}" src="${source}"></iframe>`;
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

async function openInspector(view: ExplorerView, entry: Entry) {
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
  const inspector = new WinBox({
    title: entry.name,
    mount: panel,
    class: "eta-window",
    x: "center",
    y: "center",
    width: Math.min(1180, window.innerWidth - 64),
    height: Math.min(820, window.innerHeight - 120),
    bottom: 40,
    onclose: () => {
      desktopWindows.delete(key);
      refreshTaskStrip();
    },
    onfocus: refreshTaskStrip,
    onrestore: refreshTaskStrip,
    onminimize: refreshTaskStrip,
  });
  desktopWindows.set(key, { title: entry.name, window: inspector });
  refreshTaskStrip();
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
        fileURL(view.state.root, entry.path, true),
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
  view.element("entries").addEventListener("click", (event) => {
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

async function initializeExplorer(view: ExplorerView) {
  bindExplorer(view);
  view.element("view-toggle").title =
    view.state.view === "grid" ? "Use detailed list" : "Use image grid";
  try {
    view.state.roots = await api("/api/roots");
    view.element("root-select").innerHTML = view.state.roots
      .map(
        (root) =>
          `<option value="${root.id}">${escapeHTML(root.name)}</option>`,
      )
      .join("");
    await navigate(view);
  } catch (error) {
    $("#server-status").textContent = "OFFLINE";
    showToast((error as Error).message);
  }
}

async function boot() {
  setTheme(localStorage.getItem("eta_theme_color") || "purple");
  if (window.WinBox && window.innerWidth >= 700) {
    await openExplorerWindow();
  } else {
    const view = createExplorerView("fallback", createExplorerPanel());
    await initializeExplorer(view);
  }
  iconify();
}

$("#eta-launcher").addEventListener("click", () => void openExplorerWindow());
$("#task-strip").addEventListener("click", (event) => {
  const button = (event.target as HTMLElement).closest(
    "[data-window]",
  ) as HTMLElement | null;
  if (button) focusDesktopWindow(button.dataset.window || "");
});
$("#download-button").addEventListener("click", () => {
  if (dialogView?.state.selected)
    window.open(
      fileURL(dialogView.state.root, dialogView.state.selected.path, true),
      "_blank",
      "noopener",
    );
});
$("#copy-button").addEventListener("click", copyText);
$("#close-dialog").addEventListener("click", () => $("#preview-dialog").hide());
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
void boot();
