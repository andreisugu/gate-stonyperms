(() => {
  'use strict';

  const STATE = {
    route: '/',
    wikiSlug: 'home',
    mode: 'none', // 'none' | 'demo' | 'live' | 'portable'
    apiBase: '',
    sessionToken: '',
    editor: null,
    baseline: null,
    selected: null, // { type: 'group'|'user', id: string }
    expandedSections: { tracks: true, groups: true, users: true },
    expandedTracks: {},
    sidebarQuery: '',
    nodesFilter: '',
    nodeTypeFilter: 'ALL', // 'ALL' | 'PERMISSION' | 'INHERITANCE' | 'AFFIX' | 'META'
    showInherited: false,
    sortBy: 'default', // 'default' | 'key' | 'value' | 'expiry' | 'contexts'
    sortDir: 1,
    checkedRows: new Set(),
    dirtyCount: 0,
    deletedGroups: new Set(),
    deletedTracks: new Set(),
    deletedUsers: new Set(),
    pendingTrackModal: '',
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

  /* ==================== Minecraft Color Preview Renderer ==================== */

  const MC_COLORS = {
    '0': '#000000', '1': '#0000aa', '2': '#00aa00', '3': '#00aaaa',
    '4': '#aa0000', '5': '#aa00aa', '6': '#ffaa00', '7': '#aaaaaa',
    '8': '#555555', '9': '#5555ff', 'a': '#55ff55', 'b': '#55ffff',
    'c': '#ff5555', 'd': '#ff55ff', 'e': '#ffff55', 'f': '#ffffff'
  };

  const MC_TAG_NAMES = {
    black: '#000000', dark_blue: '#0000aa', dark_green: '#00aa00', dark_aqua: '#00aaaa',
    dark_red: '#aa0000', dark_purple: '#aa00aa', gold: '#ffaa00', gray: '#aaaaaa', grey: '#aaaaaa',
    dark_gray: '#555555', dark_grey: '#555555', blue: '#5555ff', green: '#55ff55',
    aqua: '#55ffff', red: '#ff5555', light_purple: '#ff55ff', pink: '#ff55ff',
    yellow: '#ffff55', white: '#ffffff'
  };

  function renderMcText(raw) {
    const s = String(raw || '');
    if (!s) return '';
    let color = '';
    const stack = [];
    let bold = false, italic = false, under = false, strike = false;
    let out = '';
    let buf = '';

    const activeColor = () => color || (stack.length ? stack[stack.length - 1] : '');
    const flush = () => {
      if (!buf) return;
      const styles = [];
      const col = activeColor();
      if (col) styles.push('color:' + col);
      if (bold) styles.push('font-weight:700');
      if (italic) styles.push('font-style:italic');
      if (under || strike) styles.push('text-decoration:' + [under ? 'underline' : '', strike ? 'line-through' : ''].filter(Boolean).join(' '));
      out += styles.length ? `<span style="${styles.join(';')}">${esc(buf)}</span>` : esc(buf);
      buf = '';
    };

    for (let i = 0; i < s.length; i++) {
      const ch = s[i];
      if ((ch === '&' || ch === '§') && i + 1 < s.length) {
        if (s[i + 1] === '#' && /^#[0-9a-fA-F]{6}/.test(s.slice(i + 1, i + 8))) {
          flush();
          color = s.slice(i + 1, i + 8);
          i += 7;
          continue;
        }
        const code = s[i + 1].toLowerCase();
        if (MC_COLORS[code]) {
          flush();
          color = MC_COLORS[code];
          bold = italic = under = strike = false;
          i++;
          continue;
        }
        if (code === 'l') { flush(); bold = true; i++; continue; }
        if (code === 'o') { flush(); italic = true; i++; continue; }
        if (code === 'n') { flush(); under = true; i++; continue; }
        if (code === 'm') { flush(); strike = true; i++; continue; }
        if (code === 'r') { flush(); color = ''; bold = italic = under = strike = false; i++; continue; }
      }
      if (ch === '<') {
        const end = s.indexOf('>', i + 1);
        if (end > i) {
          const tag = s.slice(i + 1, end).trim().toLowerCase();
          if (tag === 'b' || tag === 'bold') { flush(); bold = true; i = end; continue; }
          if (tag === '/b' || tag === '/bold') { flush(); bold = false; i = end; continue; }
          if (tag === 'i' || tag === 'italic') { flush(); italic = true; i = end; continue; }
          if (tag === '/i' || tag === '/italic') { flush(); italic = false; i = end; continue; }
          if (tag === 'u' || tag === 'underlined') { flush(); under = true; i = end; continue; }
          if (tag === '/u' || tag === '/underlined') { flush(); under = false; i = end; continue; }
          if (tag === 's' || tag === 'st' || tag === 'strikethrough') { flush(); strike = true; i = end; continue; }
          if (tag === '/s' || tag === '/st' || tag === '/strikethrough') { flush(); strike = false; i = end; continue; }
          if (tag === 'reset' || tag === 'r') { flush(); color = ''; stack.length = 0; bold = italic = under = strike = false; i = end; continue; }
          if (tag.startsWith('/')) {
            const inner = tag.slice(1);
            if (MC_TAG_NAMES[inner] || inner.startsWith('#') || inner === 'color') {
              flush();
              stack.pop();
              color = '';
              i = end;
              continue;
            }
          } else {
            const spec = tag.startsWith('color:') ? tag.slice(6) : tag;
            if (/^#[0-9a-f]{6}$/.test(spec)) {
              flush(); stack.push(spec); color = ''; i = end; continue;
            }
            if (MC_TAG_NAMES[spec]) {
              flush(); stack.push(MC_TAG_NAMES[spec]); color = ''; i = end; continue;
            }
          }
        }
      }
      buf += ch;
    }
    flush();
    return out;
  }

  function resolveEntityPreview(entity, kind) {
    const groupsByName = Object.fromEntries((STATE.editor?.groups || []).map(g => [g.name.toLowerCase(), g]));
    const pickBestAffix = list => {
      const active = (list || []).filter(a => !a.expiry || a.expiry > Math.floor(Date.now() / 1000));
      const globalFirst = active.slice().sort((a, b) => {
        const aCtx = a.contexts && Object.keys(a.contexts).length ? 1 : 0;
        const bCtx = b.contexts && Object.keys(b.contexts).length ? 1 : 0;
        return aCtx - bCtx || (b.priority || 0) - (a.priority || 0);
      });
      return globalFirst[0]?.value || '';
    };

    if (kind === 'group') {
      const pref = pickBestAffix(entity.prefixes);
      const suff = pickBestAffix(entity.suffixes);
      const label = entity.displayName || entity.name;
      return renderMcText(pref + label + suff);
    }

    const inherited = collectInheritedGroups(entity);
    const allGroups = [];
    if (entity.primaryGroup && groupsByName[entity.primaryGroup.toLowerCase()]) {
      allGroups.push(groupsByName[entity.primaryGroup.toLowerCase()]);
    }
    inherited.forEach(g => {
      if (!allGroups.some(x => x.name === g.name)) allGroups.push(g);
    });
    allGroups.sort((a, b) => (b.weight || 0) - (a.weight || 0));

    let pref = pickBestAffix(entity.prefixes);
    if (!pref) {
      for (const g of allGroups) {
        pref = pickBestAffix(g.prefixes);
        if (pref) break;
      }
    }
    let suff = pickBestAffix(entity.suffixes);
    if (!suff) {
      for (const g of allGroups) {
        suff = pickBestAffix(g.suffixes);
        if (suff) break;
      }
    }
    const name = (entity.meta && entity.meta.nickname) || entity.username || entity.uuid;
    return renderMcText(pref + name + suff);
  }

  /* ==================== Encoding / Routing Helpers ==================== */

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

  async function encodePortableSp1(payloadObj, prefix = 'sp1_') {
    const rawBytes = new TextEncoder().encode(JSON.stringify(payloadObj));
    if (typeof CompressionStream === 'undefined') {
      throw new Error('Browser lacks CompressionStream(gzip); use JSON export instead.');
    }
    const cs = new CompressionStream('gzip');
    const writer = cs.writable.getWriter();
    await writer.write(rawBytes);
    await writer.close();
    const buf = await new Response(cs.readable).arrayBuffer();
    return prefix + bytesToBase64Url(new Uint8Array(buf));
  }

  async function decodePortableSp1(code) {
    const trimmed = String(code || '').trim();
    if (!trimmed.startsWith('sp1_') && !trimmed.startsWith('sp2_')) {
      throw new Error('Expected sp1_ or sp2_ portable code');
    }
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

  function contextsToInputString(ctxs) {
    if (!ctxs || !Object.keys(ctxs).length) return '';
    return Object.entries(ctxs).map(([k, v]) => `${k}=${v}`).join(' ');
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
    STATE.baseline = deepClone(data);
    STATE.mode = mode;
    STATE.apiBase = apiBase;
    STATE.sessionToken = token;
    STATE.dirtyCount = 0;
    STATE.deletedGroups.clear();
    STATE.deletedTracks.clear();
    STATE.deletedUsers.clear();
    STATE.checkedRows.clear();
    STATE.pendingTrackModal = '';

    const effectiveTarget = targetHint || (data.metadata && data.metadata.targetType && data.metadata.target ? `${data.metadata.targetType}:${data.metadata.target}` : '');
    if (effectiveTarget.startsWith('group:')) {
      const gn = effectiveTarget.slice(6).toLowerCase();
      if (data.groups.some(g => g.name === gn)) STATE.selected = { type: 'group', id: gn };
    } else if (effectiveTarget.startsWith('user:')) {
      const uid = effectiveTarget.slice(5).toLowerCase();
      const hit = data.users.find(u => u.uuid.toLowerCase() === uid || (u.username || '').toLowerCase() === uid);
      if (hit) STATE.selected = { type: 'user', id: hit.uuid };
    } else if (effectiveTarget.startsWith('track:')) {
      const tn = effectiveTarget.slice(6).toLowerCase();
      if (data.tracks.some(t => t.name === tn)) {
        STATE.expandedTracks[tn] = true;
        STATE.pendingTrackModal = tn;
      }
      STATE.selected = null;
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
    const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);
    const base = STATE.baseline;
    if (!base) {
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

    const baseGroups = new Map((base.groups || []).map(g => [g.name, g]));
    const baseTracks = new Map((base.tracks || []).map(t => [t.name, t]));
    const baseUsers = new Map((base.users || []).map(u => [u.uuid, u]));

    const groups = STATE.editor.groups.filter(g => !same(g, baseGroups.get(g.name))).map(g => deepClone(g));
    const tracks = STATE.editor.tracks.filter(t => !same(t, baseTracks.get(t.name))).map(t => deepClone(t));
    const users = STATE.editor.users.filter(u => !same(u, baseUsers.get(u.uuid))).map(u => deepClone(u));

    const deletedGroups = new Set(STATE.deletedGroups);
    const deletedTracks = new Set(STATE.deletedTracks);
    const deletedUsers = new Set(STATE.deletedUsers);
    const liveGroupNames = new Set(STATE.editor.groups.map(g => g.name));
    const liveTrackNames = new Set(STATE.editor.tracks.map(t => t.name));
    const liveUserIds = new Set(STATE.editor.users.map(u => u.uuid));
    baseGroups.forEach((_, name) => { if (!liveGroupNames.has(name)) deletedGroups.add(name); });
    baseTracks.forEach((_, name) => { if (!liveTrackNames.has(name)) deletedTracks.add(name); });
    baseUsers.forEach((_, id) => { if (!liveUserIds.has(id)) deletedUsers.add(id); });

    return {
      groups,
      tracks,
      users,
      deletedGroups: Array.from(deletedGroups),
      deletedTracks: Array.from(deletedTracks),
      deletedUsers: Array.from(deletedUsers),
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

  function collectInheritedGroups(entity) {
    if (!STATE.editor || !entity) return [];
    const groupsByName = Object.fromEntries(STATE.editor.groups.map(g => [g.name.toLowerCase(), g]));
    const visited = new Set();
    if (entity.name) visited.add(entity.name.toLowerCase());
    const queue = [];
    if (entity.primaryGroup) queue.push(entity.primaryGroup.toLowerCase());
    (entity.parents || []).forEach(p => queue.push(String(p.group || '').toLowerCase()));

    const out = [];
    while (queue.length) {
      const curName = queue.shift();
      if (!curName || visited.has(curName)) continue;
      visited.add(curName);
      const grp = groupsByName[curName];
      if (!grp) continue;
      out.push(grp);
      (grp.parents || []).forEach(p => queue.push(String(p.group || '').toLowerCase()));
    }
    out.sort((a, b) => (b.weight || 0) - (a.weight || 0) || a.name.localeCompare(b.name));
    return out;
  }

  // Flatten entity properties/nodes into normalized editor rows
  function buildUnifiedRows(entity, kind) {
    const rows = [];
    if (kind === 'group') {
      if (entity.displayName) {
        rows.push({
          rowId: 'displayname:0', category: 'META', badge: 'DISPLAY NAME', cls: 'type-displayname',
          key: entity.displayName, rawKey: entity.displayName, value: true, toggleable: false, editable: true,
          expiry: 0, contexts: null,
          onDelete: () => { entity.displayName = ''; markDirty(); }
        });
      }
      if (typeof entity.weight === 'number' && entity.weight !== 0) {
        rows.push({
          rowId: 'weight:0', category: 'META', badge: 'WEIGHT', cls: 'type-weight',
          key: String(entity.weight), rawKey: String(entity.weight), value: true, toggleable: false, editable: true,
          expiry: 0, contexts: null,
          onDelete: () => { entity.weight = 0; markDirty(); }
        });
      }
    }
    (entity.parents || []).forEach((p, idx) => {
      rows.push({
        rowId: `parent:${idx}`, category: 'INHERITANCE', badge: 'INHERITANCE', cls: 'type-inheritance',
        key: p.group, value: true, toggleable: false, editable: true, ref: p,
        expiry: p.expiry || 0, contexts: p.contexts || null,
        onDelete: () => { entity.parents.splice(idx, 1); markDirty(); }
      });
    });
    (entity.prefixes || []).forEach((pm, idx) => {
      rows.push({
        rowId: `prefix:${idx}`, category: 'AFFIX', badge: 'PREFIX', cls: 'type-prefix',
        key: pm.value, extraNote: `(weight: ${pm.priority})`, value: true, toggleable: false, editable: true, ref: pm,
        expiry: pm.expiry || 0, contexts: pm.contexts || null,
        onDelete: () => { entity.prefixes.splice(idx, 1); markDirty(); }
      });
    });
    (entity.suffixes || []).forEach((sm, idx) => {
      rows.push({
        rowId: `suffix:${idx}`, category: 'AFFIX', badge: 'SUFFIX', cls: 'type-suffix',
        key: sm.value, extraNote: `(weight: ${sm.priority})`, value: true, toggleable: false, editable: true, ref: sm,
        expiry: sm.expiry || 0, contexts: sm.contexts || null,
        onDelete: () => { entity.suffixes.splice(idx, 1); markDirty(); }
      });
    });
    const coveredGlobalMeta = new Set();
    (entity.metaEntries || []).forEach((me, idx) => {
      const hasCtx = me.contexts && Object.keys(me.contexts).length > 0;
      const isExp = Boolean(me.expiry && me.expiry > 0);
      if (!isExp && !hasCtx) {
        coveredGlobalMeta.add(String(me.key || '').toLowerCase());
      }
      rows.push({
        rowId: `metaEntry:${idx}`, category: 'META', badge: 'META', cls: 'type-meta',
        key: `${me.key} = ${me.value}`, value: true, toggleable: false, editable: true, ref: me,
        expiry: me.expiry || 0, contexts: me.contexts || null,
        onDelete: () => {
          entity.metaEntries.splice(idx, 1);
          if (!isExp && !hasCtx && entity.meta) {
            delete entity.meta[me.key];
          }
          markDirty();
        }
      });
    });
    Object.entries(entity.meta || {}).forEach(([mk, mv]) => {
      if (coveredGlobalMeta.has(mk.toLowerCase())) return;
      rows.push({
        rowId: `meta:${mk}`, category: 'META', badge: 'META', cls: 'type-meta',
        key: `${mk} = ${mv}`, metaKey: mk, metaVal: mv, value: true, toggleable: false, editable: true,
        expiry: 0, contexts: null,
        onDelete: () => {
          delete entity.meta[mk];
          if (Array.isArray(entity.metaEntries)) {
            entity.metaEntries = entity.metaEntries.filter(me =>
              !(String(me.key || '').toLowerCase() === mk.toLowerCase() && !me.expiry && (!me.contexts || !Object.keys(me.contexts).length))
            );
          }
          markDirty();
        }
      });
    });
    (entity.nodes || []).forEach((n, idx) => {
      rows.push({
        rowId: `perm:${idx}`, category: 'PERMISSION', badge: 'PERMISSION', cls: 'type-permission',
        key: n.key, value: Boolean(n.value), toggleable: true, editable: true,
        expiry: n.expiry || 0, contexts: n.contexts || null, nodeRef: n, ref: n,
        onToggle: () => { n.value = !n.value; markDirty(); },
        onDelete: () => { entity.nodes.splice(idx, 1); markDirty(); }
      });
    });
    return rows;
  }

  function buildInheritedRows(entity) {
    const parents = collectInheritedGroups(entity);
    const out = [];
    parents.forEach(pg => {
      (pg.nodes || []).forEach((n, idx) => {
        out.push({
          rowId: `inh-perm:${pg.name}:${idx}`, category: 'PERMISSION', badge: 'PERMISSION', cls: 'type-permission',
          inherited: true, sourceGroup: pg.name,
          key: n.key, value: Boolean(n.value), expiry: n.expiry || 0, contexts: n.contexts || null,
          copyPayload: () => {
            entity.nodes = entity.nodes || [];
            entity.nodes.push(deepClone(n));
            markDirty();
          }
        });
      });
      (pg.prefixes || []).forEach((pm, idx) => {
        out.push({
          rowId: `inh-pref:${pg.name}:${idx}`, category: 'AFFIX', badge: 'PREFIX', cls: 'type-prefix',
          inherited: true, sourceGroup: pg.name,
          key: pm.value, extraNote: `(weight: ${pm.priority})`, value: true, expiry: pm.expiry || 0, contexts: pm.contexts || null,
          copyPayload: () => {
            entity.prefixes = entity.prefixes || [];
            entity.prefixes.push(deepClone(pm));
            markDirty();
          }
        });
      });
      (pg.suffixes || []).forEach((sm, idx) => {
        out.push({
          rowId: `inh-suff:${pg.name}:${idx}`, category: 'AFFIX', badge: 'SUFFIX', cls: 'type-suffix',
          inherited: true, sourceGroup: pg.name,
          key: sm.value, extraNote: `(weight: ${sm.priority})`, value: true, expiry: sm.expiry || 0, contexts: sm.contexts || null,
          copyPayload: () => {
            entity.suffixes = entity.suffixes || [];
            entity.suffixes.push(deepClone(sm));
            markDirty();
          }
        });
      });
    });
    return out;
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
            <p>Visual browser permissions editor with one-click Live Save to Gate over HTTP API or portable <code>sp1_</code> edit codes.</p>
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
            <code>/sp track &lt;track&gt; editor</code>
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

  function compareRows(a, b) {
    const dir = STATE.sortDir;
    switch (STATE.sortBy) {
      case 'key':
        return dir * String(a.key).localeCompare(String(b.key));
      case 'value':
        return dir * (Number(b.value) - Number(a.value) || String(a.key).localeCompare(String(b.key)));
      case 'expiry':
        return dir * ((a.expiry || 0) - (b.expiry || 0) || String(a.key).localeCompare(String(b.key)));
      case 'contexts':
        return dir * contextsToInputString(a.contexts).localeCompare(contextsToInputString(b.contexts));
      default:
        return 0;
    }
  }

  function renderEditor() {
    if (!STATE.editor) {
      return renderSessionPromptCard({
        title: 'Web Permissions Editor',
        commands: ['/sp editor', '/sp user <user> editor', '/sp group <group> editor', '/sp track <track> editor', '/spb editor'],
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
      const ownRows = buildUnifiedRows(entity, STATE.selected.type);
      const inheritedRows = buildInheritedRows(entity);
      const allRows = STATE.showInherited ? ownRows.concat(inheritedRows) : ownRows;

      const nf = STATE.nodesFilter.trim().toLowerCase();
      const filteredRows = allRows.filter(r => {
        if (STATE.nodeTypeFilter !== 'ALL' && r.category !== STATE.nodeTypeFilter) return false;
        if (!nf) return true;
        return r.key.toLowerCase().includes(nf) ||
          r.badge.toLowerCase().includes(nf) ||
          (r.sourceGroup || '').toLowerCase().includes(nf) ||
          contextsToInputString(r.contexts).toLowerCase().includes(nf);
      });

      if (STATE.sortBy !== 'default') {
        filteredRows.sort(compareRows);
      }

      const sortArrow = col => STATE.sortBy === col ? (STATE.sortDir > 0 ? ' ▴' : ' ▾') : '';
      const editableFiltered = filteredRows.filter(r => !r.inherited);

      mainHtml = `
        <div class="target-header">
          <div class="target-title-row">
            <h2>${isGroup ? 'Group:' : 'User:'}</h2>
            <span class="target-badge" title="${esc(isGroup ? (entity.displayName || entity.name) : (entity.username || entity.uuid))}">${renderMcText(isGroup ? (entity.displayName || entity.name) : (entity.username || entity.uuid))}</span>
            ${isGroup
              ? `<span class="target-sub">( ${esc(entity.name)} )</span>`
              : `<img class="user-head" style="width:28px;height:28px" src="https://mc-heads.net/avatar/${esc(entity.uuid || entity.username)}/28" data-name="${esc(entity.username)}" onerror="__spHeadFallback(this)" alt="">`
            }
            <span class="mc-preview-pill" title="Live rendered prefix + name + suffix">${resolveEntityPreview(entity, STATE.selected.type)}</span>
            <div style="margin-left:auto;display:flex;gap:6px;flex-wrap:wrap">
              ${isGroup ? `<button type="button" class="btn btn-secondary btn-xs" id="btn-clone-group">Clone Group</button>` : ''}
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
                <span class="mono-chip" id="copy-user-uuid" title="Click to copy UUID" style="cursor:pointer">${esc(entity.uuid)}</span>
              </div>
            `}
            <div class="meta-line">
              <span class="meta-line-label">Parent groups:</span>
              <button type="button" class="add-parent-btn" id="btn-add-parent" title="Add parent group">+</button>
              ${(entity.parents || []).map((p, idx) => `
                <button type="button" class="chip-btn" data-remove-parent="${idx}" title="Remove parent ${esc(p.group)}">
                  ${esc(p.group)}${p.contexts && Object.keys(p.contexts).length ? ` <span style="color:var(--accent)">[${esc(contextsToInputString(p.contexts))}]</span>` : ''} <span class="x">×</span>
                </button>
              `).join('') || '<span style="color:var(--text-dim)">none</span>'}
            </div>
          </div>
        </div>

        <div class="nodes-hdr">
          <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
            <h3>Nodes <span style="color:var(--text-muted);font-weight:500">(${ownRows.length}${STATE.showInherited ? ` + ${inheritedRows.length} inherited` : ''})</span></h3>
            <button type="button" class="filter-pill ${STATE.showInherited ? 'active' : ''}" id="btn-toggle-inherited">
              ${STATE.showInherited ? 'Hide' : 'Show'} Inherited (${inheritedRows.length})
            </button>
          </div>
          <div class="nodes-tools">
            <input type="text" id="input-nodes-filter" class="nodes-filter-input" value="${esc(STATE.nodesFilter)}" placeholder="Filter nodes or contexts...">
          </div>
        </div>

        <div class="node-type-tabs">
          ${[
            ['ALL', 'All'],
            ['PERMISSION', 'Permissions'],
            ['INHERITANCE', 'Inheritance'],
            ['AFFIX', 'Prefixes / Suffixes'],
            ['META', 'Meta']
          ].map(([code, label]) => `
            <button type="button" class="filter-pill ${STATE.nodeTypeFilter === code ? 'active' : ''}" data-type-filter="${code}">${label}</button>
          `).join('')}
        </div>

        ${STATE.checkedRows.size > 0 ? `
          <div class="bulk-bar">
            <span class="bulk-bar-label">${STATE.checkedRows.size} selected:</span>
            <button type="button" class="btn btn-secondary btn-xs" id="btn-bulk-flip">Flip True/False</button>
            <button type="button" class="btn btn-secondary btn-xs" id="btn-bulk-contexts">Set Contexts</button>
            <button type="button" class="btn btn-secondary btn-xs" id="btn-bulk-expiry">Set Expiry</button>
            <button type="button" class="btn btn-danger btn-xs" id="btn-bulk-delete">Delete Selected</button>
          </div>
        ` : ''}

        <table class="nodes-table">
          <thead>
            <tr>
              <th style="width:36px"><input type="checkbox" id="chk-all-rows" ${editableFiltered.length && editableFiltered.every(r => STATE.checkedRows.has(r.rowId)) ? 'checked' : ''}></th>
              <th class="sortable" data-sort-col="key">Node${sortArrow('key')}</th>
              <th class="sortable" data-sort-col="value" style="width:90px">Value${sortArrow('value')}</th>
              <th class="sortable" data-sort-col="expiry" style="width:120px">Expiry${sortArrow('expiry')}</th>
              <th class="sortable" data-sort-col="contexts">Contexts${sortArrow('contexts')}</th>
              <th style="width:68px"></th>
            </tr>
          </thead>
          <tbody>
            ${filteredRows.map(r => `
              <tr data-rowid="${esc(r.rowId)}" class="${r.inherited ? 'inherited-row' : ''}">
                <td>${r.inherited ? '' : `<input type="checkbox" class="row-chk" data-rowid="${esc(r.rowId)}" ${STATE.checkedRows.has(r.rowId) ? 'checked' : ''}>`}</td>
                <td>
                  <div class="node-cell">
                    ${r.inherited ? `<span class="badge-pill type-inherited">INHERITED · ${esc(r.sourceGroup)}</span>` : ''}
                    <span class="badge-pill ${esc(r.cls)}">${esc(r.badge)}</span>
                    <span class="node-key" title="${esc(r.key)}">${(r.category === 'AFFIX' || r.badge === 'DISPLAY NAME') ? renderMcText(r.key) : esc(r.key)}</span>
                    ${r.extraNote ? `<span class="node-extra-note">${esc(r.extraNote)}</span>` : ''}
                  </div>
                </td>
                <td>
                  ${r.toggleable && !r.inherited
                    ? `<button type="button" class="val-pill ${r.value ? 'val-true' : 'val-false'}" data-toggle-row="${esc(r.rowId)}">${r.value ? 'true' : 'false'}</button>`
                    : `<span class="val-pill ${r.value ? 'val-true' : 'val-false'}" style="opacity:0.75">${r.value ? 'true' : 'false'}</span>`
                  }
                </td>
                <td><span style="font-family:var(--mono);font-size:12px;color:${r.expiry ? 'var(--text)' : 'var(--text-dim)'}">${esc(formatExpiry(r.expiry))}</span></td>
                <td>${formatContexts(r.contexts)}</td>
                <td>
                  <div class="row-actions">
                    ${r.inherited
                      ? `<button type="button" class="row-icon-btn" data-copy-inh="${esc(r.rowId)}" title="Copy directly onto ${esc(isGroup ? entity.name : entity.username)}">+</button>`
                      : `
                        ${r.editable ? `<button type="button" class="row-icon-btn" data-edit-row="${esc(r.rowId)}" title="Edit node">✎</button>` : ''}
                        <button type="button" class="row-del-btn" data-del-row="${esc(r.rowId)}" title="Delete node">×</button>
                      `
                    }
                  </div>
                </td>
              </tr>
            `).join('') || `<tr><td colspan="6" style="color:var(--text-dim);text-align:center;padding:24px">No matching nodes</td></tr>`}
          </tbody>
        </table>
      `;
    }

    const suggestions = computeAutocomplete(STATE.addForm.key, STATE.addForm.type, entity);

    return `
      <div class="editor-shell">
        <aside class="editor-sidebar">
          <div class="sidebar-search">
            <input type="text" id="input-sidebar-search" value="${esc(STATE.sidebarQuery)}" placeholder="Search groups, users, tracks...">
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
                        <span class="sidebar-item-title" title="${esc(g.displayName || g.name)}">${renderMcText(g.displayName || g.name)}</span>
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
              ${STATE.dirtyCount > 0 ? `<span class="mono-chip" style="color:var(--accent)">${STATE.dirtyCount} unsaved edit${STATE.dirtyCount === 1 ? '' : 's'} (Ctrl+S)</span>` : ''}
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
                <span class="dock-label">
                  <span>${STATE.addForm.type === 'PERMISSION' ? 'Permissions' : STATE.addForm.type === 'INHERITANCE' ? 'Parent Group' : 'Value'}</span>
                  ${(STATE.addForm.type === 'PREFIX' || STATE.addForm.type === 'SUFFIX') ? `<span id="dock-affix-preview" class="mc-preview-pill" style="padding:1px 6px;font-size:11px;text-transform:none">${renderMcText(STATE.addForm.key) || '<span style="color:var(--text-dim)">preview</span>'}</span>` : ''}
                </span>
                <input type="text" id="dock-key" class="dock-input" autocomplete="off"
                  value="${esc(STATE.addForm.key)}"
                  placeholder="${STATE.addForm.type === 'PERMISSION' ? 'Enter permissions or paste many' : STATE.addForm.type === 'INHERITANCE' ? 'Select or type group name' : 'Enter value'}">
                ${(STATE.addForm.type === 'PERMISSION' || STATE.addForm.type === 'INHERITANCE') && STATE.acOpen && suggestions.length ? `
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

              <div class="dock-col" style="width:142px">
                <span class="dock-label">
                  <span>Expiry</span>
                  <span class="preset-pills">
                    ${['1h', '1d', '7d', '30d'].map(p => `<button type="button" class="preset-chip" data-dock-exp-preset="${p}">${p}</button>`).join('')}
                  </span>
                </span>
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

  function computeAutocomplete(input, kind = 'PERMISSION', entity = null) {
    if (!STATE.editor) return [];
    const tokens = String(input || '').trim().split(/[\s,]+/);
    const q = (tokens[tokens.length - 1] || '').toLowerCase();
    if (kind === 'INHERITANCE') {
      const selfName = entity && entity.name ? entity.name.toLowerCase() : '';
      const groups = (STATE.editor.groups || [])
        .map(g => g.name)
        .filter(n => n.toLowerCase() !== selfName);
      return (!q ? groups : groups.filter(g => g.toLowerCase().includes(q))).slice(0, 14);
    }
    const perms = STATE.editor.knownPermissions || [];
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
    const allEntries = STATE.verbose.entries || [];
    const entries = allEntries.filter(e => {
      const res = (e.result || 'undefined').toLowerCase();
      if (STATE.verboseExclude[res]) return false;
      if (!fq) return true;
      return (e.subject || '').toLowerCase().includes(fq) || (e.permission || '').toLowerCase().includes(fq);
    });
    const countTrue = entries.filter(e => (e.result || '').toLowerCase() === 'true').length;
    const countFalse = entries.filter(e => (e.result || '').toLowerCase() === 'false').length;
    const countUndef = entries.length - countTrue - countFalse;
    const subjects = new Set(entries.map(e => e.subject || 'internal')).size;
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
            <span class="k">Count</span><span class="v">${entries.length} / ${meta.totalCount ?? allEntries.length}</span>
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

          <div style="display:flex;gap:8px;flex-wrap:wrap">
            <button type="button" class="btn btn-secondary btn-sm" id="btn-verbose-expand-all">Expand All</button>
            <button type="button" class="btn btn-secondary btn-sm" id="btn-verbose-collapse-all">Collapse All</button>
            <button type="button" class="btn btn-secondary btn-sm" id="btn-verbose-download">Download JSON</button>
          </div>
        </aside>

        <div class="viewer-main-wrap">
          <div class="verbose-stats-bar">
            <div class="stat-group">
              <span class="stat-chip">Shown: ${entries.length}</span>
              <span class="stat-chip">Subjects: ${subjects}</span>
              <span class="val-pill val-true">true: ${countTrue}</span>
              <span class="val-pill val-false">false: ${countFalse}</span>
              <span class="val-pill val-undefined">undefined: ${countUndef}</span>
            </div>
          </div>

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
        const wild = node.path + '.*';
        html += `
          <div>
            <div class="tree-row tree-branch" data-tree-toggle="${esc(node.path)}">
              <span>${collapsed ? '▸' : '▾'} ${esc(node.path)} <span style="color:var(--text-dim);font-size:11px">(${node.children.size})</span></span>
              <span class="tree-actions">
                <button type="button" class="preset-chip" data-copy-node="${esc(wild)}" title="Copy wildcard ${esc(wild)}">copy *</button>
                ${STATE.editor ? `<button type="button" class="preset-chip" data-use-tree-node="${esc(wild)}" title="Load wildcard into editor">use</button>` : ''}
              </span>
            </div>
            ${collapsed ? '' : `<div class="tree-children">${renderTreeBranch(node.children)}</div>`}
          </div>
        `;
      } else {
        html += `
          <div class="tree-row" data-copy-node="${esc(node.path)}" title="Click to copy">
            <span>${esc(node.path)}</span>
            <span class="tree-actions">
              <span style="color:var(--text-dim);font-size:11px">copy</span>
              ${STATE.editor ? `<button type="button" class="preset-chip" data-use-tree-node="${esc(node.path)}" title="Load into editor">use</button>` : ''}
            </span>
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
    const allPerms = STATE.tree.permissions || [];
    const fq = STATE.treeFilter.trim().toLowerCase();
    const perms = allPerms.filter(p => !fq || p.toLowerCase().includes(fq));
    const treeMap = buildPermTree(perms);

    return `
      <div class="viewer-layout">
        <aside class="viewer-aside">
          <h2>Permission Tree Viewer</h2>
          <div class="meta-card">
            <span class="k">Uploaded by</span><span class="v">${esc(meta.uploadedBy || 'Console')}</span>
            <span class="k">Time</span><span class="v">${esc(meta.time ? new Date(meta.time).toISOString().replace('T',' ').slice(0,19)+' UTC' : '-')}</span>
            <span class="k">Root</span><span class="v"><span class="mono-chip">${esc(meta.root || '.')}</span></span>
            <span class="k">Nodes</span><span class="v">${perms.length} / ${allPerms.length}</span>
            <span class="k">Top Branches</span><span class="v">${treeMap.size}</span>
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

  function buildContextSuggestionChips(targetInputId) {
    const pot = (STATE.editor && STATE.editor.potentialContexts) || { server: ['lobby', 'survival'], world: ['world'] };
    const pairs = [];
    Object.entries(pot).forEach(([k, vals]) => {
      (vals || []).slice(0, 5).forEach(v => pairs.push(`${k}=${v}`));
    });
    if (!pairs.length) return '';
    return `
      <div style="display:flex;flex-wrap:wrap;gap:4px">
        ${pairs.map(p => `<button type="button" class="preset-chip" data-ctx-suggest="${esc(p)}" data-target-in="${esc(targetInputId)}">+ ${esc(p)}</button>`).join('')}
      </div>
    `;
  }

  function bindContextSuggestionChips(inputEl) {
    document.querySelectorAll('[data-ctx-suggest]').forEach(btn => {
      btn.onclick = () => {
        const pair = btn.dataset.ctxSuggest;
        const parsed = parseContextsString(inputEl.value) || {};
        const idx = pair.indexOf('=');
        if (idx > 0) parsed[pair.slice(0, idx)] = pair.slice(idx + 1);
        inputEl.value = contextsToInputString(parsed);
      };
    });
  }

  function contextCatalog() {
    const pot = (STATE.editor && STATE.editor.potentialContexts) || {};
    const keys = Object.keys(pot).filter(k => Array.isArray(pot[k])).sort();
    return { pot, keys };
  }

  function contextPairDatalist() {
    const { pot } = contextCatalog();
    const options = [];
    Object.entries(pot).forEach(([k, vals]) => {
      (vals || []).slice(0, 30).forEach(v => options.push(`${k}=${v}`));
    });
    if (!options.length) return '';
    return `<datalist id="ctx-pair-list">${options.map(o => `<option value="${esc(o)}"></option>`).join('')}</datalist>`;
  }

  function openContextsModal(title, initial, onApply) {
    const { pot, keys } = contextCatalog();
    const rows = Object.entries(initial || {}).map(([k, v]) => ({ k, v }));
    if (!rows.length) rows.push({ k: 'server', v: (pot.server || [])[0] || '' });

    const valueOptions = key => (pot[key] || []).map(v => `<option value="${esc(v)}"></option>`).join('');

    openModal(title, `
      <datalist id="ctx-key-list">${keys.map(k => `<option value="${esc(k)}"></option>`).join('')}</datalist>
      <div id="ctx-row-wrap"></div>
      <button type="button" class="btn btn-secondary btn-sm" id="ctx-add-row">+ Add context</button>
      <div style="margin-top:12px;display:flex;flex-wrap:wrap;gap:4px">
        ${Object.entries(pot).slice(0, 5).flatMap(([k, vals]) =>
          (vals || []).slice(0, 4).map(v => `<button type="button" class="preset-chip" data-ctx-pair="${esc(k)}=${esc(v)}">+ ${esc(k)}=${esc(v)}</button>`)
        ).join('')}
      </div>
    `, `<button type="button" class="btn btn-primary btn-sm" id="ctx-apply">Apply Contexts</button>`);

    const draw = () => {
      const wrap = document.getElementById('ctx-row-wrap');
      if (!wrap) return;
      wrap.innerHTML = rows.map((row, idx) => `
        <div style="display:flex;gap:6px;margin-bottom:6px;align-items:center">
          <input class="field-input" style="flex:1" list="ctx-key-list" data-ctx-k="${idx}" value="${esc(row.k)}" placeholder="key (server, world, dimension-type, gamemode...)">
          <input class="field-input" style="flex:1" list="ctx-value-list-${idx}" data-ctx-v="${idx}" value="${esc(row.v)}" placeholder="value">
          <datalist id="ctx-value-list-${idx}">${valueOptions(row.k)}</datalist>
          <button type="button" class="row-del-btn" data-ctx-del="${idx}" title="Remove">×</button>
        </div>
      `).join('');
      wrap.querySelectorAll('[data-ctx-k]').forEach(inp => {
        inp.oninput = () => {
          const i = Number(inp.dataset.ctxK);
          rows[i].k = inp.value.trim();
          const dl = document.getElementById(`ctx-value-list-${i}`);
          if (dl) dl.innerHTML = valueOptions(rows[i].k);
        };
      });
      wrap.querySelectorAll('[data-ctx-v]').forEach(inp => {
        inp.oninput = () => { rows[Number(inp.dataset.ctxV)].v = inp.value; };
      });
      wrap.querySelectorAll('[data-ctx-del]').forEach(btn => {
        btn.onclick = () => { rows.splice(Number(btn.dataset.ctxDel), 1); draw(); };
      });
    };

    draw();
    document.getElementById('ctx-add-row').onclick = () => { rows.push({ k: '', v: '' }); draw(); };
    document.querySelectorAll('[data-ctx-pair]').forEach(btn => {
      btn.onclick = () => {
        const pair = btn.dataset.ctxPair;
        const idx = pair.indexOf('=');
        if (idx > 0) rows.push({ k: pair.slice(0, idx), v: pair.slice(idx + 1) });
        draw();
      };
    });
    document.getElementById('ctx-apply').onclick = () => {
      const out = {};
      rows.forEach(r => {
        const k = String(r.k || '').trim().toLowerCase();
        const v = String(r.v || '').trim();
        if (k && v) out[k] = v;
      });
      onApply(out);
      closeModal();
    };
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
      STATE.editor.tracks.forEach(t => items.push({ kind: 'TRACK', title: t.name, sub: t.groups.join(' -> ') || '(empty)', act: () => { STATE.expandedTracks[t.name] = true; STATE.pendingTrackModal = t.name; navigate('/editor'); } }));
      STATE.editor.groups.forEach(g => items.push({ kind: 'GROUP', title: g.displayName || g.name, sub: `${g.name} · weight ${g.weight}`, act: () => { STATE.selected = { type: 'group', id: g.name }; navigate('/editor'); } }));
      STATE.editor.users.forEach(u => items.push({ kind: 'USER', title: u.username || u.uuid, sub: u.uuid, act: () => { STATE.selected = { type: 'user', id: u.uuid }; navigate('/editor'); } }));
    }

    openModal('Quick Search (Ctrl+K)', `
      <input type="text" id="global-search-input" class="field-input" placeholder="Search wiki pages, groups, users, tracks, tools...">
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
        STATE.baseline = deepClone(STATE.editor);
        render();
      } catch (e) {
        liveErr = e.message || String(e);
      }
    }

    const applyCode = (liveResult && liveResult.applyCode) || '';
    let portableCode = '';
    if (!liveResult) {
      try {
        portableCode = await encodePortableSp1(payload, STATE.baseline ? 'sp2_' : 'sp1_');
      } catch {
        portableCode = '';
      }
    }
    const preferredCode = applyCode || portableCode;
    const spCmd = preferredCode ? `/sp applyedits ${preferredCode}` : '';
    const spbCmd = preferredCode ? `/spb applyedits ${preferredCode}` : '';
    const tooLong = preferredCode.length > 256;

    let banner = '';
    if (liveResult) {
      banner = `<div class="val-pill val-true" style="padding:10px 12px;width:100%;justify-content:flex-start">
        ✔ Applied live to Gate (Revision #${liveResult.revision}: ${liveResult.groupsUpdated} groups updated, ${liveResult.tracksUpdated} tracks, ${liveResult.usersUpdated} users)
      </div>`;
    } else if (STATE.mode === 'demo') {
      banner = `<div class="val-pill val-undefined" style="padding:10px 12px;width:100%;justify-content:flex-start">
        Demo Session — Copy the portable code below or export JSON to apply to a real server.
      </div>`;
    } else if (liveErr) {
      banner = `<div class="val-pill val-false" style="padding:10px 12px;width:100%;justify-content:flex-start">
        Live save failed (${esc(liveErr)}) — use the fallback code below.
      </div>`;
    } else {
      banner = `<div class="val-pill val-true" style="padding:10px 12px;width:100%;justify-content:flex-start">
        Run the short command below in Minecraft or Gate console to apply your changes:
      </div>`;
    }

    const diff = (liveResult && Array.isArray(liveResult.diff)) ? liveResult.diff : [];
    const diffLine = line => {
      const prefix = String(line).charAt(0);
      const color = prefix === '+' ? '#7ee787' : prefix === '-' ? '#ff7b72' : '#d29922';
      return `<div style="font-family:var(--mono);font-size:12px;line-height:1.5;color:${color}">${esc(line)}</div>`;
    };
    const diffHtml = diff.length ? `
      <div class="field-group">
        <label>Changes Applied (${diff.length})</label>
        <div style="max-height:220px;overflow:auto;background:var(--bg-elevated);border:1px solid var(--border);border-radius:6px;padding:8px 10px">
          ${diff.map(diffLine).join('')}
        </div>
      </div>
    ` : '';

    const transferHtml = preferredCode ? `
      <details style="margin-top:6px">
        <summary style="cursor:pointer;color:var(--text-muted);font-size:12px">Transfer / apply on another server</summary>
        <div class="field-group" style="margin-top:10px">
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
      </details>
    ` : '';

    const longWarning = (!liveResult && tooLong) ? `
      <div class="val-pill val-false" style="padding:10px 12px;width:100%;justify-content:flex-start;white-space:normal">
        This code is ${preferredCode.length} characters — Minecraft chat allows 256. Download the <code>.spedit</code> file, upload it to <code>config/stonyperms/imports/</code>, then run <code>/sp applyedits file &lt;name&gt;</code>.
      </div>
    ` : '';

    openModal('Save Web Editor Changes', `
      ${banner}
      ${diffHtml}
      ${longWarning}
      ${liveResult ? transferHtml : (spCmd ? `
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
      ` : '')}
      <div class="field-group">
        <label>Backup / Transfer</label>
        <div style="display:flex;gap:8px;flex-wrap:wrap">
          <button type="button" class="btn btn-secondary btn-sm" id="btn-download-json">Download Snapshot JSON</button>
          ${preferredCode ? `<button type="button" class="btn btn-secondary btn-sm" id="btn-download-spedit">Download .spedit Code</button>` : ''}
        </div>
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
    const btnSpEdit = document.getElementById('btn-download-spedit');
    if (btnSpEdit && preferredCode) btnSpEdit.onclick = () => {
      const blob = new Blob([preferredCode], { type: 'text/plain' });
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = 'stonyperms-changes.spedit';
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
      STATE.editor.groups.push({ name, displayName, weight, parents: [], nodes: [], meta: {}, metaEntries: [], prefixes: [], suffixes: [] });
      STATE.editor.groups.sort((a, b) => (b.weight - a.weight) || a.name.localeCompare(b.name));
      STATE.deletedGroups.delete(name);
      STATE.selected = { type: 'group', id: name };
      markDirty();
      closeModal();
      render();
    };
  }

  function openCloneGroupModal(sourceGroup) {
    openModal(`Clone Group: ${sourceGroup.name}`, `
      <div class="field-group"><label>New Group Name (lowercase a-z0-9_+-)</label><input id="clg-name" class="field-input" value="${esc(sourceGroup.name)}-copy"></div>
      <div class="field-group"><label>Weight</label><input id="clg-weight" type="number" class="field-input" value="${esc(sourceGroup.weight || 0)}"></div>
      <div class="field-group"><label>Display Name (optional)</label><input id="clg-display" class="field-input" value="${esc(sourceGroup.displayName || '')}"></div>
    `, `<button type="button" class="btn btn-primary btn-sm" id="clg-submit">Clone Group</button>`);
    document.getElementById('clg-submit').onclick = () => {
      const name = document.getElementById('clg-name').value.trim().toLowerCase();
      if (!/^[a-z0-9_+-]{1,64}$/.test(name)) return toast('Invalid group name', true);
      if (STATE.editor.groups.some(g => g.name === name)) return toast('Group already exists', true);
      const clone = deepClone(sourceGroup);
      clone.name = name;
      clone.weight = parseInt(document.getElementById('clg-weight').value || '0', 10) || 0;
      clone.displayName = document.getElementById('clg-display').value.trim();
      STATE.editor.groups.push(clone);
      STATE.editor.groups.sort((a, b) => (b.weight - a.weight) || a.name.localeCompare(b.name));
      STATE.deletedGroups.delete(name);
      STATE.selected = { type: 'group', id: name };
      markDirty();
      closeModal();
      toast(`Cloned '${sourceGroup.name}' to '${name}'`);
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
    let working = tr.groups.slice();

    const drawModal = () => {
      const available = STATE.editor.groups.map(g => g.name).filter(g => !working.includes(g));
      openModal(`Edit Track: ${tr.name}`, `
        <div class="field-group">
          <label>Ordered Promotion Ladder (lowest → highest)</label>
          <div class="track-step-list">
            ${working.map((gn, idx) => `
              <div class="track-step">
                <span class="track-step-name"><span class="track-step-idx">#${idx + 1}</span> ${esc(gn)}</span>
                <div style="display:flex;gap:4px">
                  <button type="button" class="btn btn-secondary btn-xs" data-tr-up="${idx}" ${idx === 0 ? 'disabled' : ''}>▲</button>
                  <button type="button" class="btn btn-secondary btn-xs" data-tr-dn="${idx}" ${idx === working.length - 1 ? 'disabled' : ''}>▼</button>
                  <button type="button" class="row-del-btn" data-tr-rm="${idx}">×</button>
                </div>
              </div>
            `).join('') || '<div style="color:var(--text-dim);font-size:12px">No groups on this track yet.</div>'}
          </div>
        </div>
        ${available.length ? `
          <div class="field-group">
            <label>Add Group to Track</label>
            <div style="display:flex;gap:8px">
              <select id="et-add-sel" class="field-input">
                ${available.map(gn => `<option value="${esc(gn)}">${esc(gn)}</option>`).join('')}
              </select>
              <button type="button" class="btn btn-secondary btn-sm" id="et-add-btn">+ Append</button>
            </div>
          </div>
        ` : ''}
        <div class="field-group">
          <label>Clone Track As (optional)</label>
          <div style="display:flex;gap:8px">
            <input id="et-clone-name" class="field-input" placeholder="${esc(tr.name)}_copy">
            <button type="button" class="btn btn-secondary btn-sm" id="et-clone-btn">Clone</button>
          </div>
        </div>
      `, `
        <button type="button" class="btn btn-danger btn-sm" id="et-delete">Delete Track</button>
        <button type="button" class="btn btn-primary btn-sm" id="et-save">Save Track</button>
      `);

      document.querySelectorAll('[data-tr-up]').forEach(b => {
        b.onclick = () => {
          const i = Number(b.dataset.trUp);
          if (i > 0) { [working[i - 1], working[i]] = [working[i], working[i - 1]]; drawModal(); }
        };
      });
      document.querySelectorAll('[data-tr-dn]').forEach(b => {
        b.onclick = () => {
          const i = Number(b.dataset.trDn);
          if (i < working.length - 1) { [working[i + 1], working[i]] = [working[i], working[i + 1]]; drawModal(); }
        };
      });
      document.querySelectorAll('[data-tr-rm]').forEach(b => {
        b.onclick = () => { working.splice(Number(b.dataset.trRm), 1); drawModal(); };
      });
      const addBtn = document.getElementById('et-add-btn');
      if (addBtn) {
        addBtn.onclick = () => {
          const v = document.getElementById('et-add-sel').value;
          if (v && !working.includes(v)) { working.push(v); drawModal(); }
        };
      }
      document.getElementById('et-clone-btn').onclick = () => {
        const cloneName = document.getElementById('et-clone-name').value.trim().toLowerCase();
        if (!/^[a-z0-9_+-]{1,64}$/.test(cloneName)) return toast('Invalid track name', true);
        if (STATE.editor.tracks.some(t => t.name === cloneName)) return toast('Track already exists', true);
        STATE.editor.tracks.push({ name: cloneName, groups: working.slice() });
        STATE.expandedTracks[cloneName] = true;
        STATE.deletedTracks.delete(cloneName);
        markDirty();
        closeModal();
        toast(`Cloned track '${tr.name}' to '${cloneName}'`);
        render();
      };
      document.getElementById('et-delete').onclick = () => {
        STATE.editor.tracks = STATE.editor.tracks.filter(t => t.name !== tr.name);
        STATE.deletedTracks.add(tr.name);
        markDirty();
        closeModal();
        render();
      };
      document.getElementById('et-save').onclick = () => {
        tr.groups = working.slice();
        markDirty();
        closeModal();
        render();
      };
    };

    drawModal();
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
        parents: [], nodes: [], meta: {}, metaEntries: [], prefixes: [], suffixes: []
      });
      STATE.deletedUsers.delete(id);
      STATE.selected = { type: 'user', id };
      markDirty();
      closeModal();
      render();
    };
  }

  function openEditNodeModal(entity, row) {
    const expiryPresets = `
      <div class="preset-pills" style="margin-top:4px">
        ${['never', '1h', '12h', '1d', '7d', '30d'].map(p => `<button type="button" class="preset-chip" data-en-exp="${p}">${p}</button>`).join('')}
      </div>
    `;

    if (row.rowId === 'displayname:0' || row.rowId === 'weight:0') {
      const isWeight = row.rowId === 'weight:0';
      openModal(`Edit ${isWeight ? 'Weight' : 'Display Name'}`, `
        <div class="field-group">
          <label>${isWeight ? 'Weight (integer)' : 'Display Name'}</label>
          <input id="en-val" type="${isWeight ? 'number' : 'text'}" class="field-input" value="${esc(row.rawKey)}">
        </div>
      `, `<button type="button" class="btn btn-primary btn-sm" id="en-save">Save Changes</button>`);
      document.getElementById('en-save').onclick = () => {
        const v = document.getElementById('en-val').value.trim();
        if (isWeight) entity.weight = parseInt(v || '0', 10) || 0;
        else entity.displayName = v;
        markDirty();
        closeModal();
        render();
      };
      return;
    }

    const isPerm = row.badge === 'PERMISSION';
    const isParent = row.badge === 'INHERITANCE';
    const isAffix = row.badge === 'PREFIX' || row.badge === 'SUFFIX';
    const isMeta = row.badge === 'META';

    const curKey = isMeta ? (row.ref ? row.ref.key : row.metaKey)
      : isAffix ? String(row.ref.priority || 0)
      : isParent ? row.ref.group
      : row.ref.key;
    const curVal = isMeta ? (row.ref ? row.ref.value : row.metaVal)
      : isAffix ? row.ref.value
      : '';

    openModal(`Edit ${row.badge}`, `
      ${isPerm ? `
        <div class="field-group"><label>Permission Node</label><input id="en-key" class="field-input" style="font-family:var(--mono)" value="${esc(curKey)}"></div>
        <div class="field-group">
          <label>Value</label>
          <select id="en-bool" class="field-input">
            <option value="true" ${row.value ? 'selected' : ''}>true (Allow)</option>
            <option value="false" ${!row.value ? 'selected' : ''}>false (Deny)</option>
          </select>
        </div>
      ` : ''}
      ${isParent ? `
        <div class="field-group">
          <label>Parent Group</label>
          <select id="en-key" class="field-input">
            ${STATE.editor.groups.map(g => `<option value="${esc(g.name)}" ${curKey === g.name ? 'selected' : ''}>${esc(g.name)}</option>`).join('')}
          </select>
        </div>
      ` : ''}
      ${isAffix ? `
        <div class="field-group"><label>Priority</label><input id="en-key" type="number" class="field-input" value="${esc(curKey)}"></div>
        <div class="field-group">
          <label>Affix Text <span id="en-affix-preview" class="mc-preview-pill" style="margin-left:8px">${renderMcText(curVal)}</span></label>
          <input id="en-val" class="field-input" value="${esc(curVal)}">
        </div>
      ` : ''}
      ${isMeta ? `
        <div class="field-group"><label>Meta Key</label><input id="en-key" class="field-input" value="${esc(curKey)}"></div>
        <div class="field-group"><label>Meta Value</label><input id="en-val" class="field-input" value="${esc(curVal)}"></div>
      ` : ''}
      <div class="field-group">
        <label>Expiry (duration e.g. 7d, unix epoch, or never)</label>
        <input id="en-exp" class="field-input" value="${row.expiry ? esc(formatDuration(Math.max(1, row.expiry - Math.floor(Date.now() / 1000)))) : 'never'}">
        ${expiryPresets}
      </div>
      <div class="field-group">
        <label>Contexts (autocomplete: server, world, dimension-type, gamemode)</label>
        <input id="en-ctx" class="field-input" list="ctx-pair-list" value="${esc(contextsToInputString(row.contexts))}" placeholder="none">
        ${contextPairDatalist()}
      </div>
    `, `<button type="button" class="btn btn-primary btn-sm" id="en-save">Save Changes</button>`);

    const expIn = document.getElementById('en-exp');
    document.querySelectorAll('[data-en-exp]').forEach(btn => {
      btn.onclick = () => { expIn.value = btn.dataset.enExp; };
    });
    const ctxIn = document.getElementById('en-ctx');
    bindContextSuggestionChips(ctxIn);

    const valIn = document.getElementById('en-val');
    if (isAffix && valIn) {
      valIn.oninput = () => {
        const pv = document.getElementById('en-affix-preview');
        if (pv) pv.innerHTML = renderMcText(valIn.value);
      };
    }

    document.getElementById('en-save').onclick = () => {
      const nextExp = parseExpiryInput(expIn.value);
      const nextCtx = parseContextsString(ctxIn.value);
      const k = document.getElementById('en-key').value.trim();

      if (isPerm) {
        if (!k) return toast('Permission key cannot be empty', true);
        row.ref.key = k;
        row.ref.value = document.getElementById('en-bool').value === 'true';
        if (nextExp) row.ref.expiry = nextExp; else delete row.ref.expiry;
        if (nextCtx) row.ref.contexts = nextCtx; else delete row.ref.contexts;
      } else if (isParent) {
        row.ref.group = k.toLowerCase();
        if (nextExp) row.ref.expiry = nextExp; else delete row.ref.expiry;
        if (nextCtx) row.ref.contexts = nextCtx; else delete row.ref.contexts;
      } else if (isAffix) {
        row.ref.priority = parseInt(k || '0', 10) || 0;
        row.ref.value = valIn.value;
        if (nextExp) row.ref.expiry = nextExp; else delete row.ref.expiry;
        if (nextCtx) row.ref.contexts = nextCtx; else delete row.ref.contexts;
      } else if (isMeta) {
        if (!k) return toast('Meta key cannot be empty', true);
        const v = valIn.value;
        if (row.ref) {
          const oldKey = row.ref.key;
          row.ref.key = k;
          row.ref.value = v;
          if (nextExp) row.ref.expiry = nextExp; else delete row.ref.expiry;
          if (nextCtx) row.ref.contexts = nextCtx; else delete row.ref.contexts;
          if (entity.meta && oldKey && oldKey !== k) delete entity.meta[oldKey];
          if (!nextExp && !nextCtx) {
            entity.meta = entity.meta || {};
            entity.meta[k] = v;
          }
        } else {
          delete entity.meta[row.metaKey];
          if (!nextExp && !nextCtx) {
            entity.meta[k] = v;
          }
          entity.metaEntries = entity.metaEntries || [];
          entity.metaEntries.push({ key: k, value: v, ...(nextExp ? { expiry: nextExp } : {}), ...(nextCtx ? { contexts: nextCtx } : {}) });
        }
      }

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
      sbSearch.oninput = e => { STATE.sidebarQuery = e.target.value; render(); };
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
    const ownRows = buildUnifiedRows(entity, STATE.selected.type);
    const inheritedRows = buildInheritedRows(entity);
    const allRows = STATE.showInherited ? ownRows.concat(inheritedRows) : ownRows;
    const rowById = Object.fromEntries(allRows.map(r => [r.rowId, r]));

    const weightIn = document.getElementById('input-group-weight');
    if (weightIn) weightIn.onchange = e => { entity.weight = parseInt(e.target.value || '0', 10) || 0; markDirty(); render(); };
    const dispIn = document.getElementById('input-group-display');
    if (dispIn) dispIn.onchange = e => { entity.displayName = e.target.value; markDirty(); render(); };
    const primSel = document.getElementById('select-user-primary');
    if (primSel) primSel.onchange = e => { entity.primaryGroup = e.target.value; markDirty(); render(); };

    const copyUuid = document.getElementById('copy-user-uuid');
    if (copyUuid) {
      copyUuid.onclick = () => navigator.clipboard.writeText(entity.uuid).then(() => toast('Copied UUID'));
    }

    const btnCloneGroup = document.getElementById('btn-clone-group');
    if (btnCloneGroup && isGroup) btnCloneGroup.onclick = () => openCloneGroupModal(entity);

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
        <div class="field-group">
          <label>Contexts (autocomplete: server, world, dimension-type, gamemode)</label>
          <input id="ap-ctx" class="field-input" list="ctx-pair-list">
          ${contextPairDatalist()}
        </div>
      `, `<button type="button" class="btn btn-primary btn-sm" id="ap-submit">Add Parent</button>`);
      const ctxIn = document.getElementById('ap-ctx');
      bindContextSuggestionChips(ctxIn);
      document.getElementById('ap-submit').onclick = () => {
        const g = document.getElementById('ap-group').value;
        const ctxs = parseContextsString(ctxIn.value);
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
    if (nfInput) nfInput.oninput = e => { STATE.nodesFilter = e.target.value; render(); };

    const inhToggle = document.getElementById('btn-toggle-inherited');
    if (inhToggle) inhToggle.onclick = () => { STATE.showInherited = !STATE.showInherited; render(); };

    document.querySelectorAll('[data-type-filter]').forEach(btn => {
      btn.onclick = () => { STATE.nodeTypeFilter = btn.dataset.typeFilter; STATE.checkedRows.clear(); render(); };
    });

    document.querySelectorAll('[data-sort-col]').forEach(th => {
      th.onclick = () => {
        const col = th.dataset.sortCol;
        if (STATE.sortBy === col) {
          if (STATE.sortDir === 1) STATE.sortDir = -1;
          else STATE.sortBy = 'default';
        } else {
          STATE.sortBy = col;
          STATE.sortDir = 1;
        }
        render();
      };
    });

    const chkAll = document.getElementById('chk-all-rows');
    if (chkAll) {
      chkAll.onchange = e => {
        document.querySelectorAll('.row-chk').forEach(c => {
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

    const bulkFlip = document.getElementById('btn-bulk-flip');
    if (bulkFlip) {
      bulkFlip.onclick = () => {
        let changed = 0;
        Array.from(STATE.checkedRows).forEach(id => {
          const r = rowById[id];
          if (r && r.toggleable && r.onToggle) { r.onToggle(); changed++; }
        });
        if (changed) toast(`Toggled ${changed} permission value${changed === 1 ? '' : 's'}`);
        render();
      };
    }
    const bulkCtx = document.getElementById('btn-bulk-contexts');
    if (bulkCtx) {
      bulkCtx.onclick = () => {
        openContextsModal(`Set Contexts on ${STATE.checkedRows.size} Node(s)`, {}, nextCtx => {
          Array.from(STATE.checkedRows).forEach(id => {
            const r = rowById[id];
            if (r && r.ref) {
              if (nextCtx && Object.keys(nextCtx).length) {
                r.ref.contexts = deepClone(nextCtx);
              } else {
                delete r.ref.contexts;
              }
              markDirty();
            }
          });
          render();
        });
      };
    }
    const bulkExp = document.getElementById('btn-bulk-expiry');
    if (bulkExp) {
      bulkExp.onclick = () => {
        openModal(`Set Expiry on ${STATE.checkedRows.size} Node(s)`, `
          <div class="field-group">
            <label>Expiry (duration or 'never')</label>
            <input id="be-in" class="field-input" value="7d">
            <div class="preset-pills" style="margin-top:4px">
              ${['never', '1h', '12h', '1d', '7d', '30d'].map(p => `<button type="button" class="preset-chip" data-be-exp="${p}">${p}</button>`).join('')}
            </div>
          </div>
        `, `<button type="button" class="btn btn-primary btn-sm" id="be-ok">Apply Expiry</button>`);
        const expIn = document.getElementById('be-in');
        document.querySelectorAll('[data-be-exp]').forEach(btn => { btn.onclick = () => { expIn.value = btn.dataset.beExp; }; });
        document.getElementById('be-ok').onclick = () => {
          const nextExp = parseExpiryInput(expIn.value);
          Array.from(STATE.checkedRows).forEach(id => {
            const r = rowById[id];
            if (r && r.ref) {
              if (nextExp) r.ref.expiry = nextExp;
              else delete r.ref.expiry;
              markDirty();
            }
          });
          closeModal();
          render();
        };
      };
    }
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
    document.querySelectorAll('[data-edit-row]').forEach(btn => {
      btn.onclick = () => { const r = rowById[btn.dataset.editRow]; if (r) openEditNodeModal(entity, r); };
    });
    document.querySelectorAll('[data-copy-inh]').forEach(btn => {
      btn.onclick = () => {
        const r = rowById[btn.dataset.copyInh];
        if (r && r.copyPayload) { r.copyPayload(); toast(`Copied ${r.key} directly to target`); render(); }
      };
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
    document.querySelectorAll('[data-dock-exp-preset]').forEach(btn => {
      btn.onclick = () => {
        STATE.addForm.expiry = btn.dataset.dockExpPreset;
        if (dExp) dExp.value = STATE.addForm.expiry;
      };
    });

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
        entity.metaEntries = entity.metaEntries || [];
        const ctxKey = JSON.stringify(Object.keys(ctxs || {}).sort().map(k => [k, ctxs[k]]));
        entity.metaEntries = entity.metaEntries.filter(me => {
          const sameKey = String(me.key || '').toLowerCase() === mk.toLowerCase();
          const meCtxKey = JSON.stringify(Object.keys(me.contexts || {}).sort().map(k => [k, me.contexts[k]]));
          return !(sameKey && meCtxKey === ctxKey);
        });
        entity.metaEntries.push({ key: mk, value: rawKey, ...(expiry ? { expiry } : {}), ...(ctxs ? { contexts: ctxs } : {}) });
        if (!expiry && !ctxs) {
          entity.meta = entity.meta || {};
          entity.meta[mk] = rawKey;
        }
      }

      STATE.addForm.key = '';
      STATE.acOpen = false;
      markDirty();
      render();
    };

    const updateAutocompleteDom = () => {
      const col = dKey && dKey.parentElement;
      if (!col) return;
      const old = document.getElementById('ac-popover');
      if (old) old.remove();
      const canAc = STATE.addForm.type === 'PERMISSION' || STATE.addForm.type === 'INHERITANCE';
      const list = computeAutocomplete(STATE.addForm.key, STATE.addForm.type, entity);
      if (!STATE.acOpen || !list.length || !canAc) return;
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

    if (dKey) {
      dKey.onfocus = () => {
        if (STATE.addForm.type === 'PERMISSION' || STATE.addForm.type === 'INHERITANCE') {
          STATE.acOpen = true;
          STATE.acIndex = 0;
          updateAutocompleteDom();
        }
      };
      dKey.onblur = () => {
        setTimeout(() => { STATE.acOpen = false; updateAutocompleteDom(); }, 120);
      };
      dKey.oninput = e => {
        STATE.addForm.key = e.target.value;
        STATE.acOpen = STATE.addForm.type === 'PERMISSION' || STATE.addForm.type === 'INHERITANCE';
        STATE.acIndex = 0;
        updateAutocompleteDom();
        const affixPrev = document.getElementById('dock-affix-preview');
        if (affixPrev) {
          affixPrev.innerHTML = renderMcText(e.target.value) || '<span style="color:var(--text-dim)">preview</span>';
        }
      };
      dKey.onkeydown = e => {
        const list = computeAutocomplete(STATE.addForm.key, STATE.addForm.type, entity);
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

    document.getElementById('btn-dock-add').onclick = addNodeAction;
    document.getElementById('btn-dock-ctx').onclick = () => {
      openContextsModal('Add Context Constraints', STATE.addForm.contexts, ctxs => {
        STATE.addForm.contexts = ctxs;
        render();
      });
    };
    document.querySelectorAll('[data-del-dock-ctx]').forEach(btn => {
      btn.onclick = () => { delete STATE.addForm.contexts[btn.dataset.delDockCtx]; render(); };
    });
  }

  function bindViewerAndWikiEvents() {
    const vf = document.getElementById('input-verbose-filter');
    if (vf) vf.oninput = e => { STATE.verboseFilter = e.target.value; render(); };
    document.querySelectorAll('[data-verbose-ex]').forEach(chk => {
      chk.onchange = () => { STATE.verboseExclude[chk.dataset.verboseEx] = chk.checked; render(); };
    });
    const vExpAll = document.getElementById('btn-verbose-expand-all');
    if (vExpAll) {
      vExpAll.onclick = () => {
        document.querySelectorAll('[data-verbose-idx]').forEach(r => STATE.verboseExpanded.add(Number(r.dataset.verboseIdx)));
        render();
      };
    }
    const vColAll = document.getElementById('btn-verbose-collapse-all');
    if (vColAll) {
      vColAll.onclick = () => { STATE.verboseExpanded.clear(); render(); };
    }
    const vDl = document.getElementById('btn-verbose-download');
    if (vDl && STATE.verbose) {
      vDl.onclick = () => {
        const blob = new Blob([JSON.stringify(STATE.verbose, null, 2)], { type: 'application/json' });
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = 'stonyperms-verbose.json';
        a.click();
      };
    }
    document.querySelectorAll('[data-verbose-idx]').forEach(row => {
      row.onclick = () => {
        const i = Number(row.dataset.verboseIdx);
        if (STATE.verboseExpanded.has(i)) STATE.verboseExpanded.delete(i);
        else STATE.verboseExpanded.add(i);
        render();
      };
    });

    const tf = document.getElementById('input-tree-filter');
    if (tf) tf.oninput = e => { STATE.treeFilter = e.target.value; render(); };
    const bExp = document.getElementById('btn-tree-expand');
    if (bExp) bExp.onclick = () => { STATE.treeCollapsed.clear(); render(); };
    const bCol = document.getElementById('btn-tree-collapse');
    if (bCol) bCol.onclick = () => {
      document.querySelectorAll('[data-tree-toggle]').forEach(el => STATE.treeCollapsed.add(el.dataset.treeToggle));
      render();
    };
    document.querySelectorAll('[data-tree-toggle]').forEach(el => {
      el.onclick = ev => {
        if (ev.target.closest('button')) return;
        const p = el.dataset.treeToggle;
        if (STATE.treeCollapsed.has(p)) STATE.treeCollapsed.delete(p);
        else STATE.treeCollapsed.add(p);
        render();
      };
    });
    document.querySelectorAll('[data-copy-node]').forEach(el => {
      el.onclick = ev => {
        if (ev.target.closest('[data-use-tree-node]')) return;
        ev.stopPropagation();
        navigator.clipboard.writeText(el.dataset.copyNode).then(() => toast('Copied ' + el.dataset.copyNode));
      };
    });
    document.querySelectorAll('[data-use-tree-node]').forEach(btn => {
      btn.onclick = ev => {
        ev.stopPropagation();
        STATE.addForm.type = 'PERMISSION';
        STATE.addForm.key = btn.dataset.useTreeNode;
        if (!STATE.selected && STATE.editor && STATE.editor.groups.length) {
          STATE.selected = { type: 'group', id: STATE.editor.groups[0].name };
        }
        toast('Pre-filled ' + btn.dataset.useTreeNode + ' in Editor');
        navigate('/editor');
      };
    });

    document.querySelectorAll('[data-wiki-slug]').forEach(btn => {
      btn.onclick = () => { STATE.wikiSlug = btn.dataset.wikiSlug; render(); };
    });
  }

  function render() {
    // Capture focus, selection, and scroll offsets across re-render
    const active = document.activeElement;
    const activeId = active && active.id ? active.id : '';
    const selStart = active && typeof active.selectionStart === 'number' ? active.selectionStart : null;
    const selEnd = active && typeof active.selectionEnd === 'number' ? active.selectionEnd : null;

    const scrollSelectors = ['.sidebar-scroll', '.editor-body', '.viewer-main', '.wiki-sidebar', '.wiki-article'];
    const scrolls = scrollSelectors.map(sel => {
      const node = document.querySelector(sel);
      return [sel, node ? node.scrollTop : 0];
    });

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

    // Restore scroll offsets & active input focus/cursor position
    for (const [sel, top] of scrolls) {
      const node = document.querySelector(sel);
      if (node && top) node.scrollTop = top;
    }
    if (activeId) {
      const target = document.getElementById(activeId);
      if (target) {
        target.focus({ preventScroll: true });
        if (selStart !== null && typeof target.setSelectionRange === 'function') {
          try { target.setSelectionRange(selStart, selEnd); } catch {}
        }
      }
    }

    if (STATE.route === '/editor' && STATE.pendingTrackModal) {
      const tn = STATE.pendingTrackModal;
      STATE.pendingTrackModal = '';
      openEditTrackModal(tn);
    }
  }

  async function init() {
    const loc = parseRouteFromLocation();
    STATE.route = loc.route;
    STATE.wikiSlug = loc.subSlug || 'home';
    const apiBase = resolveApiOrigin(loc.api);

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
      } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && STATE.route === '/editor' && STATE.editor) {
        e.preventDefault();
        openSaveModal();
      } else if (e.key === 'Escape') {
        closeModal();
      }
    });
    window.addEventListener('beforeunload', e => {
      if (STATE.dirtyCount > 0 && STATE.mode === 'live') {
        e.preventDefault();
        e.returnValue = '';
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
