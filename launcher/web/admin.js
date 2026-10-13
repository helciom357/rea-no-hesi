// Host panel: connect to a base (host code, or owner setup), create events
// from a race invite, and — for the owner — manage hosts.
const A = { st: null, events: null, filesUrl: '', hosts: null, newCode: null, transfers: [] };
const main = document.getElementById('main');

const NAV = { eventos: ['calendar', 'Eventos'], base: ['admin', 'Base'], jogador: ['servers', 'Jogador'] };
for (const a of document.querySelectorAll('[data-nav]')) {
  const [ic, label] = NAV[a.dataset.nav];
  a.append(icon(ic), label);
}

function route() {
  const [, page = 'eventos', arg] = location.hash.split('/');
  for (const a of document.querySelectorAll('[data-nav]')) {
    if (a.dataset.nav === page || (page === 'evento' && a.dataset.nav === 'eventos')) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }
  return { page, arg: arg && decodeURIComponent(arg) };
}

function render() {
  const { page, arg } = route();
  const scroll = main.scrollTop;
  let view;
  if (!A.st) view = h('div', { class: 'page' }, h('div', { class: 'empty' }, 'Carregando…'));
  else if (!A.st.connected) view = setupPage();
  else if (page === 'evento') view = editorPage(arg);
  else if (page === 'base') view = basePage();
  else view = eventsPage();
  main.replaceChildren(view);
  main.scrollTop = scroll;
}
window.addEventListener('hashchange', () => {
  main.scrollTop = 0;
  if (route().page !== 'evento') ED = null;
  render();
  if (route().page === 'base' && A.st?.role === 'owner') loadHosts();
});

async function loadState() {
  try { A.st = await api('/api/admin/state'); } catch (e) { toast(e.message, true); }
}

async function loadEvents() {
  try {
    const r = await api('/api/admin/events');
    A.events = r.events || [];
    A.filesUrl = r.filesUrl || '';
  } catch (e) { A.events = []; toast(e.message, true); }
}

const fileUrl = (name) => (name && A.filesUrl ? `${A.filesUrl}/${encodeURIComponent(name)}` : '');
const libImg = (kind, id) => `/img/lib?kind=${kind}&id=${encodeURIComponent(id)}&t=${TOKEN}`;

// ---- first run: join a base ----------------------------------------------------------

function setupPage() {
  const code = h('textarea', { placeholder: 'LB1-…', rows: 3, class: 'code' });
  const join = async (e) => {
    const b = e.currentTarget;
    b.disabled = true;
    try {
      const id = await api('/api/admin/host-code', { method: 'POST', body: { code: code.value } });
      toast(`Conectado à base ${id.base} como ${id.name}.`);
      await boot();
    } catch (err) { toast(err.message, true); b.disabled = false; }
  };
  const url = h('input', { type: 'text', placeholder: 'https://seusite.com.br/ac/upload.php' });
  const connect = async (e) => {
    const b = e.currentTarget;
    b.disabled = true;
    try {
      await api('/api/admin/owner-connect', { method: 'POST', body: { url: url.value } });
      toast('Base conectada. Agora crie o primeiro evento ou gere códigos para outros hosts.');
      await boot();
    } catch (err) { toast(err.message, true); b.disabled = false; }
  };
  return h('div', { class: 'page', style: 'max-width:1100px' },
    h('div', { class: 'page-head' }, h('div', {}, h('h1', {}, 'Painel do host'),
      h('p', {}, 'Publique o race invite do seu servidor com os arquivos certos. Quem entrar pelo launcher baixa exatamente o que o seu servidor roda.'))),
    h('div', { class: 'choices' },
      h('section', { class: 'choice' },
        h('div', { class: 'big' }, icon('key')),
        h('h2', {}, 'Recebi um código de host'),
        h('p', {}, 'O dono da base te mandou um código que começa com LB1-. Cole aqui — é só uma vez.'),
        code,
        h('div', {}, h('button', { class: 'btn primary', onclick: join }, 'Entrar'))),
      h('section', { class: 'choice' },
        h('div', { class: 'big' }, icon('admin')),
        h('h2', {}, 'Sou o dono da base'),
        h('p', {}, 'Você hospeda a base num site com PHP (Hostinger, por exemplo). Feito uma vez.'),
        h('div', { class: 'steps' },
          h('div', { class: 'step' }, h('div', {}, h('b', {}, 'Baixe o arquivo da base'),
            h('p', {}, 'Ele guarda os eventos e os arquivos no seu site, com uma chave só sua.'),
            h('a', { class: 'btn', href: `/dl/upload.php?t=${TOKEN}` }, icon('download'), 'Baixar upload.php'))),
          h('div', { class: 'step' }, h('div', {}, h('b', {}, 'Envie para o site'),
            h('p', {}, 'Na Hostinger: Gerenciador de Arquivos → public_html → crie a pasta "ac" → envie o upload.php.'))),
          h('div', { class: 'step' }, h('div', {}, h('b', {}, 'Cole o endereço'),
            h('div', { class: 'copy' }, url, h('button', { class: 'btn primary', onclick: connect }, 'Conectar'))))))));
}

