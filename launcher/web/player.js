// Player app: events (race invites) of the base, event page, my mods, settings.
const S = { hub: null, statuses: {}, images: {}, live: {}, acValid: true, hubError: '', job: null, mods: null, modFilter: 'all', modQuery: '', state: null, autoJoin: null, filter: 'all' };
const main = document.getElementById('main');

// ---- navigation -------------------------------------------------------------

const NAV = { eventos: ['calendar', 'Eventos'], mods: ['mods', 'Meus mods'], ajustes: ['settings', 'Ajustes'] };
for (const a of document.querySelectorAll('[data-nav]')) {
  const [ic, label] = NAV[a.dataset.nav];
  a.append(icon(ic), label);
}

function route() {
  const [, page = 'eventos', arg] = location.hash.split('/');
  for (const a of document.querySelectorAll('[data-nav]')) {
    const on = a.dataset.nav === page || (page === 'evento' && a.dataset.nav === 'eventos');
    if (on) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  }
  return { page, arg: arg && decodeURIComponent(arg) };
}

function render() {
  const { page, arg } = route();
  const scroll = main.scrollTop;
  const focused = ['modSearch', 'evPass'].includes(document.activeElement?.id) ? document.activeElement : null;
  const keep = focused ? { id: focused.id, value: focused.value, sel: [focused.selectionStart, focused.selectionEnd] } : null;
  const view = page === 'evento' ? eventPage(arg) : page === 'mods' ? modsPage() : page === 'ajustes' ? settingsPage() : eventsPage();
  main.replaceChildren(view);
  main.scrollTop = scroll;
  if (keep) {
    const el = document.getElementById(keep.id);
    if (el) { el.value = keep.value; el.focus(); el.setSelectionRange(...keep.sel); }
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
    Object.assign(S, { hub: d.hub, statuses: d.statuses || {}, images: d.images || {}, acValid: d.acValid, hubError: '' });
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

const eventById = (id) => S.hub?.events.find((e) => e.id === id);
const running = () => S.job && S.job.status === 'running';
const evImages = (ev) => [S.images[ev.id], localImg('track', ev.track?.id || '')];

// ---- events (home) -------------------------------------------------------------------

function eventsPage() {
  const totalOnline = Object.values(S.live).reduce((n, l) => n + (l.online ? l.clients : 0), 0);
  const page = h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', {}, h('h1', {}, S.hub ? S.hub.name : 'Eventos'),
        h('p', {}, S.hub?.description || 'Escolha um evento: o launcher baixa os arquivos exatos que o servidor do host está rodando.')),
      h('div', { class: 'spacer' }),
      S.hub && totalOnline ? h('span', { class: 'pill' }, h('span', { class: 'dot on' }), `${totalOnline} jogando agora`) : null));
  const warn = setupWarning();
  if (warn) page.append(warn);
  if (!S.hub) return page;
  if (!S.hub.events.length) {
    page.append(h('div', { class: 'empty' }, h('b', {}, 'Nenhum evento agora'), 'Quando um host publicar um race invite, ele aparece aqui.'));
    return page;
  }
  const match = (e, f) => f === 'all' || (f === 'public' ? !e.private : e.private);
  const count = (f) => S.hub.events.filter((e) => match(e, f)).length;
  page.append(h('div', { style: 'margin-bottom:18px' }, h('div', { class: 'seg' }, [['all', 'Todos'], ['public', 'Públicos'], ['private', 'Privados']].map(([k, l]) =>
    h('button', { 'aria-pressed': String(S.filter === k), onclick: () => { S.filter = k; render(); } }, l, h('b', {}, count(k)))))));
  page.append(h('div', { class: 'servers' }, S.hub.events.filter((e) => match(e, S.filter)).map(eventCard)));
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
      h('div', {}, h('b', {}, 'Conecte-se a uma base'), h('span', {}, 'Cole o link da base de eventos em Ajustes.')),
      h('div', { class: 'spacer' }), h('a', { class: 'btn white', href: '#/ajustes' }, 'Colar link'));
  }
  return null;
}

