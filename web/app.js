const COLORS = {
  purple: ['#7c6af7', '#5b4ec2'], blue: ['#3b82f6', '#1d4ed8'], green: ['#10b981', '#047857'],
  orange: ['#f97316', '#c2410c'], pink: ['#ec4899', '#be185d'], magenta: ['#d946ef', '#a21caf'],
  cyan: ['#06b6d4', '#0e7490'], red: ['#ef4444', '#b91c1c'], yellow: ['#eab308', '#a16207']
};
const state = { roots: [], root: 0, path: '', selected: null };
const $ = (selector) => document.querySelector(selector);

function setTheme(name) {
  const [accent, dim] = COLORS[name] || COLORS.purple;
  document.documentElement.style.setProperty('--accent', accent);
  document.documentElement.style.setProperty('--accent-dim', dim);
  document.documentElement.style.setProperty('--accent-bright', accent);
  document.documentElement.style.setProperty('--accent-glow', `${accent}26`);
  localStorage.setItem('eta_theme_color', name);
}

function iconify() { window.lucide?.createIcons({ attrs: { 'stroke-width': 1.65 } }); }
function escapeHTML(value) { const node = document.createElement('span'); node.textContent = value; return node.innerHTML; }
function fileURL(path, download = false) { const params = new URLSearchParams({ root: state.root, path }); if (download) params.set('download', '1'); return `/api/file?${params}`; }
function bytes(value) { if (!value) return '—'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1); return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`; }
function date(value) { return window.dayjs ? dayjs(value).format('MMM D, YYYY') : new Date(value).toLocaleDateString(); }

async function api(path) {
  const response = await fetch(path);
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
  return body;
}

function showError(message) {
  $('#error-message').textContent = message;
  $('#error-toast').toast();
}

function renderBreadcrumbs() {
  const root = state.roots[state.root];
  const crumbs = [{ name: root?.name || 'Archive', path: '' }];
  let current = '';
  for (const part of state.path.split('/').filter(Boolean)) { current = current ? `${current}/${part}` : part; crumbs.push({ name: part, path: current }); }
  $('#breadcrumbs').innerHTML = crumbs.map((crumb, index) => `${index ? '<i class="crumb-separator" data-lucide="chevron-right"></i>' : ''}<button class="breadcrumb" data-path="${escapeHTML(crumb.path)}">${escapeHTML(crumb.name)}</button>`).join('');
}

function renderEntries(entries) {
  $('#item-count').textContent = `${entries.length} ${entries.length === 1 ? 'item' : 'items'}`;
  const container = $('#entries');
  if (!entries.length) { container.innerHTML = '<div class="empty"><div><i data-lucide="package-open"></i>This folder is empty.</div></div>'; iconify(); return; }
  container.innerHTML = entries.map((entry) => `<button class="entry ${entry.kind}" data-path="${escapeHTML(entry.path)}" data-kind="${entry.kind}"><span class="entry-name-col"><i class="entry-icon" data-lucide="${entry.kind === 'directory' ? 'folder' : 'file'}"></i><span class="entry-name">${escapeHTML(entry.name)}</span></span><span class="entry-meta">${date(entry.modified)}</span><span class="entry-meta">${entry.kind === 'directory' ? '—' : bytes(entry.size)}</span></button>`).join('');
  iconify();
}

async function navigate(path = '') {
  state.path = path;
  $('#entries').innerHTML = '<div class="empty"><sl-spinner></sl-spinner></div>';
  renderBreadcrumbs(); iconify();
  try {
    const params = new URLSearchParams({ root: state.root, path });
    const result = await api(`/api/list?${params}`);
    if (result.entry && result.entry.kind !== 'directory') { await preview({ path, name: result.entry.name, kind: 'file' }); return; }
    renderBreadcrumbs(); renderEntries(result.entries || []);
  } catch (error) { showError(error.message); renderEntries([]); }
}

async function preview(entry) {
  state.selected = entry;
  $('#preview-dialog').label = entry.name;
  $('#preview-content').innerHTML = '<div class="empty"><sl-spinner></sl-spinner></div>';
  $('#preview-dialog').show();
  try {
    const extension = entry.name.split('.').pop().toLowerCase();
    if (['png','jpg','jpeg','gif','webp','svg'].includes(extension)) {
      $('#preview-content').innerHTML = `<img class="preview-image" alt="${escapeHTML(entry.name)}" src="${fileURL(entry.path)}">`;
    } else {
      const result = await api(`/api/preview?${new URLSearchParams({ root: state.root, path: entry.path })}`);
      $('#preview-content').innerHTML = result.binary
        ? '<p class="preview-note">This looks like a binary file. Download it to inspect it locally.</p>'
        : `<pre class="preview-text">${escapeHTML(result.text || '')}${result.truncated ? '\n\n… preview truncated at 512 KB' : ''}</pre>`;
    }
  } catch (error) { $('#preview-content').innerHTML = `<p class="preview-note">${escapeHTML(error.message)}</p>`; }
}

async function boot() {
  setTheme(localStorage.getItem('eta_theme_color') || 'purple');
  try {
    state.roots = await api('/api/roots');
    $('#root-select').innerHTML = state.roots.map((root) => `<option value="${root.id}">${escapeHTML(root.name)}</option>`).join('');
    await navigate();
  } catch (error) { $('#server-status').textContent = 'OFFLINE'; showError(error.message); }
  iconify();
}

$('#root-select').addEventListener('change', (event) => { state.root = Number(event.target.value); navigate(); });
$('#refresh-button').addEventListener('click', () => navigate(state.path));
$('#up-button').addEventListener('click', () => navigate(state.path.split('/').slice(0, -1).join('/')));
$('#breadcrumbs').addEventListener('click', (event) => { const button = event.target.closest('[data-path]'); if (button) navigate(button.dataset.path); });
$('#entries').addEventListener('click', (event) => { const row = event.target.closest('.entry'); if (!row) return; const item = { path: row.dataset.path, name: row.querySelector('.entry-name').textContent, kind: row.dataset.kind }; item.kind === 'directory' ? navigate(item.path) : preview(item); });
$('#download-button').addEventListener('click', () => { if (state.selected) window.open(fileURL(state.selected.path, true), '_blank', 'noopener'); });
$('#close-dialog').addEventListener('click', () => $('#preview-dialog').hide());
$('#theme-button').addEventListener('click', () => $('#theme-dialog').show());
$('#swatches').innerHTML = Object.entries(COLORS).map(([name, [color]]) => `<button class="swatch" style="--swatch:${color}" data-theme="${name}"><span class="swatch-dot"></span>${name}</button>`).join('');
$('#swatches').addEventListener('click', (event) => { const button = event.target.closest('[data-theme]'); if (!button) return; setTheme(button.dataset.theme); $('#theme-dialog').hide(); });
boot();
