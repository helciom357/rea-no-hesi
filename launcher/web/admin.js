// Admin app: drop zips to build the content library, create rooms, connect
// the website. Everything is published automatically.
const A = { loaded: false, name: '', description: '', rooms: [], library: [], connection: {}, tasks: [], busy: false, publishedAt: '', publishError: '', acValid: false, transfers: [], libKind: 'all', libQuery: '' };
const main = document.getElementById('main');
const dlg = document.getElementById('dlg');

const NAV = { conteudo: ['upload', 'Conteúdo'], salas: ['rooms', 'Salas'], conexao: ['link', 'Conexão'], jogador: ['servers', 'Jogador'] };
for (const a of document.querySelectorAll('[data-nav]')) {
  const [ic, label] = NAV[a.dataset.nav];
  a.append(icon(ic), label);
}

function page() {
  const p = location.hash.split('/')[1] || (A.connection.configured ? 'conteudo' : 'conexao');
  for (const a of document.querySelectorAll('[data-nav]')) {
    if (a.dataset.nav === p) a.setAttribute('aria-current', 'page'); else a.removeAttribute('aria-current');
  }
  return p;
}

function render() {
  const p = page();
  const scroll = main.scrollTop;
  main.replaceChildren(p === 'salas' ? roomsPage() : p === 'conexao' ? connectionPage() : contentPage());
  main.scrollTop = scroll;
}
window.addEventListener('hashchange', () => { main.scrollTop = 0; render(); });

async function refresh(full = true) {
  try {
    const s = await api('/api/admin/state');
    Object.assign(A, s, { loaded: true });
    A.library ||= []; A.rooms ||= []; A.tasks ||= [];
  } catch (e) { toast(e.message, true); return; }
  // Re-render only the live parts while the admin is typing.
  const typing = main.contains(document.activeElement) && /INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName);
  if (full && !typing) render();
  else { updateLive(); }
}

// One polling loop: fast while something is moving, slow otherwise.
let pollTimer;
function schedule() {
  clearTimeout(pollTimer);
  const active = A.busy || A.transfers.some((t) => !t.done);
  pollTimer = setTimeout(async () => { await refresh(); schedule(); }, active ? 1000 : 6000);
}

// ---- publish status --------------------------------------------------------------

function statusPill() {
  let dot = 'on', text;
  if (!A.connection.configured) { dot = 'warn'; text = 'Site não conectado'; }
  else if (A.busy) { dot = 'warn'; text = 'Enviando…'; }
  else if (A.publishError) { dot = 'bad'; text = 'Erro ao publicar'; }
  else if (A.publishedAt) text = `Publicado ${ago(A.publishedAt)}`;
  else { dot = ''; text = 'Nada publicado ainda'; }
  return h('span', { class: 'pill', id: 'statusPill', title: A.publishError || '' }, h('span', { class: 'dot ' + dot }), text);
}

function head(title, lead, ...extra) {
  return h('div', { class: 'page-head' }, h('div', {}, h('h1', {}, title), lead ? h('p', {}, lead) : null),
    h('div', { class: 'spacer' }), statusPill(), ...extra);
}

function updateLive() {
  document.getElementById('statusPill')?.replaceWith(statusPill());
  const act = document.getElementById('activity');
  if (act) act.replaceWith(activity());
  const lib = document.getElementById('lib');
  if (lib) lib.replaceWith(libraryGrid());
}

// ---- content ----------------------------------------------------------------------

