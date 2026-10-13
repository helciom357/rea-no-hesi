// Player app: servers grouped by track, room page, my mods, settings.
const S = { autoJoin: null, hub: null, statuses: {}, modes: [], live: {}, acValid: true, hubError: '', job: null, sel: {}, mods: null, modFilter: 'all', modQuery: '', state: null };
const main = document.getElementById('main');

// ---- navigation -------------------------------------------------------------

const NAV = { servidores: ['servers', 'Servidores'], mods: ['mods', 'Meus mods'], ajustes: ['settings', 'Ajustes'] };
for (const a of document.querySelectorAll('[data-nav]')) {
  const [ic, label] = NAV[a.dataset.nav];
  a.append(icon(ic), label);
}

function route() {
  const [, page = 'servidores', arg] = location.hash.split('/');
  for (const a of document.querySelectorAll('[data-nav]')) {
    const on = a.dataset.nav === page || (page === 'sala' && a.dataset.nav === 'servidores');
    a.toggleAttribute('aria-current', on);
    if (on) a.setAttribute('aria-current', 'page');
  }
  return { page, arg: arg && decodeURIComponent(arg) };
}

function render() {
  const { page, arg } = route();
  const scroll = main.scrollTop;
  const focused = document.activeElement?.id === 'modSearch' ? document.activeElement : null;
  const caret = focused ? [focused.selectionStart, focused.selectionEnd] : null;
  const view = page === 'sala' ? roomPage(arg) : page === 'mods' ? modsPage() : page === 'ajustes' ? settingsPage() : serversPage();
  main.replaceChildren(view);
  main.scrollTop = scroll;
  if (focused) {
    const q = document.getElementById('modSearch');
    q?.focus();
    q?.setSelectionRange(...caret);
  }
  renderFloat();
}

// Background updates (live status, install progress) must not wipe what the
// player is typing in Ajustes; there only the floating progress is updated.
function refresh() {
  if (route().page === 'ajustes') renderFloat();
  else render();
}
window.addEventListener('hashchange', () => { main.scrollTop = 0; if (route().page === 'mods') loadMods(); render(); });

// ---- data -----------------------------------------------------------------------

async function loadHub() {
  try {
    const d = await api('/api/hub');
    Object.assign(S, { hub: d.hub, statuses: d.statuses, modes: d.modes, acValid: d.acValid, hubError: '' });
  } catch (e) {
    S.hub = null;
    S.hubError = e.message;
  }
  refresh();
}

async function loadLive() {
  if (!S.hub) return;
  try { S.live = await api('/api/live'); refresh(); } catch { /* keep last */ }
}

async function loadMods() {
  try { S.mods = await api('/api/mods'); } catch (e) { toast(e.message, true); }
  if (route().page === 'mods') refresh();
}

const modeName = (id) => S.modes.find((m) => m.id === id)?.name || id;
const roomById = (id) => S.hub?.rooms.find((r) => r.id === id);
const running = () => S.job && S.job.status === 'running';

// ---- servers ----------------------------------------------------------------------

function serversPage() {
  const totalOnline = Object.values(S.live).reduce((n, l) => n + (l.online ? l.clients : 0), 0);
  const head = h('div', { class: 'page-head' },
    h('div', {}, h('h1', {}, S.hub ? S.hub.name : 'Servidores'), S.hub?.description ? h('p', {}, S.hub.description) : null),
    h('div', { class: 'spacer' }),
    S.hub ? h('span', { class: 'pill' }, h('span', { class: 'dot on' }), `${totalOnline} jogando agora`) : null);
  const page = h('div', { class: 'page' }, head);
  const warn = setupWarning();
  if (warn) page.append(warn);
  if (!S.hub) return page;

  // One card per track; its rooms become the mode tabs.
  const groups = [];
  for (const r of S.hub.rooms) {
    let g = groups.find((x) => x.track === r.track.id);
    if (!g) groups.push((g = { track: r.track.id, rooms: [] }));
    g.rooms.push(r);
  }
  if (!groups.length) {
    page.append(h('div', { class: 'empty' }, h('b', {}, 'Nenhuma sala publicada ainda'), 'Assim que o administrador publicar salas, elas aparecem aqui.'));
    return page;
  }
  page.append(h('div', { class: 'servers' }, groups.map(serverCard)));
  return page;
}

