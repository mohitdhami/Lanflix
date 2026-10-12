/* LanFlix SPA — Netflix clone for LAN */
const $ = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const fmtDur = (s) => { s = s || 0; const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60); return h ? `${h}h ${m}m` : `${m}m`; };
const fmtTime = (s) => { s = Math.floor(s || 0); const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), x = s % 60; return (h ? h + ':' : '') + String(m).padStart(2, '0') + ':' + String(x).padStart(2, '0'); };

const store = {
  get token() { return localStorage.getItem('lanflix_token'); },
  set token(v) { v ? localStorage.setItem('lanflix_token', v) : localStorage.removeItem('lanflix_token'); },
  get profile() { try { return JSON.parse(localStorage.getItem('lanflix_profile')); } catch { return null; } },
  set profile(v) { v ? localStorage.setItem('lanflix_profile', JSON.stringify(v)) : localStorage.removeItem('lanflix_profile'); },
};

const S = { user: null, profiles: [], profile: store.profile, view: 'home', rows: null, genres: [], heroIdx: 0, adminTab: 'overview', adminData: {}, search: '', searchGenre: '', searchType: '' };

async function api(path, opts = {}) {
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  if (store.token) headers.Authorization = 'Bearer ' + store.token;
  const res = await fetch(path, { ...opts, headers });
  let data = null;
  try { data = await res.json(); } catch { data = {}; }
  if (!res.ok) throw new Error(data.error || `Request failed (${res.status})`);
  return data;
}

function toast(msg) {
  const t = document.createElement('div');
  t.className = 'toast'; t.textContent = msg;
  $('#toast-root').appendChild(t);
  setTimeout(() => t.remove(), 2600);
}

function posterImg(c, cls = '') {
  const src = c.poster || '';
  const fb = `this.onerror=null;this.src='data:image/svg+xml;utf8,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="400" height="600"><rect width="400" height="600" fill="#2a2a2a"/><text x="200" y="300" font-size="34" text-anchor="middle" fill="#888" font-family="Arial">${esc(c.title).slice(0, 22)}</text></svg>`)}'`;
  return `<img loading="lazy" src="${esc(src)}" onerror="${fb}" alt="${esc(c.title)}" />`;
}

/* ================= AUTH ================= */
function renderAuth(mode = 'login') {
  S.view = 'auth';
  $('#app').innerHTML = `
  <div class="auth-wrap">
    <div class="auth-card">
      <div class="brand">LANFLIX <small>local</small></div>
      <h2>${mode === 'login' ? 'Sign In' : 'Create account'}</h2>
      <div class="auth-err" id="auth-err"></div>
      ${mode === 'signup' ? `<input id="auth-name" class="focusable" placeholder="Display name" />` : ''}
      <input id="auth-email" class="focusable" placeholder="Email" type="email" autocomplete="username" />
      <input id="auth-pass" class="focusable" placeholder="Password" type="password" autocomplete="current-password" />
      <button class="btn btn-red focusable" id="auth-go">${mode === 'login' ? 'Sign In' : 'Sign Up'}</button>
      <div class="auth-switch">${mode === 'login'
        ? `New here? <b id="to-signup">Sign up</b><br><span style="font-size:12px">Demo: demo@lan.local / demo1234 &nbsp;•&nbsp; Admin: admin@lan.local / admin123</span>`
        : `Have an account? <b id="to-login">Sign in</b>`}</div>
      <div class="lan-box" id="lan-box">Finding this server on your LAN…</div>
    </div>
  </div>`;
  const err = (m) => { const e = $('#auth-err'); e.style.display = 'block'; e.textContent = m; };
  $('#auth-go').onclick = async () => {
    try {
      const email = $('#auth-email').value.trim(), password = $('#auth-pass').value;
      const name = mode === 'signup' ? ($('#auth-name')?.value.trim() || '') : undefined;
      if (!email || !password) return err('Enter email + password');
      const url = mode === 'login' ? '/api/auth/login' : '/api/auth/signup';
      const d = await api(url, { method: 'POST', body: JSON.stringify({ email, password, name }) });
      store.token = d.token; S.user = d.user;
      await afterLogin();
    } catch (e) { err(e.message); }
  };
  $('#auth-pass').onkeydown = (e) => { if (e.key === 'Enter') $('#auth-go').click(); };
  if (mode === 'login') $('#to-signup').onclick = () => renderAuth('signup');
  else $('#to-login').onclick = () => renderAuth('login');
  fetch('/api/server-info').then(r => r.json()).then(info => {
    $('#lan-box').innerHTML = `📺 <b style="color:#fff">Watch on TV / phone (same Wi-Fi):</b><br>` +
      info.ips.map(ip => `<code>http://${ip}:${info.port}</code>`).join('<br>') +
      `<div class="qr-hint">Open one of these URLs in any browser on your network. No internet needed (demo posters need internet once).</div>`;
  }).catch(() => { $('#lan-box').textContent = 'LAN info unavailable'; });
}

async function afterLogin() {
  try {
    const me = await api('/api/auth/me');
    S.user = me.user;
    const p = await api('/api/profiles');
    S.profiles = p.profiles;
    if (!S.profiles.length) { // create default
      const c = await api('/api/profiles', { method: 'POST', body: JSON.stringify({ name: S.user.name || 'User' }) });
      S.profiles = [c.profile];
    }
    const saved = store.profile;
    const stillThere = saved && S.profiles.find(x => x.id === saved.id);
    if (stillThere) { S.profile = stillThere; store.profile = stillThere; renderMain(); }
    else renderGate();
  } catch (e) { store.token = null; renderAuth(); }
}

/* ================= PROFILES GATE ================= */
function renderGate(manage = false) {
  S.view = 'gate';
  $('#app').innerHTML = `
  <div class="gate">
    <div class="brand" style="margin-bottom:8px">LANFLIX</div>
    <h1>${manage ? 'Manage Profiles' : "Who's watching?"}</h1>
    <div class="profile-grid">
      ${S.profiles.map(p => `
        <div class="profile-card focusable" tabindex="0" data-id="${p.id}">
          <div class="avatar" style="background:${esc(p.color || '#333')}">${esc(p.avatar || '🎬')}</div>
          <div class="pname">${esc(p.name)}</div>
          ${p.is_kids ? `<span class="kids-badge">KIDS</span>` : ''}
          ${manage ? `<div style="margin-top:8px;display:flex;gap:6px;justify-content:center">
            <button class="btn btn-ghost btn-sm" data-edit="${p.id}">Edit</button>
            <button class="btn btn-ghost btn-sm" data-del="${p.id}">✕</button></div>` : ''}
        </div>`).join('')}
      ${S.profiles.length < 5 ? `<div class="profile-card focusable" tabindex="0" id="add-p">
        <div class="avatar" style="background:#222;font-size:50px;color:#888">＋</div>
        <div class="pname">Add Profile</div></div>` : ''}
    </div>
    ${manage ? `<div style="margin-top:20px;max-width:520px;width:100%" id="p-form"></div>` : ''}
    <div class="gate-actions">
      ${manage ? `<button class="btn btn-light focusable" id="done-m">Done</button>`
        : `<button class="btn btn-ghost focusable" id="manage-p">Manage Profiles</button>`}
      <button class="btn btn-ghost focusable" id="logout">Sign out (${esc(S.user?.email || '')})</button>
    </div>
  </div>`;
  $$('.profile-card[data-id]').forEach(el => {
    el.onclick = (e) => {
      if (e.target.dataset.edit || e.target.dataset.del) return;
      if (manage) return;
      const p = S.profiles.find(x => x.id == el.dataset.id);
      if (p.pin) {
        const v = prompt(`Enter PIN for ${p.name}`);
        if (v !== p.pin) return toast('Wrong PIN');
      }
      S.profile = p; store.profile = p;
      renderMain();
    };
  });
  const addBtn = $('#add-p');
  if (addBtn) addBtn.onclick = () => profileForm();
  $$('[data-del]').forEach(b => b.onclick = async (e) => {
    e.stopPropagation();
    if (!confirm('Delete this profile? Watch history will be lost.')) return;
    await api('/api/profiles/' + b.dataset.del, { method: 'DELETE' });
    S.profiles = (await api('/api/profiles')).profiles;
    renderGate(true);
  });
  $$('[data-edit]').forEach(b => b.onclick = (e) => { e.stopPropagation(); profileForm(S.profiles.find(x => x.id == b.dataset.edit)); });
  const m = $('#manage-p'); if (m) m.onclick = () => renderGate(true);
  const d = $('#done-m'); if (d) d.onclick = () => renderGate(false);
  $('#logout').onclick = () => { store.token = null; store.profile = null; S.user = null; renderAuth(); };

  function profileForm(p = null) {
    const f = $('#p-form') || (() => { const d = document.createElement('div'); d.id = 'p-form'; d.style.cssText = 'margin-top:20px;max-width:520px;width:100%'; $('.gate').appendChild(d); return d; })();
    const avatars = ['😎', '🎬', '🦊', '👑', '🐼', '🦁', '👾', '🌸'];
    f.innerHTML = `<div class="panel">
      <h3>${p ? 'Edit' : 'Add'} profile</h3>
      <div style="display:flex;gap:8px;margin-bottom:10px;flex-wrap:wrap">${avatars.map(a => `<button class="circle-btn ${p?.avatar === a ? 'on' : ''}" data-av="${a}">${a}</button>`).join('')}</div>
      <div class="form-grid">
        <input class="tbl-input" id="pf-name" placeholder="Name" value="${esc(p?.name || '')}" />
        <input class="tbl-input" id="pf-pin" placeholder="PIN (optional)" value="${esc(p?.pin || '')}" />
        <label style="font-size:13px;color:#bbb;display:flex;gap:8px;align-items:center"><input type="checkbox" id="pf-kids" ${p?.is_kids ? 'checked' : ''} /> Kids profile (hides 16+/18+)</label>
        <input class="tbl-input" id="pf-color" type="color" value="${esc(p?.color || '#E50914')}" />
      </div>
      <div style="margin-top:12px;display:flex;gap:8px">
        <button class="btn btn-red btn-sm" id="pf-save">Save</button>
      </div></div>`;
    let av = p?.avatar || '😎';
    $$('[data-av]', f).forEach(b => b.onclick = () => { av = b.dataset.av; $$('[data-av]', f).forEach(x => x.classList.remove('on')); b.classList.add('on'); });
    $('#pf-save').onclick = async () => {
      const body = { name: $('#pf-name').value.trim() || 'Profile', avatar: av, color: $('#pf-color').value, is_kids: $('#pf-kids').checked, pin: $('#pf-pin').value.trim() || null };
      if (!body.name) return toast('Name required');
      if (p) await api('/api/profiles/' + p.id, { method: 'PUT', body: JSON.stringify(body) });
      else await api('/api/profiles', { method: 'POST', body: JSON.stringify(body) });
      S.profiles = (await api('/api/profiles')).profiles;
      renderGate(manage);
    };
  }
}

