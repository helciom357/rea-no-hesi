// Shared helpers for the player and admin pages.
const TOKEN = document.querySelector('meta[name="token"]').content;

async function api(path, opts = {}) {
  const res = await fetch(path, {
    method: opts.method || 'GET',
    headers: { 'X-Token': TOKEN, 'Content-Type': 'application/json' },
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  });
  let data = null;
  try { data = await res.json(); } catch { /* empty body */ }
  if (!res.ok) throw new Error((data && data.error) || `Erro ${res.status}`);
  return data;
}

function h(tag, attrs = {}, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'html') el.innerHTML = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

// Static icon set (stroke icons, 24px grid).
const ICONS = {
  servers: '<path d="M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z"/><path d="m15.5 8.5-2 5-5 2 2-5 5-2Z"/>',
  mods: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/>',
  settings: '<path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1Z"/>',
  admin: '<path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6l-8-3Z"/>',
  rooms: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 10h18M9 20V10"/>',
  link: '<path d="M10 14a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-1 1"/><path d="M14 10a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l1-1"/>',
  play: '<path d="M7 4v16l13-8L7 4Z" fill="currentColor"/>',
  download: '<path d="M12 4v11m0 0-4-4m4 4 4-4M5 20h14"/>',
  upload: '<path d="M12 20V9m0 0-4 4m4-4 4 4M5 4h14"/>',
  users: '<path d="M16 19v-1a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v1"/><circle cx="9.5" cy="7.5" r="3.5"/><path d="M21 19v-1a4 4 0 0 0-3-3.9M15.5 4.1a3.5 3.5 0 0 1 0 6.8"/>',
  signal: '<path d="M4 20v-3M9 20v-7M14 20V9M19 20V4"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
  back: '<path d="M19 12H5m0 0 6-6m-6 6 6 6"/>',
  warn: '<path d="M12 9v4m0 4h.01M10.3 3.9 2 18a2 2 0 0 0 1.7 3h16.6a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"/>',
  check: '<path d="m5 12 5 5 9-10"/>',
  car: '<path d="M5 17h14M6 17v2M18 17v2M3 13l2-6h14l2 6v4H3v-4Z"/><circle cx="7.5" cy="14" r=".6"/><circle cx="16.5" cy="14" r=".6"/>',
  road: '<path d="M8 3 4 21M16 3l4 18M12 5v2M12 11v2M12 17v2"/>',
  folder: '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  x: '<path d="M6 6l12 12M18 6 6 18"/>',
  copy: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7"/>',
};
function icon(name) {
  const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  s.setAttribute('fill', 'none');
  s.setAttribute('stroke', 'currentColor');
  s.setAttribute('stroke-width', '1.8');
  s.setAttribute('stroke-linecap', 'round');
  s.setAttribute('stroke-linejoin', 'round');
  s.setAttribute('aria-hidden', 'true');
  s.innerHTML = ICONS[name] || '';
  return s;
}

// pic shows the first image that loads, or a placeholder icon.
function pic(urls, fallbackIcon = 'car') {
  const wrap = h('div', { class: 'pic' });
  const list = (Array.isArray(urls) ? urls : [urls]).filter(Boolean);
  const ph = h('div', { class: 'ph' }, icon(fallbackIcon));
  wrap.append(ph);
  const tryNext = (i) => {
    if (i >= list.length) return;
    const img = h('img', { alt: '', loading: 'lazy', src: list[i] });
    img.addEventListener('load', () => ph.remove());
    img.addEventListener('error', () => { img.remove(); tryNext(i + 1); });
    wrap.prepend(img);
  };
  tryNext(0);
  return wrap;
}

function bgImage(urls) {
  const bg = h('div', { class: 'bg' });
  const list = (Array.isArray(urls) ? urls : [urls]).filter(Boolean);
  const tryNext = (i) => {
    if (i >= list.length) return;
    const img = h('img', { alt: '', src: list[i] });
    img.addEventListener('error', () => { img.remove(); tryNext(i + 1); });
    bg.append(img);
  };
  tryNext(0);
  return bg;
}

const localImg = (kind, id) => `/img/local?kind=${kind}&id=${encodeURIComponent(id)}&t=${TOKEN}`;

let toastTimer;
function toast(msg, bad = false) {
  document.querySelector('.toast')?.remove();
  const t = h('div', { class: 'toast' + (bad ? ' bad' : ''), role: 'status' }, msg);
  document.body.append(t);
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.remove(), bad ? 7000 : 3500);
}

function mb(bytes) {
  if (!bytes) return '';
  const v = bytes / 1048576;
  if (v >= 1024) return (v / 1024).toFixed(1).replace('.', ',') + ' GB';
  return (v < 10 ? v.toFixed(1).replace('.', ',') : Math.round(v)) + ' MB';
}

function ago(iso) {
  if (!iso) return '';
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return 'agora';
  if (s < 3600) return `há ${Math.round(s / 60)} min`;
  return `há ${Math.round(s / 3600)} h`;
}

function plural(n, one, many) { return `${n} ${n === 1 ? one : many}`; }

const STATE = {
  ok: { label: 'Instalado', cls: 'ok' },
  missing: { label: 'Falta baixar', cls: 'bad' },
  broken: { label: 'Danificado', cls: 'bad' },
  outdated: { label: 'Atualizar', cls: 'warn' },
};