// ---- my events ---------------------------------------------------------------------

function eventsPage() {
  const page = h('div', { class: 'page' },
    h('div', { class: 'page-head' },
      h('div', {}, h('h1', {}, 'Meus eventos'), h('p', {}, `Publicados em ${A.st.baseName || 'sua base'} como ${A.st.hostName || 'host'}.`)),
      h('div', { class: 'spacer' }),
      h('a', { class: 'btn primary lg', href: '#/evento/novo' }, icon('plus'), 'Novo evento')));
  if (!A.events) { page.append(h('div', { class: 'empty' }, 'Carregando eventos…')); return page; }
  if (!A.events.length) {
    page.append(h('div', { class: 'empty' }, h('b', {}, 'Nenhum evento ainda'),
      'Crie um evento com o race invite do seu servidor. A pista e os carros são lidos do servidor.'));
    return page;
  }
  page.append(h('div', {}, A.events.map((ev) => h('div', { class: 'list-room' },
    h('div', { class: 'thumb' }, ev.track?.image ? h('img', { alt: '', src: fileUrl(ev.track.image) }) : null),
    h('div', {},
      h('h3', {}, ev.name),
      h('div', { class: 'hint', style: 'margin-bottom:8px' }, [ev.track?.name || ev.track?.id, plural(ev.cars?.length || 0, 'carro', 'carros'),
        A.st.role === 'owner' && ev.host ? 'por ' + ev.host : '', ev.updatedAt ? 'atualizado ' + ago(ev.updatedAt) : ''].filter(Boolean).join(' · ')),
      h('div', { class: 'chips' },
        h('span', { class: 'tag ' + (ev.private ? 'warn' : 'ok') }, icon(ev.private ? 'lock' : 'globe'), ev.private ? 'Privado' : 'Público'),
        ev.lan ? h('span', { class: 'tag' }, icon('lan'), 'LAN · Radmin') : h('span', { class: 'tag' }, icon('globe'), 'Internet'))),
    h('div', { style: 'display:flex;gap:8px' },
      h('a', { class: 'btn', href: '#/evento/' + encodeURIComponent(ev.id) }, 'Editar'),
      h('button', { class: 'btn ghost danger', 'aria-label': 'Apagar ' + ev.name, onclick: () => deleteEvent(ev) }, icon('trash')))))));
  return page;
}

async function deleteEvent(ev) {
  if (!confirm(`Apagar o evento "${ev.name}"? Ele some da home dos jogadores.`)) return false;
  try {
    await api('/api/admin/event-delete', { method: 'POST', body: { id: ev.id } });
    toast('Evento apagado.');
    await loadEvents();
    location.hash = '#/eventos';
    render();
    return true;
  } catch (e) { toast(e.message, true); return false; }
}

// ---- event editor --------------------------------------------------------------------

let ED = null; // { d: draft, status, probe, probeErr, task }

function openDraft(arg) {
  if (ED && ED.arg === arg) return ED;
  let d = { id: '', name: '', description: '', private: false, password: '', lan: false, radminNetwork: '', radminPassword: '', invite: '', track: '', layout: '', cars: [] };
  if (arg && arg !== 'novo') {
    const ev = (A.events || []).find((e) => e.id === arg);
    if (ev) d = { ...d, ...ev.draft, password: '' };
  }
  ED = { arg, d, status: null, probe: null, probeErr: '', taskId: null, task: null };
  validate();
  return ED;
}

let vTimer;
function validate(now = false) {
  clearTimeout(vTimer);
  const run = async () => {
    if (!ED) return;
    const ed = ED;
    try { ed.status = await api('/api/admin/draft', { method: 'POST', body: ed.d }); } catch (e) { toast(e.message, true); }
    if (ed === ED) refreshEditor();
  };
  if (now) run(); else vTimer = setTimeout(run, 350);
}