/* ================= MAIN SHELL ================= */
function shell(active, inner) {
  return `
  <nav class="nav" id="nav">
    <div class="logo" id="nav-logo">LANFLIX</div>
    <div class="nav-links">
      <span data-v="home" class="${active === 'home' ? 'active' : ''}">Home</span>
      <span data-v="series" class="${active === 'series' ? 'active' : ''}">TV Shows</span>
      <span data-v="movies" class="${active === 'movies' ? 'active' : ''}">Movies</span>
      <span data-v="mylist" class="${active === 'mylist' ? 'active' : ''}">My List</span>
      <span data-v="search" class="${active === 'search' ? 'active' : ''}">Search</span>
      ${S.user?.role === 'admin' ? `<span data-v="admin" class="${active === 'admin' ? 'active' : ''}">🛠 CRM</span>` : ''}
    </div>
    <div class="nav-right">
      <div class="nav-search"><input id="nav-q" class="focusable" placeholder="🔍 Titles…" value="${esc(S.search)}" /></div>
      <button class="icon-btn focusable" id="nav-switch" title="Switch profile">${esc(S.profile?.avatar || '👤')}</button>
      <button class="icon-btn focusable" id="nav-out" title="Sign out">⏻</button>
    </div>
  </nav>
  <div id="view">${inner}</div>
  <div class="tv-hint">TV remote: ← → ↑ ↓ navigate • OK select • ⌫ back</div>`;
}

function bindShell(active) {
  $$('#app [data-v]').forEach(el => el.onclick = () => nav(el.dataset.v));
  $('#nav-logo').onclick = () => nav('home');
  $('#nav-switch').onclick = () => renderGate();
  $('#nav-out').onclick = () => { store.token = null; store.profile = null; S.user = null; S.profile = null; renderAuth(); };
  const q = $('#nav-q');
  q.onkeydown = (e) => { if (e.key === 'Enter') { S.search = q.value.trim(); nav('search'); } };
  q.oninput = () => { S.search = q.value.trim(); };
  window.onscroll = () => $('#nav')?.classList.toggle('solid', window.scrollY > 30);
}

function nav(v) {
  S.view = v;
  renderMain();
}

async function renderMain() {
  if (!S.profile) return renderGate();
  $('#app').innerHTML = shell(S.view, `<div class="page"><div class="empty">Loading…</div></div>`);
  bindShell(S.view);
  try {
    if (S.view === 'home') await viewHome();
    else if (S.view === 'series') await viewType('series');
    else if (S.view === 'movies') await viewType('movie');
    else if (S.view === 'mylist') await viewMyList();
    else if (S.view === 'search') await viewSearch();
    else if (S.view === 'admin') await viewAdmin();
  } catch (e) { $('#view').innerHTML = `<div class="page"><div class="empty">Error: ${esc(e.message)}</div></div>`; }
}

/* ================= HOME ================= */
async function viewHome() {
  const pid = S.profile.id;
  const [rows, genres] = await Promise.all([
    api(`/api/browse/rows?profileId=${pid}`).catch(() => null),
    api('/api/genres').catch(() => ({ genres: [] })),
  ]);
  S.rows = rows; S.genres = genres.genres || [];
  if (!rows) { $('#view').innerHTML = `<div class="page"><div class="empty">No content yet. Admin → Rescan after adding files to <code>media/</code>.</div></div>`; return; }
  const hero = rows.hero[0] || rows.trending[0];
  S.heroIdx = 0;
  $('#view').innerHTML = `
    <section class="hero" id="hero"></section>
    <div class="rows" id="home-rows">
      ${rows.continue?.length ? `<div class="row"><h2>Continue Watching for ${esc(S.profile.name)}</h2><div class="rail">${rows.continue.map(c => hCard(c, true)).join('')}</div></div>` : ''}
      ${rows.myList?.length ? `<div class="row"><h2>My List</h2><div class="rail">${rows.myList.map(c => hCard(c)).join('')}</div></div>` : ''}
      <div class="row"><h2>🔥 Trending Now <span class="explore" data-explore="trending">Explore all ›</span></h2><div class="rail">${rows.trending.map(c => hCard(c)).join('')}</div></div>
      <div class="row"><h2>🏆 Top 10 on LanFlix Today</h2><div class="rail">${rows.top10.map((c, i) => `
        <div class="rank-card focusable" tabindex="0" data-open="${c.id}"><div class="num">${i + 1}</div>${posterImg(c)}</div>`).join('')}</div></div>
      <div class="row"><h2>🆕 New on LanFlix</h2><div class="rail">${rows.fresh.map(c => hCard(c)).join('')}</div></div>
      <div class="row"><h2>🎬 Movies</h2><div class="rail">${rows.movies.map(c => hCard(c, false, true)).join('')}</div></div>
      <div class="row"><h2>📺 TV Shows</h2><div class="rail">${(rows.series || []).map(c => hCard(c, false, true)).join('')}</div></div>
      ${(rows.genreRows || []).map(g => `<div class="row"><h2>${esc(g.title)} <span class="explore" data-genre="${esc(g.genre)}">Explore all ›</span></h2><div class="rail">${g.items.map(c => hCard(c)).join('')}</div></div>`).join('')}
    </div>`;
  const paintHero = (h) => {
    $('#hero').innerHTML = `
      <div class="hero-bg" style="background-image:url('${esc(h.backdrop || h.poster)}')"></div>
      <div class="hero-content">
        <div class="hero-tag"><span class="n">N</span> ${h.type === 'series' ? 'SERIES' : 'FILM'} • LANFLIX ORIGINAL</div>
        <h1>${esc(h.title)}</h1>
        <div class="hero-meta"><span class="green">${Math.round(70 + (h.views_count % 28))}% Match</span><span>${h.year || ''}</span><span class="badge">${esc(h.maturity_rating || 'U/A 13+')}</span><span>${h.type === 'movie' ? fmtDur(h.duration_sec) : (h.genres?.join(', ') || '')}</span><span class="badge green">Top 10</span></div>
        <div class="hero-desc">${esc(h.description || 'No description yet.')}</div>
        <div class="hero-btns">
          <button class="btn btn-play focusable" id="hero-play">▶ Play</button>
          <button class="btn btn-info focusable" id="hero-info">ⓘ More Info</button>
        </div>
        <div class="hero-dots">${rows.hero.slice(0, 5).map((_, i) => `<span class="${i === S.heroIdx ? 'on' : ''}" data-dot="${i}"></span>`).join('')}</div>
      </div>`;
    $('#hero-play').onclick = () => playContent(h.id);
    $('#hero-info').onclick = () => openDetail(h.id);
    $$('#hero [data-dot]').forEach(d => d.onclick = () => { S.heroIdx = +d.dataset.dot; paintHero(rows.hero[S.heroIdx]); });
  };
  if (hero) paintHero(hero);
  // rotate hero every 9s
  clearInterval(window.__heroT);
  if (rows.hero.length > 1) window.__heroT = setInterval(() => {
    if (S.view !== 'home' || $('#player-root').innerHTML) return;
    S.heroIdx = (S.heroIdx + 1) % Math.min(5, rows.hero.length);
    const h = rows.hero[S.heroIdx]; if (h && $('#hero')) paintHero(h);
  }, 9000);
  bindCards();
  $$('#view [data-explore]').forEach(e => e.onclick = () => { S.search = ''; S.searchGenre = ''; nav('search'); });
  $$('#view [data-genre]').forEach(e => e.onclick = () => { S.searchGenre = e.dataset.genre; S.search = ''; nav('search'); });
}

