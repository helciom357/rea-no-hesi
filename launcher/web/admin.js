// Admin panel: edit the base (modes, rooms, content), package mods, publish.
const $ = (s) => document.querySelector(s);
const A = { base: null, settings: null, problems: [], dirty: false, sec: 'rooms', roomId: null, packages: [], pkgDir: '', localKind: 'car', local: [], localFilter: '', exportRes: null };

async function boot() {
  const r = await api('/api/admin/base');
  A.base = r.base;
  A.settings = r.settings;
  A.problems = r.problems || [];
  A.base.modes ||= [];
  A.base.rooms ||= [];
  A.base.allowedHosts ||= [];
  A.roomId = A.base.rooms[0]?.id || null;
  if (!r.saved) setDirty(true);
  await loadPackages();
  render();
}

function setDirty(v) {
  A.dirty = v;
  $('#dirty').hidden = !v;
}
function changed() { setDirty(true); }

async function save() {
  try {
    const r = await api('/api/admin/base', { method: 'PUT', body: A.base });
    A.problems = r.problems || [];
    setDirty(false);
    toast(A.problems.length ? `Base salva, com ${A.problems.length} pendência(s) — veja Publicar.` : 'Base salva.');
    if (A.sec === 'publish') render();
  } catch (e) { toast(e.message, true); }
}
$('#save').addEventListener('click', save);
window.addEventListener('beforeunload', (e) => { if (A.dirty) { e.preventDefault(); e.returnValue = ''; } });
document.addEventListener('keydown', (e) => { if ((e.ctrlKey || e.metaKey) && e.key === 's') { e.preventDefault(); save(); } });

$('#nav').addEventListener('click', (e) => {
  const b = e.target.closest('button[data-sec]');
  if (!b) return;
  A.sec = b.dataset.sec;
  render();
});

function render() {
  for (const b of document.querySelectorAll('#nav button')) b.setAttribute('aria-current', String(b.dataset.sec === A.sec));
  const c = $('#content');
  c.replaceChildren(({ base: secBase, modes: secModes, rooms: secRooms, packages: secPackages, publish: secPublish, paths: secPaths })[A.sec]());
}

// ---- form helpers -------------------------------------------------------------

function field(label, obj, key, opts = {}) {
  const input = h(opts.textarea ? 'textarea' : 'input', {
    type: opts.type || 'text', placeholder: opts.placeholder || '', rows: opts.textarea ? 2 : undefined,
    oninput: (e) => {
      let v = e.target.value;
      if (opts.type === 'number') v = v === '' ? 0 : Number(v);
      if (opts.list) v = v.split(/[\s,]+/).filter(Boolean);
      obj[key] = v;
      changed();
      opts.after?.(v);
    },
  });
  input.value = opts.list ? (obj[key] || []).join(', ') : (obj[key] ?? '');
  return h('label', { class: 'field' }, label, input, opts.hint ? h('span', { class: 'hint' }, opts.hint) : null);
}

function cellInput(obj, key, placeholder = '', after) {
  const i = h('input', { type: 'text', placeholder, oninput: (e) => { obj[key] = e.target.value.trim(); changed(); after?.(); } });
  i.value = obj[key] || '';
  return i;
}

function slug(s) {
  return s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 60) || 'item';
}

function uniqueId(base, taken) {
  let id = base, n = 2;
  while (taken.includes(id)) id = `${base}-${n++}`;
  return id;
}

// ---- sections -------------------------------------------------------------------

function secBase() {
  const b = A.base;
  return h('div', {},
    h('section', { class: 'panel' },
      h('h2', {}, 'Base'),
      h('p', { class: 'lead' }, 'Identidade da base e de onde os jogadores baixam o conteúdo.'),
      h('div', { class: 'cols' }, field('Nome da base', b, 'name'), field('Descrição', b, 'description')),
      h('div', { style: 'height:12px' }),
      field('Endereço base dos downloads', b, 'downloadBase', {
        placeholder: 'https://downloads.seusite.com.br/ac',
        hint: 'Pasta pública onde você sobe os .zip dos pacotes. Cada item usa este endereço + o nome do arquivo, a menos que tenha um link próprio.',
      }),
      h('div', { style: 'height:12px' }),
      field('Hosts permitidos para download', b, 'allowedHosts', {
        list: true, placeholder: 'downloads.seusite.com.br',
        hint: 'O launcher só baixa de HTTPS nestes domínios (separe por vírgula). Use *.dominio.com para subdomínios.',
      })));
}