// Only the parts that depend on validation are redrawn, so typing is never interrupted.
function refreshEditor() {
  for (const id of ['secTrack', 'secCars', 'pubbar']) {
    const el = document.getElementById(id);
    if (el) el.replaceWith(({ secTrack: trackSection, secCars: carsSection, pubbar: publishBar })[id]());
  }
  for (const n of [1, 2, 3]) document.getElementById('sec' + n)?.classList.toggle('done', secDone(n));
}

function secDone(n) {
  const d = ED.d;
  if (n === 1) return !!d.name.trim() && (!d.private || !!d.password || !!d.hasPassword);
  if (n === 2) return !d.lan || !!d.radminNetwork.trim();
  if (n === 3) return !!ED.probe || (!!d.invite && !(ED.status?.problems || []).some((p) => /invite|porta|endereço/i.test(p)));
  return false;
}

function input(obj, key, attrs = {}) {
  const el = h(attrs.textarea ? 'textarea' : 'input', { type: attrs.type || 'text', placeholder: attrs.placeholder || '', rows: attrs.textarea ? 2 : undefined, autocomplete: 'off',
    oninput: (e) => { obj[key] = e.target.value; validate(); for (const n of [1, 2, 3]) document.getElementById('sec' + n)?.classList.toggle('done', secDone(n)); } });
  el.value = obj[key] || '';
  return el;
}

function toggle(options, value, onChange) {
  const wrap = h('div', { class: 'toggle', role: 'group' });
  for (const [val, label, ic] of options) {
    wrap.append(h('button', { type: 'button', 'aria-pressed': String(val === value), onclick: () => onChange(val) }, ic ? icon(ic) : null, label));
  }
  return wrap;
}

function editorPage(arg) {
  const ed = openDraft(arg);
  const d = ed.d;
  const isNew = !d.id;

  const sec = (n, title, lead, ...body) => h('section', { class: 'fsec' + (n <= 3 && secDone(n) ? ' done' : ''), id: 'sec' + n },
    h('div', { class: 'n' }, String(n)), h('div', {}, h('h2', {}, title), lead ? h('p', { class: 'lead' }, lead) : null, h('div', { class: 'body' }, ...body)));

  // 1. Event
  const pwField = d.private
    ? h('label', { class: 'field' }, 'Senha do evento', input(d, 'password', { placeholder: d.hasPassword ? 'Deixe em branco para manter a senha atual' : 'Os jogadores digitam esta senha para ver o evento' }))
    : null;
  const s1 = sec(1, 'Evento', 'Como o evento aparece na home dos jogadores.',
    h('label', { class: 'field' }, 'Nome do evento', input(d, 'name', { placeholder: 'Ex.: Racha de Opala na Bandeirantes' })),
    h('label', { class: 'field' }, 'Descrição', input(d, 'description', { textarea: true, placeholder: 'Horário, regras, o que for importante (opcional)' })),
    h('div', { style: 'display:flex;gap:14px;align-items:flex-end;flex-wrap:wrap' },
      h('div', { class: 'field', style: 'display:grid;gap:6px;color:var(--ink-2);font-size:13px;font-weight:600' }, 'Visibilidade',
        toggle([[false, 'Público', 'globe'], [true, 'Privado', 'lock']], d.private, (v) => { d.private = v; validate(); render(); })),
      pwField ? h('div', { style: 'flex:1;min-width:260px' }, pwField) : null),
    d.private ? h('span', { class: 'hint' }, 'Privado aparece na home com cadeado; o race invite e os arquivos só abrem com a senha.') : null);

  // 2. Connection
  const s2 = sec(2, 'Conexão', 'Como os jogadores chegam até o seu servidor.',
    toggle([[false, 'Internet', 'globe'], [true, 'LAN (Radmin VPN)', 'lan']], d.lan, (v) => { d.lan = v; validate(); render(); }),
    d.lan ? h('div', { class: 'grid2' },
      h('label', { class: 'field' }, 'Nome da rede no Radmin', input(d, 'radminNetwork', { placeholder: 'Ex.: RachaBR' })),
      h('label', { class: 'field' }, 'Senha da rede', input(d, 'radminPassword', { placeholder: 'Senha para entrar na rede' }))) : null,
    d.lan ? h('span', { class: 'hint' }, 'Os jogadores verão o botão para baixar o Radmin VPN, o nome e a senha da rede. No race invite use o IP do Radmin (26.x.x.x).') : null);

  // 3. Race invite
  const inv = input(d, 'invite', { placeholder: 'acmanager://race/online/join?ip=…&httpPort=…   ou   IP:porta HTTP' });
  const readBtn = h('button', { class: 'btn primary', onclick: () => probe(readBtn) }, icon('refresh'), 'Ler servidor');
  inv.addEventListener('keydown', (e) => { if (e.key === 'Enter') probe(readBtn); });
  const s3 = sec(3, 'Race invite', 'O link do seu servidor no Content Manager. O launcher lê dele a pista e os carros.',
    h('div', { class: 'copy' }, inv, readBtn),
    probeLine());

  return h('div', { class: 'page form' },
    h('div', { class: 'page-head' }, h('a', { class: 'btn sm back', href: '#/eventos' }, icon('back'), 'Eventos'),
      h('h1', { style: 'flex:1' }, isNew ? 'Novo evento' : 'Editar evento'),
      isNew ? null : h('button', { class: 'btn ghost danger', onclick: () => deleteEvent({ id: d.id, name: d.name }) }, icon('trash'), 'Apagar')),
    s1, s2, s3, trackSection(), carsSection(), publishBar());
}