function contentPage() {
  const drop = h('div', { class: 'drop' + (A.library.length ? ' compact' : ''), id: 'drop', role: 'button', tabindex: '0', onclick: () => document.getElementById('filePick').click(), onkeydown: (e) => { if (e.key === 'Enter') document.getElementById('filePick').click(); } },
    icon('upload'),
    h('div', {}, h('h2', {}, 'Arraste os .zip de carros e pistas aqui'),
      h('p', {}, 'Packs com vários carros são separados sozinhos. Se o mod já existe na base, vira uma atualização e os jogadores recebem a nova versão.')),
    h('span', { class: 'btn sm', style: 'margin-top:6px' }, 'Escolher arquivos'));
  drop.addEventListener('dragover', (e) => { e.preventDefault(); drop.classList.add('over'); });
  drop.addEventListener('dragleave', () => drop.classList.remove('over'));
  const seg = h('div', { class: 'seg' }, [['all', 'Tudo'], ['track', 'Pistas'], ['car', 'Carros']].map(([k, label]) => h('button', {
    'aria-pressed': String(A.libKind === k), onclick: () => { A.libKind = k; render(); },
  }, label, h('b', {}, k === 'all' ? A.library.length : A.library.filter((i) => i.kind === k).length))));
  const q = h('input', { type: 'search', placeholder: 'Buscar…', oninput: (e) => { A.libQuery = e.target.value.toLowerCase(); document.getElementById('lib').replaceWith(libraryGrid()); } });
  q.value = A.libQuery;
  return h('div', { class: 'page' },
    head('Conteúdo', 'Tudo que os jogadores podem baixar. O envio para o site e a publicação são automáticos.',
      h('button', { class: 'btn', onclick: openInstalled }, icon('folder'), 'Adicionar do meu Assetto')),
    A.connection.configured ? null : h('div', { class: 'alert' }, h('div', { class: 'ico' }, icon('link')),
      h('div', {}, h('b', {}, 'Conecte o seu site'), h('span', {}, 'Você já pode adicionar conteúdo; ele sobe para a base assim que o site estiver conectado.')),
      h('div', { class: 'spacer' }), h('a', { class: 'btn white', href: '#/conexao' }, 'Conectar')),
    drop,
    activity(),
    h('div', { style: 'display:flex;gap:12px;flex-wrap:wrap;margin-bottom:16px;align-items:center' },
      h('h3', { style: 'margin:0;font-size:16px' }, 'Na base'), seg, h('label', { class: 'search' }, icon('search'), q)),
    libraryGrid());
}

// Activity shows only what needs attention: what is moving now and errors.
// Finished work is reflected on the library cards and the status pill.
function activity() {
  const rows = [];
  const row = (ic, title, sub, right, extra) => h('div', { class: 'act' }, ic, h('div', {}, h('div', { class: 't' }, title), sub ? h('div', { class: 'd' }, sub) : null), right || h('span'), extra || null);
  const errIcon = () => h('span', { style: 'color:var(--bad)' }, '✕');
  for (const t of A.transfers) {
    if (t.error) rows.push(row(errIcon(), t.name, null, h('button', { class: 'btn sm ghost', onclick: () => { A.transfers = A.transfers.filter((x) => x !== t); updateLive(); } }, 'OK'), h('div', { class: 'err' }, t.error)));
    else if (!t.done) rows.push(row(h('div', { class: 'spin' }), 'Recebendo ' + t.name, `${mb(t.loaded)} de ${mb(t.total)}`, null, h('div', { class: 'prog' }, h('i', { style: `width:${t.total ? (t.loaded / t.total) * 100 : 0}%` }))));
  }
  const now = Date.now() / 1000;
  const active = A.tasks.filter((t) => t.state === 'running' || t.state === 'waiting');
  for (const t of active.filter((t) => t.state === 'running').slice(0, 3)) {
    const pct = t.total ? Math.round((t.done / t.total) * 100) : null;
    rows.push(row(h('div', { class: 'spin' }), t.label, t.total ? `${mb(t.done)} de ${mb(t.total)}` : 'processando…', h('span', { class: 'd' }, pct !== null ? pct + '%' : ''),
      pct !== null ? h('div', { class: 'prog' }, h('i', { style: `width:${pct}%` })) : null));
  }
  const waiting = active.filter((t) => t.state === 'waiting').length;
  if (waiting) rows.push(h('div', { class: 'hint', style: 'padding:0 14px' }, `+ ${plural(waiting, 'item', 'itens')} na fila`));
  for (const t of A.tasks.filter((t) => t.state === 'error' && now - t.at < 600).slice(-3)) {
    rows.push(row(errIcon(), t.label, null, null, h('div', { class: 'err' }, t.error)));
  }
  if (!rows.length) {
    const last = A.tasks.filter((t) => t.kind === 'import' && t.state === 'done' && now - t.at < 30).pop();
    if (last && last.detail) rows.push(h('div', { class: 'act summary' }, h('span', { style: 'color:var(--ok)' }, '✓'), h('div', {}, h('div', { class: 't' }, last.detail), h('div', { class: 'd' }, 'Os jogadores já recebem pelo launcher.')), h('span')));
  }
  return h('div', { class: 'activity', id: 'activity' }, rows);
}

