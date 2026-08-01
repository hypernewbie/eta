function activeTab(state) {
    return (state.tabs[state.activeTab] ?? {
        root: state.root,
        path: state.path,
        peer: state.peer,
    });
}
function syncActiveTab(state) {
    const tab = state.tabs[state.activeTab];
    if (!tab)
        return;
    state.root = tab.root;
    state.path = tab.path;
    state.peer = tab.peer;
}
// Eta shares Phi's complete accent registry. Keep names and values in sync.
const COLORS = {
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
let dialogView = null;
let contextEntry = null;
// Explorer clipboard intentionally models locations, not PCs. The transfer
// transport is selected only when a paste crosses to an enrolled peer.
let explorerClipboard = null;
let explorerClipboardOperation = "copy";
const CLIPBOARD_MIME = "application/x-eta-clipboard";
const CLIPBOARD_STORAGE_KEY = "eta.clipboard";
function saveClipboard() {
    if (!explorerClipboard) {
        localStorage.removeItem(CLIPBOARD_STORAGE_KEY);
        return;
    }
    const descriptor = {
        host: explorerClipboard.view.state.peer?.url ?? "local",
        root: explorerClipboard.view.state.root,
        path: explorerClipboard.entry.path,
        operation: explorerClipboardOperation,
    };
    localStorage.setItem(CLIPBOARD_STORAGE_KEY, JSON.stringify(descriptor));
}
function clearClipboard() {
    explorerClipboard = null;
    explorerClipboardOperation = "copy";
    localStorage.removeItem(CLIPBOARD_STORAGE_KEY);
}
function buildDescriptorFromEntry(source, operation) {
    return {
        host: source.view.state.peer?.url ?? "local",
        root: source.view.state.root,
        path: source.entry.path,
        operation,
    };
}
let explorerSequence = 0;
let localHost = {
    id: "local",
    hostname: "local",
    accent: "purple",
    glyph: "◆",
};
function createExplorerView(key, panel) {
    return {
        key,
        panel,
        state: {
            roots: [],
            root: 0,
            path: "",
            selected: null,
            rawText: "",
            view: localStorage.getItem("eta_directory_view") === "grid" ? "grid" : "list",
            peer: null,
            tabs: [{ root: 0, path: "", peer: null }],
            activeTab: 0,
        },
        element: (name) => {
            const element = panel.querySelector(`[data-explorer="${name}"]`);
            if (!element)
                throw new Error(`Missing explorer element: ${name}`);
            return element;
        },
    };
}
const $ = (selector) => {
    const element = document.querySelector(selector);
    if (!element)
        throw new Error(`Missing required element: ${selector}`);
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
const codeLanguages = {
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
function setTheme(name, persist = true) {
    const theme = COLORS[name] || COLORS.purple;
    // Host color identifies this machine only. Desktop chrome stays neutral so
    // host identity does not turn into an application-wide highlight theme.
    document.documentElement.style.setProperty("--identity-accent", theme.accent);
    document.documentElement.style.setProperty("--identity-glow", theme.accentGlow);
    document.documentElement.style.setProperty("--identity-bright", theme.accentBright);
    if (persist)
        localStorage.setItem("eta_theme_color", name);
}
function hostWindowTitle(title) {
    return `${localHost.glyph} ${title}`;
}
function windowAccent(peer) {
    return peer
        ? COLORS[peer.accent]?.accent || "#7c6af7"
        : "var(--identity-accent)";
}
function colorWindow(instance, peer) {
    instance.window?.style.setProperty("--window-accent", windowAccent(peer));
}
async function loadLocalHost() {
    const identity = (await api("/api/identity"));
    localHost = identity;
    setTheme(identity.accent, false);
    $("#hostname-display").textContent = identity.hostname.toUpperCase();
}
function iconify() {
    window.lucide?.createIcons({ attrs: { "stroke-width": 1.65 } });
}
function escapeHTML(value) {
    const node = document.createElement("span");
    node.textContent = value;
    return node.innerHTML;
}
function sourceURL(view, endpoint, params) {
    const query = new URLSearchParams(params);
    if (view.state.peer)
        query.set("peer", view.state.peer.url);
    return `${view.state.peer ? "/api/remote" : "/api"}/${endpoint}?${query}`;
}
function terminalURL(view, id = "", action = "", params = {}) {
    const query = new URLSearchParams(params);
    if (view.state.peer)
        query.set("peer", view.state.peer.url);
    const suffix = id
        ? `/${encodeURIComponent(id)}${action ? `/${action}` : ""}`
        : "";
    return `${view.state.peer ? "/api/remote" : "/api"}/terminals${suffix}?${query}`;
}
function fileURL(view, path, download = false) {
    return sourceURL(view, "file", {
        root: String(view.state.root),
        path,
        ...(download ? { download: "1" } : {}),
    });
}
function previewURL(view, path) {
    return sourceURL(view, "preview", { root: String(view.state.root), path });
}
function thumbnailURL(view, path, edge = 320) {
    return sourceURL(view, "thumbnail", {
        root: String(view.state.root),
        path,
        size: String(edge),
    });
}
function extension(name) {
    return name.includes(".")
        ? name.split(".").pop().toLowerCase()
        : name.toLowerCase();
}
function bytes(value) {
    if (!value)
        return "—";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
    return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}
function date(value) {
    return window.dayjs
        ? dayjs(value).format("MMM D, YYYY")
        : new Date(value).toLocaleDateString();
}
function parentPath(view) {
    return view.state.path.split("/").filter(Boolean).slice(0, -1).join("/");
}
async function api(path, init) {
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
const desktopWindows = new Map();
let activeWindowKey = null;
const copyTasks = new Map();
let enrolledPeers = [];
const explorerViews = new Map();
let restoringDesktop = false;
let stateSaveTimer;
function capturedDesktopState() {
    return [...desktopWindows.values()]
        .filter((item) => item.persist !== false)
        .map((item) => ({
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
    }
    catch {
        // State persistence must never interrupt normal desktop use.
    }
}
function scheduleDesktopSave() {
    if (!desktopEnabled() || restoringDesktop)
        return;
    if (stateSaveTimer)
        window.clearTimeout(stateSaveTimer);
    stateSaveTimer = window.setTimeout(() => void saveDesktopState(), 400);
}
// Structural changes (open/close) must persist immediately so a closed
// window cannot resurrect from a stale state file after a fast tab close
// or test teardown. Geometry (drag/resize/maximize) stays debounced via
// scheduleDesktopSave. Both paths share the timer: the flush cancels any
// pending debounced save before triggering one synchronously.
function flushDesktopSave() {
    if (!desktopEnabled() || restoringDesktop)
        return;
    if (stateSaveTimer) {
        window.clearTimeout(stateSaveTimer);
        stateSaveTimer = undefined;
    }
    void saveDesktopState();
}
async function loadCopyTasks() {
    try {
        const jobs = await api("/api/transfer-jobs");
        if (!Array.isArray(jobs))
            return;
        for (const job of jobs) {
            if (!job.done || job.error) {
                const id = `local:${job.id}`;
                copyTasks.set(id, {
                    id,
                    name: job.name || "transfer",
                    completed: job.completed || 0,
                    total: job.total || 0,
                    error: job.error,
                    done: Boolean(job.done),
                });
            }
        }
        refreshTaskStrip();
    }
    catch {
        // Transfer history is supplemental to the desktop shell.
    }
}
function refreshEtaMenu() {
    const local = `<button type="button" class="eta-location eta-location-local" data-location="local"><span class="eta-location-glyph">${escapeHTML(localHost.glyph)}</span><span>${escapeHTML(localHost.hostname.toUpperCase())}</span></button>`;
    const peers = enrolledPeers.map((peer) => `<button type="button" class="eta-location" style="--pc-accent:${escapeHTML(COLORS[peer.accent]?.accent || "#7c6af7")}" data-location="${escapeHTML(peer.url)}"><span class="eta-location-glyph">${escapeHTML(peer.glyph)}</span><span>${escapeHTML(peer.name.toUpperCase())}</span></button>`);
    $("#eta-menu-locations").innerHTML = [local, ...peers].join("");
}
function refreshTaskStrip() {
    const taskStrip = $("#task-strip");
    const copies = [...copyTasks.values()].map((task) => {
        const progress = task.done
            ? task.error
                ? "failed"
                : "complete"
            : `${task.completed}/${task.total}`;
        return `<sl-button size="small" class="task-button copy-task ${task.error ? "copy-task-error" : ""}" title="${escapeHTML(task.error || "")}" disabled><i data-lucide="${task.error ? "circle-alert" : task.done ? "check" : "copy"}"></i>Copy ${escapeHTML(task.name)} — ${progress}</sl-button>`;
    });
    const windows = [...desktopWindows.entries()].map(([key, item]) => {
        const icon = item.kind === "explorer"
            ? "folder-open"
            : item.kind === "terminal"
                ? "terminal-square"
                : "file-text";
        const state = [
            "task-button",
            "task-window",
            key === activeWindowKey ? "task-window-active" : "",
            item.window.min ? "task-window-minimized" : "",
        ]
            .filter(Boolean)
            .join(" ");
        return `<sl-button size="small" class="${state}" style="--window-accent:${escapeHTML(windowAccent(item.peer))}" data-window="${escapeHTML(key)}" title="${escapeHTML(item.title)}" aria-label="${escapeHTML(item.title)}"><i data-lucide="${icon}"></i><span class="task-window-label">${escapeHTML(item.title)}</span></sl-button>`;
    });
    taskStrip.innerHTML = [...windows, ...copies].join("");
    refreshEtaMenu();
    iconify();
}
function focusDesktopWindow(key) {
    const item = desktopWindows.get(key);
    if (!item)
        return;
    activeWindowKey = key;
    item.window.restore();
    item.window.focus();
    refreshTaskStrip();
}
function desktopEnabled() {
    return Boolean(window.WinBox) && document.body.classList.contains("windowed");
}
function createExplorerPanel() {
    const template = $("#explorer-template");
    const panel = template.content.firstElementChild?.cloneNode(true);
    if (!panel)
        throw new Error("Explorer template is empty");
    $("#explorer-backstore").append(panel);
    return panel;
}
async function openExplorerWindow(restored, peer = null) {
    if (!window.WinBox || window.innerWidth < 700)
        return;
    document.body.classList.add("windowed");
    const number = ++explorerSequence;
    const key = `explorer:${number}`;
    const explorerName = number === 1 ? "Explorer" : `Explorer ${number}`;
    const title = peer
        ? `${peer.glyph} ${explorerName}`
        : hostWindowTitle(explorerName);
    const panel = createExplorerPanel();
    const view = createExplorerView(key, panel);
    view.state.peer = peer;
    const windowChanged = () => {
        refreshTaskStrip();
        scheduleDesktopSave();
    };
    const windowFocused = () => {
        activeWindowKey = key;
        windowChanged();
    };
    const explorer = new window.WinBox({
        title,
        mount: panel,
        class: peer
            ? "eta-window identity-window peer-window"
            : "eta-window identity-window",
        x: restored ? restored.x : "center",
        y: restored?.y ?? 64,
        width: restored?.width ??
            Math.min(1240, Math.max(640, Math.floor(window.innerWidth * 0.86))),
        height: restored?.height ??
            Math.min(820, Math.max(420, Math.floor(window.innerHeight * 0.76))),
        bottom: 40,
        max: restored?.maximized,
        min: restored?.minimized,
        onclose: () => {
            desktopWindows.delete(key);
            if (activeWindowKey === key)
                activeWindowKey = null;
            explorerViews.delete(key);
            windowChanged();
            flushDesktopSave();
            queueMicrotask(() => panel.remove());
        },
        onfocus: windowFocused,
        onmove: windowChanged,
        onresize: windowChanged,
        onmaximize: windowChanged,
        onrestore: windowChanged,
        onminimize: windowChanged,
    });
    colorWindow(explorer, peer);
    desktopWindows.set(key, {
        title,
        kind: "explorer",
        peer,
        window: explorer,
        state: () => ({
            kind: "explorer",
            root: view.state.root,
            path: view.state.path,
            peer: view.state.peer?.url,
        }),
    });
    explorerViews.set(key, view);
    activeWindowKey = key;
    refreshTaskStrip();
    flushDesktopSave();
    await initializeExplorer(view, restored);
}
function renderBreadcrumbs(view) {
    const root = view.state.roots[view.state.root];
    const crumbs = [{ name: root?.name || "Root", path: "" }];
    let current = "";
    for (const part of view.state.path.split("/").filter(Boolean)) {
        current = current ? `${current}/${part}` : part;
        crumbs.push({ name: part, path: current });
    }
    view.element("breadcrumbs").innerHTML = crumbs
        .map((crumb, index) => `${index ? '<i class="crumb-separator" data-lucide="chevron-right"></i>' : ""}<button class="breadcrumb" data-path="${escapeHTML(crumb.path)}">${escapeHTML(crumb.name)}</button>`)
        .join("");
}
function fileIcon(entry) {
    if (entry.kind === "directory")
        return '<i class="entry-icon" data-lucide="folder"></i>';
    const icons = {
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
function entryMarkup(entry) {
    return `<button class="entry ${entry.kind}" data-path="${escapeHTML(entry.path)}" data-kind="${entry.kind}" data-size="${entry.size}" data-modified="${entry.modified}"><span class="entry-name-col">${fileIcon(entry)}<span class="entry-name">${escapeHTML(entry.name)}</span></span><span class="entry-meta">${date(entry.modified)}</span><span class="entry-meta">${entry.kind === "directory" ? "—" : bytes(entry.size)}</span></button>`;
}
function gridEntryMarkup(view, entry) {
    const image = entry.kind === "file" && thumbnailExtensions.has(extension(entry.name));
    const visual = image
        ? `<img class="thumbnail" loading="lazy" decoding="async" src="${thumbnailURL(view, entry.path)}" alt="">`
        : `<span class="grid-icon"><i data-lucide="${entry.kind === "directory" ? "folder" : "file"}"></i></span>`;
    return `<button class="entry grid-entry ${entry.kind}" data-path="${escapeHTML(entry.path)}" data-kind="${entry.kind}" data-size="${entry.size}" data-modified="${entry.modified}">${visual}<span class="grid-name">${escapeHTML(entry.name)}</span><span class="grid-meta">${entry.kind === "directory" ? "Folder" : bytes(entry.size)}</span></button>`;
}
function renderEntries(view, entries) {
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
    const renderer = view.state.view === "grid"
        ? (entry) => gridEntryMarkup(view, entry)
        : entryMarkup;
    container.innerHTML = parent + entries.map(renderer).join("");
    iconify();
}
// renderTabStrip renders the per-Explorer tab bar. Tabs are ordered;
// drag-to-reorder and click-to-switch are wired by bindExplorer once.
function renderTabStrip(view) {
    const strip = view.element("tab-strip");
    const tabs = view.state.tabs;
    const activeIdx = view.state.activeTab;
    const buttons = tabs
        .map((tab, idx) => {
        const label = tab.path === ""
            ? "/"
            : tab.path.split("/").filter(Boolean).pop() || "/";
        const peerTag = tab.peer ? ` · ${tab.peer.glyph}` : "";
        const active = idx === activeIdx ? " tab-active" : "";
        const close = tabs.length > 1
            ? `<button class="tab-close" data-tab-close="${idx}" title="Close tab" aria-label="Close tab">×</button>`
            : "";
        return `<button class="eta-tab${active}" draggable="true" data-tab="${idx}" role="tab" aria-selected="${idx === activeIdx}" title="${escapeHTML(tab.path || "/")}"><span class="tab-label">${escapeHTML(label)}${escapeHTML(peerTag)}</span>${close}</button>`;
    })
        .join("");
    const newTab = '<button class="eta-tab-new" data-tab-new title="New tab" aria-label="New tab">+</button>';
    strip.innerHTML = buttons + newTab;
    iconify();
}
function switchTab(view, idx) {
    if (!view.state.tabs[idx])
        return;
    view.state.activeTab = idx;
    const tab = view.state.tabs[idx];
    view.state.root = tab.root;
    view.state.path = tab.path;
    view.state.peer = tab.peer;
    view.element("root-select").value = String(tab.root);
    navigate(view, tab.path);
}
function closeTab(view, idx) {
    if (view.state.tabs.length <= 1)
        return; // always keep at least one
    const wasActive = idx === view.state.activeTab;
    view.state.tabs.splice(idx, 1);
    if (wasActive) {
        const newActive = Math.min(idx, view.state.tabs.length - 1);
        switchTab(view, newActive);
    }
    else {
        if (view.state.activeTab > idx)
            view.state.activeTab--;
        renderTabStrip(view);
    }
}
function openNewTab(view) {
    // Clone the active tab so the new one starts at the same root/peer
    // with an empty path (root listing). Distinct tabs are usually
    // opened to point at a subfolder, but root is a safe default.
    const current = activeTab(view.state);
    view.state.tabs.push({
        root: current.root,
        path: "",
        peer: current.peer ? { ...current.peer } : null,
    });
    view.state.activeTab = view.state.tabs.length - 1;
    switchTab(view, view.state.activeTab);
}
function reorderTabs(view, from, to) {
    if (from === to)
        return;
    const tabs = view.state.tabs;
    if (from < 0 || from >= tabs.length || to < 0 || to >= tabs.length)
        return;
    const [moved] = tabs.splice(from, 1);
    tabs.splice(to, 0, moved);
    if (view.state.activeTab === from) {
        view.state.activeTab = to;
    }
    else if (from < view.state.activeTab && to >= view.state.activeTab) {
        view.state.activeTab--;
    }
    else if (from > view.state.activeTab && to <= view.state.activeTab) {
        view.state.activeTab++;
    }
    renderTabStrip(view);
}
async function navigate(view, path = "") {
    view.state.path = path;
    // Mirror the new path onto the active tab so opening tabs in this
    // explorer don't drift from what's actually displayed.
    const tab = view.state.tabs[view.state.activeTab];
    if (tab)
        tab.path = path;
    view.element("entries").innerHTML =
        '<div class="empty"><sl-spinner></sl-spinner></div>';
    renderBreadcrumbs(view);
    renderTabStrip(view);
    iconify();
    try {
        const result = await api(sourceURL(view, "list", { root: String(view.state.root), path }));
        if (result.entry && result.entry.kind !== "directory") {
            await preview(view, result.entry);
            return;
        }
        renderBreadcrumbs(view);
        renderEntries(view, result.entries || []);
    }
    catch (error) {
        showToast(error.message);
        renderEntries(view, []);
    }
}
async function loadText(view, entry) {
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
async function renderPreview(view, entry, container) {
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
    }
    catch (error) {
        container.innerHTML = `<p class="preview-note">${escapeHTML(error.message)}</p>`;
    }
    return { rawText, binary };
}
async function openTerminal(view, entry) {
    const created = await api(sourceURL(view, "terminals", {}), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
            root: view.state.root,
            path: entry.path,
            columns: 120,
            rows: 32,
        }),
    });
    const key = `terminal:${created.id}`;
    const title = view.state.peer
        ? `${view.state.peer.glyph} Terminal — ${entry.name}`
        : hostWindowTitle(`Terminal — ${entry.name}`);
    const panel = document.createElement("section");
    panel.className = "terminal-panel";
    const terminalHost = document.createElement("div");
    terminalHost.className = "terminal-xterm";
    panel.append(terminalHost);
    // xterm.js is a version-pinned MIT CDN dependency. Eta owns the PTY API and
    // polling transport; no Phi terminal implementation is copied here.
    const xterm = window.Terminal
        ? new window.Terminal({
            cursorBlink: true,
            fontFamily: '"JetBrains Mono", ui-monospace, monospace',
            fontSize: 13,
            theme: {
                background: "#090a0d",
                foreground: "#e4e6ed",
                cursor: "#b4a7ff",
            },
        })
        : null;
    const fit = xterm && window.FitAddon ? new window.FitAddon.FitAddon() : null;
    if (xterm) {
        if (fit)
            xterm.loadAddon(fit);
        xterm.open(terminalHost);
    }
    else {
        terminalHost.textContent = "Terminal renderer did not load.";
    }
    let offset = 0;
    let stopped = false;
    const streamOutput = async () => {
        let backoffMs = 100;
        const base = window.location.origin || "";
        while (!stopped) {
            try {
                const response = await fetch(`${base}/api/terminals/${encodeURIComponent(created.id)}/stream?offset=${offset}`, { headers: { Accept: "text/event-stream" } });
                if (!response.body || !response.ok) {
                    throw new Error(`stream: ${response.status}`);
                }
                const reader = response.body.getReader();
                const decoder = new TextDecoder();
                let buffer = "";
                backoffMs = 100;
                while (!stopped) {
                    const { value, done } = await reader.read();
                    if (done)
                        break;
                    buffer += decoder.decode(value, { stream: true });
                    let sep = buffer.indexOf("\n\n");
                    while (sep !== -1) {
                        const event = buffer.slice(0, sep);
                        buffer = buffer.slice(sep + 2);
                        for (const line of event.split("\n")) {
                            if (!line.startsWith("data:"))
                                continue;
                            const payload = line.slice(5).trim();
                            if (!payload)
                                continue;
                            let parsed;
                            try {
                                parsed = JSON.parse(payload);
                            }
                            catch {
                                continue;
                            }
                            if (parsed.output && xterm)
                                xterm.write(parsed.output);
                            if (typeof parsed.offset === "number")
                                offset = parsed.offset;
                            if (parsed.closed) {
                                stopped = true;
                                return;
                            }
                        }
                        sep = buffer.indexOf("\n\n");
                    }
                }
            }
            catch {
                if (stopped)
                    return;
                // Reconnect with exponential backoff. Reset on success.
                await new Promise((r) => window.setTimeout(r, backoffMs));
                backoffMs = Math.min(backoffMs * 2, 5000);
            }
        }
    };
    void streamOutput();
    const sendResize = () => {
        if (!xterm || stopped)
            return;
        fit?.fit();
        void api(terminalURL(view, created.id, "resize"), {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ columns: xterm.cols, rows: xterm.rows }),
        });
    };
    xterm?.onData((input) => {
        if (stopped)
            return;
        void api(terminalURL(view, created.id, "input"), {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ input }),
        });
    });
    const resizeObserver = new ResizeObserver(sendResize);
    resizeObserver.observe(panel);
    const terminal = new window.WinBox({
        title,
        mount: panel,
        class: view.state.peer
            ? "eta-window identity-window peer-window"
            : "eta-window identity-window",
        x: "center",
        y: "center",
        width: Math.min(980, window.innerWidth - 64),
        height: Math.min(640, window.innerHeight - 120),
        bottom: 40,
        onresize: sendResize,
        onmaximize: sendResize,
        onrestore: sendResize,
        onfocus: () => {
            activeWindowKey = key;
            refreshTaskStrip();
        },
        onminimize: () => refreshTaskStrip(),
        onclose: () => {
            stopped = true;
            resizeObserver.disconnect();
            xterm?.dispose();
            desktopWindows.delete(key);
            if (activeWindowKey === key)
                activeWindowKey = null;
            refreshTaskStrip();
            flushDesktopSave();
            void api(terminalURL(view, created.id), {
                method: "DELETE",
            });
        },
    });
    colorWindow(terminal, view.state.peer);
    desktopWindows.set(key, {
        title,
        kind: "terminal",
        peer: view.state.peer,
        persist: false,
        window: terminal,
        state: () => ({
            kind: "file",
            root: view.state.root,
            path: entry.path,
            peer: view.state.peer?.url,
        }),
    });
    activeWindowKey = key;
    refreshTaskStrip();
    terminal.focus();
    sendResize();
    xterm?.focus();
    void streamOutput();
}
async function openInspector(view, entry, restored) {
    const WinBox = window.WinBox;
    if (!WinBox)
        return;
    const key = `file:${view.state.peer?.url || "local"}:${view.state.root}:${entry.path}`;
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
    const peer = view.state.peer;
    const title = peer
        ? `${peer.glyph} ${entry.name}`
        : hostWindowTitle(entry.name);
    const windowFocused = () => {
        activeWindowKey = key;
        windowChanged();
    };
    const inspector = new WinBox({
        title,
        mount: panel,
        class: peer
            ? "eta-window identity-window peer-window"
            : "eta-window identity-window",
        x: restored ? restored.x : "center",
        y: restored ? restored.y : "center",
        width: restored?.width ?? Math.min(1180, window.innerWidth - 64),
        height: restored?.height ?? Math.min(820, window.innerHeight - 120),
        bottom: 40,
        max: restored?.maximized,
        min: restored?.minimized,
        onclose: () => {
            desktopWindows.delete(key);
            if (activeWindowKey === key)
                activeWindowKey = null;
            windowChanged();
            flushDesktopSave();
        },
        onfocus: windowFocused,
        onmove: windowChanged,
        onresize: windowChanged,
        onmaximize: windowChanged,
        onrestore: windowChanged,
        onminimize: windowChanged,
    });
    colorWindow(inspector, peer);
    desktopWindows.set(key, {
        title,
        kind: "file",
        peer,
        window: inspector,
        state: () => ({
            kind: "file",
            root: view.state.root,
            path: entry.path,
            peer: peer?.url,
        }),
    });
    activeWindowKey = key;
    refreshTaskStrip();
    flushDesktopSave();
    const result = await renderPreview(view, entry, content);
    const copy = actions.querySelector(".inspector-copy");
    copy.disabled = result.binary;
    copy.addEventListener("click", async () => {
        if (!result.rawText)
            return;
        try {
            await navigator.clipboard.writeText(result.rawText);
            showToast("Copied text", "success");
        }
        catch {
            showToast("Clipboard access was denied");
        }
    });
    actions
        .querySelector(".inspector-download")
        ?.addEventListener("click", () => {
        window.open(fileURL(view, entry.path, true), "_blank", "noopener");
    });
}
async function preview(view, entry) {
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
    if (!dialogView?.state.rawText)
        return;
    try {
        await navigator.clipboard.writeText(dialogView.state.rawText);
        showToast("Copied text", "success");
    }
    catch {
        showToast("Clipboard access was denied");
    }
}
function bindExplorer(view) {
    view.element("root-select").addEventListener("change", (event) => {
        view.state.root = Number(event.target.value);
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
    const strip = view.element("tab-strip");
    // Tab click — focus; close button stops propagation; "+" opens a new tab.
    strip.addEventListener("click", (event) => {
        const target = event.target;
        if (target.closest("[data-tab-close]")) {
            const idx = Number(target.closest("[data-tab-close]").getAttribute("data-tab-close"));
            closeTab(view, idx);
            return;
        }
        if (target.closest("[data-tab-new]")) {
            openNewTab(view);
            return;
        }
        const tab = target.closest("[data-tab]");
        if (tab) {
            const idx = Number(tab.getAttribute("data-tab"));
            switchTab(view, idx);
        }
    });
    // HTML5 drag-to-reorder within the strip.
    strip.addEventListener("dragstart", (event) => {
        const tab = event.target.closest("[data-tab]");
        if (!tab)
            return;
        if (event.dataTransfer) {
            event.dataTransfer.effectAllowed = "move";
            event.dataTransfer.setData("text/plain", tab.getAttribute("data-tab") ?? "");
        }
    });
    strip.addEventListener("dragover", (event) => {
        if (!event.target.closest("[data-tab]"))
            return;
        event.preventDefault();
        if (event.dataTransfer)
            event.dataTransfer.dropEffect = "move";
    });
    strip.addEventListener("drop", (event) => {
        const target = event.target.closest("[data-tab]");
        if (!target)
            return;
        event.preventDefault();
        const from = Number(event.dataTransfer?.getData("text/plain") ?? "");
        const to = Number(target.getAttribute("data-tab"));
        if (Number.isInteger(from) && Number.isInteger(to) && from !== to) {
            reorderTabs(view, from, to);
        }
    });
    renderTabStrip(view);
    view
        .element("up-button")
        .addEventListener("click", () => navigate(view, parentPath(view)));
    view.element("breadcrumbs").addEventListener("click", (event) => {
        const button = event.target.closest("[data-path]");
        if (button)
            navigate(view, button.dataset.path);
    });
    view.element("entries").addEventListener("dragstart", (event) => {
        const row = event.target.closest(".entry");
        if (!row || row.dataset.parent)
            return;
        const source = {
            view,
            entry: {
                path: row.dataset.path || "",
                name: row.querySelector(".entry-name, .grid-name")?.textContent || "",
                kind: row.dataset.kind,
                size: Number(row.dataset.size),
                modified: row.dataset.modified || "",
            },
        };
        // If the user already cut this exact entry, preserve the cut
        // intent through the drag. A fresh drag of a different entry
        // is a copy. The unconditional reset here was a thorn: cutting
        // then dragging silently demoted the operation to copy.
        const preserveCut = explorerClipboard?.entry.path === source.entry.path &&
            explorerClipboardOperation === "cut";
        explorerClipboard = source;
        explorerClipboardOperation = preserveCut ? "cut" : "copy";
        saveClipboard();
        if (event.dataTransfer) {
            event.dataTransfer.effectAllowed = preserveCut ? "move" : "copy";
            event.dataTransfer.setData(CLIPBOARD_MIME, JSON.stringify(buildDescriptorFromEntry(source, preserveCut ? "cut" : "copy")));
        }
    });
    view.element("entries").addEventListener("dragover", (event) => {
        const row = event.target.closest(".entry");
        // Only directories are valid paste targets; the entries container
        // itself accepts nothing.
        if (!row || row.dataset.kind !== "directory" || row.dataset.parent)
            return;
        event.preventDefault();
        if (event.dataTransfer)
            event.dataTransfer.dropEffect = "copy";
    });
    view.element("entries").addEventListener("drop", async (event) => {
        const row = event.target.closest(".entry");
        if (!row || row.dataset.kind !== "directory" || row.dataset.parent)
            return;
        event.preventDefault();
        const payload = event.dataTransfer?.getData(CLIPBOARD_MIME);
        if (!payload)
            return;
        // explorerClipboard is already set by the dragstart handler; if the
        // drag originated from a different document (uncommon), fall back
        // to nothing rather than guessing at the source.
        if (!explorerClipboard)
            return;
        const destination = {
            view,
            entry: {
                path: row.dataset.path || "",
                name: row.querySelector(".entry-name, .grid-name")?.textContent || "",
                kind: row.dataset.kind,
                size: Number(row.dataset.size),
                modified: row.dataset.modified || "",
            },
        };
        await pasteIntoFolder(destination);
    });
    view.element("entries").addEventListener("contextmenu", (event) => {
        const row = event.target.closest(".entry");
        if (row?.dataset.parent)
            return;
        event.preventDefault();
        contextEntry = row
            ? {
                view,
                entry: {
                    path: row.dataset.path || "",
                    name: row.querySelector(".entry-name, .grid-name")?.textContent || "",
                    kind: row.dataset.kind,
                    size: Number(row.dataset.size),
                    modified: row.dataset.modified || "",
                },
            }
            : {
                view,
                entry: {
                    path: view.state.path,
                    name: view.state.path.split("/").pop() || "Root",
                    kind: "directory",
                    size: 0,
                    modified: "",
                },
            };
        const menu = $("#file-context-menu");
        menu.querySelector('[data-file-action="trusted-html"]').hidden = !htmlExtensions.has(extension(contextEntry.entry.name));
        menu.querySelector('[data-file-action="copy"]').hidden =
            contextEntry.entry.kind !== "file" &&
                contextEntry.entry.kind !== "directory";
        menu.querySelector('[data-file-action="cut"]').hidden =
            contextEntry.entry.kind !== "file" &&
                contextEntry.entry.kind !== "directory";
        menu.querySelector('[data-file-action="paste"]').hidden =
            contextEntry.entry.kind !== "directory" ||
                !explorerClipboard ||
                (explorerClipboard.entry.kind !== "file" &&
                    explorerClipboard.entry.kind !== "directory");
        menu.querySelector('[data-file-action="terminal"]').hidden = !row;
        menu.querySelector('[data-file-action="rename"]').hidden =
            !row || !!view.state.peer;
        menu.querySelector('[data-file-action="delete"]').hidden =
            !row || !!view.state.peer;
        menu.style.left = `${event.clientX}px`;
        menu.style.top = `${event.clientY}px`;
        menu.hidden = false;
        iconify();
    });
    view.element("entries").addEventListener("dblclick", (event) => {
        const row = event.target.closest(".entry");
        if (!row)
            return;
        if (row.dataset.parent) {
            navigate(view, parentPath(view));
            return;
        }
        const item = {
            path: row.dataset.path || "",
            name: row.querySelector(".entry-name, .grid-name")?.textContent || "",
            kind: row.dataset.kind,
            size: Number(row.dataset.size),
            modified: row.dataset.modified || "",
        };
        item.kind === "directory" ? navigate(view, item.path) : preview(view, item);
    });
}
async function initializeExplorer(view, restored) {
    bindExplorer(view);
    view.element("view-toggle").title =
        view.state.view === "grid" ? "Use detailed list" : "Use image grid";
    try {
        view.state.roots = await api(sourceURL(view, "roots", {}));
        view.element("root-select").innerHTML = view.state.roots
            .map((root) => `<option value="${root.id}">${escapeHTML(root.name)}</option>`)
            .join("");
        if (restored && restored.root < view.state.roots.length) {
            view.state.root = restored.root;
            view.element("root-select").value = String(restored.root);
        }
        await navigate(view, restored?.path || "");
    }
    catch (error) {
        $("#server-status").textContent = "OFFLINE";
        showToast(error.message);
    }
}
async function loadDesktopState() {
    try {
        const state = (await api("/api/state"));
        return Array.isArray(state.windows) ? state.windows : [];
    }
    catch {
        return [];
    }
}
async function restoreFileWindow(restored) {
    try {
        const peer = restored.peer
            ? enrolledPeers.find((candidate) => candidate.url === restored.peer) ||
                null
            : null;
        if (restored.peer && !peer)
            return;
        const view = { state: { root: restored.root, peer } };
        const result = await api(sourceURL(view, "list", {
            root: String(restored.root),
            path: restored.path || "",
        }));
        if (result.entry?.kind !== "file")
            return;
        await openInspector(view, result.entry, restored);
    }
    catch {
        // Missing roots and files are intentionally skipped during restore.
    }
}
async function boot() {
    setTheme(localStorage.getItem("eta_theme_color") || "purple");
    try {
        await loadLocalHost();
    }
    catch {
        // Explorer initialization reports an offline server through the normal UI.
    }
    if (window.WinBox && window.innerWidth >= 700) {
        try {
            enrolledPeers = await api("/api/peers");
        }
        catch {
            enrolledPeers = [];
        }
        await loadCopyTasks();
        const restored = await loadDesktopState();
        restoringDesktop = true;
        const explorers = restored.filter((window) => window.kind === "explorer");
        if (explorers.length) {
            for (const window of explorers) {
                const peer = window.peer
                    ? enrolledPeers.find((candidate) => candidate.url === window.peer) ||
                        null
                    : null;
                if (window.peer && !peer)
                    continue;
                await openExplorerWindow(window, peer);
            }
        }
        else {
            await openExplorerWindow();
        }
        for (const window of restored.filter((window) => window.kind === "file")) {
            await restoreFileWindow(window);
        }
        restoringDesktop = false;
    }
    else {
        const view = createExplorerView("fallback", createExplorerPanel());
        await initializeExplorer(view);
    }
    iconify();
}
async function completeCut(source) {
    if (explorerClipboardOperation !== "cut")
        return;
    if (source.view.state.peer) {
        await api(`/api/remote/delete?${new URLSearchParams({ peer: source.view.state.peer.url })}`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                root: source.view.state.root,
                path: source.entry.path,
            }),
        });
    }
    else {
        await api("/api/delete", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                root: source.view.state.root,
                path: source.entry.path,
            }),
        });
    }
    explorerClipboard = null;
    localStorage.removeItem(CLIPBOARD_STORAGE_KEY);
    await navigate(source.view, source.view.state.path);
}
async function monitorCopy(jobID, sourcePeer, source, destination) {
    const taskID = `${sourcePeer?.url || "local"}:${jobID}`;
    copyTasks.set(taskID, {
        id: taskID,
        name: source.entry.name,
        completed: 0,
        total: 0,
        done: false,
    });
    refreshTaskStrip();
    const finishTask = (error) => {
        const task = copyTasks.get(taskID);
        if (!task)
            return;
        task.done = true;
        task.error = error;
        task.completed = task.total || task.completed;
        refreshTaskStrip();
        if (!error) {
            window.setTimeout(() => {
                copyTasks.delete(taskID);
                refreshTaskStrip();
            }, 4_000);
        }
    };
    const poll = async () => {
        try {
            const endpoint = sourcePeer
                ? `/api/remote/transfer-jobs?${new URLSearchParams({ peer: sourcePeer.url, id: jobID })}`
                : `/api/transfer-jobs/${encodeURIComponent(jobID)}`;
            const status = await api(endpoint);
            const task = copyTasks.get(taskID);
            if (task) {
                task.completed = status.completed || 0;
                task.total = status.total || 0;
                refreshTaskStrip();
            }
            if (!status.done) {
                window.setTimeout(() => void poll(), 300);
                return;
            }
            if (status.error) {
                finishTask(status.error);
                showToast(`Copy failed: ${status.error}`);
                return;
            }
            try {
                await completeCut(source);
            }
            catch (error) {
                finishTask(error.message);
                showToast(`Copy completed but move could not remove its source: ${error.message}`);
                return;
            }
            finishTask();
            showToast(`${explorerClipboardOperation === "cut" ? "Moved" : "Copied"} ${source.entry.name}`, "success");
            await navigate(destination.view, destination.view.state.path);
        }
        catch (error) {
            finishTask(error.message);
            showToast(`Copy status failed: ${error.message}`);
        }
    };
    void poll();
}
async function pasteIntoFolder(destination) {
    const source = explorerClipboard;
    if (!source ||
        (source.entry.kind !== "file" && source.entry.kind !== "directory"))
        return;
    const destinationPath = destination.entry.path
        ? `${destination.entry.path}/${source.entry.name}`
        : source.entry.name;
    const sourcePeer = source.view.state.peer;
    const destinationPeer = destination.view.state.peer;
    if (!sourcePeer && !destinationPeer) {
        await api("/api/copy", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                sourceRoot: source.view.state.root,
                sourcePath: source.entry.path,
                destinationRoot: destination.view.state.root,
                destinationPath,
            }),
        });
        await completeCut(source);
        showToast(`${explorerClipboardOperation === "cut" ? "Moved" : "Copied"} ${source.entry.name}`, "success");
        await navigate(destination.view, destination.view.state.path);
        return;
    }
    if (!sourcePeer) {
        const job = await api("/api/transfers/send", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
                peer: destinationPeer.url,
                sourceRoot: source.view.state.root,
                sourcePath: source.entry.path,
                destinationRoot: destination.view.state.root,
                destinationPath,
            }),
        });
        showToast(`Copying ${source.entry.name}…`);
        await monitorCopy(job.id, undefined, source, destination);
        return;
    }
    const job = await api("/api/remote/transfers/send", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
            sourcePeer: sourcePeer.url,
            destinationPeer: destinationPeer?.url || "",
            sourceRoot: source.view.state.root,
            sourcePath: source.entry.path,
            destinationRoot: destination.view.state.root,
            destinationPath,
        }),
    });
    showToast(`Copying ${source.entry.name}…`);
    await monitorCopy(job.id, sourcePeer, source, destination);
}
$("#file-context-menu").addEventListener("click", async (event) => {
    const action = event.target.closest("[data-file-action]");
    const target = contextEntry;
    $("#file-context-menu").hidden = true;
    contextEntry = null;
    if (!action || !target)
        return;
    try {
        if (action.dataset.fileAction === "terminal") {
            await openTerminal(target.view, target.entry);
            return;
        }
        if (action.dataset.fileAction === "copy" ||
            action.dataset.fileAction === "cut") {
            explorerClipboard = target;
            explorerClipboardOperation = action.dataset.fileAction;
            saveClipboard();
            showToast(`${explorerClipboardOperation === "cut" ? "Cut" : "Copied"} ${target.entry.name} to clipboard`);
            return;
        }
        if (action.dataset.fileAction === "paste") {
            await pasteIntoFolder(target);
            return;
        }
        if (action.dataset.fileAction === "trusted-html") {
            window.open(fileURL(target.view, target.entry.path), "_blank", "noopener");
            return;
        }
        if (action.dataset.fileAction === "rename") {
            const next = window.prompt("Rename to:", target.entry.name);
            if (!next || next === target.entry.name)
                return;
            await api("/api/rename", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    root: target.view.state.root,
                    path: target.entry.path,
                    target: next,
                }),
            });
        }
        else if (action.dataset.fileAction === "delete") {
            if (!window.confirm(`Delete ${target.entry.name}? This cannot be undone.`))
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
    }
    catch (error) {
        showToast(error.message);
    }
});
document.addEventListener("pointerdown", (event) => {
    if (!event.target.closest("#file-context-menu"))
        $("#file-context-menu").hidden = true;
});
$("#eta-launcher").addEventListener("click", (event) => {
    event.stopPropagation();
    const menu = $("#eta-menu");
    menu.hidden = !menu.hidden;
});
$("#eta-menu").addEventListener("click", (event) => {
    const location = event.target.closest("[data-location]");
    if (!location)
        return;
    $("#eta-menu").hidden = true;
    if (location.dataset.location === "local") {
        void openExplorerWindow();
        return;
    }
    const peer = enrolledPeers.find((candidate) => candidate.url === location.dataset.location);
    if (peer)
        void openExplorerWindow(undefined, peer);
});
document.addEventListener("pointerdown", (event) => {
    if (!event.target.closest("#eta-menu, #eta-launcher"))
        $("#eta-menu").hidden = true;
});
$("#task-strip").addEventListener("click", (event) => {
    const button = event.target.closest("[data-window]");
    if (button) {
        focusDesktopWindow(button.dataset.window || "");
        return;
    }
});
$("#download-button").addEventListener("click", () => {
    if (dialogView?.state.selected)
        window.open(fileURL(dialogView, dialogView.state.selected.path, true), "_blank", "noopener");
});
$("#copy-button").addEventListener("click", copyText);
$("#close-dialog").addEventListener("click", () => $("#preview-dialog").hide());
$("#add-peer-button").addEventListener("click", async () => {
    const url = window.prompt("Eta peer URL (for example http://pc-b:7080):");
    if (!url)
        return;
    try {
        const peer = await api("/api/peers", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ url }),
        });
        showToast(`Added ${peer.name}`, "success");
    }
    catch (error) {
        showToast(error.message);
    }
});
$("#theme-button").addEventListener("click", () => $("#theme-dialog").show());
$("#swatches").innerHTML = Object.entries(COLORS)
    .map(([name, theme]) => `<button class="swatch" style="--swatch:${theme.accent}" data-theme="${name}"><span class="swatch-dot"></span>${name}</button>`)
    .join("");
$("#swatches").addEventListener("click", (event) => {
    const button = event.target.closest("[data-theme]");
    if (!button)
        return;
    const name = button.dataset.theme || "purple";
    setTheme(name);
    // Persist to the server so the choice survives reload even if
    // localStorage is cleared. The localStorage write inside setTheme
    // covers the prepaint race; this covers process restart + a user
    // who wipes browser storage.
    void api("/api/identity", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ accent: name }),
    }).catch(() => {
        // Server may be down or this may be a test fixture without an
        // identity file; the localStorage copy is still authoritative
        // for the current session.
    });
    $("#theme-dialog").hide();
});
document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && $("#preview-dialog").open)
        $("#preview-dialog").hide();
});
window.addEventListener("pagehide", () => {
    if (!desktopEnabled())
        return;
    navigator.sendBeacon("/api/state", new Blob([statePayload()], { type: "application/json" }));
});
void boot();