function probeLine() {
  if (ED.probeErr) return h('div', { class: 'probe bad', id: 'probe' }, icon('warn'), ED.probeErr);
  const p = ED.probe;
  if (!p) return h('div', { class: 'hint', id: 'probe' }, 'Com o servidor ligado, clique em Ler servidor. Se ele não estiver acessível deste PC, escolha a pista e os carros abaixo.');
  return h('div', { class: 'probe ok', id: 'probe' }, h('span', { style: 'color:var(--ok)' }, icon('check')),
    h('b', {}, p.name || 'Servidor'), h('span', { class: 'hint' }, [p.track + (p.layout ? ' / ' + p.layout : ''), plural(p.cars.length, 'carro', 'carros'), `${p.clients}/${p.maxClients} online`].join(' · ')));
}

async function probe(btn) {
  btn.disabled = true;
  ED.probeErr = '';
  try {
    const p = await api('/api/admin/probe', { method: 'POST', body: { invite: ED.d.invite } });
    ED.probe = p;
    ED.d.track = p.track;
    ED.d.layout = p.layout;
    ED.d.cars = p.cars;
  } catch (e) { ED.probe = null; ED.probeErr = e.message; }
  btn.disabled = false;
  document.getElementById('probe')?.replaceWith(probeLine());
  document.getElementById('sec3')?.classList.toggle('done', secDone(3));
  validate(true);
}

// One row per track/car: ready, available in the host's Assetto, or missing.
function contentRow(it, onRemove) {
  const imgs = [it.inLibrary && it.image ? libImg(it.kind, it.id) : null, it.installed ? localImg(it.kind, it.id) : null];
  const p = pic(imgs, it.kind === 'car' ? 'car' : 'road');
  let tag, action = null;
  if (it.inLibrary && !it.differs) {
    tag = h('span', { class: 'tag ok' }, icon('check'), it.uploaded ? 'Pronto' : 'Pronto · sobe ao publicar');
  } else if (it.inLibrary && it.differs) {
    tag = h('span', { class: 'tag warn', title: 'O pacote preparado não é igual ao que está no seu Assetto agora.' }, 'Seu Assetto tem outra versão');
    action = h('button', { class: 'btn sm', onclick: () => useMine([it]) }, 'Usar a do meu Assetto');
  } else if (it.installed) {
    tag = h('span', { class: 'tag warn' }, 'Falta preparar');
    action = h('button', { class: 'btn sm primary', onclick: () => useMine([it]) }, 'Usar do meu Assetto');
  } else {
    tag = h('span', { class: 'tag bad' }, 'Arraste o .zip');
  }
  const busy = A.st.tasks?.some((t) => t.kind === 'import' && (t.state === 'running' || t.state === 'waiting') && t.label === 'Preparando ' + it.id);
  if (busy) { tag = h('span', { class: 'tag' }, h('span', { class: 'spin', style: 'width:12px;height:12px' }), 'Preparando…'); action = null; }
  return h('div', { class: 'crow' }, p,
    h('div', { style: 'min-width:0' }, h('div', { class: 'nm', title: it.id }, it.name || it.id),
      h('div', { class: 'sub' }, tag, h('span', {}, [it.id, it.version ? 'v' + it.version : '', it.size ? mb(it.size) : ''].filter(Boolean).join(' · ')))),
    h('div', { class: 'acts' }, action, onRemove ? h('button', { class: 'btn sm ghost', 'aria-label': 'Remover ' + (it.name || it.id), onclick: onRemove }, icon('x')) : null));
}