function hCard(c, progress = false, landscape = false) {
  const pct = c.resume_of ? Math.round((c.resume_at / c.resume_of) * 100) : 0;
  return `<div class="card focusable ${landscape ? 'landscape' : ''}" tabindex="0" data-open="${c.id}">
    ${posterImg(c)}
    <div class="cbody"><div class="ctitle">${esc(c.title)}</div>
    <div class="cmeta">${c.year || ''} • ${esc((c.genres || []).slice(0, 2).join(', '))}</div>
    ${progress && c.resume_of ? `<div class="progress"><i style="width:${pct}%"></i></div>` : ''}</div></div>`;
}

function bindCards() {
  $$('#view [data-open]').forEach(el => {
    el.onclick = () => openDetail(el.dataset.open);
    el.onkeydown = (e) => { if (e.key === 'Enter') openDetail(el.dataset.open); };
  });
}

async function viewType(type) {
  const pid = S.profile.id;
  const d = await api(`/api/content?type=${type}&profileId=${pid}&limit=60&sort=trending`);
  $('#view').innerHTML = `<div class="page"><h1>${type === 'series' ? 'TV Shows' : 'Movies'}</h1>
    <div class="sub">${d.total} titles • streaming over your local network</div>
    <div class="grid">${d.items.map(c => hCard(c)).join('') || '<div class="empty">No titles yet.</div>'}</div></div>`;
  bindCards();
}

async function viewMyList() {
  const pid = S.profile.id;
  const [l, h] = await Promise.all([api(`/api/mylist?profileId=${pid}`), api(`/api/history?profileId=${pid}&limit=15`)]);
  $('#view').innerHTML = `<div class="page"><h1>My List</h1><div class="sub">${l.items.length} saved</div>
    ${l.items.length ? `<div class="grid">${l.items.map(c => hCard(c)).join('')}</div>` : `<div class="empty">Nothing here yet. Hover a title → ＋ My List.<br><br><button class="btn btn-light focusable" id="go-browse">Browse trending</button></div>`}
    <h1 style="margin-top:36px">Recently Watched</h1><div class="sub">Your watch history on this profile</div>
    <div class="panel"><table><tr><th>Title</th><th>Episode</th><th>Position</th><th>When</th></tr>
    ${h.items.map(x => `<tr><td>${esc(x.title)}</td><td>${x.ep_title ? `S${x.season} E${x.episode} — ${esc(x.ep_title)}` : '—'}</td><td>${fmtTime(x.position_sec)} / ${fmtTime(x.duration_sec)}</td><td>${esc(x.updated_at)}</td></tr>`).join('') || '<tr><td colspan=4>No history yet</td></tr>'}</table></div></div>`;
  bindCards();
  $('#go-browse') && ($('#go-browse').onclick = () => nav('home'));
}

async function viewSearch() {
  const pid = S.profile.id;
  const g = S.genres.length ? S.genres : (await api('/api/genres')).genres;
  S.genres = g;
  const q = new URLSearchParams({ profileId: pid, limit: 48, sort: 'added', search: S.search || '', genre: S.searchGenre || '', ...(S.searchType ? { type: S.searchType } : {}) });
  const d = await api('/api/content?' + q.toString());
  $('#view').innerHTML = `<div class="page"><h1>Search</h1>
    <div class="search-bar"><input id="sq" class="focusable" placeholder="Search titles, directors…" value="${esc(S.search)}" />
    <select id="st" class="tbl-input focusable" style="max-width:150px"><option value="">All types</option><option value="movie" ${S.searchType === 'movie' ? 'selected' : ''}>Movies</option><option value="series" ${S.searchType === 'series' ? 'selected' : ''}>TV Shows</option></select></div>
    <div class="chips"><span class="chip ${!S.searchGenre ? 'on' : ''}" data-g="">All</span>${g.map(x => `<span class="chip ${S.searchGenre === x ? 'on' : ''}" data-g="${esc(x)}">${esc(x)}</span>`).join('')}</div>
    <div class="sub">${d.total} result(s)</div>
    <div class="grid">${d.items.map(c => hCard(c)).join('') || '<div class="empty">No matches. Try another keyword.</div>'}</div></div>`;
  bindCards();
  let t;
  $('#sq').oninput = (e) => { clearTimeout(t); t = setTimeout(() => { S.search = e.target.value.trim(); viewSearch(); }, 450); };
  $('#sq').onkeydown = (e) => { if (e.key === 'Enter') { S.search = e.target.value.trim(); viewSearch(); } };
  $('#st').onchange = async (e) => {
    S.searchType = e.target.value;
    const qq = new URLSearchParams({ profileId: pid, limit: 48, sort: 'added', search: S.search || '', genre: S.searchGenre || '', type: S.searchType });
    const dd = await api('/api/content?' + qq.toString());
    $('.grid').innerHTML = dd.items.map(c => hCard(c)).join('') || '<div class="empty">No matches.</div>';
    bindCards();
  };
  $$('#view [data-g]').forEach(c => c.onclick = () => { S.searchGenre = c.dataset.g; viewSearch(); });
}

