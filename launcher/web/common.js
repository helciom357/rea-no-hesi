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
  return v >= 1024 ? (v / 1024).toFixed(1) + ' GB' : v.toFixed(v < 10 ? 1 : 0) + ' MB';
}

const STATE_LABEL = { ok: 'OK', missing: 'Falta', broken: 'Danificado', outdated: 'Desatualizado' };
const KIND_LABEL = { car: 'Carro', track: 'Pista' };