function eventCard(ev) {
  const st = S.statuses[ev.id];
  const live = S.live[ev.id];
  const open = () => { location.hash = '#/evento/' + encodeURIComponent(ev.id); };
  const badges = h('div', { class: 'badges' },
    h('span', { class: 'badge-glass' }, icon(ev.private ? 'lock' : 'globe'), ev.private ? 'Privado' : 'Público'),
    ev.lan ? h('span', { class: 'badge-glass' }, icon('lan'), 'LAN') : null);
  const chips = h('div', { class: 'chips' },
    live?.online ? h('span', { class: 'chip' }, icon('signal'), `${live.pingMs} ms`) : null,
    live ? h('span', { class: 'chip' }, h('span', { class: 'dot ' + (live.online ? 'on' : '') }), live.online ? `${live.clients}/${live.maxClients || '?'}` : ev.lan ? 'via Radmin' : 'offline') : null,
    h('span', { class: 'chip' }, icon('car'), String(ev.cars.length)),
    ev.locked ? null : st?.ready ? h('span', { class: 'chip', style: 'color:var(--ok)' }, icon('check'), 'pronto')
      : st ? h('span', { class: 'chip', style: 'color:var(--warn)' }, icon('download'), `falta ${st.pending}`) : null);
  let action;
  if (ev.locked) action = h('button', { class: 'btn white', onclick: open }, icon('lock'), 'Desbloquear');
  else if (st?.ready) action = h('button', { class: 'btn primary', onclick: () => join(ev) }, icon('play'), 'Jogar');
  else {
    action = h('button', {
      class: 'btn white', disabled: !S.acValid || running(),
      // LAN events need the Radmin step first, so they only download.
      onclick: () => { if (!ev.lan) S.autoJoin = ev.id; open(); install({ eventIds: [ev.id] }); },
    }, icon('download'), ev.lan ? 'Baixar arquivos' : 'Baixar e jogar');
  }
  return h('article', { class: 'scard' + (ev.locked ? ' locked' : '') },
    bgImage(evImages(ev)), badges,
    h('div', { class: 'content' },
      h('h2', { onclick: open, title: 'Ver evento' }, ev.name),
      h('div', { class: 'by' }, [ev.track?.name || ev.track?.id, ev.host ? 'por ' + ev.host : ''].filter(Boolean).join(' · ')),
      chips,
      h('div', { class: 'actions' }, h('button', { class: 'btn count', onclick: open, title: 'Ver evento' }, icon('list'), 'Ver'), action)));
}

// ---- event page ----------------------------------------------------------------------

function eventPage(id) {
  const ev = eventById(id);
  if (!ev) {
    return h('div', { class: 'page' }, h('a', { class: 'btn back', href: '#/eventos' }, icon('back'), 'Voltar'),
      h('div', { class: 'empty', style: 'margin-top:20px' }, S.hub ? 'Evento não encontrado — o host pode ter apagado.' : 'Carregando…'));
  }
  const live = S.live[ev.id] || {};
  const hero = h('section', { class: 'hero' },
    bgImage(evImages(ev)),
    h('a', { class: 'btn sm back', href: '#/eventos' }, icon('back'), 'Voltar'),
    h('div', { class: 'row' },
      h('div', {},
        h('h1', {}, ev.name),
        h('div', { class: 'chips' },
          ev.host ? h('span', { class: 'chip' }, icon('user'), ev.host) : null,
          h('span', { class: 'chip' }, icon(ev.private ? 'lock' : 'globe'), ev.private ? 'Privado' : 'Público'),
          h('span', { class: 'chip' }, icon(ev.lan ? 'lan' : 'globe'), ev.lan ? 'LAN · Radmin VPN' : 'Internet'),
          live.online ? h('span', { class: 'chip' }, h('span', { class: 'dot on' }), `${live.clients}/${live.maxClients || '?'} jogando`) : null,
          live.online ? h('span', { class: 'chip' }, icon('signal'), `${live.pingMs} ms`) : null))));
  const body = h('div', { class: 'room-body' });
  if (ev.description) body.append(h('p', { style: 'color:var(--ink-2);margin:0 0 22px;max-width:820px;white-space:pre-line' }, ev.description));
  if (ev.locked) {
    body.append(lockedBox(ev));
    return h('div', {}, hero, body);
  }
  const st = S.statuses[ev.id] || { items: [], pending: 0, ready: false };
  const steps = h('div', { class: 'esteps' });
  let n = 0;
  if (ev.lan) steps.append(radminStep(ev, ++n));
  steps.append(filesStep(ev, st, live, ++n));
  steps.append(joinStep(ev, st, ++n));
  body.append(steps);
  return h('div', {}, hero, body);
}