/* ================= DETAIL MODAL ================= */
async function openDetail(id) {
  const pid = S.profile.id;
  const { item: c } = await api(`/api/content/${id}?profileId=${pid}`);
  const eps = c.episodes || [];
  const seasons = [...new Set(eps.map(e => e.season))];
  const more = (await api(`/api/content?genre=${encodeURIComponent((c.genres || [])[0] || '')}&limit=8&profileId=${pid}`)).items.filter(x => x.id !== c.id);
  $('#modal-root').innerHTML = `
  <div class="modal-back" id="mback"><div class="modal">
    <button class="modal-close focusable" id="mclose">✕</button>
    <div class="modal-hero" style="background-image:url('${esc(c.backdrop || c.poster)}')">
      <div class="modal-title"><h2>${esc(c.title)}</h2>
        <div class="modal-actions">
          <button class="btn btn-play focusable" id="mplay">▶ ${c.resume_at ? 'Resume' : 'Play'}</button>
          ${c.trailer_url ? `<button class="btn btn-info btn-sm focusable" id="mtrailer">🎞 Trailer</button>` : ''}
          <button class="circle-btn focusable" id="mlist" title="My List">${c.in_list ? '✓' : '＋'}</button>
          <button class="circle-btn focusable ${c.my_rating === 1 ? 'on' : ''}" id="mlike" title="I like this">👍</button>
          <button class="circle-btn focusable ${c.my_rating === -1 ? 'on' : ''}" id="mdislike" title="Not for me">👎</button>
        </div></div></div>
    <div class="modal-body">
      <div><span class="green">${c.views_count} plays</span> &nbsp; ${c.year || ''} &nbsp; <span class="badge">${esc(c.maturity_rating || '')}</span> &nbsp; ${c.type === 'movie' ? fmtDur(c.duration_sec) : `${eps.length} episodes`}<br><br>
      <p style="font-size:14.5px;line-height:1.6">${esc(c.description || 'No description.')}</p></div>
      <div class="meta"><b>Cast:</b> ${esc((c.cast || []).join(', ') || '—')}<br><b>Genres:</b> ${esc((c.genres || []).join(', '))}<br><b>Director:</b> ${esc(c.director || '—')}<br><b>Rating:</b> ${c.rating_count ? `${c.rating_avg > 0 ? '👍' : '👎'} ${c.rating_count} vote(s)` : 'No ratings yet'}</div>
      ${c.type === 'series' ? `<div class="ep-list"><h3 style="margin:6px 0 4px">Episodes ${seasons.length > 1 ? `<select id="season-sel" class="tbl-input focusable" style="max-width:140px;margin-left:10px">${seasons.map(s => `<option>S${s}</option>`).join('')}</select>` : ''}</h3>
        <div id="epwrap">${eps.map(e => hEp(e)).join('') || '<div class="empty">No episodes found. Add video files under media/series/' + esc(c.title) + '</div>'}</div></div>` : ''}
      ${more.length ? `<div class="ep-list"><h3>More Like This</h3><div class="more-row">${more.map(x => `<div class="card focusable" data-open="${x.id}">${posterImg(x)}<div class="cbody"><div class="ctitle">${esc(x.title)}</div></div></div>`).join('')}</div></div>` : ''}
    </div></div></div>`;
  $('#mclose').onclick = closeModal;
  $('#mback').onclick = (e) => { if (e.target.id === 'mback') closeModal(); };
  $('#mplay').onclick = () => { closeModal(); playContent(c.id); };
  const mt = $('#mtrailer');
  if (mt) mt.onclick = () => {
    const t = document.createElement('div');
    t.className = 'modal-back'; t.id = 'trailer-back';
    t.innerHTML = `<div class="modal" style="max-width:720px"><button class="modal-close focusable" id="trailer-x">✕</button>
      <video controls autoplay playsinline style="width:100%;max-height:70vh;background:#000" src="${esc(c.trailer_url)}"></video></div>`;
    $('#modal-root').appendChild(t);
    $('#trailer-x').onclick = () => t.remove();
    t.onclick = (e) => { if (e.target.id === 'trailer-back') t.remove(); };
    $('#trailer-x').focus();
  };
  $('#mlist').onclick = async () => {
    if (c.in_list) { await api(`/api/mylist/${c.id}?profileId=${pid}`, { method: 'DELETE' }); c.in_list = false; $('#mlist').textContent = '＋'; }
    else { await api('/api/mylist', { method: 'POST', body: JSON.stringify({ profileId: pid, contentId: c.id }) }); c.in_list = true; $('#mlist').textContent = '✓'; toast('Added to My List'); }
  };
  $('#mlike').onclick = () => rate(c, 1);
  $('#mdislike').onclick = () => rate(c, -1);
  async function rate(c, v) {
    await api('/api/rate', { method: 'POST', body: JSON.stringify({ profileId: pid, contentId: c.id, value: v }) });
    toast(v === 1 ? '👍 Rated: liked' : '👎 Rated: disliked');
    openDetail(c.id);
  }
  $$('#modal-root [data-open]').forEach(el => el.onclick = () => openDetail(el.dataset.open));
  $$('#modal-root .ep').forEach(el => el.onclick = () => { const epId = el.dataset.ep; closeModal(); playContent(c.id, epId); });
  const sel = $('#season-sel');
  if (sel) sel.onchange = () => {
    const s = parseInt(sel.value.replace('S', ''));
    $('#epwrap').innerHTML = eps.filter(e => e.season === s).map(e => hEp(e)).join('');
    $$('#modal-root .ep').forEach(el => el.onclick = () => { closeModal(); playContent(c.id, el.dataset.ep); });
  };
}
function hEp(e) {
  return `<div class="ep focusable" tabindex="0" data-ep="${e.id}"><div class="n">${e.episode}</div>
    <img src="${esc(e.thumbnail_path || '')}" onerror="this.style.visibility='hidden'" alt="" />
    <div><div class="et">S${e.season} E${e.episode} — ${esc(e.title)}</div><div class="ed">${esc(e.description || '')} • ${fmtDur(e.duration_sec)}</div></div>
    <div class="play-mini">▶</div></div>`;
}
function closeModal() { $('#modal-root').innerHTML = ''; }

/* ================= PLAYER ================= */
let playerTimer = null, saveThrottle = 0;
async function playContent(contentId, episodeId = null) {
  const pid = S.profile.id;
  const { item: c } = await api(`/api/content/${contentId}?profileId=${pid}`);
  let ep = null;
  if (c.type === 'series') {
    const eps = c.episodes || [];
    ep = eps.find(e => e.id == episodeId) || eps[0];
    if (!ep) return toast('No episodes available');
  }
  const src = c.type === 'movie' ? (c.video_path ? `/api/stream/movie/${c.id}` : c.remote_url) : (ep.video_path ? `/api/stream/episode/${ep.id}` : ep.remote_url);
  if (!src) return toast('No video file linked. Add files to media/ and rescan.');
  await api(`/api/content/${c.id}/view`, { method: 'POST', body: JSON.stringify({ profileId: pid, episodeId: ep?.id || null }) }).catch(() => {});
  const resumeFrom = !ep ? (c.progress?.position_sec || 0) : 0;

  $('#player-root').innerHTML = `
  <div class="player">
    <div class="player-top"><button class="back focusable" id="pback">←</button>
      <div><div class="pt">${esc(c.title)}</div><div class="pe">${ep ? `S${ep.season} E${ep.episode} — ${esc(ep.title)}` : esc(c.maturity_rating || '') + ' • ' + (c.year || '')}</div></div></div>
    <video id="vid" controls autoplay playsinline preload="auto" ${resumeFrom > 10 ? `data-resume="${resumeFrom}"` : ''}></video>
    <button class="skip-btn" id="pskip" style="display:none">Skip Intro ▶</button>
    <div class="next-toast" id="pnext" style="display:none"></div>
  </div>`;
  const vid = $('#vid');
  vid.src = src;
  // subtitles
  try {
    const subs = await (await fetch(c.type === 'movie' ? `/api/subs/movie/${c.id}` : `/api/subs/episode/${ep.id}`)).json();
    (subs.subs || []).forEach((s, i) => {
      const t = document.createElement('track');
      t.kind = 'subtitles'; t.label = s.lang || `Sub ${i + 1}`; t.srclang = 'en'; t.src = s.url;
      vid.appendChild(t);
    });
    if (vid.textTracks.length) {
      const sel = document.createElement('select');
      sel.className = 'subs-sel focusable';
      const paint = () => {
        sel.innerHTML = `<option value="-1">CC: Off</option>` + [...vid.textTracks].map((t, i) =>
          `<option value="${i}" ${t.mode === 'showing' ? 'selected' : ''}>${esc(t.label || 'Track ' + (i + 1))}</option>`).join('');
      };
      paint();
      sel.onchange = () => [...vid.textTracks].forEach((t, i) => t.mode = (i === Number(sel.value) ? 'showing' : 'disabled'));
      $('.player').appendChild(sel);
    }
  } catch {}
  if (resumeFrom > 10) {
    vid.onloadedmetadata = () => {
      if (confirm(`Resume from ${fmtTime(resumeFrom)}?`)) vid.currentTime = resumeFrom;
    };
  }
  const save = (force = false) => {
    const now = Date.now();
    if (!force && now - saveThrottle < 5000) return;
    saveThrottle = now;
    if (!vid.duration || isNaN(vid.duration)) return;
    api('/api/progress', { method: 'POST', body: JSON.stringify({ profileId: pid, contentId: c.id, episodeId: ep?.id || null, position_sec: vid.currentTime, duration_sec: vid.duration }) }).catch(() => {});
  };
  vid.ontimeupdate = () => {
    save();
    const sk = $('#pskip');
    if (sk) sk.style.display = (vid.currentTime < 90 && vid.duration > 180) ? 'block' : 'none';
    // continue data refresh not needed
  };
  vid.onpause = () => save(true);
  const sk = $('#pskip');
  if (sk) sk.onclick = () => { vid.currentTime = Math.min(vid.duration - 5, Math.max(vid.currentTime + 60, 90)); };
  $('#pback').onclick = () => { save(true); closePlayer(); if (S.view === 'home') renderMain(); };
  vid.onended = async () => {
    save(true);
    if (c.type === 'series') {
      const eps = c.episodes;
      const idx = eps.findIndex(e => e.id === ep.id);
      const next = eps[idx + 1];
      if (next) {
        let n = 8;
        const box = $('#pnext');
        box.style.display = 'block';
        const tick = () => {
          if (!$('#pnext')) return;
          box.innerHTML = `<b>Up next: S${next.season} E${next.episode} — ${esc(next.title)}</b><div style="font-size:12px;color:#bbb;margin:4px 0">Playing in ${n}s…</div>
            <div style="display:flex;gap:8px"><button class="btn btn-light btn-sm" id="pnow">Play now</button><button class="btn btn-ghost btn-sm" id="pcancel">Stay</button></div>`;
          $('#pnow').onclick = () => { closePlayer(); playContent(c.id, next.id); };
          $('#pcancel').onclick = () => box.style.display = 'none';
          if (n-- <= 0) { closePlayer(); playContent(c.id, next.id); return; }
          playerTimer = setTimeout(tick, 1000);
        };
        tick();
        return;
      }
    }
    toast('Finished ✓');
    setTimeout(() => { closePlayer(); renderMain(); }, 800);
  };
  vid.onerror = () => {
    if (vid.src.includes('/api/stream/')) toast('⚠ Cannot play this file — missing on the server, or an unsupported format like .mkv (use .mp4).');
  };
  vid.focus();
}
function closePlayer() { clearTimeout(playerTimer); $('#player-root').innerHTML = ''; }