function secModes() {
  const modes = A.base.modes;
  const rows = modes.map((m, i) => {
    const inUse = A.base.rooms.filter((r) => r.modeId === m.id).length;
    return h('tr', {},
      h('td', {}, cellInput(m, 'name', 'Tráfego')),
      h('td', {}, (() => {
        // Renaming a mode id carries its rooms along.
        let prev = m.id;
        return cellInput(m, 'id', 'trafego', () => {
          for (const r of A.base.rooms) if (r.modeId === prev) r.modeId = m.id;
          prev = m.id;
        });
      })()),
      h('td', {}, cellInput(m, 'description', 'Descrição curta')),
      h('td', { class: 'hint' }, `${inUse} sala(s)`),
      h('td', {}, h('button', {
        class: 'btn sm danger', disabled: inUse > 0, title: inUse ? 'Mova ou apague as salas deste modo antes' : 'Remover',
        onclick: () => { modes.splice(i, 1); changed(); render(); },
      }, 'Remover')));
  });
  return h('section', { class: 'panel' },
    h('h2', {}, 'Modos de jogo'),
    h('p', { class: 'lead' }, 'Os modos aparecem como abas para o jogador (ex.: Tráfego, Passeio, Racha).'),
    h('table', { class: 't' }, h('thead', {}, h('tr', {}, h('th', {}, 'Nome'), h('th', {}, 'Id'), h('th', {}, 'Descrição'), h('th'), h('th'))), h('tbody', {}, rows)),
    h('div', { style: 'margin-top:12px' }, h('button', {
      class: 'btn', onclick: () => {
        modes.push({ id: uniqueId('modo', modes.map((m) => m.id)), name: 'Novo modo', description: '' });
        changed(); render();
      },
    }, '+ Adicionar modo')));
}

function secRooms() {
  const rooms = A.base.rooms;
  const list = h('div', { class: 'rooms-list' }, ...rooms.map((r) => {
    const mode = A.base.modes.find((m) => m.id === r.modeId);
    return h('div', { class: 'room-row', 'aria-selected': String(r.id === A.roomId), onclick: () => { A.roomId = r.id; render(); } },
      h('strong', { style: 'flex:1' }, r.name || r.id),
      h('span', { class: 'hint' }, mode ? mode.name : 'sem modo'),
      h('span', { class: 'badge muted' }, `${r.cars.length} carros`));
  }));
  const add = h('button', {
    class: 'btn', onclick: () => {
      const id = uniqueId('nova-sala', rooms.map((r) => r.id));
      rooms.push({ id, name: 'Nova sala', modeId: A.base.modes[0]?.id || '', server: { host: '', tcpPort: 9600, httpPort: 8081 }, track: { id: '' }, cars: [] });
      A.roomId = id; changed(); render();
    },
  }, '+ Adicionar sala');
  const room = rooms.find((r) => r.id === A.roomId);
  return h('div', {},
    h('section', { class: 'panel' }, h('h2', {}, 'Salas'),
      h('p', { class: 'lead' }, 'Cada sala é um servidor AssettoServer com uma pista e uma lista de carros fixa.'),
      list, add),
    room ? roomEditor(room) : null);
}

