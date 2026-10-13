// Player view: pick a mode, see what each room needs, install, join.
const $ = (s) => document.querySelector(s);
let state = { hub: null, statuses: {}, modes: [], live: {}, mode: null, acValid: false, settings: null };
let pollTimer = null;

async function load() {
  const st = await api('/api/state');
  state.settings = st.settings;
  state.acValid = st.acValid;
  try {
    const data = await api('/api/hub');
    Object.assign(state, { hub: data.hub, statuses: data.statuses, modes: data.modes || [], acValid: data.acValid });
    if (!state.mode || !state.modes.some((m) => m.id === state.mode)) state.mode = state.modes[0]?.id || null;
    $('#baseName').textContent = '· ' + data.hub.name;
    render();
    loadLive();
  } catch (e) {
    state.hub = null;
    render(e.message);
  }
}

async function loadLive() {
  try {
    state.live = await api('/api/live');
    render();
  } catch { /* servers offline are shown per card */ }
}

function render(hubError) {
  const notices = $('#notices');
  notices.replaceChildren();
  if (!state.acValid) {
    notices.append(h('div', { class: 'notice warn' },
      h('div', {}, h('b', {}, 'Pasta do Assetto Corsa não encontrada'),
        h('p', {}, 'Informe a pasta do jogo em Ajustes para o launcher conferir seus arquivos.')),
      h('div', { class: 'spacer' }), h('button', { class: 'btn sm', onclick: openSettings }, 'Abrir ajustes')));
  }
  if (hubError) {
    notices.append(h('div', { class: 'notice bad' },
      h('div', {}, h('b', {}, 'Nenhuma base carregada'), h('p', {}, hubError + '. Peça o link da base ao administrador e cole em Ajustes.')),
      h('div', { class: 'spacer' }), h('button', { class: 'btn sm', onclick: openSettings }, 'Abrir ajustes')));
    $('#modes').replaceChildren();
    $('#modebar').replaceChildren();
    $('#rooms').replaceChildren();
    return;
  }
  renderModes();
  renderRooms();
}

function renderModes() {
  const nav = $('#modes');
  nav.replaceChildren(...state.modes.map((m) => {
    const cls = m.pending ? 'count pending' : 'count ready';
    const label = m.pending ? `${m.pending} faltando` : 'pronto';
    return h('button', { class: 'chip', 'aria-pressed': String(m.id === state.mode), onclick: () => { state.mode = m.id; render(); } },
      m.name, h('span', { class: cls }, label));
  }));
  const mode = state.modes.find((m) => m.id === state.mode);
  const bar = $('#modebar');
  bar.replaceChildren();
  if (!mode) return;
  if (mode.description) bar.append(h('span', {}, mode.description));
  bar.append(h('div', { class: 'spacer' }));
  if (mode.pending) {
    bar.append(h('span', {}, `${mode.pending} itens${mode.pendingMB ? ' · ' + mb(mode.pendingMB * 1048576) : ''} para jogar todas as salas deste modo`),
      h('button', { class: 'btn primary', disabled: !state.acValid || isRunning(), onclick: () => install({ modeId: mode.id }) }, 'Instalar tudo deste modo'));
  } else {
    bar.append(h('span', { class: 'badge ok' }, 'Todas as salas deste modo estão prontas'));
  }
}

function renderRooms() {
  const rooms = state.hub.rooms.filter((r) => r.modeId === state.mode);
  const grid = $('#rooms');
  if (!rooms.length) {
    grid.replaceChildren(h('div', { class: 'empty' }, 'Nenhuma sala neste modo.'));
    return;
  }
  grid.replaceChildren(...rooms.map(roomCard));
}

function roomCard(room) {
  const st = state.statuses[room.id] || { items: [], pending: 0, ready: false };
  const live = state.live[room.id];
  const liveEl = live
    ? h('span', { class: 'live' + (live.online ? ' on' : '') }, h('i'), live.online ? `${live.clients}/${live.maxClients || '?'} online` : 'servidor offline')
    : h('span', { class: 'live' }, h('i'), 'consultando…');
  const pend = st.items.filter((i) => i.state !== 'ok');
  const statusBadge = st.ready
    ? h('span', { class: 'badge ok' }, 'Pronto')
    : h('span', { class: 'badge pending' }, `Faltam ${st.pending}`);
  const list = h('ul', { class: 'items' }, ...st.items.map((it) => h('li', {},
    h('span', { class: 'kind' }, KIND_LABEL[it.kind]),
    h('span', { class: 'name', title: it.id + (it.problems ? ' — ' + it.problems.join(', ') : '') }, it.name || it.id),
    h('span', { class: 'ver' }, it.state === 'outdated' ? `${it.localVersion || 's/ versão'} → ${it.version}` : (it.version || '')),
    h('span', { class: 'badge ' + it.state }, STATE_LABEL[it.state]))));
  const details = h('details', { open: pend.length > 0 && pend.length <= 6 },
    h('summary', {}, `${room.cars.length} carro${room.cars.length > 1 ? 's' : ''} · pista ${room.track.name || room.track.id}`), list);
  const pendingSize = pend.reduce((s, i) => s + (i.size || 0), 0);
  return h('article', { class: 'card' },
    h('div', { class: 'head' },
      h('div', { style: 'flex:1;min-width:0' }, h('h3', {}, room.name), h('div', { class: 'sub' }, `${room.server.host}:${room.server.httpPort}`)),
      statusBadge),
    h('div', { class: 'body' }, room.description ? h('p', { style: 'margin:0 0 8px' }, room.description) : null, details),
    h('div', { class: 'foot' }, liveEl, h('div', { class: 'spacer' }),
      st.ready
        ? h('button', { class: 'btn primary', onclick: () => join(room) }, 'Entrar pelo Content Manager')
        : h('button', { class: 'btn primary', disabled: !state.acValid || isRunning(), onclick: () => install({ roomIds: [room.id] }) },
          `Instalar faltantes${pendingSize ? ' (' + mb(pendingSize) + ')' : ''}`)));
}