/* ================= ADMIN / CRM ================= */
// Upload files into an existing title's folder (series → auto-creates episodes,
// movie → links/replaces the video). files may come from <input> or a drop.
// Relative path of a picked/dropped file (folder drops keep subfolders,
// folder-picker drops the top folder, plain picks are bare filenames).
function dropRel(f) {
  return f._rel || (f.webkitRelativePath ? f.webkitRelativePath.split('/').slice(1).join('/') : '') || f.name;
}
async function readDropEntryTop(entry, base) {
  if (entry.isFile) {
    const f = await new Promise((res, rej) => entry.file(res, rej));
    f._rel = base + entry.name;
    return [f];
  }
  const reader = entry.createReader();
  const out = [];
  let batch;
  do {
    batch = await new Promise((res, rej) => reader.readEntries(res, rej));
    for (const child of batch) out.push(...await readDropEntryTop(child, base + entry.name + '/'));
  } while (batch.length);
  return out;
}
async function collectDropFiles(dt) {
  const files = [];
  for (const it of [...(dt.items || [])].filter(i => i.kind === 'file')) {
    const entry = it.webkitGetAsEntry ? it.webkitGetAsEntry() : null;
    if (entry) files.push(...await readDropEntryTop(entry, ''));
    else { const f = it.getAsFile(); if (f) { f._rel = f.name; files.push(f); } }
  }
  return files;
}
function uploadTitleFiles(contentId, files, attach, onStatus, onDone) {
  const fd = new FormData();
  fd.append('content_id', contentId);
  fd.append('attach', attach || 'auto');
  fd.append('paths', JSON.stringify(files.map(f => dropRel(f))));
  files.forEach(f => fd.append('files', f, 'file'));
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/admin/upload');
  if (store.token) xhr.setRequestHeader('Authorization', 'Bearer ' + store.token);
  xhr.upload.onprogress = (e) => { if (e.lengthComputable && onStatus) onStatus('Uploading ' + Math.round((e.loaded / e.total) * 100) + '%…'); };
  xhr.onload = () => {
    try {
      const r = JSON.parse(xhr.responseText);
      if (xhr.status >= 400) throw new Error(r.error || 'Upload failed');
      onDone(r);
    } catch (err) { toast(err.message); if (onStatus) onStatus(''); }
  };
  xhr.onerror = () => { toast('Upload failed (network)'); if (onStatus) onStatus(''); };
  xhr.send(fd);
}
// Compact drop zone used inside per-title edit panels: drop files/folders,
// or pick via Files… / Folder… buttons. Staged files upload on demand.
const VID_RE = /\.(mp4|webm|mkv|mov|avi|m4v)$/i;
function titleDropRow(uid) {
  return `<div class="mini-drop" id="td-drop-${uid}">Drop video <b>files or a folder</b> here, or
    <button class="btn btn-ghost btn-sm" id="td-fi-b-${uid}">Files…</button>
    <button class="btn btn-ghost btn-sm" id="td-fo-b-${uid}">Folder…</button>
    <input type="file" id="td-fi-${uid}" multiple accept="video/*,.mkv,.avi,.mov,.webm,.m4v" hidden />
    <input type="file" id="td-fo-${uid}" webkitdirectory hidden /></div>
  <div id="td-pend-${uid}"></div>`;
}
function wireTitleDrop(uid, onUpload) {
  const dz = $('#td-drop-' + uid);
  if (!dz) return;
  const pend = [];
  const paint = () => {
    const box = $('#td-pend-' + uid);
    if (!box) return;
    if (!pend.length) { box.innerHTML = ''; return; }
    box.innerHTML = `<div style="font-size:12px;color:#bbb">${pend.length} file(s): ${pend.slice(0, 4).map(f => esc(dropRel(f))).join(', ')}${pend.length > 4 ? '…' : ''}</div>
      <div style="display:flex;gap:8px;margin-top:6px;align-items:center;flex-wrap:wrap">
        <button class="btn btn-light btn-sm" id="td-up-${uid}">Upload</button>
        <button class="btn btn-ghost btn-sm" id="td-cl-${uid}">Clear</button>
        <span id="td-st-${uid}" style="font-size:12px;color:#bbb"></span></div>`;
    $('#td-cl-' + uid).onclick = () => { pend.length = 0; paint(); };
    $('#td-up-' + uid).onclick = () => onUpload([...pend],
      (m) => { const s = $('#td-st-' + uid); if (s) s.textContent = m; },
      () => { pend.length = 0; paint(); });
  };
  const add = (files) => {
    for (const f of files) {
      if (!VID_RE.test(dropRel(f))) continue;
      if (!pend.some(p => dropRel(p) === dropRel(f) && p.size === f.size)) pend.push(f);
    }
    paint();
    if (!pend.length) toast('No video files in that selection');
  };
  $('#td-fi-b-' + uid).onclick = () => $('#td-fi-' + uid).click();
  $('#td-fo-b-' + uid).onclick = () => $('#td-fo-' + uid).click();
  $('#td-fi-' + uid).onchange = (e) => { add([...e.target.files]); e.target.value = ''; };
  $('#td-fo-' + uid).onchange = (e) => { add([...e.target.files]); e.target.value = ''; };
  ['dragenter', 'dragover'].forEach(ev => dz.addEventListener(ev, (e) => { e.preventDefault(); e.stopPropagation(); dz.classList.add('over'); }));
  ['dragleave', 'drop'].forEach(ev => dz.addEventListener(ev, (e) => { e.preventDefault(); e.stopPropagation(); dz.classList.remove('over'); }));
  dz.addEventListener('drop', async (e) => add(await collectDropFiles(e.dataTransfer)));
}
async function renderEpManager(contentId) {
  const box = $('#eps-' + contentId);
  if (!box) return;
  try {
    const { episodes } = await api('/api/content/' + contentId + '/episodes');
    box.innerHTML = `<b style="font-size:13px">Episodes (${episodes.length})</b>
      ${titleDropRow('e' + contentId)}
      ${episodes.map(e => `<div style="display:flex;gap:8px;align-items:center;font-size:12.5px;padding:5px 0;border-bottom:1px solid #2a2a2a">
        <span style="color:#888">S${e.season} E${e.episode}</span><span style="flex:1">${esc(e.title)}</span>
        <span style="color:#888">${e.video_path ? '💾' : e.remote_url ? '🌐' : '❌'}</span>
        <button class="btn btn-ghost btn-sm" data-epdel="${e.id}">✕</button></div>`).join('') || '<div style="font-size:12px;color:#888">No episodes yet.</div>'}
      <div style="display:flex;gap:6px;margin-top:8px;flex-wrap:wrap">
        <input class="tbl-input" id="ne-s-${contentId}" placeholder="S#" style="max-width:56px" />
        <input class="tbl-input" id="ne-e-${contentId}" placeholder="E# (auto)" style="max-width:70px" />
        <input class="tbl-input" id="ne-t-${contentId}" placeholder="Episode title *" style="flex:1;min-width:140px" />
        <input class="tbl-input" id="ne-u-${contentId}" placeholder="Direct video URL (.mp4)" style="flex:1;min-width:140px" />
        <button class="btn btn-light btn-sm" id="ne-go-${contentId}">Add</button></div>`;
    $$('[data-epdel]', box).forEach(b => b.onclick = async () => {
      if (!confirm('Delete this episode?')) return;
      await api('/api/admin/episodes/' + b.dataset.epdel, { method: 'DELETE' });
      renderEpManager(contentId);
    });
    $('#ne-go-' + contentId).onclick = async () => {
      const title = $('#ne-t-' + contentId).value.trim();
      if (!title) return toast('Episode title required');
      try {
        await api('/api/admin/episodes', { method: 'POST', body: JSON.stringify({
          content_id: contentId, season: parseInt($('#ne-s-' + contentId).value) || 1,
          episode: parseInt($('#ne-e-' + contentId).value) || undefined,
          title, remote_url: $('#ne-u-' + contentId).value.trim() }) });
        toast('Episode added'); renderEpManager(contentId);
      } catch (e) { toast(e.message); }
    };
    wireTitleDrop('e' + contentId, (files, setStatus, done) => {
      uploadTitleFiles(contentId, files, 'auto', setStatus, (r) => {
        setStatus(''); done();
        toast(`Uploaded ${r.uploaded} file(s), +${r.episodes_added} episode(s)`);
        renderEpManager(contentId);
      });
    });
  } catch (e) { box.innerHTML = `<span style="color:#f99;font-size:12px">Failed to load episodes</span>`; }
}
async function viewAdmin() {
  if (S.user?.role !== 'admin') { $('#view').innerHTML = `<div class="page"><div class="empty">Admins only.</div></div>`; return; }
  const t = S.adminTab;
  $('#view').innerHTML = `<div class="admin">
    <div class="admin-side"><h3>🛠 CRM</h3>
      ${['overview', 'users', 'content', 'views'].map(x => `<button data-tab="${x}" class="${t === x ? 'on' : ''} focusable">${{ overview: '📊 Overview', users: '👥 Users', content: '🎬 Content', views: '👁 Views' }[x]}</button>`).join('')}
      <button id="a-scan" class="focusable">🔄 Rescan media</button>
      <button id="a-home" class="focusable">← Back to app</button>
    </div>
    <div class="admin-main" id="admin-main"><div class="empty">Loading…</div></div></div>`;
  $$('#view [data-tab]').forEach(b => b.onclick = () => { S.adminTab = b.dataset.tab; viewAdmin(); });
  $('#a-home').onclick = () => nav('home');
  $('#a-scan').onclick = async () => {
    toast('Scanning media…');
    try { const r = await api('/api/admin/scan', { method: 'POST' }); toast(`Scan done: +${r.added} added, ${r.updated} updated`); viewAdmin(); }
    catch (e) { toast(e.message); }
  };
  const main = $('#admin-main');
  try {
    if (t === 'overview') {
      const s = await api('/api/admin/stats');
      S.adminData.stats = s;
      $('#a-restore').onclick = async () => {
        if (!confirm('Re-add missing demo titles?')) return;
        try { const r = await api('/api/admin/seed-demo', { method: 'POST' }); toast(`Restored ${r.inserted} title(s)`); viewAdmin(); }
        catch (e) { toast(e.message); }
      };
      const max = Math.max(1, ...s.last7.map(x => x.c));
      main.innerHTML = `
        <h1 style="font-size:22px;margin-bottom:14px">Overview — your local network</h1>
        <div style="display:flex;gap:8px;margin-bottom:14px;flex-wrap:wrap">
          <button class="btn btn-light btn-sm focusable" id="a-restore">⤺ Restore demo catalog</button>
          <span style="font-size:12.5px;color:#888;align-self:center">Re-adds missing demo titles. Never touches users or your files.</span>
        </div>
        <div class="stat-grid">
          <div class="stat"><div class="v">${s.users}</div><div class="l">Users</div></div>
          <div class="stat"><div class="v">${s.profiles}</div><div class="l">Profiles</div></div>
          <div class="stat"><div class="v">${s.movies}</div><div class="l">Movies</div></div>
          <div class="stat"><div class="v">${s.series}</div><div class="l">TV Shows</div></div>
          <div class="stat"><div class="v">${s.episodes}</div><div class="l">Episodes</div></div>
          <div class="stat"><div class="v">${s.views}</div><div class="l">Total plays</div></div>
          <div class="stat"><div class="v">${(s.storageBytes / 1e9).toFixed(2)} GB</div><div class="l">Media storage</div></div>
        </div>
        <div class="panel"><h3>Plays — last 7 days</h3>
          <div class="bar-chart">${s.last7.map(x => `<div class="bar" style="height:${Math.round((x.c / max) * 100)}%" title="${x.d}: ${x.c}"><span>${x.d.slice(5)}</span></div>`).join('') || '<span style="color:#888">No plays yet</span>'}</div><div style="height:22px"></div></div>
        <div class="panel"><h3>Top content</h3><table><tr><th>#</th><th>Title</th><th>Type</th><th>Plays</th></tr>
          ${s.top.map((x, i) => `<tr><td>${i + 1}</td><td>${esc(x.title)}</td><td><span class="pill">${x.type}</span></td><td>${x.views_count}</td></tr>`).join('')}</table></div>
        <div class="panel"><h3>How to add movies</h3><div style="font-size:13.5px;color:#bbb;line-height:1.7">1. On this server computer, drop folders into <code>media/movies/My Film (2024)/film.mp4</code> (+ optional <code>poster.jpg</code>, <code>metadata.json</code>) and <code>media/series/ShowName/S01E01.mp4</code>.<br>2. Click <b>🔄 Rescan media</b>.<br>3. Edit titles/posters in the <b>Content</b> tab.<br>Tip: use <b>.mp4 (H.264)</b> for direct browser play — .mkv often won't play in browsers.</div></div>`;
    } else if (t === 'users') {
      const { users } = await api('/api/admin/users');
      main.innerHTML = `
        <h1 style="font-size:22px;margin-bottom:14px">Users & profiles <span class="pill">${users.length}</span></h1>
        <div class="panel"><h3>Create user (you manage this locally — no cloud)</h3>
          <div class="form-grid"><input class="tbl-input focusable" id="nu-email" placeholder="email" /><input class="tbl-input focusable" id="nu-name" placeholder="name" />
          <input class="tbl-input focusable" id="nu-pass" placeholder="password" /><select id="nu-role" class="tbl-input"><option value="user">user</option><option value="admin">admin</option></select></div>
          <div style="margin-top:10px"><button class="btn btn-red btn-sm focusable" id="nu-go">Create user</button></div></div>
        <div class="panel"><table><tr><th>User</th><th>Role</th><th>Status</th><th>Profiles</th><th>Plays</th><th>Actions</th></tr>
          ${users.map(u => `<tr><td><b>${esc(u.name || '—')}</b><br><span style="color:#888">${esc(u.email)}</span></td>
            <td><span class="pill ${u.role === 'admin' ? 'admin' : ''}">${u.role}</span></td>
            <td>${u.is_active ? '<span class="pill">active</span>' : '<span class="pill off">disabled</span>'}</td>
            <td>${u.profiles.map(p => `${esc(p.avatar)} ${esc(p.name)}`).join('<br>')}</td><td>${u.watch_count}</td>
            <td style="min-width:210px"><div style="display:flex;gap:6px;flex-wrap:wrap">
              <button class="btn btn-ghost btn-sm" data-uedit="${u.id}">Edit</button>
              <button class="btn btn-ghost btn-sm" data-udis="${u.id}">${u.is_active ? 'Disable' : 'Enable'}</button>
              <button class="btn btn-ghost btn-sm" data-upw="${u.id}">Reset PW</button>
              <button class="btn btn-ghost btn-sm" data-udel="${u.id}">Delete</button></div>
              <div id="ue-${u.id}"></div></td></tr>`).join('')}</table></div>`;
      $('#nu-go').onclick = async () => {
        try {
          await api('/api/admin/users', { method: 'POST', body: JSON.stringify({ email: $('#nu-email').value.trim(), name: $('#nu-name').value.trim(), password: $('#nu-pass').value, role: $('#nu-role').value }) });
          toast('User created'); viewAdmin();
        } catch (e) { toast(e.message); }
      };
      $$('[data-uedit]', main).forEach(b => b.onclick = () => {
        const u = users.find(x => x.id == b.dataset.uedit);
        const box = $('#ue-' + b.dataset.uedit);
        box.innerHTML = `<div style="display:flex;gap:6px;margin-top:8px;flex-wrap:wrap"><input class="tbl-input" id="e-name-${b.dataset.uedit}" placeholder="new name" style="max-width:130px" />
          <select class="tbl-input" id="e-role-${b.dataset.uedit}" style="max-width:100px"><option value="user">user</option><option value="admin">admin</option></select>
          <button class="btn btn-light btn-sm" id="e-go-${b.dataset.uedit}">Save</button></div>`;
        $('#e-role-' + b.dataset.uedit).value = u.role;
        $('#e-go-' + b.dataset.uedit).onclick = async () => {
          await api('/api/admin/users/' + b.dataset.uedit, { method: 'PUT', body: JSON.stringify({ name: $('#e-name-' + b.dataset.uedit).value, role: $('#e-role-' + b.dataset.uedit).value }) });
          toast('Saved'); viewAdmin();
        };
      });
      $$('[data-udis]', main).forEach(b => b.onclick = async () => {
        const u = users.find(x => x.id == b.dataset.udis);
        await api('/api/admin/users/' + u.id, { method: 'PUT', body: JSON.stringify({ is_active: !u.is_active }) });
        viewAdmin();
      });
      $$('[data-upw]', main).forEach(b => b.onclick = async () => {
        const v = prompt('New password for user:');
        if (!v) return;
        await api('/api/admin/users/' + b.dataset.upw, { method: 'PUT', body: JSON.stringify({ password: v }) });
        toast('Password reset');
      });
      $$('[data-udel]', main).forEach(b => b.onclick = async () => {
        if (!confirm('Delete user + all their profiles?')) return;
        try { await api('/api/admin/users/' + b.dataset.udel, { method: 'DELETE' }); viewAdmin(); }
        catch (e) { toast(e.message); }
      });
    } else if (t === 'content') {
      const { items } = await api('/api/admin/content');
      main.innerHTML = `
        <h1 style="font-size:22px;margin-bottom:14px">Content <span class="pill">${items.length}</span></h1>
        <div style="display:flex;gap:8px;margin-bottom:12px;flex-wrap:wrap;align-items:center">
          <button class="btn btn-red btn-sm focusable" id="c-add">＋ Add title</button>
          <span style="font-size:12.5px;color:#888">Files on disk? Use Rescan — or drop a folder below.</span>
        </div>
        <div id="dz" class="dropzone focusable" tabindex="0">
          <div style="font-size:26px">📁</div>
          <div><b>Drop a movie / series folder here</b> — or <u>click to browse</u></div>
          <div style="font-size:12px;color:#888">Videos + poster.jpg + .vtt land in media/ and get scanned automatically</div>
          <div style="margin-top:8px" id="dz-target-wrap">
            <select id="dz-target" class="tbl-input focusable" style="max-width:160px"><option value="movies">🎬 → Movies</option><option value="series">📺 → TV Series</option></select>
          </div>
          <input type="file" id="dz-input" webkitdirectory multiple hidden />
        </div>
        <div id="dz-pending"></div>
        <div id="c-add-form"></div>
        <div class="search-bar"><input id="cs" class="focusable" placeholder="Filter titles…" /></div>
        <div class="panel" style="overflow-x:auto"><table id="ctbl"><tr><th>Title</th><th>Type</th><th>Year</th><th>Genres</th><th>Plays</th><th>Featured</th><th>Video?</th><th>Actions</th></tr>
          ${items.map(c => `<tr data-t="${esc(c.title.toLowerCase())}"><td style="max-width:200px"><b>${esc(c.title)}</b><br><span style="color:#888;font-size:12px">${esc((c.description || '').slice(0, 60))}</span></td>
            <td><span class="pill">${c.type}</span></td><td>${c.year || '—'}</td><td style="max-width:150px">${esc((c.genres || []).join(', '))}</td><td>${c.views_count}</td>
            <td>${c.is_featured ? '⭐' : ''}</td><td>${c.video_path ? '💾 local' : c.remote_url ? '🌐 demo' : '❌ none'}</td>
            <td><div style="display:flex;gap:6px;flex-wrap:wrap"><button class="btn btn-ghost btn-sm" data-cedit="${c.id}">Edit</button><button class="btn btn-ghost btn-sm" data-cdel="${c.id}">Delete</button></div><div id="ce-${c.id}"></div></td></tr>`).join('')}</table></div>`;
      $('#c-add').onclick = () => {
        const f = $('#c-add-form');
        if (f.innerHTML) { f.innerHTML = ''; return; }
        f.innerHTML = `<div class="panel"><h3>New title</h3><div class="form-grid">
          <select id="n-type" class="tbl-input focusable"><option value="movie">Movie</option><option value="series">TV Series</option></select>
          <input id="n-year" class="tbl-input focusable" placeholder="Year (e.g. 2024)" />
          <input id="n-title" class="tbl-input focusable full" placeholder="Title *" />
          <input id="n-desc" class="tbl-input focusable full" placeholder="Description" />
          <input id="n-remote" class="tbl-input focusable full" placeholder="Direct video URL (.mp4 — movies only, optional)" />
          <input id="n-trailer" class="tbl-input focusable full" placeholder="Trailer URL (optional)" />
          <input id="n-genres" class="tbl-input focusable" placeholder="Genres (comma separated)" />
          <input id="n-cast" class="tbl-input focusable" placeholder="Cast (comma separated)" />
          <input id="n-director" class="tbl-input focusable" placeholder="Director" />
          <input id="n-maturity" class="tbl-input focusable" placeholder="Maturity (default U/A 13+)" />
          <label style="font-size:12px;color:#bbb;display:flex;gap:6px;align-items:center"><input type="checkbox" id="n-feat" /> Featured (hero)</label>
          </div><div style="margin-top:10px;display:flex;gap:8px">
          <button class="btn btn-red btn-sm focusable" id="n-go">Create</button>
          <button class="btn btn-ghost btn-sm" id="n-cancel">Cancel</button></div></div>`;
        $('#n-cancel').onclick = () => f.innerHTML = '';
        $('#n-go').onclick = async () => {
          const title = $('#n-title').value.trim();
          if (!title) return toast('Title required');
          try {
            const r = await api('/api/admin/content', { method: 'POST', body: JSON.stringify({
              type: $('#n-type').value, title, description: $('#n-desc').value, year: parseInt($('#n-year').value) || null,
              remote_url: $('#n-remote').value.trim(), trailer_url: $('#n-trailer').value.trim(), genres: $('#n-genres').value, cast: $('#n-cast').value,
              director: $('#n-director').value, maturity_rating: $('#n-maturity').value.trim() || 'U/A 13+',
              is_featured: $('#n-feat').checked }) });
            toast('Created — now add posters/episodes via Edit'); viewAdmin();
          } catch (e) { toast(e.message); }
        };
      };
      $('#cs').oninput = (e) => {
        const v = e.target.value.toLowerCase();
        $$('#ctbl tr[data-t]').forEach(tr => tr.style.display = tr.dataset.t.includes(v) ? '' : 'none');
      };
      // ---- drag & drop folder upload ----
      const dz = $('#dz'), dzInput = $('#dz-input');
      const DZ_OK = /\.(mp4|webm|mkv|mov|avi|m4v|jpg|jpeg|png|webp|vtt|srt|json)$/i;
      dz.onclick = (e) => { if (e.target.id !== 'dz-target') dzInput.click(); };
      ['dragenter', 'dragover'].forEach(ev => dz.addEventListener(ev, (e) => { e.preventDefault(); e.stopPropagation(); dz.classList.add('over'); }));
      ['dragleave', 'drop'].forEach(ev => dz.addEventListener(ev, (e) => { e.preventDefault(); e.stopPropagation(); dz.classList.remove('over'); }));
      dz.addEventListener('drop', async (e) => showDzPending(await collectDropFiles(e.dataTransfer)));
      dzInput.onchange = () => {
        showDzPending([...dzInput.files]);
        dzInput.value = '';
      };
      function showDzPending(files) {
        const box = $('#dz-pending');
        const target = $('#dz-target').value;
        // series scanner needs show folders: nest loose files so nothing is silently ignored
        const mapped = files
          .map(f => ({ file: f, rel: dropRel(f) }))
          .filter(m => m.rel && DZ_OK.test(m.rel))
          .map(m => ({ file: m.file, rel: (target === 'series' && !m.rel.includes('/')) ? 'Unsorted/' + m.rel : m.rel }));
        if (!mapped.length) { box.innerHTML = ''; toast('No media files found in that drop'); return; }
        box._files = mapped;
        const total = mapped.reduce((a, m) => a + m.file.size, 0);
        const size = total > 1e9 ? (total / 1e9).toFixed(2) + ' GB' : Math.max(1, Math.round(total / 1e6)) + ' MB';
        box.innerHTML = `<div class="panel"><h3>Ready to upload (${mapped.length} files, ${size}) → <code>media/${target}/</code></h3>
          <div style="font-size:12.5px;color:#bbb;max-height:120px;overflow:auto">${mapped.slice(0, 30).map(m => esc(m.rel)).join('<br>')}${mapped.length > 30 ? `<br>… +${mapped.length - 30} more` : ''}</div>
          ${target === 'series' && mapped.some(m => m.rel.startsWith('Unsorted/')) ? `<div style="font-size:12px;color:#e8a33d;margin-top:6px">Loose files go to an <code>Unsorted</code> show folder — rename it on disk later + Rescan.</div>` : ''}
          <div style="margin-top:10px;display:flex;gap:10px;align-items:center">
            <button class="btn btn-red btn-sm focusable" id="dz-go">Upload + Scan</button>
            <div style="flex:1;height:8px;background:#333;border-radius:4px;overflow:hidden"><i id="dz-bar" style="display:block;height:100%;width:0;background:var(--red)"></i></div>
            <span id="dz-pct" style="font-size:12px;color:#bbb;min-width:40px"></span></div></div>`;
        $('#dz-go').onclick = () => {
          const fd = new FormData();
          fd.append('target', $('#dz-target').value);
          fd.append('paths', JSON.stringify(box._files.map(m => m.rel)));
          box._files.forEach(m => fd.append('files', m.file, 'file'));
          const xhr = new XMLHttpRequest();
          xhr.open('POST', '/api/admin/upload');
          if (store.token) xhr.setRequestHeader('Authorization', 'Bearer ' + store.token);
          xhr.upload.onprogress = (e) => {
            if (!e.lengthComputable) return;
            const p = Math.round((e.loaded / e.total) * 100);
            if ($('#dz-bar')) $('#dz-bar').style.width = p + '%';
            if ($('#dz-pct')) $('#dz-pct').textContent = p + '%';
          };
          xhr.onload = () => {
            try {
              const r = JSON.parse(xhr.responseText);
              if (xhr.status >= 400) throw new Error(r.error || 'Upload failed');
              toast(`Uploaded ${r.uploaded} file(s), +${r.added} title(s)${r.skipped && r.skipped.length ? `, skipped ${r.skipped.length}` : ''}`);
              viewAdmin();
            } catch (err) { toast(err.message); }
          };
          xhr.onerror = () => toast('Upload failed (network)');
          xhr.send(fd);
          $('#dz-go').disabled = true;
          $('#dz-go').textContent = 'Uploading…';
        };
      }
      $('#dz-target').onchange = () => { if ($('#dz-pending')._files) showDzPending($('#dz-pending')._files.map(m => m.file)); };
      $$('[data-cdel]', main).forEach(b => b.onclick = async () => {
        if (!confirm('Delete this title from catalog? (files on disk stay)')) return;
        await api('/api/admin/content/' + b.dataset.cdel, { method: 'DELETE' });
        viewAdmin();
      });
      $$('[data-cedit]', main).forEach(b => b.onclick = async () => {
        const c = items.find(x => x.id == b.dataset.cedit);
        const box = $('#ce-' + c.id);
        box.innerHTML = `<div class="panel" style="margin-top:8px;min-width:280px"><div class="form-grid">
          <input class="tbl-input full" id="f-t-${c.id}" value="${esc(c.title)}" placeholder="Title" />
          <input class="tbl-input" id="f-y-${c.id}" value="${c.year || ''}" placeholder="Year" />
          <input class="tbl-input" id="f-m-${c.id}" value="${esc(c.maturity_rating)}" placeholder="Maturity" />
          <input class="tbl-input full" id="f-d-${c.id}" value="${esc(c.description)}" placeholder="Description" />
          <input class="tbl-input full" id="f-g-${c.id}" value="${esc((c.genres || []).join(', '))}" placeholder="Genres (comma)" />
          <input class="tbl-input full" id="f-c-${c.id}" value="${esc((c.cast || []).join(', '))}" placeholder="Cast (comma)" />
          <input class="tbl-input" id="f-dir-${c.id}" value="${esc(c.director || '')}" placeholder="Director" />
          <label style="font-size:12px;color:#bbb;display:flex;gap:6px;align-items:center"><input type="checkbox" id="f-f-${c.id}" ${c.is_featured ? 'checked' : ''}/> Featured (hero)</label>
          <input class="tbl-input full" id="f-r-${c.id}" value="${esc(c.remote_url || '')}" placeholder="Direct video URL (.mp4 — movies)" />
          <input class="tbl-input full" id="f-tr-${c.id}" value="${esc(c.trailer_url || '')}" placeholder="Trailer URL (optional)" />
          <div class="full" id="f-vid-${c.id}" style="font-size:12px;color:#888">Local file: <code>${esc(c.video_path || '—')}</code>${c.remote_url ? ` &nbsp;•&nbsp; link: <code>${esc(c.remote_url.slice(0, 60))}</code>` : ''}</div>
          ${c.type === 'movie' ? titleDropRow('m' + c.id) : ''}
          <div class="full"><label style="font-size:12px;color:#bbb">Poster: <input type="file" id="f-p-${c.id}" accept="image/*" /></label>
          <label style="font-size:12px;color:#bbb;margin-left:10px">Backdrop: <input type="file" id="f-b-${c.id}" accept="image/*" /></label></div>
          ${c.type === 'series' ? `<div class="full" id="eps-${c.id}" style="border-top:1px solid #333;padding-top:10px">Loading episodes…</div>` : ''}
          </div><div style="margin-top:10px"><button class="btn btn-red btn-sm" id="f-go-${c.id}">Save</button></div></div>`;
        if (c.type === 'series') renderEpManager(c.id);
        if (c.type === 'movie') wireTitleDrop('m' + c.id, (files, setStatus, done) => {
          const hasVideo = c.video_path || c.remote_url;
          if (hasVideo && !confirm('Replace the current video with the uploaded file?')) return;
          uploadTitleFiles(c.id, files, hasVideo ? 'replace' : 'auto', setStatus, (r) => {
            setStatus(''); done();
            if (r.linked_video) {
              c.video_path = r.linked_video; c.remote_url = '';
              $('#f-vid-' + c.id).innerHTML = `Local file: <code>${esc(r.linked_video)}</code>`;
            }
            toast(`Uploaded ${r.uploaded} file(s)${r.linked_video ? ' and linked' : ''}`);
          });
        });
        $('#f-go-' + c.id).onclick = async () => {
          const genres = $('#f-g-' + c.id).value.split(',').map(s => s.trim()).filter(Boolean);
          const cast = $('#f-c-' + c.id).value.split(',').map(s => s.trim()).filter(Boolean);
          await api('/api/admin/content/' + c.id, { method: 'PUT', body: JSON.stringify({ title: $('#f-t-' + c.id).value, year: parseInt($('#f-y-' + c.id).value) || null, maturity_rating: $('#f-m-' + c.id).value, description: $('#f-d-' + c.id).value, genres, cast, director: $('#f-dir-' + c.id).value, remote_url: $('#f-r-' + c.id).value.trim(), trailer_url: $('#f-tr-' + c.id).value.trim(), is_featured: $('#f-f-' + c.id).checked }) });
          const pf = $('#f-p-' + c.id).files[0], bf = $('#f-b-' + c.id).files[0];
          if (pf || bf) {
            const fd = new FormData();
            if (pf) fd.append('poster', pf);
            if (bf) fd.append('backdrop', bf);
            const headers = {}; if (store.token) headers.Authorization = 'Bearer ' + store.token;
            await fetch('/api/admin/content/' + c.id + '/poster', { method: 'POST', headers, body: fd });
          }
          toast('Saved'); viewAdmin();
        };
      });
    } else if (t === 'views') {
      const { items } = await api('/api/admin/views');
      main.innerHTML = `<h1 style="font-size:22px;margin-bottom:14px">Recent plays</h1>
        <div class="panel"><table><tr><th>When</th><th>Profile</th><th>Title</th><th>Event</th></tr>
        ${items.map(v => `<tr><td>${esc(v.started_at)}</td><td>${esc(v.profile || '—')}</td><td>${esc(v.title || '#' + v.content_id)}</td><td>${esc(v.event)}</td></tr>`).join('') || '<tr><td colspan=4>No plays yet</td></tr>'}</table></div>`;
    }
  } catch (e) { main.innerHTML = `<div class="empty">Error: ${esc(e.message)}</div>`; }
}