function lockedBox(ev) {
  const pass = h('input', { type: 'text', id: 'evPass', placeholder: 'Senha do evento', autocomplete: 'off' });
  const btn = h('button', { class: 'btn primary' }, 'Desbloquear');
  const go = async () => {
    btn.disabled = true;
    try {
      await api('/api/unlock', { method: 'POST', body: { eventId: ev.id, password: pass.value } });
      toast('Evento desbloqueado.');
      await loadHub();
      loadLive();
    } catch (err) { toast(err.message, true); btn.disabled = false; }
  };
  btn.addEventListener('click', go);
  pass.addEventListener('keydown', (e) => { if (e.key === 'Enter') go(); });
  const img = (c) => (c.image && S.hub.filesUrl ? `${S.hub.filesUrl}/${encodeURIComponent(c.image)}` : '');
  return h('div', {},
    h('div', { class: 'estep' }, h('header', {}, h('span', { class: 'n' }, icon('lock')), h('h3', {}, 'Evento privado')),
      h('div', { class: 'lockbox' }, h('span', { style: 'color:var(--ink-2)' }, 'Peça a senha ao host. Com ela você vê o race invite e baixa os arquivos do servidor.'),
        h('div', { class: 'copy' }, pass, btn))),
    h('div', { class: 'section-title', style: 'margin-top:26px' }, h('h3', {}, 'Carros do evento')),
    h('div', { class: 'cards' }, ev.cars.map((c) => contentCard({ ...c, image: img(c) }, 'car', null))));
}

function radminStep(ev, n) {
  const r = ev.radmin || {};
  const kv = (label, value) => h('div', {}, h('div', { style: 'flex:1;min-width:0' }, h('small', {}, label), h('b', {}, value || '—')),
    value ? h('button', { class: 'btn sm ghost', 'aria-label': 'Copiar ' + label, onclick: () => copyText(value) }, icon('copy')) : null);
  return h('section', { class: 'estep' },
    h('header', {}, h('span', { class: 'n' }, String(n)), h('h3', {}, 'Entre na rede do host (Radmin VPN)')),
    h('p', { style: 'margin:0 0 14px;color:var(--ink-2)' }, 'Este servidor roda em LAN. Instale o Radmin VPN, clique em "Entrar em uma rede existente" e use os dados abaixo.'),
    h('button', { class: 'btn', onclick: () => api('/api/radmin', { method: 'POST', body: {} }).catch((e) => toast(e.message, true)) }, icon('download'), 'Baixar Radmin VPN'),
    h('div', { class: 'kv' }, kv('Nome da rede', r.network), kv('Senha da rede', r.password)));
}