function setupWarning() {
  if (!S.acValid) {
    return h('div', { class: 'alert' }, h('div', { class: 'ico' }, icon('warn')),
      h('div', {}, h('b', {}, 'Não encontramos o Assetto Corsa'), h('span', {}, 'Informe a pasta do jogo para o launcher conferir seus arquivos.')),
      h('div', { class: 'spacer' }), h('a', { class: 'btn white', href: '#/ajustes' }, 'Configurar'));
  }
  if (S.hubError) {
    return h('div', { class: 'alert' }, h('div', { class: 'ico' }, icon('link')),
      h('div', {}, h('b', {}, 'Conecte-se a uma base'), h('span', {}, 'Cole o link da base que o administrador passou.')),
      h('div', { class: 'spacer' }), h('a', { class: 'btn white', href: '#/ajustes' }, 'Colar link'));
  }
  return null;
}

function serverCard(g) {
  const sel = roomById(S.sel[g.track]) && S.sel[g.track] || g.rooms[0].id;
  const room = roomById(sel);
  const st = S.statuses[room.id] || { pending: 0, ready: false };
  const live = S.live[room.id];
  const tabs = h('div', { class: 'tabs', role: 'tablist' }, g.rooms.map((r) => {
    const l = S.live[r.id];
    return h('button', {
      role: 'tab', 'aria-selected': String(r.id === sel),
      onclick: () => { S.sel[g.track] = r.id; render(); },
    }, h('b', {}, modeName(r.modeId)), h('span', {}, l?.online ? `${l.clients} online` : l ? 'offline' : '…'));
  }));
  const open = () => { location.hash = '#/sala/' + encodeURIComponent(room.id); };
  const chips = h('div', { class: 'chips' },
    live?.online ? h('span', { class: 'chip' }, icon('signal'), `${live.pingMs} ms`) : null,
    h('span', { class: 'chip' }, h('span', { class: 'dot ' + (live?.online ? 'on' : '') }), live?.online ? `${live.clients}/${live.maxClients || '?'}` : live ? 'offline' : '…'),
    st.ready ? h('span', { class: 'chip' }, icon('check'), 'pronto') : h('span', { class: 'chip', style: 'color:var(--warn)' }, icon('download'), `falta ${st.pending}`));
  const action = st.ready
    ? h('button', { class: 'btn primary', onclick: () => join(room) }, icon('play'), `Jogar ${modeName(room.modeId)}`)
    : h('button', { class: 'btn white', disabled: !S.acValid || running(), onclick: () => { S.autoJoin = room.id; open(); install({ roomIds: [room.id] }); } }, icon('download'), 'Baixar e jogar');
  return h('article', { class: 'scard' },
    bgImage([room.image, room.track.image, localImg('track', room.track.id)]),
    tabs,
    h('div', { class: 'content' },
      h('h2', { onclick: open, title: 'Ver sala' }, room.track.name || room.name),
      chips,
      h('div', { class: 'actions' },
        h('button', { class: 'btn count', onclick: open, title: 'Ver carros da sala' }, icon('list'), String(room.cars.length)),
        action)));
}

// ---- room ---------------------------------------------------------------------