function roomEditor(r) {
  r.server ||= {};
  r.track ||= {};
  r.cars ||= [];
  const modeSel = h('select', { onchange: (e) => { r.modeId = e.target.value; changed(); } },
    ...A.base.modes.map((m) => h('option', { value: m.id, selected: m.id === r.modeId }, m.name)));
  const dup = h('button', {
    class: 'btn sm', onclick: () => {
      const copy = JSON.parse(JSON.stringify(r));
      copy.id = uniqueId(r.id + '-copia', A.base.rooms.map((x) => x.id));
      copy.name = r.name + ' (cópia)';
      A.base.rooms.push(copy); A.roomId = copy.id; changed(); render();
    },
  }, 'Duplicar');
  const del = h('button', {
    class: 'btn sm danger', onclick: () => {
      if (!confirm(`Apagar a sala "${r.name}"?`)) return;
      A.base.rooms = A.base.rooms.filter((x) => x !== r);
      A.roomId = A.base.rooms[0]?.id || null; changed(); render();
    },
  }, 'Apagar sala');

  return h('div', {},
    h('section', { class: 'panel' },
      h('div', { style: 'display:flex;gap:8px;align-items:center;margin-bottom:12px' }, h('h2', { style: 'flex:1' }, r.name || r.id), dup, del),
      h('div', { class: 'cols' },
        field('Nome da sala', r, 'name', { after: () => { $('.room-row[aria-selected="true"] strong').textContent = r.name; } }),
        field('Id', r, 'id', { hint: 'Sem espaços. Vira o nome da pasta do servidor na exportação.', after: (v) => { A.roomId = v; } }),
        h('label', { class: 'field' }, 'Modo', modeSel)),
      h('div', { style: 'height:12px' }),
      field('Descrição', r, 'description', { placeholder: 'Ex.: trecho Jundiaí – Campinas, tráfego pesado' }),
      h('div', { style: 'height:12px' }),
      h('div', { class: 'cols' },
        field('Endereço do servidor (IP ou domínio)', r.server, 'host'),
        field('Porta TCP/UDP', r.server, 'tcpPort', { type: 'number' }),
        field('Porta HTTP', r.server, 'httpPort', { type: 'number', hint: 'A HTTP_PORT do server_cfg.ini' }))),
    h('section', { class: 'panel' },
      h('h2', {}, 'Pista'),
      h('p', { class: 'lead' }, 'Escolha um pacote de pista já preparado ou preencha à mão.'),
      pickPackage('track', (p) => { Object.assign(r.track, entryFromPackage(p)); if (p.layouts?.length === 1) r.track.layout = p.layouts[0]; changed(); render(); }),
      h('div', { style: 'height:12px' }),
      h('div', { class: 'cols' },
        field('Id da pasta', r.track, 'id', { placeholder: 'br_bandeirantes' }),
        field('Layout (se houver)', r.track, 'layout', { placeholder: 'deixe vazio se a pista tem um só' }),
        field('Nome exibido', r.track, 'name'),
        field('Versão', r.track, 'version', { placeholder: '1.0' })),
      h('div', { style: 'height:12px' }),
      h('div', { class: 'cols' },
        field('Arquivo do pacote', r.track, 'file', { placeholder: 'br_bandeirantes.zip' }),
        field('Ou link completo', r.track, 'url', { placeholder: 'https://…' }),
        field('sha256', r.track, 'sha256', { hint: 'Preenchido ao escolher o pacote' }))),
    h('section', { class: 'panel' },
      h('h2', {}, 'Carros'),
      h('p', { class: 'lead' }, 'Todos os carros que a entry list desta sala usa.'),
      carsTable(r),
      h('div', { style: 'display:flex;gap:8px;margin-top:12px;flex-wrap:wrap;align-items:center' },
        pickPackage('car', (p) => {
          if (r.cars.some((c) => c.id === p.id)) { toast('Este carro já está na sala.', true); return; }
          r.cars.push(entryFromPackage(p)); changed(); render();
        }, true),
        h('button', { class: 'btn', onclick: () => { r.cars.push({ id: '', version: '' }); changed(); render(); } }, '+ Carro manual'))));
}