function trackSection() {
  const st = ED.status;
  const d = ED.d;
  let body;
  if (!d.track) {
    body = [h('div', { class: 'hint' }, 'Leia o servidor no passo 3 ou escolha a pista:'), pickerRow('track')];
  } else {
    const t = st?.track && st.track.id === d.track ? st.track : { kind: 'track', id: d.track, name: d.track };
    const layouts = t.layouts || [];
    body = [contentRow(t, () => { d.track = ''; d.layout = ''; validate(true); refreshEditor(); })];
    if (layouts.length > 1 || (d.layout && !layouts.includes(d.layout) && layouts.length)) {
      const sel = h('select', { onchange: (e) => { d.layout = e.target.value; validate(); } }, h('option', { value: '' }, 'Escolha o traçado…'),
        layouts.map((l) => h('option', { value: l, selected: l === d.layout }, l)));
      body.push(h('label', { class: 'field', style: 'max-width:320px' }, 'Traçado', sel));
    } else if (d.layout) {
      body.push(h('span', { class: 'hint' }, 'Traçado: ' + d.layout));
    }
  }
  return h('section', { class: 'fsec' + (st?.track?.inLibrary && !st.track.differs ? ' done' : ''), id: 'secTrack' },
    h('div', { class: 'n' }, '4'), h('div', {}, h('h2', {}, 'Pista'), h('div', { class: 'body' }, ...body)));
}

function carsSection() {
  const st = ED.status;
  const d = ED.d;
  const rows = (st?.cars || d.cars.map((id) => ({ kind: 'car', id, name: id }))).filter((c) => d.cars.includes(c.id));
  const items = [...rows, ...(st?.track ? [st.track] : [])];
  const fromMine = items.filter((i) => i.installed && (!i.inLibrary || i.differs));
  const missing = items.filter((i) => !i.inLibrary && !i.installed);
  const bulk = fromMine.length || missing.length ? h('div', { class: 'bulk' },
    fromMine.length ? h('button', { class: 'btn primary', onclick: () => useMine(fromMine) }, icon('folder'), `Usar os arquivos do meu Assetto (${fromMine.length})`) : null,
    h('span', {}, missing.length ? `${plural(missing.length, 'item não está', 'itens não estão')} no seu Assetto — arraste os .zip para esta janela.` : 'São os mesmos arquivos que o seu servidor usa.')) : null;
  const done = rows.length > 0 && rows.every((c) => c.inLibrary && !c.differs);
  return h('section', { class: 'fsec' + (done ? ' done' : ''), id: 'secCars' },
    h('div', { class: 'n' }, '5'),
    h('div', {}, h('h2', {}, 'Carros'),
      h('p', { class: 'lead' }, rows.length ? `${plural(rows.length, 'carro', 'carros')} da entry list do servidor.` : 'Leia o servidor no passo 3 ou adicione os carros.'),
      h('div', { class: 'body' }, bulk,
        h('div', { class: 'crows' }, rows.map((c) => contentRow(c, () => { d.cars = d.cars.filter((x) => x !== c.id); validate(true); refreshEditor(); }))),
        pickerRow('car'))));
}

// pickerRow adds a track or car by name from what this PC knows.
function pickerRow(kind) {
  const listId = 'known-' + kind;
  const inp = h('input', { type: 'search', list: listId, placeholder: kind === 'car' ? 'Adicionar carro pelo nome da pasta…' : 'Nome da pasta da pista…' });
  const dl = h('datalist', { id: listId });
  const known = new Map();
  for (const it of A.st.library || []) if (it.kind === kind) known.set(it.id, it.name);
  api('/api/admin/installed?kind=' + kind).then((r) => {
    for (const it of r.items || []) if (!known.has(it.id)) known.set(it.id, it.name || it.id);
    dl.replaceChildren(...[...known].map(([id, name]) => h('option', { value: id }, name)));
  }).catch(() => {});
  const add = () => {
    const id = inp.value.trim();
    if (!id) return;
    if (kind === 'car') { if (!ED.d.cars.includes(id)) ED.d.cars.push(id); } else { ED.d.track = id; ED.d.layout = ''; }
    inp.value = '';
    validate(true);
  };
  inp.addEventListener('keydown', (e) => { if (e.key === 'Enter') add(); });
  return h('div', { class: 'copy', style: 'max-width:560px' }, inp, dl, h('button', { class: 'btn', onclick: add }, icon('plus'), 'Adicionar'));
}