function roomPage(id) {
  const room = roomById(id);
  if (!room) {
    return h('div', { class: 'page' }, h('a', { class: 'btn back', href: '#/servidores' }, icon('back'), 'Voltar'),
      h('div', { class: 'empty', style: 'margin-top:20px' }, S.hub ? 'Sala não encontrada.' : 'Carregando…'));
  }
  const st = S.statuses[room.id] || { items: [], pending: 0, ready: false };
  const live = S.live[room.id] || {};
  const pend = st.items.filter((i) => i.state !== 'ok');
  const size = pend.reduce((n, i) => n + (i.size || 0), 0);

  const hero = h('section', { class: 'hero' },
    bgImage([room.image, room.track.image, localImg('track', room.track.id)]),
    h('a', { class: 'btn sm back', href: '#/servidores' }, icon('back'), 'Voltar'),
    h('div', { class: 'row' },
      h('div', {},
        h('h1', {}, room.name),
        h('div', { class: 'chips' },
          h('span', { class: 'chip' }, h('span', { class: 'dot ' + (live.online ? 'on' : '') }), live.online ? `${live.clients}/${live.maxClients || '?'} jogando` : 'servidor offline'),
          h('span', { class: 'chip' }, modeName(room.modeId)),
          live.online ? h('span', { class: 'chip' }, icon('signal'), `${live.pingMs} ms`) : null)),
      h('div', { class: 'spacer' }),
      h('button', { class: 'btn lg ' + (st.ready ? 'primary' : ''), disabled: !st.ready, title: st.ready ? '' : 'Instale o conteúdo faltante primeiro', onclick: () => join(room) }, icon('play'), 'Jogar')));

  const body = h('div', { class: 'room-body' });
  const job = S.job;
  const jobHere = job && job.items.some((p) => st.items.some((i) => i.kind === p.kind && i.id === p.id));
  if (running() && jobHere) {
    body.append(progressAlert(job));
  } else if (!st.ready) {
    body.append(h('div', { class: 'alert' }, h('div', { class: 'ico' }, icon('warn')),
      h('div', {}, h('b', {}, 'Ação necessária'), h('span', {}, `${plural(pend.length, 'item precisa', 'itens precisam')} ser baixado${pend.length > 1 ? 's' : ''} antes de entrar${size ? ' · ' + mb(size) : ''}.`)),
      h('div', { class: 'spacer' }),
      h('button', { class: 'btn white', disabled: !S.acValid || running(), onclick: () => install({ roomIds: [room.id] }) }, icon('download'), 'Instalar conteúdo faltante')));
  } else {
    body.append(h('div', { class: 'alert ok' }, h('div', { class: 'ico' }, icon('check')),
      h('div', {}, h('b', {}, 'Tudo pronto'), h('span', {}, 'Você tem todos os carros e a pista desta sala. Clique em Jogar para entrar pelo Content Manager.'))));
  }
  if (room.description) body.append(h('p', { style: 'color:var(--ink-2);margin:-10px 0 22px' }, room.description));

  const item = (kind, id) => st.items.find((i) => i.kind === kind && i.id === id) || { state: 'ok' };
  const cars = room.cars.map((c) => contentCard(c, 'car', item('car', c.id), live.slots?.[c.id]));
  body.append(
    h('div', { class: 'section-title' }, h('h3', {}, 'Carros desta sala'), h('span', { class: 'hint' }, 'Para entrar, você precisa ter todos instalados')),
    h('div', { class: 'cards' }, cars),
    h('div', { class: 'section-title', style: 'margin-top:28px' }, h('h3', {}, 'Pista')),
    h('div', { class: 'cards' }, contentCard(room.track, 'track', item('track', room.track.id))));
  return h('div', {}, hero, body);
}