function carsTable(r) {
  if (!r.cars.length) return h('div', { class: 'empty', style: 'padding:16px' }, 'Nenhum carro ainda.');
  return h('table', { class: 't' },
    h('thead', {}, h('tr', {}, h('th', {}, 'Id da pasta'), h('th', {}, 'Nome'), h('th', {}, 'Versão'), h('th', {}, 'Arquivo ou link'), h('th', {}, 'sha256'), h('th'))),
    h('tbody', {}, ...r.cars.map((c, i) => h('tr', {},
      h('td', {}, cellInput(c, 'id', 'br_opala_ss')),
      h('td', {}, cellInput(c, 'name', 'Opala SS')),
      h('td', { style: 'width:90px' }, cellInput(c, 'version', '1.0')),
      h('td', {}, cellInput(c, c.url ? 'url' : 'file', 'arquivo.zip ou https://…')),
      h('td', { class: 'mono', title: c.sha256 || '' }, c.sha256 ? c.sha256.slice(0, 10) + '…' : '—'),
      h('td', {}, h('button', { class: 'btn sm danger', onclick: () => { r.cars.splice(i, 1); changed(); render(); } }, 'Remover'))))));
}

function entryFromPackage(p) {
  return { id: p.id, name: p.name || p.id, version: p.version || '', file: p.file, sha256: p.sha256, size: p.size };
}

function pickPackage(kind, onPick, compact) {
  const pkgs = A.packages.filter((p) => p.kind === kind);
  const sel = h('select', { style: compact ? 'width:auto;min-width:260px' : '' },
    h('option', { value: '' }, pkgs.length ? `Escolher pacote de ${kind === 'car' ? 'carro' : 'pista'}…` : `Nenhum pacote de ${kind === 'car' ? 'carro' : 'pista'} na pasta de pacotes`),
    ...pkgs.map((p) => h('option', { value: p.file }, `${p.name || p.id} · ${p.id} · v${p.version || '?'} · ${mb(p.size)}`)));
  sel.addEventListener('change', () => {
    const p = pkgs.find((x) => x.file === sel.value);
    if (p) onPick(p);
  });
  return compact ? sel : h('label', { class: 'field' }, 'Usar pacote', sel);
}

// ---- packages ---------------------------------------------------------------------

async function loadPackages() {
  try {
    const r = await api('/api/admin/packages');
    A.packages = r.packages || [];
    A.pkgDir = r.dir;
  } catch (e) { toast(e.message, true); }
}

async function loadLocal() {
  try {
    const r = await api('/api/admin/local?kind=' + A.localKind);
    A.local = r.items || [];
    A.localValid = r.acValid;
  } catch (e) { toast(e.message, true); }
}