/* ================= TV / KEYBOARD NAV ================= */
// never let a stray file-drop navigate the app away
window.addEventListener('dragover', (e) => e.preventDefault());
window.addEventListener('drop', (e) => e.preventDefault());
document.addEventListener('keydown', (e) => {
  // player back
  if ($('#player-root').innerHTML && (e.key === 'Escape' || e.key === 'BrowserBack' || e.key === 'Backspace')) {
    const v = $('#vid'); if (v && document.activeElement?.tagName === 'INPUT') return;
    e.preventDefault(); $('#pback')?.click(); return;
  }
  if ($('#modal-root').innerHTML && e.key === 'Escape') { closeModal(); return; }
  // arrows: move focus spatially (TV friendly)
  if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) {
    const f = $$('.focusable, button, input, select, [tabindex]').filter(el => el.offsetParent !== null);
    if (!f.length) return;
    const cur = document.activeElement;
    if (!f.includes(cur)) { f[0].focus(); e.preventDefault(); return; }
    const r1 = cur.getBoundingClientRect();
    const c1 = { x: r1.left + r1.width / 2, y: r1.top + r1.height / 2 };
    let best = null, bestD = Infinity;
    for (const el of f) {
      if (el === cur) continue;
      const r2 = el.getBoundingClientRect();
      const c2 = { x: r2.left + r2.width / 2, y: r2.top + r2.height / 2 };
      const dx = c2.x - c1.x, dy = c2.y - c1.y;
      if (e.key === 'ArrowRight' && dx < 20) continue;
      if (e.key === 'ArrowLeft' && dx > -20) continue;
      if (e.key === 'ArrowDown' && dy < 20) continue;
      if (e.key === 'ArrowUp' && dy > -20) continue;
      const d = Math.hypot(dx, dy) + Math.abs(e.key === 'ArrowLeft' || e.key === 'ArrowRight' ? dy * 3 : dx * 3);
      if (d < bestD) { bestD = d; best = el; }
    }
    if (best) { e.preventDefault(); best.focus(); best.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'smooth' }); }
  }
});

/* ================= INIT ================= */
(async function init() {
  if (!store.token) return renderAuth();
  try { await afterLogin(); }
  catch { renderAuth(); }
})();