function usedIn(it) {
  return A.rooms.filter((r) => (it.kind === 'track' ? r.track === it.id : r.cars.includes(it.id))).map((r) => r.name);
}

function libraryGrid() {
  const list = A.library.filter((i) => (A.libKind === 'all' || i.kind === A.libKind) &&
    (!A.libQuery || i.name.toLowerCase().includes(A.libQuery) || i.id.toLowerCase().includes(A.libQuery)));
  if (!A.library.length) {
    return h('div', { class: 'empty', id: 'lib' }, h('b', {}, 'A base ainda está vazia'), 'Arraste o .zip de uma pista e dos carros para começar.');
  }
  const sending = new Set(A.tasks.filter((t) => t.kind === 'upload' && (t.state === 'running' || t.state === 'waiting')).map((t) => t.label));
  return h('div', { class: 'cards', id: 'lib' }, list.map((it) => {
    const p = pic(it.image ? `/img/lib?kind=${it.kind}&id=${encodeURIComponent(it.id)}&t=${TOKEN}&v=${it.sha256.slice(0, 8)}` : null, it.kind === 'car' ? 'car' : 'road');
    p.append(h('span', { class: 'corner tag', style: 'background:rgba(15,17,19,.8)' }, 'v' + it.version));
    p.append(h('span', { class: 'corner-l tag', style: 'background:rgba(15,17,19,.8)' }, it.kind === 'car' ? 'Carro' : 'Pista'));
    const used = usedIn(it);
    const status = it.uploaded ? h('span', { class: 'tag ok' }, 'Na base')
      : sending.has('Enviando ' + it.name) ? h('span', { class: 'tag warn' }, 'Enviando…')
        : h('span', { class: 'tag' }, A.connection.configured ? 'Aguardando envio' : 'Só neste PC');
    return h('div', { class: 'ccard' }, p, h('div', { class: 'info' },
      h('div', { class: 'name', title: it.name }, it.name),
      h('div', { class: 'meta', title: used.join(', ') }, `${it.id} · ${mb(it.size)}${used.length ? ' · ' + plural(used.length, 'sala', 'salas') : ''}`),
      h('div', { class: 'row' }, status, h('div', { class: 'spacer' }),
        h('button', { class: 'btn sm ghost danger', title: 'Remover da base', 'aria-label': 'Remover ' + it.name, onclick: () => removeItem(it, used) }, icon('trash')))));
  }));
}

async function removeItem(it, used) {
  const msg = used.length
    ? `"${it.name}" é usado em: ${used.join(', ')}.\nAo remover, essas salas ficam incompletas e saem do ar até você trocar o item.\n\nRemover mesmo assim?`
    : `Remover "${it.name}" da base?`;
  if (!confirm(msg)) return;
  try { await api('/api/admin/library/remove', { method: 'POST', body: { kind: it.kind, id: it.id } }); await refresh(); } catch (e) { toast(e.message, true); }
}

// ---- drag & drop upload ------------------------------------------------------------