function contentCard(entry, kind, it, slot) {
  const s = STATE[it.state] || STATE.ok;
  const p = pic([entry.image, localImg(kind, entry.id)], kind === 'car' ? 'car' : 'road');
  if (slot) p.append(h('span', { class: 'corner slots' + (slot.free ? '' : ' full'), title: 'Vagas livres / total' }, `${slot.free}/${slot.total}`));
  const ver = it.state === 'outdated' ? `v${it.localVersion || '?'} → v${it.version}` : entry.version ? `v${entry.version}` : '';
  return h('div', { class: 'ccard' + (it.state === 'ok' ? '' : ' need') }, p,
    h('div', { class: 'info' },
      h('div', { class: 'name', title: entry.name || entry.id }, entry.name || entry.id),
      h('div', { class: 'row' }, h('span', { class: 'tag ' + s.cls }, s.label), h('span', { class: 'meta' }, [ver, it.state !== 'ok' && entry.size ? mb(entry.size) : ''].filter(Boolean).join(' · ')))));
}

function progressAlert(job) {
  const total = job.items.length;
  const done = job.items.filter((p) => p.phase === 'done').length;
  const bytes = job.items.reduce((n, p) => n + (p.phase === 'done' ? (p.total || 0) : p.downloaded || 0), 0);
  const all = job.items.reduce((n, p) => n + (p.total || p.size || 0), 0);
  const cur = job.items.find((p) => p.phase === 'downloading' || p.phase === 'extracting');
  return h('div', { class: 'alert' }, h('div', { class: 'ico' }, h('div', { class: 'spin' })),
    h('div', {}, h('b', {}, `Instalando ${done + 1 > total ? total : done + 1} de ${total}`),
      h('span', {}, cur ? `${cur.phase === 'extracting' ? 'Instalando' : 'Baixando'} ${cur.name || cur.id}${all ? ` · ${mb(bytes)} de ${mb(all)}` : ''}` : 'Preparando…')),
    h('div', { class: 'spacer' }),
    h('button', { class: 'btn sm', onclick: () => api('/api/job/cancel', { method: 'POST', body: {} }) }, 'Cancelar'),
    h('div', { class: 'prog' }, h('i', { style: `width:${all ? (bytes / all) * 100 : (done / total) * 100}%` })));
}

// ---- my mods -------------------------------------------------------------------

function modsPage() {
  const page = h('div', { class: 'page' },
    h('div', { class: 'page-head' }, h('div', {}, h('h1', {}, 'Meus mods'),
      h('p', {}, 'O que está instalado no seu Assetto Corsa e o que as salas da base precisam.'))));
  const warn = setupWarning();
  if (warn) page.append(warn);
  if (!S.mods) { page.append(h('div', { class: 'empty' }, 'Lendo suas pastas…')); return page; }
  const items = S.mods.items || [];
  const missing = items.filter((m) => m.required && m.state !== 'ok');
  if (missing.length) {
    const size = missing.reduce((n, m) => n + (m.installed ? 0 : m.size || 0), 0);
    page.append(h('div', { class: 'alert' }, h('div', { class: 'ico' }, icon('warn')),
      h('div', {}, h('b', {}, `${plural(missing.length, 'item necessário', 'itens necessários')} para as salas`), h('span', {}, `Instale para poder entrar em qualquer sala da base${size ? ' · ' + mb(size) : ''}.`)),
      h('div', { class: 'spacer' }),
      h('button', { class: 'btn white', disabled: running() || !S.acValid, onclick: () => install({}) }, icon('download'), 'Instalar tudo que falta')));
  }
  const counts = {
    all: items.length,
    required: items.filter((m) => m.required).length,
    missing: missing.length,
    car: items.filter((m) => m.kind === 'car' && m.installed).length,
    track: items.filter((m) => m.kind === 'track' && m.installed).length,
  };
  const labels = { all: 'Todos', required: 'Usados na base', missing: 'Faltando', car: 'Carros', track: 'Pistas' };
  const seg = h('div', { class: 'seg' }, Object.keys(labels).map((k) => h('button', {
    'aria-pressed': String(S.modFilter === k), onclick: () => { S.modFilter = k; render(); },
  }, labels[k], h('b', {}, counts[k]))));
  const q = h('input', { type: 'search', id: 'modSearch', placeholder: 'Buscar por nome ou pasta…', oninput: (e) => { S.modQuery = e.target.value.toLowerCase(); drawGrid(); } });
  q.value = S.modQuery;
  page.append(h('div', { style: 'display:flex;gap:12px;flex-wrap:wrap;margin-bottom:18px' }, h('label', { class: 'search' }, icon('search'), q), seg));
  const grid = h('div', { class: 'cards' });
  page.append(grid);
  function drawGrid() {
    const f = S.modFilter;
    const list = items.filter((m) => (f === 'all' || (f === 'required' && m.required) || (f === 'missing' && m.required && m.state !== 'ok') ||
      (f === m.kind && m.installed)) && (!S.modQuery || m.name.toLowerCase().includes(S.modQuery) || m.id.toLowerCase().includes(S.modQuery)));
    grid.replaceChildren(...list.slice(0, 400).map(modCard));
    if (!list.length) grid.replaceChildren(h('div', { class: 'empty', style: 'grid-column:1/-1' }, 'Nada encontrado.'));
  }
  drawGrid();
  return page;
}