function filesStep(ev, st, live, n) {
  const pend = st.items.filter((i) => i.state !== 'ok');
  const sec = h('section', { class: 'estep' + (st.ready ? ' done' : '') },
    h('header', {}, h('span', { class: 'n' }, st.ready ? icon('check') : String(n)), h('h3', {}, 'Arquivos do servidor')));
  const job = S.job;
  const jobHere = job && job.items.some((p) => st.items.some((i) => i.kind === p.kind && i.id === p.id));
  if (running() && jobHere) {
    sec.append(progressAlert(job));
  } else if (!st.ready) {
    const diff = pend.filter((i) => i.state === 'outdated').length;
    sec.append(h('div', { class: 'alert' }, h('div', { class: 'ico' }, icon('warn')),
      h('div', {}, h('b', {}, `${plural(pend.length, 'arquivo precisa', 'arquivos precisam')} ser baixado${pend.length > 1 ? 's' : ''}`),
        h('span', {}, diff ? `${plural(diff, 'mod que você tem é', 'mods que você tem são')} de outra versão — sem trocar, o servidor recusa a conexão.`
          : `Para entrar você precisa dos mesmos arquivos do servidor${st.bytes ? ' · ' + mb(st.bytes) : ''}.`)),
      h('div', { class: 'spacer' }),
      h('button', { class: 'btn white', disabled: !S.acValid || running(), onclick: () => install({ eventIds: [ev.id] }) }, icon('download'), 'Instalar conteúdo faltante')));
  } else {
    sec.append(h('div', { class: 'alert ok' }, h('div', { class: 'ico' }, icon('check')),
      h('div', {}, h('b', {}, 'Você tem os arquivos certos'), h('span', {}, 'Pista e carros iguais aos do servidor.'))));
  }
  const item = (kind, id) => st.items.find((i) => i.kind === kind && i.id === id) || { state: 'ok' };
  sec.append(
    h('div', { class: 'section-title' }, h('h3', {}, 'Carros'), h('span', { class: 'hint' }, 'Todos precisam estar instalados')),
    h('div', { class: 'cards' }, ev.cars.map((c) => contentCard({ ...c, image: item('car', c.id).image }, 'car', item('car', c.id), live.slots?.[c.id]))),
    h('div', { class: 'section-title', style: 'margin-top:24px' }, h('h3', {}, 'Pista')),
    h('div', { class: 'cards' }, contentCard({ ...ev.track, image: item('track', ev.track.id).image }, 'track', item('track', ev.track.id))));
  return sec;
}

function joinStep(ev, st, n) {
  return h('section', { class: 'estep' },
    h('header', {}, h('span', { class: 'n' }, String(n)), h('h3', {}, 'Entrar no servidor')),
    h('div', { style: 'display:flex;gap:14px;align-items:center;flex-wrap:wrap' },
      h('button', { class: 'btn lg ' + (st.ready ? 'primary' : ''), disabled: !st.ready, onclick: () => join(ev) }, icon('play'), 'Entrar pelo Content Manager'),
      h('span', { class: 'hint' }, st.ready ? 'Abre o race invite no Content Manager.' + (ev.lan ? ' Confira se você está na rede do Radmin.' : '') : 'Instale os arquivos primeiro.')));
}

function contentCard(entry, kind, it, slot) {
  const s = it ? STATE[it.state] || STATE.ok : null;
  const p = pic([entry.image, localImg(kind, entry.id)], kind === 'car' ? 'car' : 'road');
  if (slot) p.append(h('span', { class: 'corner slots' + (slot.free ? '' : ' full'), title: 'Vagas livres / total' }, `${slot.free}/${slot.total}`));
  const meta = [entry.version ? 'v' + entry.version : '', it && it.state !== 'ok' && entry.size ? mb(entry.size) : ''].filter(Boolean).join(' · ');
  return h('div', { class: 'ccard' + (it && it.state !== 'ok' ? ' need' : '') }, p,
    h('div', { class: 'info' },
      h('div', { class: 'name', title: entry.id }, entry.name || entry.id),
      h('div', { class: 'row' }, s ? h('span', { class: 'tag ' + s.cls, title: (it.problems || []).join(', ') }, s.label) : null, h('span', { class: 'meta' }, meta))));
}

function progressAlert(job) {
  const total = job.items.length;
  const done = job.items.filter((p) => p.phase === 'done').length;
  const bytes = job.items.reduce((n, p) => n + (p.phase === 'done' ? (p.total || 0) : p.downloaded || 0), 0);
  const all = job.items.reduce((n, p) => n + (p.total || p.size || 0), 0);
  const cur = job.items.find((p) => p.phase === 'downloading' || p.phase === 'extracting');
  return h('div', { class: 'alert' }, h('div', { class: 'ico' }, h('div', { class: 'spin' })),
    h('div', {}, h('b', {}, `Instalando ${Math.min(done + 1, total)} de ${total}`),
      h('span', {}, cur ? `${cur.phase === 'extracting' ? 'Instalando' : 'Baixando'} ${cur.name || cur.id}${all ? ` · ${mb(bytes)} de ${mb(all)}` : ''}` : 'Preparando…')),
    h('div', { class: 'spacer' }),
    h('button', { class: 'btn sm', onclick: () => api('/api/job/cancel', { method: 'POST', body: {} }) }, 'Cancelar'),
    h('div', { class: 'prog' }, h('i', { style: `width:${all ? (bytes / all) * 100 : (done / total) * 100}%` })));
}