function sendFiles(files) {
  for (const f of files) {
    if (!/\.zip$/i.test(f.name)) {
      toast(`${f.name}: só .zip. Para .7z ou .rar, instale no Assetto e use "Adicionar do meu Assetto".`, true);
      continue;
    }
    const t = { name: f.name, loaded: 0, total: f.size, done: false, error: '' };
    A.transfers.push(t);
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/admin/drop');
    xhr.setRequestHeader('X-Token', TOKEN);
    xhr.setRequestHeader('X-Filename', encodeURIComponent(f.name));
    xhr.upload.onprogress = (e) => { t.loaded = e.loaded; t.total = e.total; updateLive(); };
    xhr.onload = () => {
      t.done = true;
      if (xhr.status !== 200) { try { t.error = JSON.parse(xhr.responseText).error; } catch { t.error = 'Falha ao receber o arquivo'; } }
      refresh();
    };
    xhr.onerror = () => { t.done = true; t.error = 'Falha ao receber o arquivo'; updateLive(); };
    xhr.send(f);
  }
  if (page() !== 'conteudo') location.hash = '#/conteudo';
  updateLive();
  schedule();
}

const overlay = document.getElementById('overlay');
let dragDepth = 0;
window.addEventListener('dragenter', (e) => { if (e.dataTransfer?.types?.includes('Files')) { dragDepth++; overlay.classList.add('show'); } });
window.addEventListener('dragleave', () => { if (--dragDepth <= 0) { dragDepth = 0; overlay.classList.remove('show'); } });
window.addEventListener('dragover', (e) => e.preventDefault());
window.addEventListener('drop', (e) => {
  e.preventDefault();
  dragDepth = 0;
  overlay.classList.remove('show');
  document.getElementById('drop')?.classList.remove('over');
  if (e.dataTransfer?.files?.length) sendFiles(e.dataTransfer.files);
});
document.getElementById('filePick').addEventListener('change', (e) => { sendFiles(e.target.files); e.target.value = ''; });

// ---- add from the admin's own Assetto Corsa ------------------------------------------

async function openInstalled() {
  let kind = 'car';
  let items = [];
  let query = '';
  const list = h('div', { class: 'cards', style: 'grid-template-columns:repeat(auto-fill,minmax(180px,1fr))' });
  const draw = () => {
    const have = new Set(A.library.map((i) => i.kind + ':' + i.id));
    const f = items.filter((i) => !query || i.id.toLowerCase().includes(query) || (i.name || '').toLowerCase().includes(query)).slice(0, 120);
    list.replaceChildren(...f.map((i) => {
      const inBase = have.has(kind + ':' + i.id);
      return h('div', { class: 'ccard' }, pic(localImg(kind, i.id), kind === 'car' ? 'car' : 'road'),
        h('div', { class: 'info' }, h('div', { class: 'name', title: i.id }, i.name || i.id), h('div', { class: 'meta' }, i.id),
          h('button', {
            class: 'btn sm' + (inBase ? '' : ' primary'), onclick: async (e) => {
              e.target.disabled = true;
              try { await api('/api/admin/installed/add', { method: 'POST', body: { kind, id: i.id } }); toast(`${i.name || i.id} sendo adicionado…`); refresh(); } catch (err) { toast(err.message, true); }
            },
          }, inBase ? 'Atualizar na base' : 'Adicionar')));
    }));
    if (!f.length) list.replaceChildren(h('div', { class: 'empty', style: 'grid-column:1/-1' }, A.acValid ? 'Nada encontrado.' : 'Configure a pasta do Assetto Corsa na tela do jogador (Ajustes).'));
  };
  const load = async () => { items = (await api('/api/admin/installed?kind=' + kind)).items || []; draw(); };
  const seg = h('div', { class: 'seg' }, [['car', 'Carros'], ['track', 'Pistas']].map(([k, l]) => h('button', { 'aria-pressed': String(k === kind), onclick: (e) => { kind = k; seg.querySelectorAll('button').forEach((b) => b.setAttribute('aria-pressed', String(b === e.currentTarget))); load(); } }, l)));
  const q = h('input', { type: 'search', placeholder: 'Buscar…', oninput: (e) => { query = e.target.value.toLowerCase(); draw(); } });
  dlg.replaceChildren(h('div', { class: 'modal' },
    h('header', {}, h('h2', {}, 'Adicionar do meu Assetto'), h('button', { class: 'btn sm ghost', onclick: () => dlg.close(), 'aria-label': 'Fechar' }, icon('x'))),
    h('div', { class: 'body' }, h('p', { class: 'hint', style: 'margin:0' }, 'Útil para mods que vieram em .7z ou .rar: instale no seu Assetto e adicione daqui.'),
      h('div', { style: 'display:flex;gap:10px' }, seg, h('label', { class: 'search' }, icon('search'), q)), list),
    h('footer', {}, h('div', { class: 'spacer' }), h('button', { class: 'btn', onclick: () => dlg.close() }, 'Fechar'))));
  dlg.showModal();
  load();
}