// ---- install ----------------------------------------------------------------

let lastJob = null;
function isRunning() { return lastJob && lastJob.status === 'running'; }

async function install(body) {
  try {
    lastJob = await api('/api/install', { method: 'POST', body });
    showDock();
    poll();
    render();
  } catch (e) { toast(e.message, true); }
}

function poll() {
  clearTimeout(pollTimer);
  pollTimer = setTimeout(async () => {
    try { lastJob = await api('/api/job'); } catch { return poll(); }
    showDock();
    if (lastJob && lastJob.status === 'running') poll();
    else load();
  }, 400);
}

const PHASE = { waiting: 'na fila', downloading: 'baixando', extracting: 'instalando', done: 'instalado', error: 'erro' };

function showDock() {
  const j = lastJob;
  if (!j) return;
  $('#dock').hidden = false;
  const done = j.items.filter((i) => i.phase === 'done').length;
  const errs = j.items.filter((i) => i.phase === 'error').length;
  const titles = { running: 'Instalando conteúdo', done: 'Instalação concluída', error: 'Instalação terminou com erros', cancelled: 'Instalação cancelada' };
  $('#dockTitle').textContent = titles[j.status] || 'Instalação';
  $('#dockSub').textContent = `${done}/${j.items.length} itens${errs ? ` · ${errs} com erro` : ''}`;
  $('#dockCancel').hidden = j.status !== 'running';
  $('#dockClose').hidden = j.status === 'running';
  $('#dockList').replaceChildren(...j.items.map((p) => {
    const pct = p.phase === 'done' ? 100 : p.total ? Math.min(100, (p.downloaded / p.total) * 100) : p.phase === 'extracting' ? 100 : 0;
    const cls = 'prog' + (p.phase === 'done' ? ' ok' : p.phase === 'error' ? ' bad' : '');
    const sub = p.phase === 'error' ? p.error : p.phase === 'downloading' && p.total ? `${mb(p.downloaded)} de ${mb(p.total)}` : PHASE[p.phase];
    return h('div', { class: 'pi' },
      h('span', { class: 'n', title: p.id }, `${KIND_LABEL[p.kind]} · ${p.name || p.id}`),
      h('div', { class: cls }, h('i', { style: `width:${pct}%` })),
      h('span', { class: 's', title: sub }, sub));
  }));
}

$('#dockCancel').addEventListener('click', () => api('/api/job/cancel', { method: 'POST', body: {} }));
$('#dockClose').addEventListener('click', () => { $('#dock').hidden = true; });

async function join(room) {
  try {
    await api('/api/join', { method: 'POST', body: { roomId: room.id } });
    toast('Abrindo o Content Manager…');
  } catch (e) { toast(e.message, true); }
}

// ---- settings ---------------------------------------------------------------

function openSettings() {
  const f = $('#settings form');
  f.acPath.value = state.settings?.acPath || '';
  f.hubSource.value = state.settings?.hubSource || '';
  $('#acHint').textContent = state.acValid ? 'Pasta encontrada.' : 'A pasta precisa conter acs.exe e content\\cars.';
  $('#settings').showModal();
}
$('#openSettings').addEventListener('click', openSettings);
$('#settings').addEventListener('close', async () => {
  if ($('#settings').returnValue !== 'save') return;
  const f = $('#settings form');
  try {
    const r = await api('/api/settings', { method: 'POST', body: { acPath: f.acPath.value, hubSource: f.hubSource.value } });
    if (!r.acValid) toast('A pasta informada não parece ser do Assetto Corsa.', true);
    load();
  } catch (e) { toast(e.message, true); }
});
$('#refresh').addEventListener('click', load);

load().then(async () => {
  try {
    lastJob = await api('/api/job');
    if (lastJob && lastJob.status === 'running') { showDock(); poll(); }
  } catch { /* no job yet */ }
});
setInterval(loadLive, 30000);