function secPackages() {
  const pk = h('table', { class: 't' },
    h('thead', {}, h('tr', {}, h('th', {}, 'Arquivo'), h('th', {}, 'Tipo'), h('th', {}, 'Id'), h('th', {}, 'Versão'), h('th', {}, 'Tamanho'), h('th', {}, 'sha256'))),
    h('tbody', {}, ...A.packages.map((p) => h('tr', {},
      h('td', {}, p.file),
      h('td', {}, p.error ? h('span', { class: 'badge missing', title: p.error }, 'inválido') : KIND_LABEL[p.kind]),
      h('td', {}, p.id || '—', p.layouts?.length ? h('span', { class: 'hint' }, ' · layouts: ' + p.layouts.join(', ')) : null),
      h('td', {}, p.version ? p.version : h('span', { class: 'badge outdated' }, 'sem versão')),
      h('td', {}, mb(p.size)),
      h('td', { class: 'mono', title: p.sha256 }, (p.sha256 || '').slice(0, 12) + '…')))));

  const filterInput = h('input', { type: 'text', placeholder: 'Filtrar por id ou nome', oninput: (e) => { A.localFilter = e.target.value.toLowerCase(); renderLocal(); } });
  filterInput.value = A.localFilter;
  const kindSel = h('select', { style: 'width:auto', onchange: async (e) => { A.localKind = e.target.value; await loadLocal(); renderLocal(); } },
    h('option', { value: 'car', selected: A.localKind === 'car' }, 'Carros'), h('option', { value: 'track', selected: A.localKind === 'track' }, 'Pistas'));
  const localBox = h('div', { id: 'localBox' });

  function renderLocal() {
    if (A.localValid === false) {
      localBox.replaceChildren(h('div', { class: 'notice warn' }, h('div', {}, h('b', {}, 'Pasta do Assetto Corsa não configurada'), h('p', {}, 'Configure em Pastas para empacotar o conteúdo instalado neste PC.'))));
      return;
    }
    const items = A.local.filter((l) => !A.localFilter || l.id.toLowerCase().includes(A.localFilter) || (l.name || '').toLowerCase().includes(A.localFilter)).slice(0, 150);
    localBox.replaceChildren(h('table', { class: 't' },
      h('thead', {}, h('tr', {}, h('th', {}, 'Id'), h('th', {}, 'Nome'), h('th', {}, 'Versão atual'), h('th', {}, 'Versão do pacote'), h('th'))),
      h('tbody', {}, ...items.map((l) => {
        const ver = h('input', { type: 'text', placeholder: l.version || '1.0', style: 'width:90px' });
        ver.value = l.version || '';
        const btn = h('button', {
          class: 'btn sm', onclick: async () => {
            btn.disabled = true; btn.textContent = 'Empacotando…';
            try {
              const info = await api('/api/admin/package', { method: 'POST', body: { kind: A.localKind, id: l.id, version: ver.value.trim() } });
              toast(`Pacote ${info.file} criado (${mb(info.size)}).`);
              await loadPackages(); render();
            } catch (e) { toast(e.message, true); btn.disabled = false; btn.textContent = 'Empacotar'; }
          },
        }, 'Empacotar');
        return h('tr', {}, h('td', { class: 'mono' }, l.id), h('td', {}, l.name || ''), h('td', {}, l.version || h('span', { class: 'hint' }, 'nenhuma')), h('td', {}, ver), h('td', {}, btn));
      }))),
      A.local.length > 150 ? h('p', { class: 'hint' }, 'Mostrando 150 — use o filtro.') : null);
  }
  loadLocal().then(renderLocal);

  return h('div', {},
    h('section', { class: 'panel' },
      h('div', { style: 'display:flex;gap:8px;align-items:center' }, h('h2', { style: 'flex:1' }, 'Pacotes prontos'),
        h('button', { class: 'btn sm', onclick: async () => { await loadPackages(); render(); } }, 'Atualizar'),
        h('button', { class: 'btn sm', onclick: () => api('/api/admin/open', { method: 'POST', body: { which: 'packages' } }).catch((e) => toast(e.message, true)) }, 'Abrir pasta')),
      h('p', { class: 'lead' }, 'Arquivos .zip em ', h('span', { class: 'mono' }, A.pkgDir), '. Estes são os arquivos que você sobe para o endereço de downloads.'),
      A.packages.length ? pk : h('div', { class: 'empty', style: 'padding:16px' }, 'Nenhum pacote ainda. Empacote abaixo ou copie .zip para a pasta.')),
    h('section', { class: 'panel' },
      h('h2', {}, 'Empacotar do meu Assetto Corsa'),
      h('p', { class: 'lead' }, 'Gera o .zip no formato que o launcher instala. A versão é gravada só dentro do pacote — sua instalação não é alterada. Sempre que mudar um mod, aumente a versão.'),
      h('div', { style: 'display:flex;gap:8px;margin-bottom:10px' }, kindSel, filterInput),
      localBox));
}

// ---- publish ------------------------------------------------------------------------