// ---- rooms --------------------------------------------------------------------------

function roomsPage() {
  const tracks = A.library.filter((i) => i.kind === 'track');
  const cars = A.library.filter((i) => i.kind === 'car');
  const pageEl = h('div', { class: 'page' }, head('Salas', 'Cada sala é um servidor com uma pista e os carros liberados. Ao salvar, ela já aparece para os jogadores.',
    h('button', { class: 'btn primary', disabled: !tracks.length || !cars.length, onclick: () => editRoom(null) }, icon('plus'), 'Nova sala')));
  if (!tracks.length || !cars.length) {
    pageEl.append(h('div', { class: 'empty' }, h('b', {}, 'Adicione conteúdo primeiro'), 'Uma sala precisa de ao menos uma pista e um carro na base. ', h('a', { href: '#/conteudo', style: 'color:var(--accent)' }, 'Ir para Conteúdo')));
    return pageEl;
  }
  if (!A.rooms.length) {
    pageEl.append(h('div', { class: 'empty' }, h('b', {}, 'Nenhuma sala ainda'), 'Crie a primeira: escolha a pista, os carros e informe o endereço do servidor.'));
    return pageEl;
  }
  for (const r of A.rooms) {
    const track = tracks.find((t) => t.id === r.track);
    const ok = !r.problems.length;
    pageEl.append(h('div', { class: 'list-room' },
      h('div', { class: 'thumb' }, track?.image ? h('img', { alt: '', src: `/img/lib?kind=track&id=${encodeURIComponent(track.id)}&t=${TOKEN}` }) : null),
      h('div', {}, h('h3', {}, r.name || 'Sala sem nome'),
        h('div', { class: 'hint', style: 'margin-bottom:8px' }, [r.mode, track?.name || r.track, plural(r.cars.length, 'carro', 'carros'), r.host ? `${r.host}:${r.httpPort}` : ''].filter(Boolean).join(' · ')),
        ok ? h('span', { class: 'tag ok' }, 'No ar') : h('span', { class: 'tag warn', title: r.problems.join('\n') }, 'Incompleta: ' + r.problems[0])),
      h('button', { class: 'btn', onclick: () => editRoom(r) }, 'Editar')));
  }
  return pageEl;
}