async function useMine(items) {
  try {
    await api('/api/admin/installed/add', { method: 'POST', body: { items: items.map((i) => ({ kind: i.kind, id: i.id })) } });
    await loadState();
    refreshEditor();
    watch();
  } catch (e) { toast(e.message, true); }
}

function publishBar() {
  const st = ED?.status;
  const problems = st?.problems || [];
  const task = ED?.taskId ? A.st.tasks?.find((t) => t.id === ED.taskId) : null;
  const running = task && (task.state === 'running' || task.state === 'waiting');
  let msg;
  if (running) msg = h('span', { class: 'msg' }, h('span', { class: 'spin', style: 'display:inline-block;vertical-align:-3px;margin-right:8px' }), task.detail || 'Publicando…');
  else if (task?.state === 'error') msg = h('span', { class: 'msg', style: 'color:var(--bad)' }, task.error);
  else if (!st) msg = h('span', { class: 'msg' }, 'Conferindo…');
  else if (problems.length) msg = h('span', { class: 'msg' }, h('b', {}, 'Falta: '), problems.slice(0, 3).join(' · ') + (problems.length > 3 ? ` (+${problems.length - 3})` : ''));
  else msg = h('span', { class: 'msg' }, 'Tudo certo. Ao publicar, os arquivos sobem para a base e o evento aparece para os jogadores.');
  const pct = running && task.total ? (task.done / task.total) * 100 : null;
  return h('div', { class: 'publishbar', id: 'pubbar' }, msg,
    h('button', { class: 'btn primary lg', disabled: !st || problems.length > 0 || running, onclick: publish }, icon('upload'), ED?.d.id ? 'Publicar alterações' : 'Publicar evento'),
    pct !== null ? h('div', { class: 'prog' }, h('i', { style: `width:${pct}%` })) : null);
}

async function publish() {
  try {
    const t = await api('/api/admin/publish', { method: 'POST', body: ED.d });
    ED.taskId = t.id;
    await loadState();
    refreshEditor();
    watch();
  } catch (e) { toast(e.message, true); }
}

// watch polls while imports or a publication are running.
let wTimer;
function watch() {
  clearTimeout(wTimer);
  wTimer = setTimeout(async () => {
    const before = A.st;
    await loadState();
    const t = ED?.taskId ? A.st.tasks.find((x) => x.id === ED.taskId) : null;
    if (t && t.state === 'done') {
      toast('Evento publicado. Já aparece na home dos jogadores.');
      ED = null;
      await loadEvents();
      location.hash = '#/eventos';
      render();
      return;
    }
    if (ED) {
      const imported = (A.st.library || []).length !== (before?.library || []).length || A.st.busy !== before?.busy;
      if (imported) validate(true); else refreshEditor();
    }
    showTransfers();
    if (A.st.busy || A.transfers.some((x) => !x.done)) watch();
    else if (ED) validate(true);
  }, 700);
}

// ---- drag & drop zips ------------------------------------------------------------------

function showTransfers() {
  let box = document.getElementById('transfers');
  const rows = [];
  for (const t of A.transfers) {
    if (t.error) rows.push(h('div', { class: 'act' }, h('span', { style: 'color:var(--bad)' }, '✕'), h('div', {}, h('div', { class: 't' }, t.name), h('div', { class: 'err' }, t.error)),
      h('button', { class: 'btn sm ghost', onclick: () => { A.transfers = A.transfers.filter((x) => x !== t); showTransfers(); } }, 'OK')));
    else if (!t.done) rows.push(h('div', { class: 'act' }, h('div', { class: 'spin' }), h('div', {}, h('div', { class: 't' }, 'Recebendo ' + t.name), h('div', { class: 'd' }, `${mb(t.loaded)} de ${mb(t.total)}`)), h('span'),
      h('div', { class: 'prog' }, h('i', { style: `width:${t.total ? (t.loaded / t.total) * 100 : 0}%` }))));
  }
  for (const t of (A.st?.tasks || []).filter((t) => t.kind === 'import' && (t.state === 'running' || t.state === 'waiting' || (t.state === 'error' && Date.now() / 1000 - t.at < 300)))) {
    rows.push(h('div', { class: 'act' }, t.state === 'error' ? h('span', { style: 'color:var(--bad)' }, '✕') : h('div', { class: 'spin' }),
      h('div', {}, h('div', { class: 't' }, t.label), t.error ? h('div', { class: 'err' }, t.error) : h('div', { class: 'd' }, t.state === 'waiting' ? 'na fila' : 'separando carros e pistas…')), h('span')));
  }
  if (!box) {
    box = h('div', { id: 'transfers', class: 'activity', style: 'position:fixed;right:20px;bottom:90px;width:360px;z-index:40;margin:0' });
    document.body.append(box);
  }
  box.replaceChildren(...rows);
}