function modCard(m) {
  const imgs = m.installed && m.hasLocalImage ? [localImg(m.kind, m.id), m.image] : [m.image];
  const p = pic(imgs, m.kind === 'car' ? 'car' : 'road');
  if (m.required) p.append(h('span', { class: 'corner-l tag accent', title: 'Usado em: ' + (m.rooms || []).join(', ') }, 'Usado na base'));
  const s = m.required ? STATE[m.state] : m.installed ? { label: 'Instalado', cls: '' } : null;
  const needs = m.required && m.state !== 'ok';
  const btn = needs
    ? h('button', { class: 'btn sm primary', disabled: running() || !S.acValid, onclick: () => install({ only: [m.kind + ':' + m.id] }) }, icon('download'), m.state === 'outdated' ? 'Atualizar' : 'Baixar')
    : m.installed ? h('button', { class: 'btn sm ghost', title: 'Abrir pasta', 'aria-label': 'Abrir pasta de ' + m.name, onclick: () => api('/api/mods/open', { method: 'POST', body: { kind: m.kind, id: m.id } }).catch((e) => toast(e.message, true)) }, icon('folder')) : null;
  return h('div', { class: 'ccard' + (m.installed ? '' : ' dim') + (needs ? ' need' : '') }, p,
    h('div', { class: 'info' },
      h('div', { class: 'name', title: m.name }, m.name),
      h('div', { class: 'meta', title: m.id }, `${m.kind === 'car' ? 'Carro' : 'Pista'} · ${m.id}`),
      h('div', { class: 'row', style: 'flex-wrap:nowrap' }, s ? h('span', { class: 'tag ' + s.cls }, s.label) : null,
        h('span', { class: 'meta' }, mb(m.size)), h('div', { class: 'spacer' }), btn)));
}

// ---- settings ---------------------------------------------------------------------

function settingsPage() {
  const st = S.state || {};
  const ac = h('input', { type: 'text', placeholder: 'C:\\Program Files (x86)\\Steam\\steamapps\\common\\assettocorsa' });
  ac.value = st.acPath || '';
  const hub = h('input', { type: 'text', placeholder: 'https://seusite.com.br/ac/files/hub.json' });
  hub.value = st.hubSource || '';
  const save = async () => {
    try {
      const r = await api('/api/settings', { method: 'POST', body: { acPath: ac.value, hubSource: hub.value } });
      S.state = await api('/api/state');
      toast(r.acValid ? 'Salvo.' : 'Salvo — mas essa pasta não parece ser do Assetto Corsa.', !r.acValid);
      await loadHub();
      loadLive();
    } catch (e) { toast(e.message, true); }
  };
  return h('div', { class: 'page', style: 'max-width:760px' },
    h('div', { class: 'page-head' }, h('h1', {}, 'Ajustes')),
    h('div', { class: 'panel' },
      h('label', { class: 'field' }, 'Pasta do Assetto Corsa', ac, h('span', { class: 'hint' }, st.acValid ? '✓ Pasta encontrada' : 'A pasta onde fica o acs.exe')),
      h('div', { style: 'height:16px' }),
      h('label', { class: 'field' }, 'Link da base', hub,
        h('span', { class: 'hint' }, st.hubUsed && !st.hubSource ? `Usando a base publicada por você: ${st.hubUsed}` : 'O administrador da base passa este link.')),
      h('div', { style: 'display:flex;gap:8px;margin-top:18px' }, h('button', { class: 'btn primary', onclick: save }, 'Salvar'))),
    h('div', { class: 'panel' },
      h('h2', {}, 'Administrar uma base'),
      h('p', { class: 'lead' }, 'Adicione carros e pistas, crie salas e publique para os jogadores.'),
      h('a', { class: 'btn', href: '/admin' }, icon('admin'), 'Abrir painel do administrador')),
    h('p', { class: 'hint' }, `Launcher Brasil ${st.version || ''}`));
}

