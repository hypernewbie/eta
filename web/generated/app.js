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
let desktopShortcuts = [];
let desktopContextKey = null;
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
    // Repaint the tab icon so switching swatch recolours it immediately.
    updateFavicon();
    if (persist)
        localStorage.setItem("eta_theme_color", name);
}
function hostWindowTitle(title) {
    return `${localHost.glyph} ${title}`;
}
// Windows Explorer titles a window with the folder you are in, not with
// the application name. Eta follows that: the glyph says which host owns
// the window, the label says which folder it is showing. At the top of a
// root the root's own name is the folder.
function explorerFolderLabel(state) {
    const segment = state.path.split("/").filter(Boolean).pop();
    return segment || state.roots[state.root]?.name || "/";
}
function explorerWindowTitle(view) {
    const glyph = view.state.peer?.glyph || localHost.glyph;
    return `${glyph} ${explorerFolderLabel(view.state)}`;
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
    // Tab title names the machine, matching phi's "<glyph> <host>". The
    // static "eta" told you nothing when several boxes were open.
    // Hostnames are UPPERCASE everywhere in eta.
    document.title = `η ${identity.hostname.toUpperCase()}`;
    updateFavicon();
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
    return terminalURLFor(view.state.peer, id, action, params);
}
function terminalURLFor(peer, id = "", action = "", params = {}) {
    const query = new URLSearchParams(params);
    if (peer)
        query.set("peer", peer.url);
    const suffix = id
        ? `/${encodeURIComponent(id)}${action ? `/${action}` : ""}`
        : "";
    return `${peer ? "/api/remote" : "/api"}/terminals${suffix}?${query}`;
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
    // The icon was fixed at circle-alert, so a success read as a warning
    // wearing a green border.
    const icon = alert.querySelector('[slot="icon"]');
    if (icon) {
        icon.outerHTML = `<i slot="icon" data-lucide="${variant === "success"
            ? "check-circle"
            : variant === "danger"
                ? "circle-alert"
                : "info"}"></i>`;
        iconify();
    }
    $("#error-message").textContent = message;
    alert.toast();
}
const desktopWindows = new Map();
let activeWindowKey = null;
const copyTasks = new Map();
let enrolledPeers = [];
const explorerViews = new Map();
// Hidden files are off by default and the choice is global, the way a
// file manager treats it — a per-window setting would mean the same
// folder disagreeing with itself in two windows.
const SHOW_HIDDEN_KEY = "eta.showHidden";
let showHiddenEntries = localStorage.getItem(SHOW_HIDDEN_KEY) === "1";
function visibleEntries(entries) {
    return showHiddenEntries ? entries : entries.filter((entry) => !entry.hidden);
}
function setShowHidden(show) {
    showHiddenEntries = show;
    localStorage.setItem(SHOW_HIDDEN_KEY, show ? "1" : "0");
    for (const view of explorerViews.values()) {
        syncHiddenToggle(view);
        // Re-list rather than re-filter a cached array: the listing is
        // cheap and cannot go stale against what is on disk.
        void navigate(view, view.state.path);
    }
}
function syncHiddenToggle(view) {
    const button = view.element("hidden-toggle");
    button.setAttribute("aria-pressed", String(showHiddenEntries));
    button.classList.toggle("is-active", showHiddenEntries);
    button.title = showHiddenEntries ? "Hide hidden files" : "Show hidden files";
    button.innerHTML = `<i data-lucide="${showHiddenEntries ? "eye" : "eye-off"}"></i>`;
}
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
    return JSON.stringify({
        version: 1,
        windows: capturedDesktopState(),
        shortcuts: desktopShortcuts,
    });
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
    void renderDesktopIcons();
    iconify();
}
function focusDesktopWindow(key) {
    const item = desktopWindows.get(key);
    if (!item)
        return;
    activeWindowKey = key;
    const wasMinimized = item.window.min;
    const target = wasMinimized ? dockButtonRect(key) : null;
    item.window.restore();
    item.window.focus();
    const element = item.window.window;
    if (element && target && !reducedMotion()) {
        animateWindowToDock(element, target, "restore");
    }
    refreshTaskStrip();
}
// Keep the window frame and its dock button showing the current folder.
// Called on every navigation, so it no-ops when the title is unchanged.
function retitleExplorer(view) {
    const item = desktopWindows.get(view.key);
    if (!item)
        return;
    const title = explorerWindowTitle(view);
    if (item.title === title)
        return;
    item.title = title;
    item.window.setTitle(title);
    refreshTaskStrip();
}
// Taskbar semantics: clicking the button of the window you are already in
// minimizes it, clicking any other button restores and focuses it. Without
// this the dock can only ever raise a window, and since a minimized window
// is hidden entirely there would be no way to put one away from the dock.
function toggleDesktopWindow(key) {
    const item = desktopWindows.get(key);
    if (!item)
        return;
    if (!item.window.min && key === activeWindowKey) {
        minimizeDesktopWindow(key);
        return;
    }
    focusDesktopWindow(key);
}
// A minimized window is hidden outright, so without this it just blinks
// out of existence and the eye has nothing to follow. Animating it into
// its own dock button is what makes the button legible as where the
// window went.
const MINIMIZE_MS = 170;
function reducedMotion() {
    return Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches);
}
function dockButtonRect(key) {
    const button = $("#task-strip").querySelector(`[data-window="${CSS.escape(key)}"]`);
    const rect = button?.getBoundingClientRect();
    return rect && rect.width > 0 ? rect : null;
}
function animateWindowToDock(element, target, direction) {
    const from = element.getBoundingClientRect();
    if (!from.width || !from.height)
        return null;
    // Scale and translate about the centre, so the two agree on where the
    // window is heading.
    const open = { transform: "translate(0px, 0px) scale(1, 1)", opacity: 1 };
    const docked = {
        transform: `translate(${target.left + target.width / 2 - (from.left + from.width / 2)}px, ` +
            `${target.top + target.height / 2 - (from.top + from.height / 2)}px) ` +
            `scale(${Math.max(target.width / from.width, 0.04)}, ${Math.max(target.height / from.height, 0.04)})`,
        opacity: 0.15,
    };
    return element.animate(direction === "minimize" ? [open, docked] : [docked, open], {
        duration: MINIMIZE_MS,
        easing: direction === "minimize" ? "ease-in" : "ease-out",
    });
}
// WinBox's own minimize button calls minimize() directly, which hides
// the window instantly and skips the animation the dock button plays.
// Intercepting in the capture phase — before WinBox's own handler on
// the button — routes both paths through the same code, so a window
// leaves the same way however you sent it away.
document.addEventListener("click", (event) => {
    const button = event.target?.closest?.(".wb-min");
    if (!button)
        return;
    const frame = button.closest(".winbox");
    const entry = [...desktopWindows.entries()].find(([, item]) => item.window.window === frame);
    if (!entry)
        return;
    event.preventDefault();
    event.stopPropagation();
    minimizeDesktopWindow(entry[0]);
}, true);
function minimizeDesktopWindow(key) {
    const item = desktopWindows.get(key);
    if (!item)
        return;
    const element = item.window.window;
    const target = dockButtonRect(key);
    const animation = element && target && !reducedMotion()
        ? animateWindowToDock(element, target, "minimize")
        : null;
    // The window stays visible for the duration and is hidden only once
    // the animation lands, otherwise it would be gone before it moved.
    if (!animation) {
        item.window.minimize();
        refreshTaskStrip();
        return;
    }
    animation.addEventListener("finish", () => {
        item.window.minimize();
        refreshTaskStrip();
    });
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
async function openExplorerWindow(restored, peer = null, startRoot) {
    if (!window.WinBox || window.innerWidth < 700)
        return;
    document.body.classList.add("windowed");
    const number = ++explorerSequence;
    const key = `explorer:${number}`;
    // The window frame exists before its first listing resolves, so it opens
    // with the owning host's glyph alone; navigate() retitles it to the
    // current folder as soon as roots and the listing land. This placeholder
    // is only visible during that first load, or if the load fails.
    const title = `${peer ? peer.glyph : localHost.glyph} …`;
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
    await initializeExplorer(view, restored, startRoot);
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
    return ((icon && fileTypeSVG(icon)) ||
        '<i class="entry-icon" data-lucide="file"></i>');
}
// File-type icons are inlined from web/vendor/icons/file-icons.js rather
// than resolved by an icon component. The icon set is fixed and small, and
// a component that resolves icons at runtime is a network dependency and a
// rendering race for no benefit here. Icon bodies come from the vendored
// collection, so they are trusted markup, not user input.
function fileTypeSVG(name) {
    const icon = window.ETA_FILE_ICONS?.[name];
    if (!icon)
        return "";
    return `<svg class="entry-icon file-type-icon" viewBox="0 0 ${icon.width} ${icon.height}" aria-hidden="true" focusable="false">${icon.body}</svg>`;
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
// A bare item count is the least a status bar can say. Report the
// folder/file split and the size on disk of what is listed, which is
// what you actually want to know before copying a directory around.
function renderStatusBar(view, entries, hidden = 0) {
    const folders = entries.filter((entry) => entry.kind === "directory").length;
    const files = entries.length - folders;
    const total = entries.reduce((sum, entry) => (entry.kind === "file" ? sum + entry.size : sum), 0);
    const parts = [];
    if (folders)
        parts.push(`${folders} ${folders === 1 ? "folder" : "folders"}`);
    if (files)
        parts.push(`${files} ${files === 1 ? "file" : "files"}`);
    // Say when something is being withheld, so an empty-looking folder is
    // distinguishable from one that is only hiding its dotfiles.
    if (hidden)
        parts.push(`${hidden} hidden`);
    view.element("item-count").textContent = parts.length
        ? parts.join(", ")
        : "empty folder";
    // Directories report no meaningful size here, so this is the size of
    // the files in view, not of the tree beneath it.
    view.element("total-size").textContent = files ? bytes(total) : "";
    updateSelectionInfo(view);
}
function updateSelectionInfo(view) {
    const entry = view.state.selected;
    view.element("selection-info").textContent = entry
        ? `${entry.name} — ${entry.kind === "directory" ? "folder" : bytes(entry.size)}`
        : "";
}
function renderEntries(view, allEntries) {
    const entries = visibleEntries(allEntries);
    // Rows are rebuilt here, so any previous highlight is gone with them.
    view.state.selected = null;
    renderStatusBar(view, entries, allEntries.length - entries.length);
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
    // Retitle before the fetch: the title reflects the folder being opened,
    // so a failed listing still names the folder the user asked for.
    retitleExplorer(view);
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
    return `<section class="code-inspector"><header class="code-toolbar${truncated ? " is-truncated" : ""}"><span>${label}</span><span>${truncated ? "preview truncated at 512 KB" : ""}</span></header><pre class="preview-text line-numbers language-${prismLanguage}"><code class="language-${prismLanguage}">${escapeHTML(raw)}</code></pre></section>`;
}
async function renderPreview(view, entry, container, 
// The dialog has only a thin label, so it still wants the name/size/date
// line. A window puts the name in its title bar, so repeating it there
// is just a second title.
facts = true) {
    container.innerHTML = '<div class="empty"><sl-spinner></sl-spinner></div>';
    let rawText = "";
    let binary = true;
    try {
        const ext = extension(entry.name);
        const source = fileURL(view, entry.path);
        let content = facts ? fileFacts(entry) : "";
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
    return openTerminalWindow({
        peer: view.state.peer,
        root: view.state.root,
        path: entry.path,
        label: entry.name,
    });
}
async function openTerminalWindow(target) {
    // xterm measures the font once, when it is constructed, and caches the
    // cell size from that. Constructed before the webfont arrives it locks
    // in fallback metrics for the life of the terminal, which is what made
    // the type look wrong rather than merely small.
    try {
        await document.fonts?.load('14px "JetBrains Mono"');
    }
    catch {
        // A missing font is a cosmetic problem, never a reason not to open
        // a terminal.
    }
    const query = new URLSearchParams();
    if (target.peer)
        query.set("peer", target.peer.url);
    const created = await api(`${target.peer ? "/api/remote" : "/api"}/terminals?${query}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
            root: target.root,
            path: target.path,
            columns: 120,
            rows: 32,
            ...(target.tmux ? { tmux: target.tmux } : {}),
        }),
    });
    const key = `terminal:${created.id}`;
    const label = target.tmux
        ? `tmux — ${target.tmux}`
        : `Terminal — ${target.label}`;
    const title = target.peer
        ? `${target.peer.glyph} ${label}`
        : hostWindowTitle(label);
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
            // 14px, as phi uses. Below that JetBrains Mono hints badly at
            // this weight and the terminal looks like a fallback font.
            fontSize: 14,
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
                // Must go through terminalURL: a peer's session lives on that
                // peer, so a hardcoded /api/terminals path asks the local
                // instance for an id it has never heard of. Input and resize
                // already routed correctly, so a remote terminal accepted
                // keystrokes and never showed a byte of output.
                const response = await fetch(base +
                    terminalURLFor(target.peer, created.id, "stream", {
                        offset: String(offset),
                    }), { headers: { Accept: "text/event-stream" } });
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
    // One reader per terminal. Two concurrent SSE readers on the same
    // xterm write every byte twice, which shows up as doubled keystroke
    // echo, doubled prompts and doubled command output — the terminal
    // looks like it is running everything twice when it is only being
    // drawn twice. Guarded so a second call site cannot reintroduce it.
    let streaming = false;
    const startStream = () => {
        if (streaming)
            return;
        streaming = true;
        void streamOutput();
    };
    startStream();
    const sendResize = () => {
        if (!xterm || stopped)
            return;
        fit?.fit();
        void api(terminalURLFor(target.peer, created.id, "resize"), {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ columns: xterm.cols, rows: xterm.rows }),
        });
    };
    xterm?.onData((input) => {
        if (stopped)
            return;
        void api(terminalURLFor(target.peer, created.id, "input"), {
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
        class: target.peer
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
            void api(terminalURLFor(target.peer, created.id), {
                method: "DELETE",
            });
        },
    });
    colorWindow(terminal, target.peer);
    desktopWindows.set(key, {
        title,
        kind: "terminal",
        peer: target.peer,
        persist: false,
        window: terminal,
        state: () => ({
            kind: "file",
            root: target.root,
            path: target.path,
            peer: target.peer?.url,
        }),
    });
    activeWindowKey = key;
    refreshTaskStrip();
    terminal.focus();
    sendResize();
    xterm?.focus();
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
    // One bar: what you are looking at on the left, what you can do with
    // it on the right. Actions are icon buttons with tooltips rather than
    // two full-width labelled buttons, which is a lot of furniture for a
    // read-only viewer.
    actions.innerHTML =
        '<span class="inspector-facts"></span>' +
            '<span class="inspector-buttons">' +
            '<button type="button" class="inspector-wrap icon-button" title="Toggle word wrap" aria-pressed="false"><i data-lucide="wrap-text"></i></button>' +
            '<button type="button" class="inspector-copy icon-button" title="Copy text" disabled><i data-lucide="copy"></i></button>' +
            '<button type="button" class="inspector-download icon-button" title="Download"><i data-lucide="download"></i></button>' +
            "</span>";
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
    const result = await renderPreview(view, entry, content, false);
    // Facts move to the status bar, and gain the two a viewer should
    // report that a directory listing cannot: syntax and line count.
    const code = content.querySelector(".code-inspector");
    const language = code
        ?.querySelector(".code-toolbar span:first-child")
        ?.textContent?.trim();
    const lines = result.rawText ? result.rawText.split("\n").length : 0;
    actions.querySelector(".inspector-facts").textContent = [
        language,
        lines ? `${lines} ${lines === 1 ? "line" : "lines"}` : "",
        bytes(entry.size),
        date(entry.modified),
    ]
        .filter(Boolean)
        .join("  ·  ");
    const pre = content.querySelector(".preview-text");
    const wrap = actions.querySelector(".inspector-wrap");
    // Long lines otherwise mean horizontal scrolling with no way out.
    if (!pre)
        wrap.hidden = true;
    wrap.addEventListener("click", () => {
        const wrapped = pre.classList.toggle("is-wrapped");
        wrap.setAttribute("aria-pressed", String(wrapped));
        wrap.classList.toggle("is-active", wrapped);
    });
    const copy = actions.querySelector(".inspector-copy");
    copy.disabled = result.binary || !result.rawText;
    copy.addEventListener("click", async () => {
        if (!result.rawText)
            return;
        try {
            await navigator.clipboard.writeText(result.rawText);
            // Confirm on the control that was pressed, not only in a toast
            // in the corner of the screen.
            copy.classList.add("is-done");
            copy.title = "Copied";
            copy.innerHTML = '<i data-lucide="check"></i>';
            iconify();
            window.setTimeout(() => {
                copy.classList.remove("is-done");
                copy.title = "Copy text";
                copy.innerHTML = '<i data-lucide="copy"></i>';
                iconify();
            }, 1400);
        }
        catch {
            showToast("Clipboard access was denied");
        }
    });
    iconify();
    actions
        .querySelector(".inspector-download")
        ?.addEventListener("click", () => {
        window.open(fileURL(view, entry.path, true), "_blank", "noopener");
    });
}
async function preview(view, entry) {
    view.state.selected = entry;
    updateSelectionInfo(view);
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
    view.element("hidden-toggle").addEventListener("click", () => {
        setShowHidden(!showHiddenEntries);
    });
    syncHiddenToggle(view);
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
        menu.querySelector('[data-file-action="pin"]').hidden =
            !row;
        menu.querySelector('[data-file-action="rename"]').hidden =
            !row || !!view.state.peer;
        menu.querySelector('[data-file-action="delete"]').hidden =
            !row || !!view.state.peer;
        menu.style.left = `${event.clientX}px`;
        menu.style.top = `${event.clientY}px`;
        menu.hidden = false;
        iconify();
    });
    // Single click selects, double click opens: the split every file
    // manager uses. Entries previously had no click handler at all, so
    // nothing was ever selected and the only way to touch a file was to
    // open it or right-click it.
    const entries = view.element("entries");
    entries.addEventListener("click", (event) => {
        const row = event.target.closest(".entry");
        if (!row) {
            selectEntry(view, null);
            return;
        }
        // Ctrl/Cmd-click on the selected row clears it, so a selection can
        // be undone without hunting for empty space.
        const modified = event.ctrlKey || event.metaKey;
        selectEntry(view, modified && row.classList.contains("is-selected") ? null : row);
    });
    entries.addEventListener("keydown", (event) => {
        const key = event.key;
        if (key === "Escape") {
            selectEntry(view, null);
            return;
        }
        if (key !== "ArrowDown" && key !== "ArrowUp")
            return;
        // Arrow keys walk the list and carry the selection with them, rather
        // than only moving focus and leaving the selection behind.
        event.preventDefault();
        const rows = [...entries.querySelectorAll(".entry")];
        if (!rows.length)
            return;
        const current = rows.findIndex((row) => row.classList.contains("is-selected"));
        const next = key === "ArrowDown"
            ? Math.min(current + 1, rows.length - 1)
            : Math.max(current - 1, 0);
        const target = rows[current === -1 ? 0 : next];
        target.focus();
        selectEntry(view, target);
    });
    // Right-click acts on the row it opened over, so the menu and the
    // highlight cannot disagree about the target.
    entries.addEventListener("contextmenu", (event) => {
        const row = event.target.closest(".entry");
        if (row)
            selectEntry(view, row);
    });
    entries.addEventListener("dblclick", (event) => {
        const row = event.target.closest(".entry");
        if (!row)
            return;
        if (row.dataset.parent) {
            navigate(view, parentPath(view));
            return;
        }
        const item = entryFromRow(row);
        item.kind === "directory" ? navigate(view, item.path) : preview(view, item);
    });
}
function entryFromRow(row) {
    return {
        path: row.dataset.path || "",
        name: row.querySelector(".entry-name, .grid-name")?.textContent || "",
        kind: row.dataset.kind,
        size: Number(row.dataset.size),
        modified: row.dataset.modified || "",
    };
}
function selectEntry(view, row) {
    const container = view.element("entries");
    container.querySelectorAll(".entry.is-selected").forEach((entry) => {
        entry.classList.remove("is-selected");
        entry.setAttribute("aria-selected", "false");
    });
    // ".." is a navigation control, not a file you can act on.
    if (row && !row.dataset.parent) {
        row.classList.add("is-selected");
        row.setAttribute("aria-selected", "true");
        view.state.selected = entryFromRow(row);
    }
    else {
        view.state.selected = null;
    }
    updateSelectionInfo(view);
}
async function initializeExplorer(view, restored, startRoot) {
    bindExplorer(view);
    view.element("view-toggle").title =
        view.state.view === "grid" ? "Use detailed list" : "Use image grid";
    try {
        view.state.roots = await api(sourceURL(view, "roots", {}));
        view.element("root-select").innerHTML = view.state.roots
            .map((root) => `<option value="${root.id}">${escapeHTML(root.name)}</option>`)
            .join("");
        if (startRoot !== undefined && startRoot < view.state.roots.length) {
            view.state.root = startRoot;
            view.element("root-select").value =
                String(startRoot);
        }
        if (restored && restored.root < view.state.roots.length) {
            view.state.root = restored.root;
            view.element("root-select").value = String(restored.root);
        }
        await navigate(view, restored?.path || "");
    }
    catch (error) {
        setServerOffline(true);
        showToast(error.message);
    }
}
// "READ ONLY" was simply false — eta copies, moves and deletes — and
// "CONNECTED" was hardcoded markup that only ever changed on failure.
// The header now says nothing until something is actually wrong.
// Favicon is drawn rather than shipped, the way phi does it: a rounded
// tile in the identity accent with the app glyph, so a row of pinned
// tabs is told apart by machine colour instead of by hovering them.
function updateFavicon() {
    const style = getComputedStyle(document.documentElement);
    const accent = style.getPropertyValue("--identity-accent").trim() || "#7c6af7";
    const glow = style.getPropertyValue("--identity-glow").trim() || accent;
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
        `<defs><radialGradient id="g" cx="50%" cy="50%" r="50%">` +
        `<stop offset="0%" stop-color="${accent}"/>` +
        `<stop offset="100%" stop-color="${glow}"/>` +
        `</radialGradient></defs>` +
        `<rect width="32" height="32" rx="8" fill="url(#g)"/>` +
        `<text x="50%" y="61%" font-family="system-ui, -apple-system, sans-serif" ` +
        `font-size="21" font-weight="bold" fill="#fff" text-anchor="middle" ` +
        `dominant-baseline="middle">η</text></svg>`;
    let link = document.querySelector('link[rel="icon"]');
    if (!link) {
        link = document.createElement("link");
        link.rel = "icon";
        document.head.append(link);
    }
    link.type = "image/svg+xml";
    link.href = "data:image/svg+xml;utf8," + encodeURIComponent(svg);
}
function setServerOffline(offline) {
    const status = $("#header-status");
    status.hidden = !offline;
    $("#server-status").textContent = offline ? "OFFLINE" : "";
}
async function loadDesktopState() {
    try {
        const state = (await api("/api/state"));
        // Go omits zero fields, so root 0 comes back absent rather than 0.
        // Left undefined it would silently resolve to the first root, which
        // is right by luck for root 0 and wrong for every other one.
        desktopShortcuts = (Array.isArray(state.shortcuts) ? state.shortcuts : []).map((shortcut) => ({ ...shortcut, root: Number(shortcut.root) || 0 }));
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
        window.setInterval(() => void refreshPeerIdentities(), PEER_IDENTITY_POLL_MS);
        // A tab left open in the background is the common case for this;
        // catch up the moment it is looked at again rather than waiting out
        // the interval.
        document.addEventListener("visibilitychange", () => {
            if (!document.hidden)
                void refreshPeerIdentities();
        });
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
        if (action.dataset.fileAction === "pin") {
            pinToDesktop(target.view, target.entry);
            return;
        }
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
// A desktop with nothing on it is just a wallpaper. Each root and each
// enrolled peer gets an icon, so opening a location does not require
// going through the launcher menu every time.
function shortcutKey(shortcut) {
    return `${shortcut.peer || "local"}:${shortcut.root}:${shortcut.path}`;
}
function pinToDesktop(view, entry) {
    const shortcut = {
        name: entry.name,
        kind: entry.kind,
        root: view.state.root,
        path: entry.path,
        peer: view.state.peer?.url,
    };
    if (desktopShortcuts.some((other) => shortcutKey(other) === shortcutKey(shortcut))) {
        showToast(`${entry.name} is already on the desktop`);
        return;
    }
    desktopShortcuts.push(shortcut);
    void renderDesktopIcons();
    scheduleDesktopSave();
    showToast(`Added ${entry.name} to the desktop`, "success");
}
function unpinFromDesktop(key) {
    const before = desktopShortcuts.length;
    desktopShortcuts = desktopShortcuts.filter((shortcut) => shortcutKey(shortcut) !== key);
    if (desktopShortcuts.length === before)
        return;
    void renderDesktopIcons();
    scheduleDesktopSave();
}
function openShortcut(shortcut, peer) {
    if (shortcut.kind === "directory") {
        void openExplorerWindow({
            kind: "explorer",
            root: shortcut.root,
            path: shortcut.path,
            peer: shortcut.peer,
        }, peer);
        return;
    }
    // A file shortcut opens the viewer, which needs an explorer view to
    // resolve the file against, so it opens through the same restore path
    // the desktop uses for file windows.
    void restoreFileWindow({
        kind: "file",
        root: shortcut.root,
        path: shortcut.path,
        peer: shortcut.peer,
    });
}
let desktopIconIndex = new Map();
let renderedDesktopIcons = "";
function desktopIconModel(roots) {
    const icons = [];
    // Computers first, then the folders on them.
    icons.push({
        id: "computer:local",
        label: localHost.hostname.toUpperCase(),
        title: localHost.hostname.toUpperCase(),
        peer: null,
        art: { glyph: localHost.glyph },
        open: () => void openExplorerWindow(),
        removable: false,
    });
    for (const peer of enrolledPeers) {
        icons.push({
            id: `computer:${peer.url}`,
            label: peer.name.toUpperCase(),
            title: peer.name.toUpperCase(),
            peer,
            art: { glyph: peer.glyph },
            open: () => void openExplorerWindow(undefined, peer),
            removable: false,
        });
    }
    icons.push({
        id: "tmux",
        label: "TMUX",
        title: "tmux sessions on every PC",
        peer: null,
        art: { lucide: "square-terminal" },
        open: () => void openTmuxWindow(),
        removable: false,
    });
    roots.forEach((root, index) => {
        icons.push({
            id: `drive:${index}`,
            label: root.name,
            title: root.name,
            peer: null,
            art: { lucide: "hard-drive" },
            open: () => void openExplorerWindow(undefined, null, index),
            removable: false,
        });
    });
    for (const shortcut of desktopShortcuts) {
        const owner = shortcut.peer
            ? enrolledPeers.find((candidate) => candidate.url === shortcut.peer) ||
                null
            : null;
        icons.push({
            id: `shortcut:${shortcutKey(shortcut)}`,
            label: shortcut.name,
            title: shortcut.peer
                ? `${shortcut.path} on ${owner?.name.toUpperCase() || shortcut.peer}`
                : shortcut.path,
            peer: owner,
            art: {
                lucide: shortcut.kind === "directory" ? "folder" : "file-text",
            },
            open: () => {
                // A shortcut to a peer that is no longer enrolled must not
                // quietly open the same path on this machine.
                if (shortcut.peer && !owner) {
                    showToast(`${shortcut.name} is on a PC that is no longer added`);
                    return;
                }
                openShortcut(shortcut, owner);
            },
            removable: true,
        });
    }
    return icons;
}
function desktopIconMarkup(icon) {
    const art = "glyph" in icon.art
        ? escapeHTML(icon.art.glyph)
        : `<i data-lucide="${escapeHTML(icon.art.lucide)}"></i>`;
    const glyph = "glyph" in icon.art ? " desktop-icon-glyph" : "";
    return (`<button type="button" class="desktop-icon" data-desktop-icon="${escapeHTML(icon.id)}"` +
        ` style="--icon-accent:${escapeHTML(windowAccent(icon.peer))}" title="${escapeHTML(icon.title)}">` +
        `<span class="desktop-icon-art${glyph}">${art}</span>` +
        `<span class="desktop-icon-label">${escapeHTML(icon.label)}</span></button>`);
}
// A PC can be renamed or recoloured while this desktop is open. The
// server re-probes peers behind each list call, so polling picks the
// new identity up; without this the peer keeps the colour and name it
// had when it was enrolled, on every surface here, forever.
const PEER_IDENTITY_POLL_MS = 20_000;
function peerIdentityFingerprint(list) {
    return list
        .map((peer) => `${peer.url}|${peer.name}|${peer.accent}|${peer.glyph}`)
        .join("\n");
}
async function refreshPeerIdentities() {
    if (!desktopEnabled())
        return;
    let latest;
    try {
        latest = await api("/api/peers");
    }
    catch {
        return;
    }
    if (peerIdentityFingerprint(latest) === peerIdentityFingerprint(enrolledPeers)) {
        return;
    }
    enrolledPeers = latest;
    // Windows already on screen hold their own copy of the peer, and are
    // coloured once when opened, so they have to be repainted rather than
    // left to the next open.
    for (const item of desktopWindows.values()) {
        if (!item.peer)
            continue;
        const updated = enrolledPeers.find((peer) => peer.url === item.peer.url);
        if (!updated)
            continue;
        item.peer = updated;
        colorWindow(item.window, updated);
    }
    for (const view of explorerViews.values()) {
        if (!view.state.peer)
            continue;
        const updated = enrolledPeers.find((peer) => peer.url === view.state.peer.url);
        if (updated)
            view.state.peer = updated;
    }
    // Redraws the dock, the computers menu and the desktop icons.
    refreshTaskStrip();
}
function tmuxHosts() {
    return [
        {
            peer: null,
            label: localHost.hostname.toUpperCase(),
            glyph: localHost.glyph,
        },
        ...enrolledPeers.map((peer) => ({
            peer,
            label: peer.name.toUpperCase(),
            glyph: peer.glyph,
        })),
    ];
}
function tmuxURL(peer) {
    const query = new URLSearchParams();
    if (peer)
        query.set("peer", peer.url);
    return `${peer ? "/api/remote" : "/api"}/tmux?${query}`;
}
async function loadTmuxHosts() {
    // In parallel: one slow or dead PC must not hold up the others.
    return Promise.all(tmuxHosts().map(async (host) => {
        try {
            const body = await api(tmuxURL(host.peer));
            return {
                ...host,
                sessions: (body.sessions || []),
                available: body.available !== false,
                reachable: true,
            };
        }
        catch {
            return { ...host, sessions: [], available: false, reachable: false };
        }
    }));
}
function tmuxSessionRow(host, session) {
    const windows = `${session.windows} ${session.windows === 1 ? "window" : "windows"}`;
    // "attached" means someone is already looking at it, which on a
    // shared box is the difference between resuming and interrupting.
    const state = session.attached
        ? '<span class="tmux-attached">attached</span>'
        : "";
    return (`<button type="button" class="tmux-session" data-peer="${escapeHTML(host.peer?.url || "")}" data-session="${escapeHTML(session.name)}">` +
        `<i data-lucide="square-terminal"></i>` +
        `<span class="tmux-session-name">${escapeHTML(session.name)}</span>` +
        `<span class="tmux-session-meta">${windows}${session.created ? ` · started ${escapeHTML(date(session.created))}` : ""}</span>` +
        state +
        "</button>");
}
function tmuxHostMarkup(host) {
    const body = !host.reachable
        ? '<p class="tmux-note">Not reachable.</p>'
        : !host.available
            ? '<p class="tmux-note">tmux is not installed on this PC.</p>'
            : host.sessions.length
                ? host.sessions.map((session) => tmuxSessionRow(host, session)).join("")
                : '<p class="tmux-note">No sessions yet.</p>';
    // New sessions can only be made where tmux exists.
    const create = host.reachable && host.available
        ? `<button type="button" class="tmux-new" data-peer="${escapeHTML(host.peer?.url || "")}" title="New session on ${escapeHTML(host.label)}"><i data-lucide="plus"></i>New</button>`
        : "";
    return (`<section class="tmux-host" style="--icon-accent:${escapeHTML(windowAccent(host.peer))}">` +
        `<header class="tmux-host-header"><span class="tmux-host-glyph">${escapeHTML(host.glyph)}</span>` +
        `<span class="tmux-host-name">${escapeHTML(host.label)}</span>${create}</header>` +
        `<div class="tmux-host-body">${body}</div></section>`);
}
async function refreshTmuxPanel(panel) {
    const list = panel.querySelector(".tmux-list");
    list.innerHTML = '<div class="empty"><sl-spinner></sl-spinner></div>';
    const hosts = await loadTmuxHosts();
    list.innerHTML = hosts.map(tmuxHostMarkup).join("");
    iconify();
}
function tmuxPeerFor(url) {
    return url ? enrolledPeers.find((peer) => peer.url === url) || null : null;
}
async function openTmuxWindow() {
    const WinBox = window.WinBox;
    if (!WinBox)
        return;
    const key = "tmux:sessions";
    if (desktopWindows.has(key)) {
        focusDesktopWindow(key);
        return;
    }
    const panel = document.createElement("section");
    panel.className = "tmux-window";
    panel.innerHTML =
        '<div class="tmux-list"></div>' +
            '<footer class="tmux-actions"><span class="tmux-hint">Double-click a session to attach</span>' +
            '<button type="button" class="tmux-refresh icon-button" title="Refresh"><i data-lucide="refresh-cw"></i></button></footer>';
    const title = `${localHost.glyph} tmux sessions`;
    const tmuxWindow = new WinBox({
        title,
        mount: panel,
        class: "eta-window identity-window",
        x: "center",
        y: "center",
        width: Math.min(720, window.innerWidth - 64),
        height: Math.min(560, window.innerHeight - 120),
        bottom: 40,
        onclose: () => {
            desktopWindows.delete(key);
            refreshTaskStrip();
            return false;
        },
        onfocus: () => {
            activeWindowKey = key;
            refreshTaskStrip();
        },
    });
    colorWindow(tmuxWindow, null);
    desktopWindows.set(key, {
        title,
        kind: "terminal",
        peer: null,
        persist: false,
        window: tmuxWindow,
        state: () => ({ kind: "file", root: 0, path: "" }),
    });
    activeWindowKey = key;
    refreshTaskStrip();
    // Same interaction split as the explorer and the desktop: click
    // selects, double click opens.
    panel.addEventListener("click", (event) => {
        const row = event.target.closest(".tmux-session");
        panel
            .querySelectorAll(".tmux-session.is-selected")
            .forEach((other) => other.classList.remove("is-selected"));
        if (row)
            row.classList.add("is-selected");
    });
    panel.addEventListener("dblclick", (event) => {
        const row = event.target.closest(".tmux-session");
        if (row)
            void attachTmuxSession(row);
    });
    panel.addEventListener("keydown", (event) => {
        const row = event.target.closest(".tmux-session");
        if (row && event.key === "Enter")
            void attachTmuxSession(row);
    });
    panel.querySelector(".tmux-refresh")?.addEventListener("click", () => {
        void refreshTmuxPanel(panel);
    });
    panel.addEventListener("click", async (event) => {
        const button = event.target.closest(".tmux-new");
        if (!button)
            return;
        const peer = tmuxPeerFor(button.dataset.peer || "");
        const name = window.prompt("New tmux session name:");
        if (!name)
            return;
        try {
            await api(tmuxURL(peer), {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ name }),
            });
            await refreshTmuxPanel(panel);
            showToast(`Created ${name}`, "success");
        }
        catch (error) {
            showToast(error.message);
        }
    });
    await refreshTmuxPanel(panel);
}
async function attachTmuxSession(row) {
    const peer = tmuxPeerFor(row.dataset.peer || "");
    const session = row.dataset.session || "";
    if (!session)
        return;
    try {
        await openTerminalWindow({
            peer,
            root: 0,
            path: "",
            label: session,
            tmux: session,
        });
    }
    catch (error) {
        showToast(error.message);
    }
}
async function renderDesktopIcons() {
    const layer = $("#desktop-icons");
    if (!desktopEnabled()) {
        layer.hidden = true;
        return;
    }
    let roots = [];
    try {
        roots = await api("/api/roots");
    }
    catch {
        roots = [];
    }
    const icons = desktopIconModel(roots);
    desktopIconIndex = new Map(icons.map((icon) => [icon.id, icon]));
    const markup = icons.map(desktopIconMarkup).join("");
    layer.hidden = false;
    // This runs on every taskbar refresh, and replacing the markup while
    // lucide is mid-walk detaches the nodes it is about to swap, which
    // throws "removeChild ... not a child of this node". Nothing to do
    // when the desktop has not changed.
    if (markup === renderedDesktopIcons)
        return;
    renderedDesktopIcons = markup;
    layer.innerHTML = markup;
    iconify();
}
function openDesktopIcon(element) {
    const icon = desktopIconIndex.get(element.dataset.desktopIcon || "");
    icon?.open();
}
$("#desktop-icons").addEventListener("click", (event) => {
    const icon = event.target.closest(".desktop-icon");
    // Single click selects, double click opens, matching the explorer.
    $("#desktop-icons")
        .querySelectorAll(".desktop-icon.is-selected")
        .forEach((other) => other.classList.remove("is-selected"));
    if (icon)
        icon.classList.add("is-selected");
});
$("#desktop-icons").addEventListener("dblclick", (event) => {
    const icon = event.target.closest(".desktop-icon");
    if (icon)
        openDesktopIcon(icon);
});
$("#desktop-icons").addEventListener("contextmenu", (event) => {
    const icon = event.target.closest(".desktop-icon");
    // Only removable icons have anything to offer here; computers and
    // drives are not shortcuts.
    const model = desktopIconIndex.get(icon?.dataset.desktopIcon || "");
    if (!icon || !model?.removable)
        return;
    event.preventDefault();
    desktopContextKey = model.id;
    const menu = $("#desktop-context-menu");
    menu.style.left = `${event.clientX}px`;
    menu.style.top = `${event.clientY}px`;
    menu.hidden = false;
    iconify();
});
$("#desktop-context-menu").addEventListener("click", (event) => {
    const action = event.target.closest("[data-desktop-action]");
    const key = desktopContextKey;
    $("#desktop-context-menu").hidden = true;
    desktopContextKey = null;
    if (!action || !key)
        return;
    if (action.dataset.desktopAction === "unpin") {
        unpinFromDesktop(key.replace(/^shortcut:/, ""));
        return;
    }
    desktopIconIndex.get(key)?.open();
});
$("#desktop-icons").addEventListener("keydown", (event) => {
    const icon = event.target.closest(".desktop-icon");
    if (icon && event.key === "Enter")
        openDesktopIcon(icon);
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
    if (!event.target.closest("#desktop-context-menu"))
        $("#desktop-context-menu").hidden = true;
});
$("#task-strip").addEventListener("click", (event) => {
    const button = event.target.closest("[data-window]");
    if (button) {
        toggleDesktopWindow(button.dataset.window || "");
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
        // The server knew about the new PC immediately; nothing on screen
        // did, because enrolledPeers was only ever read at boot. So a peer
        // added successfully stayed invisible in the computers menu and on
        // the desktop until a reload.
        try {
            enrolledPeers = await api("/api/peers");
        }
        catch {
            enrolledPeers = [...enrolledPeers, peer];
        }
        // Rebuilds the dock, the computers menu and the desktop icons.
        refreshTaskStrip();
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