// ---- my mods -------------------------------------------------------------------

function modsPage() {
  const page = h('div', { class: 'page' },
    h('div', { class: 'page-head' }, h('div', {}, h('h1', {}, 'Meus mods'),
      h('p', {}, 'O que está instalado no seu Assetto Corsa e o que os eventos abertos precisam.'))));
  const warn = setupWarning();
  if (warn) page.append(warn);
  if (!S.mods) { page.append(h('div', { class: 'empty' }, 'Lendo suas pastas…')); return page; }
  const items = S.mods.items || [];
  const missing = items.filter((m) => m.required && m.state !== 'ok');
  if (missing.length) {
    const size = missing.reduce((n, m) => n + (m.installed ? 0 : m.size || 0), 0);
    page.append(h('div', { class: 'alert' }, h('div', { class: 'ico' }, icon('warn')),
      h('div', {}, h('b', {}, `${plural(missing.length, 'item necessário', 'itens necessários')} para os eventos`), h('span', {}, `Instale para poder entrar em qualquer evento aberto${size ? ' · ' + mb(size) : ''}.`)),
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
  const labels = { all: 'Todos', required: 'Usados nos eventos', missing: 'Faltando', car: 'Carros', track: 'Pistas' };
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
  if (m.required) p.append(h('span', { class: 'corner-l tag accent', title: 'Usado em: ' + (m.events || []).join(', ') }, 'Usado em eventos'));
  const s = m.required ? STATE[m.state] : m.installed ? { label: 'Instalado', cls: '' } : null;
  const needs = m.required && m.state !== 'ok';
  const btn = needs
    ? h('button', { class: 'btn sm primary', disabled: running() || !S.acValid, onclick: () => install({ only: [m.kind + ':' + m.id] }) }, icon('download'), m.state === 'outdated' ? 'Trocar' : 'Baixar')
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
  const hub = h('input', { type: 'text', placeholder: 'https://seusite.com.br/ac/upload.php' });
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
      h('label', { class: 'field' }, 'Link da base de eventos', hub,
        h('span', { class: 'hint' }, st.hubUsed && !st.hubSource ? `Usando a base do seu painel: ${st.hubUsed}` : 'Quem administra a base passa este link.')),
      h('div', { style: 'display:flex;gap:8px;margin-top:18px' }, h('button', { class: 'btn primary', onclick: save }, 'Salvar'))),
    h('div', { class: 'panel' },
      h('h2', {}, 'Tem um servidor?'),
      h('p', { class: 'lead' }, 'Publique o race invite do seu servidor com os arquivos certos para os jogadores.'),
      h('a', { class: 'btn', href: '/admin' }, icon('admin'), 'Abrir painel do host')),
    h('p', { class: 'hint' }, `Launcher Brasil ${st.version || ''}`));
}

// ---- actions ----------------------------------------------------------------------

async function install(body) {
  try {
    S.job = await api('/api/install', { method: 'POST', body });
    render();
    pollJob();
  } catch (e) { S.autoJoin = null; toast(e.message, true); }
}

let jobTimer;
function pollJob() {
  clearTimeout(jobTimer);
  jobTimer = setTimeout(async () => {
    try { S.job = await api('/api/job'); } catch { return pollJob(); }
    if (running()) { refresh(); pollJob(); return; }
    const errs = S.job?.items.filter((p) => p.phase === 'error') || [];
    if (S.job?.status === 'done') toast('Arquivos instalados.');
    else if (errs.length) toast(`${errs[0].name || errs[0].id}: ${errs[0].error}`, true);
    await loadHub();
    if (route().page === 'mods') loadMods();
    const auto = S.autoJoin;
    S.autoJoin = null;
    if (auto && S.job?.status === 'done' && S.statuses[auto]?.ready) join(eventById(auto));
  }, 500);
}

function renderFloat() {
  const f = document.getElementById('float');
  if (!running() || route().page === 'evento') { f.hidden = true; return; }
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

async function join(ev) {
  if (!ev) return;
  try {
    await api('/api/join', { method: 'POST', body: { eventId: ev.id } });
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