// ---- actions ----------------------------------------------------------------------

async function install(body) {
  try {
    S.job = await api('/api/install', { method: 'POST', body });
    render();
    pollJob();
  } catch (e) { toast(e.message, true); }
}

let jobTimer;
function pollJob() {
  clearTimeout(jobTimer);
  jobTimer = setTimeout(async () => {
    try { S.job = await api('/api/job'); } catch { return pollJob(); }
    if (running()) { refresh(); pollJob(); return; }
    const errs = S.job?.items.filter((p) => p.phase === 'error') || [];
    if (S.job?.status === 'done') toast('Conteúdo instalado.');
    else if (errs.length) toast(`${errs[0].name || errs[0].id}: ${errs[0].error}`, true);
    await loadHub();
    if (route().page === 'mods') loadMods();
    const auto = S.autoJoin;
    S.autoJoin = null;
    if (auto && S.job?.status === 'done' && S.statuses[auto]?.ready) join(roomById(auto));
  }, 500);
}

function renderFloat() {
  const f = document.getElementById('float');
  const onRoom = route().page === 'sala';
  if (!running() || onRoom) { f.hidden = true; return; }
  const job = S.job;
  const done = job.items.filter((p) => p.phase === 'done').length;
  const bytes = job.items.reduce((n, p) => n + (p.phase === 'done' ? (p.total || 0) : p.downloaded || 0), 0);
  const all = job.items.reduce((n, p) => n + (p.total || p.size || 0), 0);
  const cur = job.items.find((p) => p.phase === 'downloading' || p.phase === 'extracting');
  f.hidden = false;
  f.replaceChildren(
    h('div', { class: 'top' }, h('div', { class: 'spin' }), h('b', {}, `Instalando ${Math.min(done + 1, job.items.length)} de ${job.items.length}`), h('div', { class: 'spacer' }),
      h('button', { class: 'btn sm ghost', onclick: () => api('/api/job/cancel', { method: 'POST', body: {} }) }, 'Cancelar')),
    h('div', { class: 'sub' }, cur ? `${cur.name || cur.id}${all ? ` · ${mb(bytes)} de ${mb(all)}` : ''}` : 'Preparando…'),
    h('div', { class: 'prog' }, h('i', { style: `width:${all ? (bytes / all) * 100 : (done / job.items.length) * 100}%` })));
}

async function join(room) {
  try {
    await api('/api/join', { method: 'POST', body: { roomId: room.id } });
    toast('Abrindo o Content Manager…');
  } catch (e) { toast(e.message, true); }
}

// ---- start ------------------------------------------------------------------------

(async () => {
  render();
  S.state = await api('/api/state').catch(() => null);
  await loadHub();
  loadLive();
  try { S.job = await api('/api/job'); if (running()) pollJob(); } catch { /* none */ }
  if (route().page === 'mods') loadMods();
  setInterval(loadLive, 20000);
})();