function sendFiles(files) {
  for (const f of files) {
    if (!/\.zip$/i.test(f.name)) {
      toast(`${f.name}: só .zip. Para .7z ou .rar, instale no seu Assetto e use "Usar do meu Assetto".`, true);
      continue;
    }
    const t = { name: f.name, loaded: 0, total: f.size, done: false, error: '' };
    A.transfers.push(t);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/admin/drop');
    xhr.setRequestHeader('X-Token', TOKEN);
    xhr.setRequestHeader('X-Filename', encodeURIComponent(f.name));
    xhr.upload.onprogress = (e) => { t.loaded = e.loaded; t.total = e.total; showTransfers(); };
    xhr.onload = () => {
      t.done = true;
      if (xhr.status !== 200) { try { t.error = JSON.parse(xhr.responseText).error; } catch { t.error = 'Falha ao receber o arquivo'; } }
      else A.transfers = A.transfers.filter((x) => x !== t);
      showTransfers();
      watch();
    };
    xhr.onerror = () => { t.done = true; t.error = 'Falha ao receber o arquivo'; showTransfers(); };
    xhr.send(f);
  }
  showTransfers();
  watch();
}

const overlay = document.getElementById('overlay');
let dragDepth = 0;
window.addEventListener('dragenter', (e) => { if (A.st?.connected && e.dataTransfer?.types?.includes('Files')) { dragDepth++; overlay.classList.add('show'); } });
window.addEventListener('dragleave', () => { if (--dragDepth <= 0) { dragDepth = 0; overlay.classList.remove('show'); } });
window.addEventListener('dragover', (e) => e.preventDefault());
window.addEventListener('drop', (e) => {
  e.preventDefault();
  dragDepth = 0;
  overlay.classList.remove('show');
  if (A.st?.connected && e.dataTransfer?.files?.length) sendFiles(e.dataTransfer.files);
});
document.getElementById('filePick').addEventListener('change', (e) => { sendFiles(e.target.files); e.target.value = ''; });

// ---- base (owner) / account (host) ---------------------------------------------------------

async function loadHosts() {
  try { A.hosts = (await api('/api/admin/hosts')).hosts || []; } catch (e) { toast(e.message, true); A.hosts = []; }
  if (route().page === 'base') render();
}

function basePage() {
  const st = A.st;
  const link = h('div', { class: 'copy' }, h('input', { type: 'text', readonly: true, value: st.hubUrl }),
    h('button', { class: 'btn', onclick: () => copyText(st.hubUrl) }, icon('copy'), 'Copiar'));
  const page = h('div', { class: 'page', style: 'max-width:900px' },
    h('div', { class: 'page-head' }, h('div', {}, h('h1', {}, st.role === 'owner' ? 'Base' : 'Conta'),
      h('p', {}, st.role === 'owner' ? `Você é o dono de ${st.baseName || 'sua base'}.` : `Você publica em ${st.baseName || 'uma base'} como ${st.hostName}.`))),
    h('div', { class: 'panel' }, h('h2', {}, 'Link para os jogadores'),
      h('p', { class: 'lead' }, 'Eles colam em Ajustes → Link da base no launcher. Todos os eventos da base aparecem por ele.'), link));
  if (st.role === 'owner') {
    const nameIn = h('input', { type: 'text', value: st.baseName || '', placeholder: 'Ex.: Rodovias BR' });
    const descIn = h('input', { type: 'text', placeholder: 'Uma linha sobre a base (opcional)' });
    page.append(h('div', { class: 'panel' }, h('h2', {}, 'Nome da base'), h('p', { class: 'lead' }, 'Aparece no topo da home dos jogadores.'),
      h('div', { class: 'grid2' }, h('label', { class: 'field' }, 'Nome', nameIn), h('label', { class: 'field' }, 'Descrição', descIn)),
      h('div', { style: 'margin-top:14px' }, h('button', {
        class: 'btn', onclick: async () => {
          try { await api('/api/admin/base', { method: 'POST', body: { name: nameIn.value.trim(), description: descIn.value.trim() } }); await loadState(); toast('Salvo.'); render(); } catch (e) { toast(e.message, true); }
        },
      }, 'Salvar'))));
    page.append(hostsPanel());
  }
  page.append(h('div', { class: 'panel' }, h('h2', {}, 'Desconectar este PC'),
    h('p', { class: 'lead' }, st.role === 'owner' ? 'Para reconectar você precisa do mesmo upload.php. Os eventos continuam no site.' : 'Para voltar, cole o código de host de novo. Seus eventos continuam publicados.'),
    h('button', { class: 'btn danger', onclick: async () => { if (!confirm('Desconectar este PC da base?')) return; await api('/api/admin/disconnect', { method: 'POST', body: {} }); await boot(); } }, 'Desconectar')));
  return page;
}

