(() => {
  'use strict';

  const STATE = {
    route: '/',
    wikiSlug: 'home',
    mode: 'none', // 'none' | 'demo' | 'live' | 'portable'
    apiBase: '',
    sessionToken: '',
    editor: null,
    selected: null, // { type: 'group'|'user', id: string }
    expandedSections: { tracks: true, groups: true, users: true },
    expandedTracks: {},
    sidebarQuery: '',
    nodesFilter: '',
    checkedRows: new Set(),
    dirtyCount: 0,
    deletedGroups: new Set(),
    deletedTracks: new Set(),
    deletedUsers: new Set(),
    addForm: {
      type: 'PERMISSION',
      key: '',
      metaKey: '',
      priority: '0',
      value: true,
      expiry: '',
      contexts: {}
    },
    acIndex: -1,
    acOpen: false,
    verbose: null,
    verboseFilter: '',
    verboseExclude: { true: false, false: false, undefined: false },
    verboseExpanded: new Set(),
    tree: null,
    treeFilter: '',
    treeCollapsed: new Set()
  };

  function detectBasePath() {
    const p = window.location.pathname;
    for (const suffix of ['/editor', '/verbose', '/tree', '/wiki', '/index.html', '/404.html']) {
      if (p.endsWith(suffix)) return (p.slice(0, -suffix.length) || '/').replace(/\/?$/, '/');
      if (p.endsWith(suffix + '/')) return (p.slice(0, -(suffix.length + 1)) || '/').replace(/\/?$/, '/');
    }
    return p.endsWith('/') ? p : p + '/';
  }

  const BASE_PATH = detectBasePath();

  function esc(str) {
    return String(str ?? '').replace(/[&<>"']/g, ch => ({
      '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'
    }[ch]));
  }

  function deepClone(v) {
    return JSON.parse(JSON.stringify(v));
  }

  function toast(msg, isError = false) {
    const root = document.getElementById('toast-root');
    const el = document.createElement('div');
    el.className = 'toast' + (isError ? ' error' : '');
    el.textContent = msg;
    root.appendChild(el);
    setTimeout(() => el.remove(), 3800);
  }

  function fallbackHeadSvg(name) {
    const label = ((name || '?')[0] || '?').toUpperCase();
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><rect width="24" height="24" rx="3" fill="#2c313c"/><text x="12" y="17" text-anchor="middle" font-family="sans-serif" font-weight="800" font-size="13" fill="#94df03">${label}</text></svg>`;
    return 'data:image/svg+xml;utf8,' + encodeURIComponent(svg);
  }
  window.__spHeadFallback = function(img) {
    img.onerror = null;
    img.src = fallbackHeadSvg(img.dataset.name);
  };

  function bytesToBase64Url(bytes) {
    let bin = '';
    for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
    return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
  }

  function base64UrlToBytes(str) {
    let b64 = str.replace(/-/g, '+').replace(/_/g, '/');
    while (b64.length % 4) b64 += '=';
    const bin = atob(b64);
    const arr = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
    return arr;
  }

  async function encodePortableSp1(payloadObj) {
    const rawBytes = new TextEncoder().encode(JSON.stringify(payloadObj));
    if (typeof CompressionStream === 'undefined') {
      throw new Error('Browser lacks CompressionStream(gzip); use JSON export instead.');
    }
    const cs = new CompressionStream('gzip');
    const writer = cs.writable.getWriter();
    await writer.write(rawBytes);
    await writer.close();
    const buf = await new Response(cs.readable).arrayBuffer();
    return 'sp1_' + bytesToBase64Url(new Uint8Array(buf));
  }

  async function decodePortableSp1(code) {
    const trimmed = String(code || '').trim();
    if (!trimmed.startsWith('sp1_')) throw new Error('Expected sp1_ portable code');
    const compressed = base64UrlToBytes(trimmed.slice(4));
    if (typeof DecompressionStream === 'undefined') {
      throw new Error('Browser lacks DecompressionStream(gzip) to read sp1_ payloads.');
    }
    const ds = new DecompressionStream('gzip');
    const writer = ds.writable.getWriter();
    await writer.write(compressed);
    await writer.close();
    const buf = await new Response(ds.readable).arrayBuffer();
    if (buf.byteLength > 64 * 1024) {
      throw new Error('Portable payload exceeds 64 KiB cap');
    }
    return JSON.parse(new TextDecoder().decode(buf));
  }

  function resolveApiOrigin(explicitApi) {
    if (explicitApi) return explicitApi.replace(/\/$/, '');
    const h = window.location.hostname;
    if (h.endsWith('github.io')) return '';
    return window.location.origin;
  }

  function parseRouteFromLocation() {
    const url = new URL(window.location.href);
    const hash = url.hash || '';
    const path = url.pathname;

    let route = '/';
    let subSlug = 'home';

    if (hash.startsWith('#/')) {
      const cleanHash = hash.slice(1).split('?')[0].split('#')[0];
      if (cleanHash.startsWith('/wiki/')) {
        route = '/wiki';
        subSlug = cleanHash.slice(6) || 'home';
      } else if (['/editor', '/verbose', '/tree', '/wiki', '/'].includes(cleanHash)) {
        route = cleanHash;
      }
    } else {
      for (const r of ['/editor', '/verbose', '/tree', '/wiki']) {
        if (path.endsWith(r) || path.endsWith(r + '/')) {
          route = r;
          break;
        }
      }
      if (path.includes('/wiki/')) {
        route = '/wiki';
        subSlug = path.split('/wiki/')[1].replace(/\/$/, '') || 'home';
      }
    }

    const params = url.searchParams;
    const hashDataMatch = hash.match(/data=(sp1_[A-Za-z0-9_-]+)/);
    return {
      route,
      subSlug,
      session: params.get('session') || params.get('token') || '',
      target: params.get('target') || '',
      api: params.get('api') || '',
      dataCode: hashDataMatch ? hashDataMatch[1] : ''
    };
  }

  function navigate(route, extraQuery = '') {
    let clean = route.startsWith('/') ? route : '/' + route;
    const baseNoSlash = BASE_PATH.replace(/\/$/, '');
    const targetPath = (clean === '/' ? BASE_PATH : baseNoSlash + clean) + extraQuery;
    try {
      window.history.pushState({}, '', targetPath);
    } catch {
      window.location.hash = '#' + clean + extraQuery;
    }
    STATE.route = clean.startsWith('/wiki/') ? '/wiki' : clean;
    if (clean.startsWith('/wiki/')) STATE.wikiSlug = clean.slice(6) || 'home';
    document.getElementById('tools-dropdown').classList.remove('open');
    render();
  }

  function updateModePill() {
    const pill = document.getElementById('session-mode-pill');
    if (STATE.route !== '/editor' && STATE.route !== '/verbose' && STATE.route !== '/tree') {
      pill.classList.add('hidden');
      return;
    }
    if (STATE.mode === 'live') {
      pill.textContent = 'LIVE SESSION';
      pill.className = 'mode-pill';
    } else if (STATE.mode === 'portable') {
      pill.textContent = 'PORTABLE SNAPSHOT';
      pill.className = 'mode-pill portable';
    } else if (STATE.mode === 'demo') {
      pill.textContent = 'DEMO';
      pill.className = 'mode-pill demo';
    } else {
      pill.classList.add('hidden');
    }
  }

  function formatDuration(secDelta) {
    if (!secDelta || secDelta <= 0) return 'never';
    const d = Math.floor(secDelta / 86400);
    const h = Math.floor((secDelta % 86400) / 3600);
    const m = Math.floor((secDelta % 3600) / 60);
    const parts = [];
    if (d) parts.push(`${d}d`);
    if (h) parts.push(`${h}h`);
    if (m) parts.push(`${m}m`);
    if (!parts.length) parts.push(`${secDelta}s`);
    return parts.join(' ');
  }

  function parseExpiryInput(raw) {
    const s = String(raw || '').trim().toLowerCase();
    if (!s || s === 'never' || s === '0') return 0;
    if (/^\d+$/.test(s)) return parseInt(s, 10);
    const m = s.match(/^(\d+)([smhdwy])$/);
    if (m) {
      const n = parseInt(m[1], 10);
      const mult = { s: 1, m: 60, h: 3600, d: 86400, w: 604800, y: 31536000 }[m[2]];
      return Math.floor(Date.now() / 1000) + n * mult;
    }
    const ts = Date.parse(raw);
    if (!Number.isNaN(ts)) return Math.floor(ts / 1000);
    return 0;
  }

  function formatExpiry(unixSec) {
    if (!unixSec || unixSec <= 0) return 'never';
    const rem = unixSec - Math.floor(Date.now() / 1000);
    if (rem <= 0) return 'expired';
    return 'in ' + formatDuration(rem);
  }

  function parseContextsString(raw) {
    const s = String(raw || '').trim();
    if (!s || s === 'none') return undefined;
    const out = {};
    for (const part of s.split(/[,\s]+/)) {
      if (!part) continue;
      const idx = part.indexOf('=');
      if (idx > 0) {
        const k = part.slice(0, idx).trim().toLowerCase();
        const v = part.slice(idx + 1).trim();
        if (k && v) out[k] = v;
      }
    }
    return Object.keys(out).length ? out : undefined;
  }

  function formatContexts(ctxs) {
    if (!ctxs || !Object.keys(ctxs).length) return '<span class="mono-chip">none</span>';
    return Object.entries(ctxs).map(([k, v]) =>
      `<span class="mono-chip"><span style="color:var(--text-muted)">${esc(k)}:</span> ${esc(v)}</span>`
    ).join(' ');
  }

  function loadEditorSession(sessionObj, mode, apiBase, token, targetHint = '') {
    const data = deepClone(sessionObj);
    data.groups = (data.groups || []).slice().sort((a, b) => (b.weight - a.weight) || a.name.localeCompare(b.name));
    data.tracks = (data.tracks || []).slice().sort((a, b) => a.name.localeCompare(b.name));
    data.users = (data.users || []).slice().sort((a, b) => (a.username || a.uuid).localeCompare(b.username || b.uuid));
    data.knownPermissions = data.knownPermissions || [];
    data.potentialContexts = data.potentialContexts || { server: ['lobby', 'survival'], world: ['world'] };

    STATE.editor = data;
    STATE.mode = mode;
    STATE.apiBase = apiBase;
    STATE.sessionToken = token;
    STATE.dirtyCount = 0;
    STATE.deletedGroups.clear();
    STATE.deletedTracks.clear();
    STATE.deletedUsers.clear();
    STATE.checkedRows.clear();

    const effectiveTarget = targetHint || (data.metadata && data.metadata.targetType && data.metadata.target ? `${data.metadata.targetType}:${data.metadata.target}` : '');
    if (effectiveTarget.startsWith('group:')) {
      const gn = effectiveTarget.slice(6).toLowerCase();
      if (data.groups.some(g => g.name === gn)) STATE.selected = { type: 'group', id: gn };
    } else if (effectiveTarget.startsWith('user:')) {
      const uid = effectiveTarget.slice(5).toLowerCase();
      const hit = data.users.find(u => u.uuid.toLowerCase() === uid || (u.username || '').toLowerCase() === uid);
      if (hit) STATE.selected = { type: 'user', id: hit.uuid };
    } else {
      STATE.selected = null;
    }
    if (data.tracks.length && Object.keys(STATE.expandedTracks).length === 0) {
      STATE.expandedTracks[data.tracks[0].name] = true;
    }
  }

  function markDirty() {
    STATE.dirtyCount++;
  }

  function buildSavePayload() {
    return {
      groups: deepClone(STATE.editor.groups),
      tracks: deepClone(STATE.editor.tracks),
      users: deepClone(STATE.editor.users),
      deletedGroups: Array.from(STATE.deletedGroups),
      deletedTracks: Array.from(STATE.deletedTracks),
      deletedUsers: Array.from(STATE.deletedUsers),
      baseRevision: (STATE.editor.metadata && STATE.editor.metadata.revision) || 0,
      actor: (STATE.editor.metadata && STATE.editor.metadata.actor) || 'StonyPermsWeb'
    };
  }

  function selectedEntity() {
    if (!STATE.editor || !STATE.selected) return null;
    if (STATE.selected.type === 'group') {
      return STATE.editor.groups.find(g => g.name === STATE.selected.id) || null;
    }
    return STATE.editor.users.find(u => u.uuid === STATE.selected.id) || null;
  }

  // Flatten entity properties/nodes into unified LuckPerms-style rows
  function buildUnifiedRows(entity, kind) {
    const rows = [];
    if (kind === 'group') {
      if (entity.displayName) {
        rows.push({
          rowId: 'displayname:0', badge: 'DISPLAY NAME', cls: 'type-displayname',
          key: entity.displayName, value: true, toggleable: false, expiry: 0, contexts: null,
          onDelete: () => { entity.displayName = ''; markDirty(); }
        });
      }
      if (typeof entity.weight === 'number' && entity.weight !== 0) {
        rows.push({
          rowId: 'weight:0', badge: 'WEIGHT', cls: 'type-weight',
          key: String(entity.weight), value: true, toggleable: false, expiry: 0, contexts: null,
          onDelete: () => { entity.weight = 0; markDirty(); }
        });
      }
    }
    (entity.parents || []).forEach((p, idx) => {
      rows.push({
        rowId: `parent:${idx}`, badge: 'INHERITANCE', cls: 'type-inheritance',
        key: p.group, value: true, toggleable: false, expiry: p.expiry || 0, contexts: p.contexts || null,
        onDelete: () => { entity.parents.splice(idx, 1); markDirty(); }
      });
    });
    (entity.prefixes || []).forEach((pm, idx) => {
      rows.push({
        rowId: `prefix:${idx}`, badge: 'PREFIX', cls: 'type-prefix',
        key: pm.value, extraNote: `(weight: ${pm.priority})`, value: true, toggleable: false,
        expiry: pm.expiry || 0, contexts: pm.contexts || null,
        onDelete: () => { entity.prefixes.splice(idx, 1); markDirty(); }
      });
    });
    (entity.suffixes || []).forEach((sm, idx) => {
      rows.push({
        rowId: `suffix:${idx}`, badge: 'SUFFIX', cls: 'type-suffix',
        key: sm.value, extraNote: `(weight: ${sm.priority})`, value: true, toggleable: false,
        expiry: sm.expiry || 0, contexts: sm.contexts || null,
        onDelete: () => { entity.suffixes.splice(idx, 1); markDirty(); }
      });
    });
    Object.entries(entity.meta || {}).forEach(([mk, mv]) => {
      rows.push({
        rowId: `meta:${mk}`, badge: 'META', cls: 'type-meta',
        key: `${mk} = ${mv}`, value: true, toggleable: false, expiry: 0, contexts: null,
        onDelete: () => { delete entity.meta[mk]; markDirty(); }
      });
    });
    (entity.nodes || []).forEach((n, idx) => {
      rows.push({
        rowId: `perm:${idx}`, badge: 'PERMISSION', cls: 'type-permission',
        key: n.key, value: Boolean(n.value), toggleable: true,
        expiry: n.expiry || 0, contexts: n.contexts || null, nodeRef: n,
        onToggle: () => { n.value = !n.value; markDirty(); },
        onDelete: () => { entity.nodes.splice(idx, 1); markDirty(); }
      });
    });
    return rows;
  }

  /* ==================== Views ==================== */

  function renderHome() {
    return `
      <div class="home-wrap">
        <section class="hero">
          <div class="hero-copy">
            <div class="hero-kicker">Minekube Gate · Paper · Fabric</div>
            <h1>StonyPerms &amp; StonyPermsWeb</h1>
            <p>Authoritative permissions engine for Gate proxies and backend Minecraft servers via <strong>StonyPermsBridge</strong>. Manage groups, inheritance trees, promotion tracks, contextual nodes, and verbose traces right from your browser.</p>
            <div class="hero-actions">
              <button type="button" class="btn btn-primary" id="home-open-demo">View Interactive Demo</button>
              <a href="#/editor" class="btn btn-secondary" data-route="/editor">Web Editor</a>
              <a href="#/verbose" class="btn btn-secondary" data-route="/verbose">Verbose Viewer</a>
              <a href="#/tree" class="btn btn-secondary" data-route="/tree">Permission Tree</a>
              <a href="#/wiki" class="btn btn-secondary" data-route="/wiki">Documentation</a>
            </div>
          </div>
          <div class="hero-logo-box"><img src="./favicon.svg" alt="StonyPerms Logo"></div>
        </section>

        <section class="feature-grid">
          <div class="feature-card">
            <h3>Web Permissions Editor</h3>
            <p>Visual LuckPermsWeb-style editor with one-click Live Save to Gate over HTTP API or portable <code>sp1_</code> edit codes.</p>
            <code>/sp editor [filter]</code>
          </div>
          <div class="feature-card">
            <h3>Verbose Check Recorder</h3>
            <p>Silently record live permission evaluations with wildcard filters and inspect every check and source online.</p>
            <code>/sp verbose record &amp;&amp; /sp verbose paste</code>
          </div>
          <div class="feature-card">
            <h3>Permission Tree Inspector</h3>
            <p>Explore Gate, plugin, Paper/Bukkit, and Fabric permission hierarchies in an expandable interactive tree.</p>
            <code>/sp tree [prefix]</code>
          </div>
          <div class="feature-card">
            <h3>StonyPermsBridge R/W</h3>
            <p>Sync resolved snapshots, SSE live updates, Vault prefix/suffix chat metadata, and <code>/spb editor</code> across Paper &amp; Fabric.</p>
            <code>/spb user &lt;player&gt; editor</code>
          </div>
          <div class="feature-card">
            <h3>Track Progressions</h3>
            <p>Define ordered promotion tracks (e.g. donors or staff) and promote/demote players atomically via Gate or API.</p>
            <code>/sp user &lt;player&gt; promote staff</code>
          </div>
          <div class="feature-card">
            <h3>Portable Offline Codes</h3>
            <p>Even with HTTP API disabled, open your complete snapshot in GitHub Pages and apply edits back in chat.</p>
            <code>/sp applyedits sp1_...</code>
          </div>
        </section>
      </div>
    `;
  }

  function renderSessionPromptCard({ title, commands, onDemoId, extraButtonId, extraButtonLabel }) {
    return `
      <div class="center-stage">
        <div class="session-card">
          <div class="session-card-head">
            <img src="./favicon.svg" alt="StonyPerms" class="session-card-logo">
            <div class="session-card-title">
              <h1>StonyPerms</h1>
              <p>${esc(title)}</p>
              <button type="button" class="btn btn-primary" id="${esc(onDemoId)}">View Demo</button>
            </div>
          </div>
          <p style="margin:0;color:var(--text-muted)">To start a new session, run one of these commands in Gate or Minecraft:</p>
          <ul>
            ${commands.map(c => `<li><code>${esc(c)}</code></li>`).join('')}
          </ul>
          <div class="session-card-actions">
            ${extraButtonId ? `<button type="button" class="btn btn-secondary btn-sm" id="${esc(extraButtonId)}">${esc(extraButtonLabel)}</button>` : ''}
            <a href="#/wiki" class="btn btn-secondary btn-sm" data-route="/wiki">Read Docs</a>
          </div>
        </div>
      </div>
    `;
  }

  function renderEditor() {
    if (!STATE.editor) {
      return renderSessionPromptCard({
        title: 'Web Permissions Editor',
        commands: ['/sp editor', '/sp user <user> editor', '/sp group <group> editor', '/spb editor'],
        onDemoId: 'btn-editor-demo',
        extraButtonId: 'btn-open-import',
        extraButtonLabel: 'Import JSON / sp1_ Code'
      });
    }

    const q = STATE.sidebarQuery.trim().toLowerCase();
    const tracks = STATE.editor.tracks.filter(t => !q || t.name.toLowerCase().includes(q) || t.groups.some(g => g.includes(q)));
    const groups = STATE.editor.groups.filter(g => !q || g.name.toLowerCase().includes(q) || (g.displayName || '').toLowerCase().includes(q));
    const users = STATE.editor.users.filter(u => !q || (u.username || '').toLowerCase().includes(q) || u.uuid.toLowerCase().includes(q));

    const entity = selectedEntity();
    let mainHtml = `<div class="empty-selection">← Choose a group or user from the side bar</div>`;

    if (entity) {
      const isGroup = STATE.selected.type === 'group';
      const rows = buildUnifiedRows(entity, STATE.selected.type);
      const nf = STATE.nodesFilter.trim().toLowerCase();
      const filteredRows = nf ? rows.filter(r => r.key.toLowerCase().includes(nf) || r.badge.toLowerCase().includes(nf)) : rows;

      mainHtml = `
        <div class="target-header">
          <div class="target-title-row">
            <h2>${isGroup ? 'Group:' : 'User:'}</h2>
            <span class="target-badge">${esc(isGroup ? (entity.displayName || entity.name) : (entity.username || entity.uuid))}</span>
            ${isGroup
              ? `<span class="target-sub">( ${esc(entity.name)} )</span>`
              : `<img class="user-head" style="width:28px;height:28px" src="https://mc-heads.net/avatar/${esc(entity.uuid || entity.username)}/28" data-name="${esc(entity.username)}" onerror="__spHeadFallback(this)" alt="">`
            }
            <div style="margin-left:auto;display:flex;gap:6px;">
              ${isGroup && entity.name !== 'default' ? `<button type="button" class="btn btn-danger btn-xs" id="btn-delete-entity">Delete Group</button>` : ''}
              ${!isGroup ? `<button type="button" class="btn btn-danger btn-xs" id="btn-delete-entity">Delete User</button>` : ''}
            </div>
          </div>

          <div class="target-meta-grid">
            ${isGroup ? `
              <div class="meta-line">
                <span class="meta-line-label">Weight:</span>
                <input type="number" id="input-group-weight" class="dock-input" style="height:26px;width:90px;font-family:var(--mono)" value="${esc(entity.weight)}">
                <span class="meta-line-label" style="min-width:auto;margin-left:14px">Display Name:</span>
                <input type="text" id="input-group-display" class="dock-input" style="height:26px;width:160px" value="${esc(entity.displayName || '')}" placeholder="(none)">
              </div>
            ` : `
              <div class="meta-line">
                <span class="meta-line-label">Primary group:</span>
                <select id="select-user-primary" class="dock-select" style="height:28px;font-family:var(--mono)">
                  ${STATE.editor.groups.map(g => `<option value="${esc(g.name)}" ${entity.primaryGroup === g.name ? 'selected' : ''}>${esc(g.name)}</option>`).join('')}
                </select>
                <span class="mono-chip" title="Player UUID">${esc(entity.uuid)}</span>
              </div>
            `}
            <div class="meta-line">
              <span class="meta-line-label">Parent groups:</span>
              <button type="button" class="add-parent-btn" id="btn-add-parent" title="Add parent group">+</button>
              ${(entity.parents || []).map((p, idx) => `
                <button type="button" class="chip-btn" data-remove-parent="${idx}" title="Remove parent ${esc(p.group)}">
                  ${esc(p.group)} <span class="x">×</span>
                </button>
              `).join('') || '<span style="color:var(--text-dim)">none</span>'}
            </div>
          </div>
        </div>

        <div class="nodes-hdr">
          <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
            <h3>Nodes <span style="color:var(--text-muted);font-weight:500">(${rows.length})</span></h3>
            ${STATE.checkedRows.size > 0 ? `
              <button type="button" class="btn btn-danger btn-xs" id="btn-bulk-delete">Delete selected (${STATE.checkedRows.size})</button>
            ` : ''}
          </div>
          <div class="nodes-tools">
            <input type="text" id="input-nodes-filter" class="nodes-filter-input" value="${esc(STATE.nodesFilter)}" placeholder="Filter nodes...">
          </div>
        </div>

        <table class="nodes-table">
          <thead>
            <tr>
              <th style="width:36px"><input type="checkbox" id="chk-all-rows" ${filteredRows.length && filteredRows.every(r => STATE.checkedRows.has(r.rowId)) ? 'checked' : ''}></th>
              <th>Node</th>
              <th style="width:90px">Value</th>
              <th style="width:120px">Expiry</th>
              <th>Contexts</th>
              <th style="width:38px"></th>
            </tr>
          </thead>
          <tbody>
            ${filteredRows.map(r => `
              <tr data-rowid="${esc(r.rowId)}">
                <td><input type="checkbox" class="row-chk" data-rowid="${esc(r.rowId)}" ${STATE.checkedRows.has(r.rowId) ? 'checked' : ''}></td>
                <td>
                  <div class="node-cell">
                    <span class="badge-pill ${esc(r.cls)}">${esc(r.badge)}</span>
                    <span class="node-key">${esc(r.key)}</span>
                    ${r.extraNote ? `<span class="node-extra-note">${esc(r.extraNote)}</span>` : ''}
                  </div>
                </td>
                <td>
                  ${r.toggleable
                    ? `<button type="button" class="val-pill ${r.value ? 'val-true' : 'val-false'}" data-toggle-row="${esc(r.rowId)}">${r.value ? 'true' : 'false'}</button>`
                    : `<span class="val-pill val-true" style="opacity:0.75">true</span>`
                  }
                </td>
                <td><span style="font-family:var(--mono);font-size:12px;color:${r.expiry ? 'var(--text)' : 'var(--text-dim)'}">${esc(formatExpiry(r.expiry))}</span></td>
                <td>${formatContexts(r.contexts)}</td>
                <td><button type="button" class="row-del-btn" data-del-row="${esc(r.rowId)}" title="Delete node">×</button></td>
              </tr>
            `).join('') || `<tr><td colspan="6" style="color:var(--text-dim);text-align:center;padding:24px">No matching nodes</td></tr>`}
          </tbody>
        </table>
      `;
    }

    const suggestions = computeAutocomplete(STATE.addForm.key);

    return `
      <div class="editor-shell">
        <aside class="editor-sidebar">
          <div class="sidebar-search">
            <input type="text" id="input-sidebar-search" value="${esc(STATE.sidebarQuery)}" placeholder="Search">
          </div>
          <div class="sidebar-scroll">
            <div class="sidebar-section-hdr">
              <button type="button" class="sidebar-section-toggle" data-toggle-sec="tracks">
                <span>${STATE.expandedSections.tracks ? '▾' : '▸'}</span>
                <span>TRACKS <span class="sidebar-count">(${tracks.length})</span></span>
              </button>
              <button type="button" class="sidebar-plus" id="btn-create-track" title="Create track">+</button>
            </div>
            ${STATE.expandedSections.tracks ? `
              <div class="sidebar-list">
                ${tracks.map(t => {
                  const open = !!STATE.expandedTracks[t.name];
                  return `
                    <div class="track-node">
                      <button type="button" class="track-hdr" data-toggle-track="${esc(t.name)}">
                        <span>${open ? '▾' : '▸'} ${esc(t.name.toUpperCase())}</span>
                        <span class="btn btn-secondary btn-xs" data-edit-track="${esc(t.name)}" style="font-size:10px;padding:1px 6px">edit</span>
                      </button>
                      ${open ? `
                        <div class="track-groups">
                          ${t.groups.map(gn => `
                            <button type="button" class="track-group-btn" data-select-group="${esc(gn)}">${esc(gn)}</button>
                          `).join('') || '<div style="font-size:11px;color:var(--text-dim);padding:4px 12px">(empty)</div>'}
                        </div>
                      ` : ''}
                    </div>
                  `;
                }).join('')}
              </div>
            ` : ''}

            <div class="sidebar-section-hdr">
              <button type="button" class="sidebar-section-toggle" data-toggle-sec="groups">
                <span>${STATE.expandedSections.groups ? '▾' : '▸'}</span>
                <span>GROUPS <span class="sidebar-count">(${groups.length})</span></span>
              </button>
              <button type="button" class="sidebar-plus" id="btn-create-group" title="Create group">+</button>
            </div>
            ${STATE.expandedSections.groups ? `
              <div class="sidebar-list">
                ${groups.map(g => {
                  const active = STATE.selected && STATE.selected.type === 'group' && STATE.selected.id === g.name;
                  return `
                    <button type="button" class="sidebar-item ${active ? 'active' : ''}" data-select-group="${esc(g.name)}">
                      <div class="sidebar-item-main">
                        <span class="sidebar-item-title">${esc(g.displayName || g.name)}</span>
                        ${g.displayName && g.displayName !== g.name ? `<span class="sidebar-item-sub">(${esc(g.name)})</span>` : ''}
                      </div>
                      <span class="sidebar-item-weight">${esc(g.weight)}</span>
                    </button>
                  `;
                }).join('')}
              </div>
            ` : ''}

            <div class="sidebar-section-hdr">
              <button type="button" class="sidebar-section-toggle" data-toggle-sec="users">
                <span>${STATE.expandedSections.users ? '▾' : '▸'}</span>
                <span>USERS <span class="sidebar-count">(${users.length})</span></span>
              </button>
              <button type="button" class="sidebar-plus" id="btn-create-user" title="Add user">+</button>
            </div>
            ${STATE.expandedSections.users ? `
              <div class="sidebar-list">
                ${users.map(u => {
                  const active = STATE.selected && STATE.selected.type === 'user' && STATE.selected.id === u.uuid;
                  return `
                    <button type="button" class="user-row ${active ? 'active' : ''}" data-select-user="${esc(u.uuid)}">
                      <img class="user-head" src="https://mc-heads.net/avatar/${esc(u.uuid || u.username)}/24" data-name="${esc(u.username)}" onerror="__spHeadFallback(this)" alt="">
                      <span class="sidebar-item-title">${esc(u.username || u.uuid)}</span>
                    </button>
                  `;
                }).join('')}
              </div>
            ` : ''}
          </div>
        </aside>

        <section class="editor-main">
          <div class="editor-topbar">
            <div class="editor-topbar-left">
              <span class="editor-topbar-title">Web Permissions Editor</span>
              ${STATE.dirtyCount > 0 ? `<span class="mono-chip" style="color:var(--accent)">${STATE.dirtyCount} unsaved edit${STATE.dirtyCount === 1 ? '' : 's'}</span>` : ''}
            </div>
            <div class="editor-topbar-actions">
              <button type="button" class="btn btn-secondary btn-sm" id="btn-open-import">Import / Export</button>
              <button type="button" class="icon-square-btn" id="btn-editor-quicksearch" title="Search (Ctrl+K)">⌕</button>
              <button type="button" class="btn btn-primary btn-sm" id="btn-save-editor">💾 Save</button>
            </div>
          </div>

          <div class="editor-body">${mainHtml}</div>

          ${entity ? `
            <div class="add-dock">
              <div class="dock-col">
                <span class="dock-label">Type</span>
                <select id="dock-type" class="dock-select">
                  ${['PERMISSION', 'INHERITANCE', 'PREFIX', 'SUFFIX', 'WEIGHT', 'DISPLAY NAME', 'META'].map(t =>
                    `<option value="${t}" ${STATE.addForm.type === t ? 'selected' : ''}>${t}</option>`
                  ).join('')}
                </select>
              </div>

              ${STATE.addForm.type === 'META' ? `
                <div class="dock-col" style="width:140px">
                  <span class="dock-label">Meta Key</span>
                  <input type="text" id="dock-meta-key" class="dock-input" value="${esc(STATE.addForm.metaKey)}" placeholder="key">
                </div>
              ` : ''}

              ${(STATE.addForm.type === 'PREFIX' || STATE.addForm.type === 'SUFFIX') ? `
                <div class="dock-col" style="width:84px">
                  <span class="dock-label">Priority</span>
                  <input type="number" id="dock-priority" class="dock-input" value="${esc(STATE.addForm.priority)}">
                </div>
              ` : ''}

              <div class="dock-col dock-col-grow">
                <span class="dock-label">${STATE.addForm.type === 'PERMISSION' ? 'Permissions' : 'Value'}</span>
                <input type="text" id="dock-key" class="dock-input" autocomplete="off"
                  value="${esc(STATE.addForm.key)}"
                  placeholder="${STATE.addForm.type === 'PERMISSION' ? 'Enter permissions or paste many' : 'Enter value'}">
                ${STATE.addForm.type === 'PERMISSION' && STATE.acOpen && suggestions.length ? `
                  <div class="autocomplete-popover" id="ac-popover">
                    ${suggestions.map((perm, idx) => `
                      <div class="ac-item ${idx === STATE.acIndex ? 'selected' : ''}" data-ac-val="${esc(perm)}">
                        <span>${esc(perm)}</span>
                        ${idx === STATE.acIndex ? `<span class="ac-hint">Press enter to select</span>` : ''}
                      </div>
                    `).join('')}
                  </div>
                ` : ''}
              </div>

              ${STATE.addForm.type === 'PERMISSION' ? `
                <div class="dock-col">
                  <span class="dock-label">Value</span>
                  <button type="button" id="dock-value-btn" class="dock-value-btn ${STATE.addForm.value ? 'val-true' : 'val-false'}">${STATE.addForm.value ? 'true' : 'false'}</button>
                </div>
              ` : ''}

              <div class="dock-col" style="width:105px">
                <span class="dock-label">Expiry</span>
                <input type="text" id="dock-expiry" class="dock-input" value="${esc(STATE.addForm.expiry)}" placeholder="never">
              </div>

              <div class="dock-col" style="min-width:155px">
                <span class="dock-label">Contexts <button type="button" class="add-parent-btn" style="width:16px;height:16px;font-size:12px" id="btn-dock-ctx">+</button></span>
                <div style="display:flex;flex-wrap:wrap;gap:4px;min-height:36px;align-items:center">
                  ${Object.entries(STATE.addForm.contexts).map(([ck, cv]) =>
                    `<button type="button" class="chip-btn" data-del-dock-ctx="${esc(ck)}">${esc(ck)}: ${esc(cv)} <span class="x">×</span></button>`
                  ).join('') || '<span style="color:var(--text-dim);font-size:12px">none</span>'}
                </div>
              </div>

              <button type="button" id="btn-dock-add" class="dock-add-btn"><span>+</span><span>Add</span></button>
            </div>
          ` : ''}
        </section>
      </div>
    `;
  }

  function computeAutocomplete(input) {
    if (!STATE.editor) return [];
    const perms = STATE.editor.knownPermissions || [];
    const tokens = String(input || '').trim().split(/[\s,]+/);
    const q = (tokens[tokens.length - 1] || '').toLowerCase();
    if (!q) return perms.slice(0, 12);
    return perms.filter(p => p.toLowerCase().includes(q)).slice(0, 14);
  }

  function renderVerbose() {
    if (!STATE.verbose) {
      return renderSessionPromptCard({
        title: 'Verbose Viewer',
        commands: ['/sp verbose record [filter]', '/sp verbose paste', '/spb verbose record [filter]'],
        onDemoId: 'btn-verbose-demo'
      });
    }
    const meta = STATE.verbose.metadata || {};
    const fq = STATE.verboseFilter.trim().toLowerCase();
    const entries = (STATE.verbose.entries || []).filter(e => {
      const res = (e.result || 'undefined').toLowerCase();
      if (STATE.verboseExclude[res]) return false;
      if (!fq) return true;
      return (e.subject || '').toLowerCase().includes(fq) || (e.permission || '').toLowerCase().includes(fq);
    });
    const durSec = Math.max(0, Math.round((meta.durationMs || 0) / 1000));

    return `
      <div class="viewer-layout">
        <aside class="viewer-aside">
          <h2>Verbose viewer</h2>
          <div class="meta-card">
            <span class="k">Uploaded by</span><span class="v">${esc(meta.uploadedBy || 'Console')}</span>
            <span class="k">Start time</span><span class="v">${esc(meta.startTime ? new Date(meta.startTime).toISOString().replace('T',' ').slice(0,19)+' UTC' : '-')}</span>
            <span class="k">End time</span><span class="v">${esc(meta.endTime ? new Date(meta.endTime).toISOString().replace('T',' ').slice(0,19)+' UTC' : '-')}</span>
            <span class="k">Duration</span><span class="v">${durSec}s</span>
            <span class="k">Count</span><span class="v">${entries.length} / ${meta.totalCount ?? (STATE.verbose.entries || []).length}</span>
            <span class="k">Filter</span><span class="v"><span class="mono-chip">${esc(meta.filter || 'any')}</span></span>
            <span class="k">Truncated</span><span class="v"><span class="val-pill ${meta.truncated ? 'val-true' : 'val-false'}">${meta.truncated ? 'true' : 'false'}</span></span>
          </div>

          <div class="field-group">
            <label>Filter</label>
            <input type="text" id="input-verbose-filter" class="field-input" value="${esc(STATE.verboseFilter)}" placeholder="Enter filter here">
          </div>

          <div>
            ${['true', 'false', 'undefined'].map(kind => `
              <label class="filter-check-row">
                <input type="checkbox" data-verbose-ex="${kind}" ${STATE.verboseExclude[kind] ? 'checked' : ''}>
                <span>Exclude</span>
                <span class="val-pill val-${kind}">${kind}</span>
              </label>
            `).join('')}
          </div>
        </aside>

        <section class="viewer-main">
          ${entries.map((e, idx) => {
            const res = (e.result || 'undefined').toLowerCase();
            const mark = res === 'true' ? '✔' : res === 'false' ? '✘' : '—';
            const open = STATE.verboseExpanded.has(idx);
            return `
              <div class="verbose-row" data-verbose-idx="${idx}">
                <span class="verbose-subj">${esc(e.subject || 'internal')}</span>
                <span class="verbose-perm"><span>${esc(e.permission)}</span></span>
                <span class="val-pill val-${esc(res)}">${esc(res)} ${mark}</span>
                ${open ? `<div class="verbose-trace">Source: ${esc(e.source || 'none')} · Timestamp: ${esc(e.timestamp ? new Date(e.timestamp).toISOString() : '-')}</div>` : ''}
              </div>
            `;
          }).join('') || '<div style="padding:32px;color:var(--text-dim);text-align:center">No recorded checks match your filter.</div>'}
        </section>
      </div>
    `;
  }

  function buildPermTree(perms) {
    const root = new Map();
    for (const full of perms) {
      const parts = full.split('.');
      let cur = root;
      let acc = '';
      parts.forEach((seg, i) => {
        acc = acc ? acc + '.' + seg : seg;
        if (!cur.has(acc)) cur.set(acc, { path: acc, isLeafPerm: false, children: new Map() });
        const node = cur.get(acc);
        if (i === parts.length - 1) node.isLeafPerm = true;
        cur = node.children;
      });
    }
    return root;
  }

  function renderTreeBranch(map) {
    let html = '';
    for (const node of map.values()) {
      const hasChildren = node.children.size > 0;
      if (hasChildren) {
        const collapsed = STATE.treeCollapsed.has(node.path);
        html += `
          <div>
            <div class="tree-row tree-branch" data-tree-toggle="${esc(node.path)}">
              <span>${collapsed ? '▸' : '▾'} ${esc(node.path)}</span>
              <span style="color:var(--text-dim);font-size:11px">${node.children.size}</span>
            </div>
            ${collapsed ? '' : `<div class="tree-children">${renderTreeBranch(node.children)}</div>`}
          </div>
        `;
      } else {
        html += `
          <div class="tree-row" data-copy-node="${esc(node.path)}" title="Click to copy">
            <span>${esc(node.path)}</span>
            <span style="color:var(--text-dim);font-size:11px">copy</span>
          </div>
        `;
      }
    }
    return html;
  }

  function renderTree() {
    if (!STATE.tree) {
      return renderSessionPromptCard({
        title: 'Permission Tree Viewer',
        commands: ['/sp tree', '/sp tree gate', '/spb tree'],
        onDemoId: 'btn-tree-demo'
      });
    }
    const meta = STATE.tree.metadata || {};
    const fq = STATE.treeFilter.trim().toLowerCase();
    const perms = (STATE.tree.permissions || []).filter(p => !fq || p.toLowerCase().includes(fq));
    const treeMap = buildPermTree(perms);

    return `
      <div class="viewer-layout">
        <aside class="viewer-aside">
          <h2>Permission Tree Viewer</h2>
          <div class="meta-card">
            <span class="k">Uploaded by</span><span class="v">${esc(meta.uploadedBy || 'Console')}</span>
            <span class="k">Time</span><span class="v">${esc(meta.time ? new Date(meta.time).toISOString().replace('T',' ').slice(0,19)+' UTC' : '-')}</span>
            <span class="k">Root</span><span class="v"><span class="mono-chip">${esc(meta.root || '.')}</span></span>
            <span class="k">Nodes</span><span class="v">${perms.length}</span>
          </div>

          <div style="display:flex;gap:8px;flex-wrap:wrap">
            <button type="button" class="btn btn-secondary btn-sm" id="btn-tree-expand">+ Expand</button>
            <button type="button" class="btn btn-secondary btn-sm" id="btn-tree-collapse">- Collapse</button>
          </div>

          <div class="field-group">
            <label>Filter Nodes</label>
            <input type="text" id="input-tree-filter" class="field-input" value="${esc(STATE.treeFilter)}" placeholder="e.g. gate.command">
          </div>
        </aside>

        <section class="viewer-main">
          ${renderTreeBranch(treeMap) || '<div style="padding:32px;color:var(--text-dim);text-align:center">No permissions matched.</div>'}
        </section>
      </div>
    `;
  }

  function renderWiki() {
    const docs = window.STONYPERMS_WIKI || [];
    const current = docs.find(d => d.slug === STATE.wikiSlug) || docs[0];
    const cats = [];
    for (const d of docs) {
      if (!cats.includes(d.category)) cats.push(d.category);
    }

    return `
      <div class="wiki-layout">
        <aside class="wiki-sidebar">
          ${cats.map(cat => `
            <div class="wiki-cat">${esc(cat)}</div>
            ${docs.filter(d => d.category === cat).map(d => `
              <button type="button" class="wiki-link ${d.slug === current.slug ? 'active' : ''}" data-wiki-slug="${esc(d.slug)}">${esc(d.title)}</button>
            `).join('')}
          `).join('')}
        </aside>
        <article class="wiki-article">${current ? current.html : 'Not found'}</article>
      </div>
    `;
  }

  /* ==================== Modals ==================== */

  function openModal(title, bodyHtml, footerHtml) {
    const root = document.getElementById('modal-root');
    root.innerHTML = `
      <div class="modal-box">
        <div class="modal-hdr">
          <h3>${esc(title)}</h3>
          <button type="button" class="row-del-btn" id="modal-close-x">×</button>
        </div>
        <div class="modal-body">${bodyHtml}</div>
        <div class="modal-ftr">${footerHtml || '<button type="button" class="btn btn-secondary btn-sm" id="modal-close-btn">Close</button>'}</div>
      </div>
    `;
    root.classList.remove('hidden');
    const closeBtn = document.getElementById('modal-close-btn');
    const closeX = document.getElementById('modal-close-x');
    if (closeBtn) closeBtn.onclick = closeModal;
    if (closeX) closeX.onclick = closeModal;
  }

  function closeModal() {
    const root = document.getElementById('modal-root');
    root.classList.add('hidden');
    root.innerHTML = '';
  }

  function openSearchModal() {
    const docs = window.STONYPERMS_WIKI || [];
    const items = [];
    items.push({ kind: 'TOOL', title: 'Web Permissions Editor', sub: '/sp editor', act: () => navigate('/editor') });
    items.push({ kind: 'TOOL', title: 'Verbose Viewer', sub: '/sp verbose paste', act: () => navigate('/verbose') });
    items.push({ kind: 'TOOL', title: 'Permission Tree Viewer', sub: '/sp tree', act: () => navigate('/tree') });
    items.push({ kind: 'DEMO', title: 'Open Interactive Demo Editor', sub: 'Loads sample groups, tracks & users', act: () => { loadEditorSession(window.STONYPERMS_DEMO.editor, 'demo', '', ''); navigate('/editor'); } });
    docs.forEach(d => items.push({ kind: 'WIKI', title: d.title, sub: d.category, act: () => { STATE.wikiSlug = d.slug; navigate('/wiki'); } }));
    if (STATE.editor) {
      STATE.editor.groups.forEach(g => items.push({ kind: 'GROUP', title: g.displayName || g.name, sub: `${g.name} · weight ${g.weight}`, act: () => { STATE.selected = { type: 'group', id: g.name }; navigate('/editor'); } }));
      STATE.editor.users.forEach(u => items.push({ kind: 'USER', title: u.username || u.uuid, sub: u.uuid, act: () => { STATE.selected = { type: 'user', id: u.uuid }; navigate('/editor'); } }));
    }

    openModal('Quick Search (Ctrl+K)', `
      <input type="text" id="global-search-input" class="field-input" placeholder="Search wiki pages, groups, users, tools...">
      <div id="global-search-results" style="display:flex;flex-direction:column;gap:4px;max-height:340px;overflow-y:auto"></div>
    `, '');

    const input = document.getElementById('global-search-input');
    const list = document.getElementById('global-search-results');
    const draw = () => {
      const q = input.value.trim().toLowerCase();
      const hits = items.filter(it => !q || it.title.toLowerCase().includes(q) || it.sub.toLowerCase().includes(q)).slice(0, 20);
      list.innerHTML = hits.map((it, idx) => `
        <button type="button" class="dropdown-item" data-gs-idx="${idx}" style="border:1px solid var(--border)">
          <div style="display:flex;justify-content:space-between;width:100%">
            <strong>${esc(it.title)}</strong>
            <span class="badge-pill type-permission">${esc(it.kind)}</span>
          </div>
          <span>${esc(it.sub)}</span>
        </button>
      `).join('') || '<div style="color:var(--text-dim);padding:12px">No matches found</div>';
      list.querySelectorAll('[data-gs-idx]').forEach(btn => {
        btn.onclick = () => {
          closeModal();
          hits[Number(btn.dataset.gsIdx)].act();
        };
      });
    };
    input.oninput = draw;
    draw();
    input.focus();
  }

  function openDownloadModal() {
    openModal('Download StonyPerms Suite', `
      <p style="margin:0;color:var(--text-muted)">Deploy the Minekube Gate proxy module and backend bridge plugins:</p>
      <div class="feature-card" style="padding:14px">
        <h3>gate-stonyperms (Minekube Gate Proxy + StonyPermsWeb)</h3>
        <p>Core permission engine, JSON storage, HTTP API, CLI commands &amp; embedded web UI.</p>
        <a href="https://github.com/andreisugu/gate-stonyperms/releases" target="_blank" rel="noopener noreferrer" class="btn btn-primary btn-sm">View Gate Releases</a>
      </div>
      <div class="feature-card" style="padding:14px">
        <h3>stonyperms-bridge (Paper / Folia / Fabric)</h3>
        <p>Cross-server permission &amp; Vault metadata bridge with <code>/spb</code> commands.</p>
        <a href="https://github.com/andreisugu/stonyperms-bridge/releases" target="_blank" rel="noopener noreferrer" class="btn btn-primary btn-sm">View Bridge Releases</a>
      </div>
    `);
  }

  async function openSaveModal() {
    const payload = buildSavePayload();
    let liveResult = null;
    let liveErr = '';

    if (STATE.mode === 'live' && STATE.apiBase && STATE.sessionToken) {
      try {
        const resp = await fetch(`${STATE.apiBase}/v1/editor/sessions/${encodeURIComponent(STATE.sessionToken)}/save`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const json = await resp.json();
        if (!resp.ok) throw new Error(json.message || `HTTP ${resp.status}`);
        liveResult = json;
        STATE.dirtyCount = 0;
        if (STATE.editor.metadata) STATE.editor.metadata.revision = json.revision;
        render();
      } catch (e) {
        liveErr = e.message || String(e);
      }
    }

    let applyCode = (liveResult && liveResult.applyCode) || '';
    let portableCode = '';
    try {
      portableCode = await encodePortableSp1(payload);
    } catch {
      portableCode = '';
    }
    const preferredCode = portableCode || applyCode;
    const spCmd = preferredCode ? `/sp applyedits ${preferredCode}` : '';
    const spbCmd = preferredCode ? `/spb applyedits ${preferredCode}` : '';

    let banner = '';
    if (liveResult) {
      banner = `<div class="val-pill val-true" style="padding:10px 12px;width:100%;justify-content:flex-start">
        ✔ Saved live to Gate (Revision #${liveResult.revision}: ${liveResult.groupsUpdated} groups updated, ${liveResult.tracksUpdated} tracks, ${liveResult.usersUpdated} users)
      </div>`;
    } else if (STATE.mode === 'demo') {
      banner = `<div class="val-pill val-undefined" style="padding:10px 12px;width:100%;justify-content:flex-start">
        Demo Session — Copy the portable command below or export JSON to apply to a real server.
      </div>`;
    } else if (liveErr) {
      banner = `<div class="val-pill val-false" style="padding:10px 12px;width:100%;justify-content:flex-start">
        Live save failed (${esc(liveErr)}) — use /sp applyedits below as fallback!
      </div>`;
    } else {
      banner = `<div class="val-pill val-true" style="padding:10px 12px;width:100%;justify-content:flex-start">
        Run the command below in Minecraft or Gate console to apply your changes:
      </div>`;
    }

    openModal('Save Web Editor Changes', `
      ${banner}
      ${spCmd ? `
        <div class="field-group">
          <label>Gate Command (/sp applyedits)</label>
          <div style="display:flex;gap:8px">
            <input type="text" readonly class="field-input" style="font-family:var(--mono)" id="save-sp-cmd" value="${esc(spCmd)}">
            <button type="button" class="btn btn-primary btn-sm" id="btn-copy-sp">Copy</button>
          </div>
        </div>
        <div class="field-group">
          <label>Backend Bridge Command (/spb applyedits)</label>
          <div style="display:flex;gap:8px">
            <input type="text" readonly class="field-input" style="font-family:var(--mono)" id="save-spb-cmd" value="${esc(spbCmd)}">
            <button type="button" class="btn btn-secondary btn-sm" id="btn-copy-spb">Copy</button>
          </div>
        </div>
      ` : ''}
      <div class="field-group">
        <label>Backup / Transfer JSON</label>
        <button type="button" class="btn btn-secondary btn-sm" id="btn-download-json">Download Snapshot JSON</button>
      </div>
    `);

    const copyText = (id, msg) => {
      const el = document.getElementById(id);
      if (!el) return;
      navigator.clipboard.writeText(el.value).then(() => toast(msg)).catch(() => { el.select(); toast('Selected command'); });
    };
    const btnSp = document.getElementById('btn-copy-sp');
    if (btnSp) btnSp.onclick = () => copyText('save-sp-cmd', 'Copied /sp applyedits command!');
    const btnSpb = document.getElementById('btn-copy-spb');
    if (btnSpb) btnSpb.onclick = () => copyText('save-spb-cmd', 'Copied /spb applyedits command!');
    const btnJson = document.getElementById('btn-download-json');
    if (btnJson) btnJson.onclick = () => {
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = 'stonyperms-edits.json';
      a.click();
    };
  }

  function openImportExportModal() {
    openModal('Import / Export Snapshot', `
      <div class="field-group">
        <label>Paste sp1_ code or JSON Snapshot</label>
        <textarea id="import-text" class="field-textarea" placeholder="Paste sp1_... code or JSON snapshot here..."></textarea>
      </div>
    `, `
      <button type="button" class="btn btn-secondary btn-sm" id="btn-export-now">Export Current JSON</button>
      <button type="button" class="btn btn-primary btn-sm" id="btn-do-import">Load Into Editor</button>
    `);

    document.getElementById('btn-export-now').onclick = () => {
      const json = JSON.stringify(STATE.editor || buildSavePayload(), null, 2);
      document.getElementById('import-text').value = json;
      toast('Exported editor state to text box');
    };
    document.getElementById('btn-do-import').onclick = async () => {
      const raw = document.getElementById('import-text').value.trim();
      if (!raw) return;
      try {
        const parsed = raw.startsWith('sp1_') ? await decodePortableSp1(raw) : JSON.parse(raw);
        if (!parsed.groups && !parsed.users) throw new Error('Missing groups or users arrays');
        const wrapped = parsed.metadata ? parsed : {
          token: 'imported',
          metadata: { commandAlias: 'sp', actor: 'Imported', defaultGroup: 'default', liveSave: false },
          groups: parsed.groups || [],
          tracks: parsed.tracks || [],
          users: parsed.users || [],
          knownPermissions: (window.STONYPERMS_DEMO.editor.knownPermissions || []),
          potentialContexts: (window.STONYPERMS_DEMO.editor.potentialContexts || {})
        };
        loadEditorSession(wrapped, 'portable', '', '');
        closeModal();
        render();
        toast('Snapshot loaded cleanly!');
      } catch (err) {
        toast('Import failed: ' + err.message, true);
      }
    };
  }

  function openCreateGroupModal() {
    openModal('Create New Group', `
      <div class="field-group"><label>Name (lowercase a-z0-9_+-)</label><input id="cg-name" class="field-input" placeholder="e.g. mvip"></div>
      <div class="field-group"><label>Weight</label><input id="cg-weight" type="number" class="field-input" value="20"></div>
      <div class="field-group"><label>Display Name (optional)</label><input id="cg-display" class="field-input" placeholder="e.g. MVIP"></div>
    `, `<button type="button" class="btn btn-primary btn-sm" id="cg-submit">Create Group</button>`);
    document.getElementById('cg-submit').onclick = () => {
      const name = document.getElementById('cg-name').value.trim().toLowerCase();
      if (!/^[a-z0-9_+-]{1,64}$/.test(name)) return toast('Invalid group name', true);
      if (STATE.editor.groups.some(g => g.name === name)) return toast('Group already exists', true);
      const weight = parseInt(document.getElementById('cg-weight').value || '0', 10) || 0;
      const displayName = document.getElementById('cg-display').value.trim();
      STATE.editor.groups.push({ name, displayName, weight, parents: [], nodes: [], meta: {}, prefixes: [], suffixes: [] });
      STATE.editor.groups.sort((a, b) => (b.weight - a.weight) || a.name.localeCompare(b.name));
      STATE.deletedGroups.delete(name);
      STATE.selected = { type: 'group', id: name };
      markDirty();
      closeModal();
      render();
    };
  }

  function openCreateTrackModal() {
    openModal('Create Promotion Track', `
      <div class="field-group"><label>Track Name</label><input id="ct-name" class="field-input" placeholder="e.g. builders"></div>
      <div class="field-group"><label>Ordered Groups (comma-separated)</label><input id="ct-groups" class="field-input" placeholder="default, vip, vipplus"></div>
    `, `<button type="button" class="btn btn-primary btn-sm" id="ct-submit">Create Track</button>`);
    document.getElementById('ct-submit').onclick = () => {
      const name = document.getElementById('ct-name').value.trim().toLowerCase();
      if (!/^[a-z0-9_+-]{1,64}$/.test(name)) return toast('Invalid track name', true);
      if (STATE.editor.tracks.some(t => t.name === name)) return toast('Track already exists', true);
      const groups = document.getElementById('ct-groups').value.split(/[,\s]+/).map(s => s.trim().toLowerCase()).filter(Boolean);
      for (const gn of groups) {
        if (!STATE.editor.groups.some(g => g.name === gn)) return toast(`Unknown group: ${gn}`, true);
      }
      STATE.editor.tracks.push({ name, groups });
      STATE.expandedTracks[name] = true;
      STATE.deletedTracks.delete(name);
      markDirty();
      closeModal();
      render();
    };
  }

  function openEditTrackModal(trackName) {
    const tr = STATE.editor.tracks.find(t => t.name === trackName);
    if (!tr) return;
    openModal(`Edit Track: ${tr.name}`, `
      <div class="field-group">
        <label>Ordered Groups (comma-separated)</label>
        <input id="et-groups" class="field-input" value="${esc(tr.groups.join(', '))}">
      </div>
    `, `
      <button type="button" class="btn btn-danger btn-sm" id="et-delete">Delete Track</button>
      <button type="button" class="btn btn-primary btn-sm" id="et-save">Save Track</button>
    `);
    document.getElementById('et-delete').onclick = () => {
      STATE.editor.tracks = STATE.editor.tracks.filter(t => t.name !== tr.name);
      STATE.deletedTracks.add(tr.name);
      markDirty();
      closeModal();
      render();
    };
    document.getElementById('et-save').onclick = () => {
      const groups = document.getElementById('et-groups').value.split(/[,\s]+/).map(s => s.trim().toLowerCase()).filter(Boolean);
      for (const gn of groups) {
        if (!STATE.editor.groups.some(g => g.name === gn)) return toast(`Unknown group: ${gn}`, true);
      }
      tr.groups = groups;
      markDirty();
      closeModal();
      render();
    };
  }

  function openCreateUserModal() {
    openModal('Add User', `
      <div class="field-group"><label>Username</label><input id="cu-name" class="field-input" placeholder="e.g. Notch"></div>
      <div class="field-group"><label>UUID (optional — auto-derived offline if empty)</label><input id="cu-uuid" class="field-input" placeholder="00000000-0000-0000-0000-000000000000"></div>
    `, `<button type="button" class="btn btn-primary btn-sm" id="cu-submit">Add User</button>`);
    document.getElementById('cu-submit').onclick = () => {
      const username = document.getElementById('cu-name').value.trim();
      const rawUuid = document.getElementById('cu-uuid').value.trim();
      if (!username && !rawUuid) return toast('Provide username or UUID', true);
      const id = rawUuid || crypto.randomUUID();
      if (STATE.editor.users.some(u => u.uuid === id)) return toast('User already exists', true);
      STATE.editor.users.push({
        uuid: id, username: username || id, primaryGroup: (STATE.editor.metadata && STATE.editor.metadata.defaultGroup) || 'default',
        parents: [], nodes: [], meta: {}, prefixes: [], suffixes: []
      });
      STATE.deletedUsers.delete(id);
      STATE.selected = { type: 'user', id };
      markDirty();
      closeModal();
      render();
    };
  }

  /* ==================== Event Binding ==================== */

  function bindCommonActions() {
    document.querySelectorAll('[data-route]').forEach(el => {
      el.onclick = e => {
        e.preventDefault();
        navigate(el.getAttribute('data-route'));
      };
    });
    const demoNav = document.getElementById('btn-nav-demo-editor');
    if (demoNav) {
      demoNav.onclick = () => {
        loadEditorSession(window.STONYPERMS_DEMO.editor, 'demo', '', '');
        navigate('/editor');
      };
    }

    const homeDemo = document.getElementById('home-open-demo');
    if (homeDemo) homeDemo.onclick = () => { loadEditorSession(window.STONYPERMS_DEMO.editor, 'demo', '', ''); navigate('/editor'); };
    const edDemo = document.getElementById('btn-editor-demo');
    if (edDemo) edDemo.onclick = () => { loadEditorSession(window.STONYPERMS_DEMO.editor, 'demo', '', ''); render(); };
    const vbDemo = document.getElementById('btn-verbose-demo');
    if (vbDemo) vbDemo.onclick = () => { STATE.verbose = deepClone(window.STONYPERMS_DEMO.verbose); STATE.mode = 'demo'; render(); };
    const trDemo = document.getElementById('btn-tree-demo');
    if (trDemo) trDemo.onclick = () => { STATE.tree = deepClone(window.STONYPERMS_DEMO.tree); STATE.mode = 'demo'; render(); };

    const impBtn = document.getElementById('btn-open-import');
    if (impBtn) impBtn.onclick = openImportExportModal;
  }

  function bindEditorEvents() {
    const sbSearch = document.getElementById('input-sidebar-search');
    if (sbSearch) {
      sbSearch.oninput = e => { STATE.sidebarQuery = e.target.value; render(); document.getElementById('input-sidebar-search').focus(); };
    }
    document.querySelectorAll('[data-toggle-sec]').forEach(el => {
      el.onclick = () => { const k = el.dataset.toggleSec; STATE.expandedSections[k] = !STATE.expandedSections[k]; render(); };
    });
    document.querySelectorAll('[data-toggle-track]').forEach(el => {
      el.onclick = e => {
        if (e.target.dataset.editTrack) return openEditTrackModal(e.target.dataset.editTrack);
        const k = el.dataset.toggleTrack;
        STATE.expandedTracks[k] = !STATE.expandedTracks[k];
        render();
      };
    });
    document.querySelectorAll('[data-select-group]').forEach(el => {
      el.onclick = () => { STATE.selected = { type: 'group', id: el.dataset.selectGroup }; STATE.checkedRows.clear(); render(); };
    });
    document.querySelectorAll('[data-select-user]').forEach(el => {
      el.onclick = () => { STATE.selected = { type: 'user', id: el.dataset.selectUser }; STATE.checkedRows.clear(); render(); };
    });

    const btnCG = document.getElementById('btn-create-group');
    if (btnCG) btnCG.onclick = openCreateGroupModal;
    const btnCT = document.getElementById('btn-create-track');
    if (btnCT) btnCT.onclick = openCreateTrackModal;
    const btnCU = document.getElementById('btn-create-user');
    if (btnCU) btnCU.onclick = openCreateUserModal;

    const btnSave = document.getElementById('btn-save-editor');
    if (btnSave) btnSave.onclick = openSaveModal;
    const btnQS = document.getElementById('btn-editor-quicksearch');
    if (btnQS) btnQS.onclick = openSearchModal;

    const entity = selectedEntity();
    if (!entity) return;

    const isGroup = STATE.selected.type === 'group';
    const rows = buildUnifiedRows(entity, STATE.selected.type);
    const rowById = Object.fromEntries(rows.map(r => [r.rowId, r]));

    const weightIn = document.getElementById('input-group-weight');
    if (weightIn) weightIn.onchange = e => { entity.weight = parseInt(e.target.value || '0', 10) || 0; markDirty(); render(); };
    const dispIn = document.getElementById('input-group-display');
    if (dispIn) dispIn.onchange = e => { entity.displayName = e.target.value; markDirty(); render(); };
    const primSel = document.getElementById('select-user-primary');
    if (primSel) primSel.onchange = e => { entity.primaryGroup = e.target.value; markDirty(); render(); };

    const btnDelEntity = document.getElementById('btn-delete-entity');
    if (btnDelEntity) {
      btnDelEntity.onclick = () => {
        if (isGroup) {
          STATE.editor.groups = STATE.editor.groups.filter(g => g.name !== entity.name);
          STATE.deletedGroups.add(entity.name);
        } else {
          STATE.editor.users = STATE.editor.users.filter(u => u.uuid !== entity.uuid);
          STATE.deletedUsers.add(entity.uuid);
        }
        STATE.selected = null;
        markDirty();
        render();
      };
    }

    document.getElementById('btn-add-parent').onclick = () => {
      openModal('Add Parent Group', `
        <div class="field-group">
          <label>Select Parent Group</label>
          <select id="ap-group" class="field-input">
            ${STATE.editor.groups.filter(g => !isGroup || g.name !== entity.name).map(g => `<option value="${esc(g.name)}">${esc(g.name)}</option>`).join('')}
          </select>
        </div>
        <div class="field-group"><label>Contexts (optional, e.g. server=lobby)</label><input id="ap-ctx" class="field-input"></div>
      `, `<button type="button" class="btn btn-primary btn-sm" id="ap-submit">Add Parent</button>`);
      document.getElementById('ap-submit').onclick = () => {
        const g = document.getElementById('ap-group').value;
        const ctxs = parseContextsString(document.getElementById('ap-ctx').value);
        entity.parents = entity.parents || [];
        entity.parents.push({ group: g, ...(ctxs ? { contexts: ctxs } : {}) });
        markDirty();
        closeModal();
        render();
      };
    };

    document.querySelectorAll('[data-remove-parent]').forEach(btn => {
      btn.onclick = () => { entity.parents.splice(Number(btn.dataset.removeParent), 1); markDirty(); render(); };
    });

    const nfInput = document.getElementById('input-nodes-filter');
    if (nfInput) nfInput.oninput = e => { STATE.nodesFilter = e.target.value; render(); document.getElementById('input-nodes-filter').focus(); };

    const chkAll = document.getElementById('chk-all-rows');
    if (chkAll) {
      chkAll.onchange = e => {
        const visible = document.querySelectorAll('.row-chk');
        visible.forEach(c => {
          if (e.target.checked) STATE.checkedRows.add(c.dataset.rowid);
          else STATE.checkedRows.delete(c.dataset.rowid);
        });
        render();
      };
    }
    document.querySelectorAll('.row-chk').forEach(chk => {
      chk.onchange = () => {
        if (chk.checked) STATE.checkedRows.add(chk.dataset.rowid);
        else STATE.checkedRows.delete(chk.dataset.rowid);
        render();
      };
    });
    const bulkDel = document.getElementById('btn-bulk-delete');
    if (bulkDel) {
      bulkDel.onclick = () => {
        const dels = Array.from(STATE.checkedRows).map(id => rowById[id]).filter(Boolean).reverse();
        dels.forEach(r => r.onDelete());
        STATE.checkedRows.clear();
        render();
      };
    }

    document.querySelectorAll('[data-toggle-row]').forEach(btn => {
      btn.onclick = () => { const r = rowById[btn.dataset.toggleRow]; if (r && r.onToggle) { r.onToggle(); render(); } };
    });
    document.querySelectorAll('[data-del-row]').forEach(btn => {
      btn.onclick = () => { const r = rowById[btn.dataset.delRow]; if (r) { r.onDelete(); render(); } };
    });

    // Bottom Dock Events
    const dType = document.getElementById('dock-type');
    const dKey = document.getElementById('dock-key');
    const dVal = document.getElementById('dock-value-btn');
    const dExp = document.getElementById('dock-expiry');
    const dMetaKey = document.getElementById('dock-meta-key');
    const dPrio = document.getElementById('dock-priority');

    if (dType) dType.onchange = e => { STATE.addForm.type = e.target.value; STATE.acOpen = false; render(); };
    if (dMetaKey) dMetaKey.oninput = e => { STATE.addForm.metaKey = e.target.value; };
    if (dPrio) dPrio.oninput = e => { STATE.addForm.priority = e.target.value; };
    if (dExp) dExp.oninput = e => { STATE.addForm.expiry = e.target.value; };
    if (dVal) dVal.onclick = () => { STATE.addForm.value = !STATE.addForm.value; render(); };

    const addNodeAction = () => {
      const t = STATE.addForm.type;
      const rawKey = STATE.addForm.key.trim();
      const expiry = parseExpiryInput(STATE.addForm.expiry);
      const ctxs = Object.keys(STATE.addForm.contexts).length ? deepClone(STATE.addForm.contexts) : undefined;
      if (!rawKey && t !== 'WEIGHT') return;

      if (t === 'PERMISSION') {
        entity.nodes = entity.nodes || [];
        for (const token of rawKey.split(/[\s,]+/).filter(Boolean)) {
          entity.nodes.push({ key: token, value: STATE.addForm.value, ...(expiry ? { expiry } : {}), ...(ctxs ? { contexts: deepClone(ctxs) } : {}) });
        }
      } else if (t === 'INHERITANCE') {
        entity.parents = entity.parents || [];
        entity.parents.push({ group: rawKey.toLowerCase(), ...(expiry ? { expiry } : {}), ...(ctxs ? { contexts: ctxs } : {}) });
      } else if (t === 'PREFIX') {
        entity.prefixes = entity.prefixes || [];
        entity.prefixes.push({ priority: parseInt(STATE.addForm.priority || '0', 10) || 0, value: rawKey, ...(expiry ? { expiry } : {}), ...(ctxs ? { contexts: ctxs } : {}) });
      } else if (t === 'SUFFIX') {
        entity.suffixes = entity.suffixes || [];
        entity.suffixes.push({ priority: parseInt(STATE.addForm.priority || '0', 10) || 0, value: rawKey, ...(expiry ? { expiry } : {}), ...(ctxs ? { contexts: ctxs } : {}) });
      } else if (t === 'WEIGHT' && isGroup) {
        entity.weight = parseInt(rawKey || '0', 10) || 0;
      } else if (t === 'DISPLAY NAME' && isGroup) {
        entity.displayName = rawKey;
      } else if (t === 'META') {
        const mk = STATE.addForm.metaKey.trim();
        if (!mk) return toast('Meta key is required', true);
        entity.meta = entity.meta || {};
        entity.meta[mk] = rawKey;
      }

      STATE.addForm.key = '';
      STATE.acOpen = false;
      markDirty();
      render();
    };

    if (dKey) {
      dKey.onfocus = () => {
        if (STATE.addForm.type === 'PERMISSION') {
          STATE.acOpen = true;
          STATE.acIndex = 0;
          updateAutocompleteDom();
        }
      };
      dKey.oninput = e => {
        STATE.addForm.key = e.target.value;
        STATE.acOpen = STATE.addForm.type === 'PERMISSION';
        STATE.acIndex = 0;
        updateAutocompleteDom();
      };
      dKey.onkeydown = e => {
        const list = computeAutocomplete(STATE.addForm.key);
        if (STATE.acOpen && list.length) {
          if (e.key === 'ArrowDown') { e.preventDefault(); STATE.acIndex = (STATE.acIndex + 1) % list.length; updateAutocompleteDom(); return; }
          if (e.key === 'ArrowUp') { e.preventDefault(); STATE.acIndex = (STATE.acIndex - 1 + list.length) % list.length; updateAutocompleteDom(); return; }
          if (e.key === 'Escape') { STATE.acOpen = false; updateAutocompleteDom(); return; }
          if (e.key === 'Enter' && STATE.acIndex >= 0 && list[STATE.acIndex] && STATE.addForm.key.trim() !== list[STATE.acIndex]) {
            e.preventDefault();
            STATE.addForm.key = list[STATE.acIndex];
            dKey.value = STATE.addForm.key;
            STATE.acOpen = false;
            updateAutocompleteDom();
            return;
          }
        }
        if (e.key === 'Enter') {
          e.preventDefault();
          addNodeAction();
        }
      };
    }

    const updateAutocompleteDom = () => {
      const col = dKey && dKey.parentElement;
      if (!col) return;
      const old = document.getElementById('ac-popover');
      if (old) old.remove();
      const list = computeAutocomplete(STATE.addForm.key);
      if (!STATE.acOpen || !list.length || STATE.addForm.type !== 'PERMISSION') return;
      const pop = document.createElement('div');
      pop.className = 'autocomplete-popover';
      pop.id = 'ac-popover';
      pop.innerHTML = list.map((perm, idx) => `
        <div class="ac-item ${idx === STATE.acIndex ? 'selected' : ''}" data-ac-val="${esc(perm)}">
          <span>${esc(perm)}</span>
          ${idx === STATE.acIndex ? `<span class="ac-hint">Press enter to select</span>` : ''}
        </div>
      `).join('');
      col.appendChild(pop);
      pop.querySelectorAll('[data-ac-val]').forEach(item => {
        item.onmousedown = ev => {
          ev.preventDefault();
          STATE.addForm.key = item.dataset.acVal;
          dKey.value = STATE.addForm.key;
          STATE.acOpen = false;
          updateAutocompleteDom();
        };
      });
    };

    document.getElementById('btn-dock-add').onclick = addNodeAction;
    document.getElementById('btn-dock-ctx').onclick = () => {
      const pot = (STATE.editor.potentialContexts && STATE.editor.potentialContexts.server) || ['lobby', 'survival'];
      openModal('Add Context Constraint', `
        <div class="field-group"><label>Key (e.g. server, world)</label><input id="dc-k" class="field-input" value="server"></div>
        <div class="field-group"><label>Value (suggestions: ${esc(pot.join(', '))})</label><input id="dc-v" class="field-input" value="${esc(pot[0] || '')}"></div>
      `, `<button type="button" class="btn btn-primary btn-sm" id="dc-ok">Add Context</button>`);
      document.getElementById('dc-ok').onclick = () => {
        const ck = document.getElementById('dc-k').value.trim().toLowerCase();
        const cv = document.getElementById('dc-v').value.trim();
        if (ck && cv) STATE.addForm.contexts[ck] = cv;
        closeModal();
        render();
      };
    };
    document.querySelectorAll('[data-del-dock-ctx]').forEach(btn => {
      btn.onclick = () => { delete STATE.addForm.contexts[btn.dataset.delDockCtx]; render(); };
    });
  }

  function bindViewerAndWikiEvents() {
    const vf = document.getElementById('input-verbose-filter');
    if (vf) vf.oninput = e => { STATE.verboseFilter = e.target.value; render(); document.getElementById('input-verbose-filter').focus(); };
    document.querySelectorAll('[data-verbose-ex]').forEach(chk => {
      chk.onchange = () => { STATE.verboseExclude[chk.dataset.verboseEx] = chk.checked; render(); };
    });
    document.querySelectorAll('[data-verbose-idx]').forEach(row => {
      row.onclick = () => {
        const i = Number(row.dataset.verboseIdx);
        if (STATE.verboseExpanded.has(i)) STATE.verboseExpanded.delete(i);
        else STATE.verboseExpanded.add(i);
        render();
      };
    });

    const tf = document.getElementById('input-tree-filter');
    if (tf) tf.oninput = e => { STATE.treeFilter = e.target.value; render(); document.getElementById('input-tree-filter').focus(); };
    const bExp = document.getElementById('btn-tree-expand');
    if (bExp) bExp.onclick = () => { STATE.treeCollapsed.clear(); render(); };
    const bCol = document.getElementById('btn-tree-collapse');
    if (bCol) bCol.onclick = () => {
      document.querySelectorAll('[data-tree-toggle]').forEach(el => STATE.treeCollapsed.add(el.dataset.treeToggle));
      render();
    };
    document.querySelectorAll('[data-tree-toggle]').forEach(el => {
      el.onclick = () => {
        const p = el.dataset.treeToggle;
        if (STATE.treeCollapsed.has(p)) STATE.treeCollapsed.delete(p);
        else STATE.treeCollapsed.add(p);
        render();
      };
    });
    document.querySelectorAll('[data-copy-node]').forEach(el => {
      el.onclick = () => navigator.clipboard.writeText(el.dataset.copyNode).then(() => toast('Copied ' + el.dataset.copyNode));
    });

    document.querySelectorAll('[data-wiki-slug]').forEach(btn => {
      btn.onclick = () => { STATE.wikiSlug = btn.dataset.wikiSlug; render(); };
    });
  }

  function render() {
    updateModePill();
    document.querySelectorAll('[data-route]').forEach(a => a.classList.toggle('active', a.getAttribute('data-route') === STATE.route));
    const app = document.getElementById('app');

    if (STATE.route === '/editor') app.innerHTML = renderEditor();
    else if (STATE.route === '/verbose') app.innerHTML = renderVerbose();
    else if (STATE.route === '/tree') app.innerHTML = renderTree();
    else if (STATE.route === '/wiki') app.innerHTML = renderWiki();
    else app.innerHTML = renderHome();

    bindCommonActions();
    if (STATE.route === '/editor' && STATE.editor) bindEditorEvents();
    bindViewerAndWikiEvents();
  }

  async function init() {
    const loc = parseRouteFromLocation();
    STATE.route = loc.route;
    STATE.wikiSlug = loc.subSlug || 'home';
    const apiBase = resolveApiOrigin(loc.api);

    // Navbar controls
    document.getElementById('nav-brand').onclick = e => { e.preventDefault(); navigate('/'); };
    document.getElementById('btn-global-search').onclick = openSearchModal;
    document.getElementById('nav-download').onclick = openDownloadModal;
    const dd = document.getElementById('tools-dropdown');
    document.getElementById('btn-tools-menu').onclick = ev => { ev.stopPropagation(); dd.classList.toggle('open'); };
    window.addEventListener('click', () => dd.classList.remove('open'));
    window.addEventListener('keydown', e => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        openSearchModal();
      } else if (e.key === 'Escape') {
        closeModal();
      }
    });
    window.addEventListener('popstate', () => {
      const next = parseRouteFromLocation();
      STATE.route = next.route;
      STATE.wikiSlug = next.subSlug || 'home';
      render();
    });

    try {
      if (loc.dataCode && (loc.route === '/editor' || loc.route === '/verbose' || loc.route === '/tree')) {
        const decoded = await decodePortableSp1(loc.dataCode);
        if (loc.route === '/verbose') { STATE.verbose = decoded; STATE.mode = 'portable'; }
        else if (loc.route === '/tree') { STATE.tree = decoded; STATE.mode = 'portable'; }
        else {
          const wrapped = decoded.metadata ? decoded : {
            token: 'portable',
            metadata: { commandAlias: 'sp', actor: decoded.actor || 'Gate', defaultGroup: 'default', liveSave: false },
            groups: decoded.groups || [],
            tracks: decoded.tracks || [],
            users: decoded.users || [],
            knownPermissions: (window.STONYPERMS_DEMO.editor.knownPermissions || []),
            potentialContexts: (window.STONYPERMS_DEMO.editor.potentialContexts || {})
          };
          loadEditorSession(wrapped, 'portable', '', '', loc.target);
        }
      } else if (loc.session && apiBase) {
        const kind = loc.route === '/verbose' ? 'verbose' : loc.route === '/tree' ? 'tree' : 'editor';
        const resp = await fetch(`${apiBase}/v1/${kind}/sessions/${encodeURIComponent(loc.session)}`);
        if (!resp.ok) throw new Error(`Session expired or not found (HTTP ${resp.status})`);
        const payload = await resp.json();
        if (kind === 'verbose') { STATE.verbose = payload; STATE.mode = 'live'; }
        else if (kind === 'tree') { STATE.tree = payload; STATE.mode = 'live'; }
        else { loadEditorSession(payload, 'live', apiBase, loc.session, loc.target); }
      }
    } catch (err) {
      toast(err.message || 'Failed to load session', true);
    }

    render();
  }

  init();
})();