function secPublish() {
  const probs = A.problems.length
    ? h('ul', { class: 'problems' }, ...A.problems.map((p) => h('li', {}, p)))
    : h('p', { class: 'badge ok', style: 'display:inline-block' }, 'Nenhuma pendência');
  const res = A.exportRes;
  const btn = h('button', {
    class: 'btn primary', onclick: async () => {
      if (A.dirty) await save();
      try { A.exportRes = await api('/api/admin/export', { method: 'POST', body: {} }); A.problems = A.exportRes.problems || []; render(); toast('Arquivos gerados.'); }
      catch (e) { toast(e.message, true); }
    },
  }, 'Gerar arquivos de publicação');
  return h('div', {},
    h('section', { class: 'panel' }, h('h2', {}, 'Pendências'), h('p', { class: 'lead' }, 'Itens sem versão ou sem link fazem o launcher errar a detecção de faltantes.'), probs),
    h('section', { class: 'panel' },
      h('h2', {}, 'Publicar'),
      h('p', { class: 'lead' }, 'Gera o hub.json (lido pelo launcher) e um content.json por sala (lido pelo AssettoServer, que repassa os links ao Content Manager).'),
      h('div', { style: 'display:flex;gap:8px' }, btn,
        h('button', { class: 'btn', onclick: () => api('/api/admin/open', { method: 'POST', body: { which: 'export' } }).catch((e) => toast(e.message, true)) }, 'Abrir pasta')),
      res ? h('div', { style: 'margin-top:16px' },
        h('ol', { style: 'padding-left:18px;color:var(--ink-2);display:grid;gap:8px' },
          h('li', {}, 'Suba os pacotes para ', h('span', { class: 'mono' }, A.base.downloadBase || '(defina o endereço de downloads em Base)'), ': ',
            res.upload?.length ? h('span', { class: 'mono' }, res.upload.join(', ')) : 'nenhum arquivo local referenciado.'),
          h('li', {}, 'Suba ', h('span', { class: 'mono' }, 'hub.json'), ' para o seu site e passe o link aos jogadores (eles colam em Ajustes).'),
          h('li', {}, 'Em cada servidor, copie ', h('span', { class: 'mono' }, 'servidores/<sala>/cfg/cm_content/content.json'), ' para a pasta ', h('span', { class: 'mono' }, 'cfg/cm_content/'), ' do AssettoServer e reinicie.')),
        h('p', { class: 'hint' }, 'Gerado em ', h('span', { class: 'mono' }, res.dir))) : null));
}

// ---- paths --------------------------------------------------------------------------

function secPaths() {
  const s = A.settings;
  const f = (label, key, hint) => {
    const i = h('input', { type: 'text' });
    i.value = s[key] || '';
    i.dataset.key = key;
    return h('label', { class: 'field' }, label, i, hint ? h('span', { class: 'hint' }, hint) : null);
  };
  const box = h('div', { style: 'display:grid;gap:12px' },
    f('Pasta do Assetto Corsa', 'acPath', 'Usada para empacotar o conteúdo instalado neste PC.'),
    f('Arquivo da base (base.json)', 'adminBase', 'Onde o painel salva a base. Faça backup deste arquivo.'),
    f('Pasta de pacotes', 'packageDir'),
    f('Pasta de publicação', 'exportDir'),
    f('Base usada pela tela do jogador neste PC', 'hubSource', 'Para testar, aponte para o hub.json da pasta de publicação.'));
  return h('section', { class: 'panel' },
    h('h2', {}, 'Pastas'),
    h('p', { class: 'lead' }, 'Locais usados pelo painel neste computador.'),
    box,
    h('div', { class: 'actions', style: 'margin-top:14px' },
      h('button', {
        class: 'btn', onclick: () => {
          box.querySelector('[data-key=hubSource]').value = (s.exportDir || '') + (s.exportDir?.includes('\\') ? '\\' : '/') + 'hub.json';
        },
      }, 'Usar a publicação como base do jogador'),
      h('button', {
        class: 'btn primary', onclick: async () => {
          const body = {};
          box.querySelectorAll('input').forEach((i) => { body[i.dataset.key] = i.value; });
          try {
            const r = await api('/api/settings', { method: 'POST', body });
            A.settings = r.settings;
            const b = await api('/api/admin/base');
            if (!A.dirty) { A.base = b.base; A.problems = b.problems; }
            await loadPackages();
            toast(r.acValid ? 'Pastas salvas.' : 'Pastas salvas — a pasta do Assetto Corsa não foi reconhecida.', !r.acValid);
          } catch (e) { toast(e.message, true); }
        },
      }, 'Salvar pastas')));
}

boot().catch((e) => toast(e.message, true));