function hostsPanel() {
  const nameIn = h('input', { type: 'text', placeholder: 'Nome do host (ex.: Zé do Opala)' });
  const add = async () => {
    const name = nameIn.value.trim();
    if (!name) { nameIn.focus(); return; }
    try {
      const r = await api('/api/admin/hosts/add', { method: 'POST', body: { name } });
      A.newCode = { name, code: r.code };
      await loadHosts();
    } catch (e) { toast(e.message, true); }
  };
  nameIn.addEventListener('keydown', (e) => { if (e.key === 'Enter') add(); });
  const codeBox = A.newCode ? h('div', { class: 'alert ok', style: 'margin:14px 0 0' }, h('div', { class: 'ico' }, icon('key')),
    h('div', { style: 'flex:1;min-width:0' }, h('b', {}, `Código de ${A.newCode.name}`), h('span', { class: 'code' }, A.newCode.code),
      h('div', { class: 'hint', style: 'margin-top:6px' }, 'Mande para o host. Ele cola no painel do host. Este código só aparece agora.')),
    h('button', { class: 'btn white', onclick: () => copyText(A.newCode.code) }, icon('copy'), 'Copiar')) : null;
  const list = A.hosts === null ? h('div', { class: 'hint' }, 'Carregando…')
    : !A.hosts.length ? h('div', { class: 'hint' }, 'Nenhum host ainda. Você também pode criar eventos como dono.')
      : h('div', { class: 'crows' }, A.hosts.map((x) => h('div', { class: 'crow', style: 'grid-template-columns:40px 1fr auto' },
        h('div', { class: 'big', style: 'width:36px;height:36px;border-radius:10px;display:grid;place-items:center;background:var(--surface-3)' }, icon('user')),
        h('div', {}, h('div', { class: 'nm' }, x.name), h('div', { class: 'sub' }, `${plural(x.events, 'evento', 'eventos')} · desde ${new Date(x.createdAt).toLocaleDateString('pt-BR')}`)),
        h('button', {
          class: 'btn sm ghost danger', onclick: async () => {
            if (!confirm(`Remover ${x.name}? Os eventos dele saem da base e o código para de funcionar.`)) return;
            try { await api('/api/admin/hosts/remove', { method: 'POST', body: { id: x.id } }); await loadHosts(); } catch (e) { toast(e.message, true); }
          },
        }, 'Remover'))));
  return h('div', { class: 'panel' }, h('h2', {}, 'Hosts'),
    h('p', { class: 'lead' }, 'Quem tem servidor e vai criar eventos na sua base. Cada host recebe um código próprio.'),
    h('div', { class: 'copy', style: 'max-width:560px' }, nameIn, h('button', { class: 'btn primary', onclick: add }, icon('plus'), 'Gerar código')),
    codeBox, h('div', { style: 'margin-top:14px' }, list));
}

// ---- start --------------------------------------------------------------------------------

async function boot() {
  await loadState();
  if (A.st?.connected) await loadEvents();
  if (!location.hash) history.replaceState(null, '', '#/eventos');
  render();
  if (route().page === 'base' && A.st?.role === 'owner') loadHosts();
}
boot();