function editRoom(room) {
  const isNew = !room;
  const r = room ? JSON.parse(JSON.stringify(room)) : { id: '', name: '', mode: '', host: '', httpPort: 8081, track: '', layout: '', cars: [], description: '' };
  delete r.problems;
  const tracks = A.library.filter((i) => i.kind === 'track');
  const cars = A.library.filter((i) => i.kind === 'car');
  const modes = [...new Set(A.rooms.map((x) => x.mode).filter(Boolean))];

  const name = h('input', { type: 'text', placeholder: 'Ex.: Bandeirantes — Tráfego pesado', value: r.name });
  const mode = h('input', { type: 'text', list: 'modeList', placeholder: 'Ex.: Tráfego pesado', value: r.mode });
  const modeList = h('datalist', { id: 'modeList' }, ['Tráfego leve', 'Tráfego pesado', 'Racha', 'Passeio', ...modes].filter((v, i, a) => a.indexOf(v) === i).map((m) => h('option', { value: m })));
  const addr = h('input', { type: 'text', placeholder: '123.45.67.89:8081', value: r.host ? `${r.host}:${r.httpPort || ''}` : '' });
  const desc = h('input', { type: 'text', placeholder: 'Opcional — aparece na página da sala', value: r.description || '' });
  const trackSel = h('select', {}, h('option', { value: '' }, 'Escolha a pista…'), tracks.map((t) => h('option', { value: t.id, selected: t.id === r.track }, t.name)));
  const layoutBox = h('div');
  const drawLayout = () => {
    const t = tracks.find((x) => x.id === trackSel.value);
    layoutBox.replaceChildren();
    if (t && t.layouts?.length > 1) {
      const sel = h('select', { id: 'layoutSel' }, h('option', { value: '' }, 'Escolha o traçado…'), t.layouts.map((l) => h('option', { value: l, selected: l === r.layout }, l)));
      layoutBox.append(h('label', { class: 'field' }, 'Traçado', sel));
    }
  };
  trackSel.addEventListener('change', drawLayout);
  drawLayout();
  const chosen = new Set(r.cars);
  const counter = h('span', { class: 'hint' });
  const updCount = () => { counter.textContent = plural(chosen.size, 'carro escolhido', 'carros escolhidos'); };
  updCount();
  let q = '';
  const pickBox = h('div', { class: 'pick' });
  const drawPick = () => {
    pickBox.replaceChildren(...cars.filter((c) => !q || c.name.toLowerCase().includes(q) || c.id.toLowerCase().includes(q)).map((c) => {
      const cb = h('input', { type: 'checkbox', checked: chosen.has(c.id), onchange: (e) => { if (e.target.checked) chosen.add(c.id); else chosen.delete(c.id); updCount(); } });
      return h('label', { title: c.id }, cb, c.image ? h('img', { alt: '', loading: 'lazy', src: `/img/lib?kind=car&id=${encodeURIComponent(c.id)}&t=${TOKEN}` }) : h('div', { class: 'ph' }), h('span', {}, c.name));
    }));
  };
  drawPick();
  const carSearch = h('input', { type: 'search', placeholder: 'Buscar carro…', oninput: (e) => { q = e.target.value.toLowerCase(); drawPick(); } });
  const errBox = h('div');

  const save = async () => {
    const m = addr.value.trim().replace(/^https?:\/\//, '').replace(/\/.*$/, '').match(/^(.+?)(?::(\d+))?$/);
    r.name = name.value.trim();
    r.mode = mode.value.trim();
    r.host = m ? m[1] : '';
    r.httpPort = m && m[2] ? Number(m[2]) : 0;
    r.description = desc.value.trim();
    r.track = trackSel.value;
    r.layout = document.getElementById('layoutSel')?.value || '';
    r.cars = cars.map((c) => c.id).filter((id) => chosen.has(id));
    const missing = [];
    if (!r.name) missing.push('o nome');
    if (!r.mode) missing.push('o modo');
    if (!r.host || !r.httpPort) missing.push('o endereço com a porta HTTP (ex.: 1.2.3.4:8081)');
    if (!r.track) missing.push('a pista');
    if (!r.cars.length) missing.push('os carros');
    if (missing.length) {
      errBox.replaceChildren(h('p', { class: 'problems', style: 'padding:0' }, 'Falta preencher: ' + missing.join(', ') + '.'));
      return;
    }
    const rooms = A.rooms.map(({ problems, ...x }) => x);
    if (isNew) rooms.push(r); else rooms[A.rooms.findIndex((x) => x.id === room.id)] = r;
    await saveBase({ rooms });
    dlg.close();
    toast(isNew ? 'Sala criada — publicando…' : 'Sala salva — publicando…');
  };
  const del = async () => {
    if (!confirm(`Apagar a sala "${room.name}"? Ela sai do ar para os jogadores.`)) return;
    await saveBase({ rooms: A.rooms.filter((x) => x.id !== room.id).map(({ problems, ...x }) => x) });
    dlg.close();
  };
  const cmLink = !isNew && !room.problems.length
    ? h('details', {}, h('summary', { class: 'hint', style: 'cursor:pointer' }, 'Para quem entra pelo Content Manager sem o launcher'),
      h('p', { class: 'hint' }, 'Baixe o content.json e coloque em cfg/cm_content/ do AssettoServer desta sala. O CM passa a oferecer "Install missing content" com os mesmos arquivos.'),
      h('a', { class: 'btn sm', href: `/dl/content.json?room=${encodeURIComponent(room.id)}&t=${TOKEN}` }, icon('download'), 'Baixar content.json'))
    : null;

  dlg.replaceChildren(h('div', { class: 'modal' },
    h('header', {}, h('h2', {}, isNew ? 'Nova sala' : 'Editar sala'), h('button', { class: 'btn sm ghost', onclick: () => dlg.close(), 'aria-label': 'Fechar' }, icon('x'))),
    h('div', { class: 'body' },
      h('div', { class: 'grid2' }, h('label', { class: 'field' }, 'Nome da sala', name), h('label', { class: 'field' }, 'Modo de jogo', mode, modeList)),
      h('div', { class: 'grid2' },
        h('label', { class: 'field' }, 'Endereço do servidor', addr, h('span', { class: 'hint' }, 'IP ou domínio : HTTP_PORT do server_cfg.ini')),
        h('label', { class: 'field' }, 'Descrição', desc)),
      h('div', { class: 'grid2' }, h('label', { class: 'field' }, 'Pista', trackSel), layoutBox),
      h('div', { class: 'field', style: 'display:grid;gap:8px' }, h('div', { style: 'display:flex;align-items:center;gap:10px' }, h('b', { style: 'font-size:13px;color:var(--ink-2)' }, 'Carros liberados'), counter, h('div', { class: 'spacer' }),
        h('button', { class: 'btn sm ghost', onclick: () => { cars.forEach((c) => chosen.add(c.id)); drawPick(); updCount(); } }, 'Todos'),
        h('button', { class: 'btn sm ghost', onclick: () => { chosen.clear(); drawPick(); updCount(); } }, 'Nenhum')),
      h('label', { class: 'search' }, icon('search'), carSearch), pickBox),
      cmLink, errBox),
    h('footer', {}, isNew ? null : h('button', { class: 'btn ghost danger', onclick: del }, icon('trash'), 'Apagar sala'), h('div', { class: 'spacer' }),
      h('button', { class: 'btn', onclick: () => dlg.close() }, 'Cancelar'), h('button', { class: 'btn primary', onclick: save }, 'Salvar e publicar'))));
  dlg.showModal();
  name.focus();
}

async function saveBase(patch) {
  const body = { name: A.name, description: A.description, rooms: A.rooms.map(({ problems, ...x }) => x), ...patch };
  try { await api('/api/admin/base', { method: 'PUT', body }); await refresh(); schedule(); } catch (e) { toast(e.message, true); }
}

// ---- connection ---------------------------------------------------------------------

function connectionPage() {
  const c = A.connection;
  const url = h('input', { type: 'text', placeholder: 'https://seusite.com.br/ac/upload.php', value: c.url || '' });
  const connect = async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    btn.textContent = 'Testando…';
    try {
      await api('/api/admin/connection', { method: 'POST', body: { url: url.value.trim() } });
      toast('Site conectado. Enviando o conteúdo…');
      await refresh();
      schedule();
    } catch (err) { toast(err.message, true); btn.disabled = false; btn.textContent = 'Conectar'; }
  };
  const nameIn = h('input', { type: 'text', value: A.name, placeholder: 'Ex.: Rodovias BR' });
  const descIn = h('input', { type: 'text', value: A.description || '', placeholder: 'Uma linha sobre a base (opcional)' });
  const hubUrl = c.hubUrl || '';
  const copyBox = h('div', { class: 'copy' }, h('input', { type: 'text', readonly: true, value: hubUrl }),
    h('button', { class: 'btn', onclick: () => navigator.clipboard.writeText(hubUrl).then(() => toast('Link copiado.')) }, icon('copy'), 'Copiar'));
  return h('div', { class: 'page', style: 'max-width:860px' },
    head('Conexão', 'Liga o painel ao seu site (Hostinger ou qualquer hospedagem com PHP). É feito uma vez só.'),
    h('div', { class: 'panel' }, h('div', { class: 'steps' },
      h('div', { class: 'step' + (c.configured ? ' done' : '') }, h('div', {},
        h('b', {}, 'Baixe o arquivo de envio'),
        h('p', {}, 'Ele recebe os pacotes no seu site. Tem uma chave secreta só sua — não compartilhe.'),
        h('a', { class: 'btn', href: `/dl/upload.php?t=${TOKEN}` }, icon('download'), 'Baixar upload.php'))),
      h('div', { class: 'step' + (c.configured ? ' done' : '') }, h('div', {},
        h('b', {}, 'Envie para o seu site'),
        h('p', {}, 'Na Hostinger: Gerenciador de Arquivos → public_html → crie a pasta "ac" → envie o upload.php para dentro dela.'))),
      h('div', { class: 'step' + (c.configured ? ' done' : '') }, h('div', {},
        h('b', {}, 'Cole o endereço e conecte'),
        h('div', { class: 'copy' }, url, h('button', { class: 'btn' + (c.configured ? '' : ' primary'), onclick: connect }, c.configured ? 'Reconectar' : 'Conectar')))),
      c.configured ? h('div', { class: 'step done' }, h('div', {},
        h('b', {}, 'Passe este link para os jogadores'),
        h('p', {}, 'Eles colam em Ajustes → Link da base. Tudo o que você publicar chega por ele.'),
        copyBox)) : null)),
    h('div', { class: 'panel' },
      h('h2', {}, 'Nome da base'),
      h('p', { class: 'lead' }, 'Aparece no topo da tela dos jogadores.'),
      h('div', { class: 'grid2' }, h('label', { class: 'field' }, 'Nome', nameIn), h('label', { class: 'field' }, 'Descrição', descIn)),
      h('div', { style: 'margin-top:14px' }, h('button', { class: 'btn', onclick: () => saveBase({ name: nameIn.value.trim() || 'Minha base', description: descIn.value.trim() }).then(() => toast('Salvo.')) }, 'Salvar'))),
    A.publishError ? h('div', { class: 'alert' }, h('div', { class: 'ico' }, icon('warn')), h('div', {}, h('b', {}, 'Último erro ao publicar'), h('span', {}, A.publishError)),
      h('div', { class: 'spacer' }), h('button', { class: 'btn white', onclick: () => api('/api/admin/publish', { method: 'POST', body: {} }).then(() => { refresh(); schedule(); }) }, icon('refresh'), 'Tentar de novo')) : null);
}

// ---- start ----------------------------------------------------------------------------

(async () => {
  await refresh(false);
  // Fix the starting page in the URL so later refreshes never jump away.
  if (!location.hash) history.replaceState(null, '', A.connection.configured ? '#/conteudo' : '#/conexao');
  render();
  schedule();
})();
