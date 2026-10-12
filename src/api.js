const fs = require('fs');
const path = require('path');
const multer = require('multer');
const { db, DATA_DIR } = require('./db');
const { hashPassword, verifyPassword, signToken, authRequired, adminRequired } = require('./auth');
const { scanMedia, MOVIES_DIR, SERIES_DIR, parseEpisodeFile } = require('./scanner');
const { seedCatalog } = require('./seed');

const POSTERS_DIR = path.join(__dirname, '..', 'public', 'posters');
if (!fs.existsSync(POSTERS_DIR)) fs.mkdirSync(POSTERS_DIR, { recursive: true });
const upload = multer({ dest: POSTERS_DIR });

const MIME = { '.mp4': 'video/mp4', '.webm': 'video/webm', '.mkv': 'video/x-matroska', '.mov': 'video/quicktime', '.avi': 'video/x-msvideo', '.m4v': 'video/x-m4v', '.vtt': 'text/vtt', '.srt': 'text/plain' };
function mimeOf(p) { return MIME[path.extname(p).toLowerCase()] || 'application/octet-stream'; }

function sendRange(req, res, filePath) {
  if (!filePath || !fs.existsSync(filePath)) return res.status(404).json({ error: 'File not found' });
  const stat = fs.statSync(filePath);
  const size = stat.size;
  const range = req.headers.range;
  const type = mimeOf(filePath);
  if (range) {
    const parts = range.replace(/bytes=/, '').split('-');
    const start = parseInt(parts[0], 10);
    const end = parts[1] ? parseInt(parts[1], 10) : size - 1;
    if (isNaN(start) || isNaN(end) || start >= size || end >= size) {
      res.status(416).header('Content-Range', `bytes */${size}`).end();
      return;
    }
    const chunk = end - start + 1;
    res.writeHead(206, {
      'Content-Range': `bytes ${start}-${end}/${size}`,
      'Accept-Ranges': 'bytes',
      'Content-Length': chunk,
      'Content-Type': type,
    });
    fs.createReadStream(filePath, { start, end }).pipe(res);
  } else {
    res.writeHead(200, { 'Content-Length': size, 'Content-Type': type, 'Accept-Ranges': 'bytes' });
    fs.createReadStream(filePath).pipe(res);
  }
}

function row(c) {
  if (!c) return c;
  try { c.genres = JSON.parse(c.genres || '[]'); } catch { c.genres = []; }
  try { c.cast = JSON.parse(c.cast || '[]'); } catch { c.cast = []; }
  c.poster = c.poster_path && c.poster_path.startsWith('http') ? c.poster_path : (c.poster_path ? `/api/file?path=${encodeURIComponent(c.poster_path)}` : '');
  c.backdrop = c.backdrop_path && c.backdrop_path.startsWith('http') ? c.backdrop_path : (c.backdrop_path ? `/api/file?path=${encodeURIComponent(c.backdrop_path)}` : '');
  c.rating_avg = c.rating_count ? (c.rating_sum / c.rating_count) : 0;
  return c;
}

function kidsFilter(profileId) {
  if (!profileId) return { clause: '', params: [] };
  const p = db.prepare('SELECT * FROM profiles WHERE id=?').get(profileId);
  if (p && p.is_kids) return { clause: ` AND maturity_rating NOT IN ('A 18+','U/A 16+')`, params: [] };
  return { clause: '', params: [] };
}

// A profile belongs to the logged-in user (or the user is admin).
// Strict: profileId required in query or body (for my list, ratings, progress…).
function ownProfile(req, res, next) {
  const pid = req.query.profileId || (req.body && req.body.profileId);
  if (!pid) return res.status(400).json({ error: 'profileId required' });
  const p = db.prepare('SELECT * FROM profiles WHERE id=?').get(pid);
  if (!p) return res.status(404).json({ error: 'Profile not found' });
  if (p.user_id !== req.user.id && req.user.role !== 'admin') return res.status(403).json({ error: 'Not your profile' });
  req.profile = p;
  next();
}

// Optional: if a profileId is supplied (personalization / kids filter),
// it must be valid + owned. Otherwise the route stays public.
function optionalProfile(req, res, next) {
  const pid = req.query.profileId || (req.body && req.body.profileId);
  if (!pid) return next();
  authRequired(req, res, () => ownProfile(req, res, next));
}

function getContentOr404(res, id) {
  const c = db.prepare('SELECT * FROM content WHERE id=?').get(id);
  if (!c) { res.status(404).json({ error: 'Title not found' }); return null; }
  return c;
}

function registerApi(app) {
  // ---------- public: server info ----------
  app.get('/api/server-info', (req, res) => {
    const os = require('os');
    const nets = os.networkInterfaces();
    const ips = [];
    for (const n of Object.values(nets)) for (const i of n) if (i.family === 'IPv4' && !i.internal) ips.push(i.address);
    const movies = db.prepare("SELECT COUNT(*) c FROM content WHERE type='movie'").get().c;
    const series = db.prepare("SELECT COUNT(*) c FROM content WHERE type='series'").get().c;
    res.json({ ips, port: process.env.PORT || 8080, movies, series, name: 'LanFlix' });
  });

  app.get('/api/file', (req, res) => {
    const p = req.query.path || '';
    if (!p) return res.status(400).end();
    // allow http passthrough? no
    if (p.startsWith('http')) return res.redirect(p);
    const allowed = [path.join(__dirname, '..', 'media'), path.join(__dirname, '..', 'public'), require('os').tmpdir()];
    const abs = path.resolve(p);
    if (!allowed.some(a => abs.startsWith(path.resolve(a)))) return res.status(403).json({ error: 'Forbidden' });
    if (!fs.existsSync(abs)) return res.status(404).end();
    const ext = path.extname(abs).toLowerCase();
    if (['.jpg', '.jpeg', '.png', '.webp', '.gif'].includes(ext)) {
      res.sendFile(abs);
    } else {
      res.sendFile(abs);
    }
  });

  // ---------- auth ----------
  app.post('/api/auth/signup', (req, res) => {
    const { email, password, name } = req.body || {};
    if (!email || !password || password.length < 4) return res.status(400).json({ error: 'Email + password (min 4 chars) required' });
    const exists = db.prepare('SELECT * FROM users WHERE email=?').get(email.toLowerCase());
    if (exists) return res.status(400).json({ error: 'Email already registered. Please log in.' });
    const total = db.prepare('SELECT COUNT(*) c FROM users').get().c;
    const role = total === 0 ? 'admin' : 'user';
    const r = db.prepare('INSERT INTO users (email,password_hash,name,role) VALUES (?,?,?,?)')
      .run(email.toLowerCase(), hashPassword(password), name || email.split('@')[0], role);
    const user = db.prepare('SELECT * FROM users WHERE id=?').get(r.lastInsertRowid);
    db.prepare('INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)').run(user.id, (name || 'User').split(' ')[0], '😎', '#0071eb');
    const token = signToken(user);
    res.json({ token, user: { id: user.id, email: user.email, name: user.name, role: user.role } });
  });

  app.post('/api/auth/login', (req, res) => {
    const { email, password } = req.body || {};
    if (!email || !password) return res.status(400).json({ error: 'Email + password required' });
    const user = db.prepare('SELECT * FROM users WHERE email=?').get(email.toLowerCase());
    if (!user || !verifyPassword(password, user.password_hash)) return res.status(401).json({ error: 'Invalid email or password' });
    if (!user.is_active) return res.status(403).json({ error: 'Account disabled. Contact admin.' });
    const token = signToken(user);
    res.json({ token, user: { id: user.id, email: user.email, name: user.name, role: user.role } });
  });

  app.get('/api/auth/me', authRequired, (req, res) => {
    const user = db.prepare('SELECT id,email,name,role,created_at FROM users WHERE id=?').get(req.user.id);
    res.json({ user });
  });

  // ---------- profiles ----------
  app.get('/api/profiles', authRequired, (req, res) => {
    let uid = req.user.id;
    if (req.query.userId && req.user.role === 'admin') uid = req.query.userId;
    const rows = db.prepare('SELECT * FROM profiles WHERE user_id=? ORDER BY id').all(uid);
    res.json({ profiles: rows });
  });

  app.post('/api/profiles', authRequired, (req, res) => {
    const { name, avatar, color, is_kids, pin, userId } = req.body || {};
    if (!name) return res.status(400).json({ error: 'Name required' });
    let uid = req.user.id;
    if (userId && req.user.role === 'admin') uid = userId;
    const count = db.prepare('SELECT COUNT(*) c FROM profiles WHERE user_id=?').get(uid).c;
    if (count >= 5) return res.status(400).json({ error: 'Max 5 profiles per account' });
    const r = db.prepare('INSERT INTO profiles (user_id,name,avatar,color,is_kids,pin) VALUES (?,?,?,?,?,?)')
      .run(uid, name, avatar || '🎬', color || '#E50914', is_kids ? 1 : 0, pin || null);
    res.json({ profile: db.prepare('SELECT * FROM profiles WHERE id=?').get(r.lastInsertRowid) });
  });

  app.put('/api/profiles/:id', authRequired, (req, res) => {
    const p = db.prepare('SELECT * FROM profiles WHERE id=?').get(req.params.id);
    if (!p) return res.status(404).json({ error: 'Not found' });
    if (p.user_id !== req.user.id && req.user.role !== 'admin') return res.status(403).json({ error: 'Forbidden' });
    const { name, avatar, color, is_kids, pin } = req.body || {};
    db.prepare('UPDATE profiles SET name=?,avatar=?,color=?,is_kids=?,pin=? WHERE id=?')
      .run(name ?? p.name, avatar ?? p.avatar, color ?? p.color, is_kids != null ? (is_kids ? 1 : 0) : p.is_kids, pin !== undefined ? pin : p.pin, p.id);
    res.json({ profile: db.prepare('SELECT * FROM profiles WHERE id=?').get(p.id) });
  });

  app.delete('/api/profiles/:id', authRequired, (req, res) => {
    const p = db.prepare('SELECT * FROM profiles WHERE id=?').get(req.params.id);
    if (!p) return res.status(404).json({ error: 'Not found' });
    if (p.user_id !== req.user.id && req.user.role !== 'admin') return res.status(403).json({ error: 'Forbidden' });
    db.prepare('DELETE FROM profiles WHERE id=?').run(p.id);
    res.json({ ok: true });
  });

  // ---------- catalog ----------
  app.get('/api/genres', (req, res) => {
    const all = db.prepare('SELECT genres FROM content').all();
    const set = new Set();
    for (const r of all) { try { JSON.parse(r.genres || '[]').forEach(g => set.add(g)); } catch {} }
    res.json({ genres: [...set].sort() });
  });

  app.get('/api/content', optionalProfile, (req, res) => {
    const { search = '', genre = '', type = '', sort = 'added', limit = 48, offset = 0, profileId } = req.query;
    const kf = kidsFilter(profileId);
    let where = '1=1' + kf.clause;
    const params = [];
    if (search) { where += ' AND (title LIKE ? OR description LIKE ? OR director LIKE ?)'; params.push(`%${search}%`, `%${search}%`, `%${search}%`); }
    if (genre) { where += ' AND genres LIKE ?'; params.push(`%${genre}%`); }
    if (type) { where += ' AND type=?'; params.push(type); }
    const order = sort === 'trending' ? 'views_count DESC' : sort === 'year' ? 'year DESC' : sort === 'title' ? 'title ASC' : 'added_at DESC';
    const total = db.prepare(`SELECT COUNT(*) c FROM content WHERE ${where}`).get(...params).c;
    const rows = db.prepare(`SELECT * FROM content WHERE ${where} ORDER BY ${order} LIMIT ? OFFSET ?`).all(...params, Number(limit), Number(offset)).map(row);
    res.json({ total, items: rows });
  });

  app.get('/api/browse/rows', optionalProfile, (req, res) => {
    const { profileId } = req.query;
    const kf = kidsFilter(profileId);
    const q = (extra, order, lim) => db.prepare(`SELECT * FROM content WHERE 1=1 ${kf.clause} ${extra} ORDER BY ${order} LIMIT ${lim}`).all().map(row);
    let continueItems = [];
    if (profileId) {
      continueItems = db.prepare(`
        SELECT c.*, w.position_sec, w.duration_sec, w.episode_id, w.updated_at FROM watch_history w
        JOIN content c ON c.id = w.content_id
        WHERE w.profile_id=? AND w.completed=0 AND w.position_sec > 5
        ORDER BY w.updated_at DESC LIMIT 12`).all(profileId).map(r => {
        const c = row(r);
        c.resume_at = r.position_sec; c.resume_of = r.duration_sec; c.resume_episode = r.episode_id;
        return c;
      });
    }
    let myList = [];
    if (profileId) {
      myList = db.prepare(`SELECT c.* FROM my_list m JOIN content c ON c.id=m.content_id WHERE m.profile_id=? ORDER BY m.added_at DESC LIMIT 24`).all(profileId).map(row);
    }
    const hero = q('AND is_featured=1', 'views_count DESC, added_at DESC', 5);
    const heroFallback = hero.length ? hero : q('', 'views_count DESC', 5);
    const trending = q('', 'views_count DESC', 12);
    const fresh = q('', 'added_at DESC', 12);
    const movies = q("AND type='movie'", 'views_count DESC', 12);
    const series = q("AND type='series'", 'views_count DESC', 12);
    // genre rows for top genres
    const genreRows = [];
    for (const g of ['Drama', 'Comedy', 'Action', 'Sci-Fi', 'Thriller', 'Romance', 'Animation']) {
      const items = db.prepare(`SELECT * FROM content WHERE genres LIKE ? ${kf.clause} ORDER BY views_count DESC LIMIT 10`).all(`%${g}%`).map(row);
      if (items.length >= 3) genreRows.push({ title: `${g}`, genre: g, items });
    }
    res.json({ hero: heroFallback, continue: continueItems, myList, trending, top10: trending.slice(0, 10), fresh, movies, series, genreRows });
  });

  app.get('/api/content/:id', optionalProfile, (req, res) => {
    const c = db.prepare('SELECT * FROM content WHERE id=?').get(req.params.id);
    if (!c) return res.status(404).json({ error: 'Not found' });
    const out = row(c);
    out.episodes = db.prepare('SELECT * FROM episodes WHERE content_id=? ORDER BY season,episode').all(c.id);
    const { profileId } = req.query;
    if (profileId) {
      out.in_list = !!db.prepare('SELECT 1 FROM my_list WHERE profile_id=? AND content_id=?').get(profileId, c.id);
      out.my_rating = db.prepare('SELECT value FROM ratings WHERE profile_id=? AND content_id=?').get(profileId, c.id)?.value || 0;
      out.progress = db.prepare('SELECT * FROM watch_history WHERE profile_id=? AND content_id=? AND episode_id IS NULL').get(profileId, c.id) || null;
    }
    res.json({ item: out });
  });

  app.get('/api/content/:id/episodes', (req, res) => {
    const eps = db.prepare('SELECT * FROM episodes WHERE content_id=? ORDER BY season,episode').all(req.params.id);
    res.json({ episodes: eps });
  });

  app.post('/api/content/:id/view', authRequired, ownProfile, (req, res) => {
    const { episodeId } = req.body || {};
    if (!getContentOr404(res, req.params.id)) return;
    if (episodeId && !db.prepare('SELECT 1 FROM episodes WHERE id=?').get(episodeId)) return res.status(404).json({ error: 'Episode not found' });
    const profileId = req.profile.id;
    db.prepare('UPDATE content SET views_count = views_count + 1 WHERE id=?').run(req.params.id);
    if (profileId) db.prepare('INSERT INTO views_log (profile_id,content_id,episode_id,event) VALUES (?,?,?,?)').run(profileId, req.params.id, episodeId || null, 'play');
    res.json({ ok: true });
  });

  // ---------- my list / ratings ----------
  app.get('/api/mylist', authRequired, ownProfile, (req, res) => {
    const profileId = req.profile.id;
    const items = db.prepare(`SELECT c.* FROM my_list m JOIN content c ON c.id=m.content_id WHERE m.profile_id=? ORDER BY m.added_at DESC`).all(profileId).map(row);
    res.json({ items });
  });
  app.post('/api/mylist', authRequired, ownProfile, (req, res) => {
    const profileId = req.profile.id;
    const { contentId } = req.body || {};
    if (!contentId) return res.status(400).json({ error: 'contentId required' });
    if (!getContentOr404(res, contentId)) return;
    db.prepare('INSERT OR IGNORE INTO my_list (profile_id,content_id) VALUES (?,?)').run(profileId, contentId);
    res.json({ ok: true });
  });
  app.delete('/api/mylist/:contentId', authRequired, ownProfile, (req, res) => {
    db.prepare('DELETE FROM my_list WHERE profile_id=? AND content_id=?').run(req.profile.id, req.params.contentId);
    res.json({ ok: true });
  });

  app.post('/api/rate', authRequired, ownProfile, (req, res) => {
    const profileId = req.profile.id;
    const { contentId, value } = req.body || {};
    if (!contentId || ![1, -1].includes(value)) return res.status(400).json({ error: 'contentId, value (1|-1) required' });
    if (!getContentOr404(res, contentId)) return;
    const prev = db.prepare('SELECT value FROM ratings WHERE profile_id=? AND content_id=?').get(profileId, contentId);
    if (prev) {
      if (prev.value === value) {
        db.prepare('DELETE FROM ratings WHERE profile_id=? AND content_id=?').run(profileId, contentId);
        db.prepare('UPDATE content SET rating_sum = rating_sum - ?, rating_count = rating_count - 1 WHERE id=?').run(value, contentId);
      } else {
        db.prepare('UPDATE ratings SET value=? WHERE profile_id=? AND content_id=?').run(value, profileId, contentId);
        db.prepare('UPDATE content SET rating_sum = rating_sum + ? WHERE id=?').run(value - prev.value, contentId);
      }
    } else {
      db.prepare('INSERT INTO ratings (profile_id,content_id,value) VALUES (?,?,?)').run(profileId, contentId, value);
      db.prepare('UPDATE content SET rating_sum = rating_sum + ?, rating_count = rating_count + 1 WHERE id=?').run(value, contentId);
    }
    res.json({ ok: true });
  });

  app.post('/api/progress', authRequired, ownProfile, (req, res) => {
    const profileId = req.profile.id;
    const { contentId, episodeId, position_sec, duration_sec } = req.body || {};
    if (!contentId) return res.status(400).json({ error: 'contentId required' });
    if (!getContentOr404(res, contentId)) return;
    if (episodeId && !db.prepare('SELECT 1 FROM episodes WHERE id=? AND content_id=?').get(episodeId, contentId)) return res.status(404).json({ error: 'Episode not found for this title' });
    const pos = Math.floor(position_sec || 0), dur = Math.floor(duration_sec || 0);
    const completed = dur > 0 && pos >= dur * 0.95 ? 1 : 0;
    // Manual upsert (episode_id can be NULL, and NULLs never conflict in SQLite)
    const upd = episodeId
      ? db.prepare(`UPDATE watch_history SET position_sec=?, duration_sec=?, completed=?, updated_at=datetime('now') WHERE profile_id=? AND content_id=? AND episode_id=?`)
          .run(pos, dur, completed, profileId, contentId, episodeId)
      : db.prepare(`UPDATE watch_history SET position_sec=?, duration_sec=?, completed=?, updated_at=datetime('now') WHERE profile_id=? AND content_id=? AND episode_id IS NULL`)
          .run(pos, dur, completed, profileId, contentId);
    if (upd.changes === 0) {
      db.prepare(`INSERT INTO watch_history (profile_id,content_id,episode_id,position_sec,duration_sec,completed,updated_at)
        VALUES (?,?,?,?,?,?,datetime('now'))`).run(profileId, contentId, episodeId || null, pos, dur, completed);
    }
    res.json({ ok: true, completed: !!completed });
  });

  app.get('/api/history', authRequired, ownProfile, (req, res) => {
    const profileId = req.profile.id;
    const { limit = 30 } = req.query;
    const rows = db.prepare(`
      SELECT w.*, c.title, c.type, e.title as ep_title, e.season, e.episode FROM watch_history w
      JOIN content c ON c.id=w.content_id LEFT JOIN episodes e ON e.id=w.episode_id
      WHERE w.profile_id=? ORDER BY w.updated_at DESC LIMIT ?`).all(profileId, Number(limit));
    res.json({ items: rows });
  });

  // ---------- streaming ----------
  app.get('/api/stream/movie/:id', (req, res) => {
    const c = db.prepare('SELECT * FROM content WHERE id=?').get(req.params.id);
    if (!c) return res.status(404).json({ error: 'Not found' });
    if (c.video_path && fs.existsSync(c.video_path)) return sendRange(req, res, c.video_path);
    if (c.remote_url) return res.redirect(c.remote_url);
    return res.status(404).json({ error: 'No video file. Add files to media/ and re-scan.' });
  });

  app.get('/api/stream/episode/:id', (req, res) => {
    const e = db.prepare('SELECT * FROM episodes WHERE id=?').get(req.params.id);
    if (!e) return res.status(404).json({ error: 'Not found' });
    if (e.video_path && fs.existsSync(e.video_path)) return sendRange(req, res, e.video_path);
    if (e.remote_url) return res.redirect(e.remote_url);
    return res.status(404).json({ error: 'No video file' });
  });

  // subtitles: find .vtt/.srt next to video
  app.get('/api/subs/movie/:id', (req, res) => {
    const c = db.prepare('SELECT * FROM content WHERE id=?').get(req.params.id);
    if (!c || !c.video_path) return res.json({ subs: [] });
    const dir = path.dirname(c.video_path);
    const base = path.parse(c.video_path).name;
    const out = [];
    if (fs.existsSync(dir)) for (const f of fs.readdirSync(dir)) {
      if ((f.startsWith(base) || f.toLowerCase().includes('sub')) && ['.vtt'].includes(path.extname(f).toLowerCase()))
        out.push({ lang: path.parse(f).name, url: `/api/file?path=${encodeURIComponent(path.join(dir, f))}` });
    }
    res.json({ subs: out });
  });
  app.get('/api/subs/episode/:id', (req, res) => {
    const e = db.prepare('SELECT * FROM episodes WHERE id=?').get(req.params.id);
    if (!e || !e.video_path) return res.json({ subs: [] });
    const dir = path.dirname(e.video_path);
    const base = path.parse(e.video_path).name;
    const out = [];
    if (fs.existsSync(dir)) for (const f of fs.readdirSync(dir)) {
      if ((f.startsWith(base)) && ['.vtt'].includes(path.extname(f).toLowerCase()))
        out.push({ lang: path.parse(f).name, url: `/api/file?path=${encodeURIComponent(path.join(dir, f))}` });
    }
    res.json({ subs: out });
  });

  // ---------- admin ----------
  app.get('/api/admin/stats', adminRequired, (req, res) => {
    const users = db.prepare('SELECT COUNT(*) c FROM users').get().c;
    const profiles = db.prepare('SELECT COUNT(*) c FROM profiles').get().c;
    const movies = db.prepare("SELECT COUNT(*) c FROM content WHERE type='movie'").get().c;
    const series = db.prepare("SELECT COUNT(*) c FROM content WHERE type='series'").get().c;
    const episodes = db.prepare('SELECT COUNT(*) c FROM episodes').get().c;
    const views = db.prepare('SELECT COUNT(*) c FROM views_log').get().c;
    const top = db.prepare('SELECT id,title,type,views_count FROM content ORDER BY views_count DESC LIMIT 8').all();
    const last7 = db.prepare(`SELECT date(started_at) d, COUNT(*) c FROM views_log WHERE started_at >= date('now','-7 days') GROUP BY d ORDER BY d`).all();
    const recent = db.prepare(`SELECT v.*, c.title, p.name as profile FROM views_log v LEFT JOIN content c ON c.id=v.content_id LEFT JOIN profiles p ON p.id=v.profile_id ORDER BY v.started_at DESC LIMIT 20`).all();
    // storage
    let bytes = 0;
    const walk = (dir) => {
      if (!fs.existsSync(dir)) return;
      for (const f of fs.readdirSync(dir)) {
        const p = path.join(dir, f);
        const s = fs.statSync(p);
        if (s.isDirectory()) walk(p); else bytes += s.size;
      }
    };
    walk(path.join(__dirname, '..', 'media'));
    res.json({ users, profiles, movies, series, episodes, views, top, last7, recent, storageBytes: bytes });
  });

  app.get('/api/admin/users', adminRequired, (req, res) => {
    const users = db.prepare('SELECT id,email,name,role,is_active,created_at FROM users ORDER BY id').all();
    for (const u of users) {
      u.profiles = db.prepare('SELECT * FROM profiles WHERE user_id=?').all(u.id);
      u.watch_count = db.prepare(`SELECT COUNT(*) c FROM views_log v JOIN profiles p ON p.id=v.profile_id WHERE p.user_id=?`).get(u.id).c;
    }
    res.json({ users });
  });

  app.post('/api/admin/users', adminRequired, (req, res) => {
    const { email, password, name, role } = req.body || {};
    if (!email || !password) return res.status(400).json({ error: 'email + password required' });
    try {
      const r = db.prepare('INSERT INTO users (email,password_hash,name,role) VALUES (?,?,?,?)')
        .run(email.toLowerCase(), hashPassword(password), name || '', role === 'admin' ? 'admin' : 'user');
      db.prepare('INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)').run(r.lastInsertRowid, (name || 'User').split(' ')[0], '😎', '#0071eb');
      res.json({ ok: true, id: r.lastInsertRowid });
    } catch (e) { res.status(400).json({ error: 'Email already exists' }); }
  });

  app.put('/api/admin/users/:id', adminRequired, (req, res) => {
    const u = db.prepare('SELECT * FROM users WHERE id=?').get(req.params.id);
    if (!u) return res.status(404).json({ error: 'Not found' });
    const { name, role, is_active, password } = req.body || {};
    // role omitted => keep existing (previously this silently demoted to 'user')
    const newRole = role === undefined ? u.role : (role === 'admin' ? 'admin' : 'user');
    if (Number(req.params.id) === req.user.id) {
      if (newRole !== 'admin') return res.status(400).json({ error: 'You cannot demote yourself' });
      if (is_active === 0 || is_active === false) return res.status(400).json({ error: 'You cannot disable yourself' });
    }
    db.prepare('UPDATE users SET name=?, role=?, is_active=? WHERE id=?').run(
      name ?? u.name, newRole, is_active != null ? (is_active ? 1 : 0) : u.is_active, u.id);
    if (password) db.prepare('UPDATE users SET password_hash=? WHERE id=?').run(hashPassword(password), u.id);
    res.json({ ok: true });
  });

  app.delete('/api/admin/users/:id', adminRequired, (req, res) => {
    if (Number(req.params.id) === req.user.id) return res.status(400).json({ error: 'Cannot delete yourself' });
    db.prepare('DELETE FROM users WHERE id=?').run(req.params.id);
    res.json({ ok: true });
  });

  app.get('/api/admin/content', adminRequired, (req, res) => {
    const { type = '', search = '' } = req.query;
    let where = '1=1'; const params = [];
    if (type) { where += ' AND type=?'; params.push(type); }
    if (search) { where += ' AND title LIKE ?'; params.push(`%${search}%`); }
    const items = db.prepare(`SELECT * FROM content WHERE ${where} ORDER BY added_at DESC LIMIT 200`).all(...params).map(c => {
      const o = row(c);
      o.episode_count = db.prepare('SELECT COUNT(*) c FROM episodes WHERE content_id=?').get(c.id).c;
      return o;
    });
    res.json({ items });
  });

  // Create a new title manually (movie with a direct .mp4 URL, or an empty series shell to add episodes to).
  // For files on disk, prefer dropping them in media/ + Rescan instead.
  app.post('/api/admin/content', adminRequired, (req, res) => {
    const b = req.body || {};
    if (!b.title || !b.title.trim()) return res.status(400).json({ error: 'Title required' });
    const type = b.type === 'series' ? 'series' : 'movie';
    const str = (v) => (v === undefined || v === null) ? '' : String(v);
    const r = db.prepare(`INSERT INTO content (type,title,description,year,genres,cast,director,maturity_rating,duration_sec,poster_path,backdrop_path,trailer_url,video_path,remote_url,is_featured)
      VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`).run(
      type, b.title.trim(), str(b.description), b.year ? Number(b.year) : null,
      JSON.stringify(Array.isArray(b.genres) ? b.genres : (b.genres ? String(b.genres).split(',').map(s => s.trim()).filter(Boolean) : [])),
      JSON.stringify(Array.isArray(b.cast) ? b.cast : (b.cast ? String(b.cast).split(',').map(s => s.trim()).filter(Boolean) : [])),
      str(b.director), str(b.maturity_rating) || 'U/A 13+',
      b.duration_sec ? Number(b.duration_sec) : 0,
      str(b.poster_path), str(b.backdrop_path), str(b.trailer_url), str(b.video_path), str(b.remote_url),
      b.is_featured ? 1 : 0);
    res.json({ ok: true, id: r.lastInsertRowid });
  });

  app.put('/api/admin/content/:id', adminRequired, (req, res) => {
    const c = db.prepare('SELECT * FROM content WHERE id=?').get(req.params.id);
    if (!c) return res.status(404).json({ error: 'Not found' });
    const b = req.body || {};
    const arr = (v, fallback) => JSON.stringify(v === undefined ? JSON.parse(fallback || '[]') : (Array.isArray(v) ? v : String(v).split(',').map(s => s.trim()).filter(Boolean)));
    db.prepare(`UPDATE content SET title=?,description=?,year=?,genres=?,cast=?,director=?,maturity_rating=?,duration_sec=?,poster_path=?,backdrop_path=?,trailer_url=?,video_path=?,remote_url=?,is_featured=? WHERE id=?`).run(
      b.title ?? c.title, b.description ?? c.description, b.year !== undefined ? (b.year || null) : c.year,
      arr(b.genres, c.genres), arr(b.cast, c.cast),
      b.director ?? c.director, b.maturity_rating ?? c.maturity_rating,
      b.duration_sec !== undefined ? Number(b.duration_sec) : c.duration_sec,
      b.poster_path ?? c.poster_path, b.backdrop_path ?? c.backdrop_path,
      b.trailer_url ?? c.trailer_url, b.video_path ?? c.video_path, b.remote_url ?? c.remote_url,
      b.is_featured !== undefined ? (b.is_featured ? 1 : 0) : c.is_featured, c.id);
    res.json({ ok: true });
  });

  // Episodes: add / remove (for series created manually or scanned)
  app.post('/api/admin/episodes', adminRequired, (req, res) => {
    const { content_id, season, episode, title, description, duration_sec, remote_url, video_path } = req.body || {};
    if (!content_id || !title) return res.status(400).json({ error: 'content_id + title required' });
    const show = db.prepare("SELECT * FROM content WHERE id=? AND type='series'").get(content_id);
    if (!show) return res.status(404).json({ error: 'Series not found' });
    const s = Number(season) || 1, e = Number(episode) || ((db.prepare('SELECT MAX(episode) m FROM episodes WHERE content_id=? AND season=?').get(content_id, s).m || 0) + 1);
    if (db.prepare('SELECT 1 FROM episodes WHERE content_id=? AND season=? AND episode=?').get(content_id, s, e))
      return res.status(400).json({ error: `S${s} E${e} already exists` });
    const r = db.prepare('INSERT INTO episodes (content_id,season,episode,title,description,duration_sec,remote_url,video_path) VALUES (?,?,?,?,?,?,?,?)')
      .run(content_id, s, e, title, description || '', duration_sec ? Number(duration_sec) : 0, remote_url || '', video_path || '');
    res.json({ ok: true, id: r.lastInsertRowid });
  });

  app.delete('/api/admin/episodes/:id', adminRequired, (req, res) => {
    const e = db.prepare('SELECT * FROM episodes WHERE id=?').get(req.params.id);
    if (!e) return res.status(404).json({ error: 'Not found' });
    db.prepare('DELETE FROM episodes WHERE id=?').run(e.id);
    res.json({ ok: true });
  });

  // Restore the demo catalog (only adds missing demo titles, never touches users)
  app.post('/api/admin/seed-demo', adminRequired, (req, res) => {
    const r = seedCatalog();
    res.json({ ok: true, ...r });
  });

  app.delete('/api/admin/content/:id', adminRequired, (req, res) => {
    db.prepare('DELETE FROM content WHERE id=?').run(req.params.id);
    res.json({ ok: true });
  });

  app.post('/api/admin/scan', adminRequired, (req, res) => {
    const r = scanMedia(false);
    res.json({ ok: true, ...r });
  });

  // Change a title's type (e.g. added as movie, turns out to be a series).
  // movie -> series is always safe (linked video stays stored, unused).
  // series -> movie is refused while episodes exist (delete them first).
  app.post('/api/admin/content/:id/convert', adminRequired, (req, res) => {
    const c = db.prepare('SELECT * FROM content WHERE id=?').get(req.params.id);
    if (!c) return res.status(404).json({ error: 'Not found' });
    const to = req.body && req.body.type;
    if (!['movie', 'series'].includes(to)) return res.status(400).json({ error: 'type must be movie or series' });
    if (to === c.type) return res.json({ ok: true, unchanged: true });
    if (c.type === 'series' && to === 'movie') {
      const n = db.prepare('SELECT COUNT(*) n FROM episodes WHERE content_id=?').get(c.id).n;
      if (n > 0) return res.status(400).json({ error: `Delete its ${n} episode(s) first` });
    }
    db.prepare('UPDATE content SET type=? WHERE id=?').run(to, c.id);
    res.json({ ok: true, from: c.type, to });
  });

  app.get('/api/admin/views', adminRequired, (req, res) => {
    const rows = db.prepare(`SELECT v.*, c.title, p.name as profile FROM views_log v LEFT JOIN content c ON c.id=v.content_id LEFT JOIN profiles p ON p.id=v.profile_id ORDER BY v.started_at DESC LIMIT 100`).all();
    res.json({ items: rows });
  });

  app.post('/api/admin/content/:id/poster', adminRequired, upload.fields([{ name: 'poster' }, { name: 'backdrop' }]), (req, res) => {
    const c = db.prepare('SELECT * FROM content WHERE id=?').get(req.params.id);
    if (!c) return res.status(404).json({ error: 'Not found' });
    const updates = {};
    if (req.files?.poster?.[0]) {
      const f = req.files.poster[0];
      const dest = path.join(POSTERS_DIR, `c${c.id}-poster${path.extname(f.originalname || '.jpg')}`);
      fs.renameSync(f.path, dest);
      updates.poster_path = dest;
    }
    if (req.files?.backdrop?.[0]) {
      const f = req.files.backdrop[0];
      const dest = path.join(POSTERS_DIR, `c${c.id}-backdrop${path.extname(f.originalname || '.jpg')}`);
      fs.renameSync(f.path, dest);
      updates.backdrop_path = dest;
    }
    if (updates.poster_path) db.prepare('UPDATE content SET poster_path=? WHERE id=?').run(updates.poster_path, c.id);
    if (updates.backdrop_path) db.prepare('UPDATE content SET backdrop_path=? WHERE id=?').run(updates.backdrop_path, c.id);
    res.json({ ok: true });
  });

  // ---------- folder / file upload (drag & drop from CRM) ----------
  // Client sends: files[] (multipart) + paths (JSON array, same order, folder-relative)
  // + target ('movies' | 'series'). Files land in media/<target>/<path>, then auto-rescan.
  const UPLOAD_TMP = path.join(DATA_DIR, 'tmp-uploads');
  if (!fs.existsSync(UPLOAD_TMP)) fs.mkdirSync(UPLOAD_TMP, { recursive: true });
  const uploadMany = multer({
    dest: UPLOAD_TMP,
    limits: { fileSize: 25 * 1024 * 1024 * 1024, files: 500 },
  });
  const UPLOAD_EXTS = ['.mp4', '.webm', '.mkv', '.mov', '.avi', '.m4v', '.jpg', '.jpeg', '.png', '.webp', '.vtt', '.srt', '.json'];
  function safeRel(p) {
    const parts = String(p || '').split(/[\\/]+/)
      .filter(s => s && s !== '.' && s !== '..')
      .map(s => s.replace(/^[.]+/, '').replace(/[<>:"|?*\x00-\x1f]/g, '_').trim())
      .filter(Boolean);
    return parts.join('/');
  }

  const VIDEO_EXTS = ['.mp4', '.webm', '.mkv', '.mov', '.avi', '.m4v'];
  app.post('/api/admin/upload', adminRequired, uploadMany.array('files', 500), (req, res) => {
    let paths = [];
    try { paths = JSON.parse(req.body.paths || '[]'); } catch { paths = []; }
    if (!Array.isArray(paths)) paths = [];
    // Attach to an existing title? Files go into that title's folder and
    // episode/movie rows are created or linked automatically.
    let contentRow = null;
    if (req.body.content_id) {
      contentRow = db.prepare('SELECT * FROM content WHERE id=?').get(req.body.content_id);
      if (!contentRow) {
        (req.files || []).forEach(f => { try { fs.unlinkSync(f.path); } catch {} });
        return res.status(404).json({ error: 'Title not found' });
      }
    }
    let base;
    if (contentRow && contentRow.type === 'series') {
      const firstEp = db.prepare("SELECT video_path FROM episodes WHERE content_id=? AND video_path != '' LIMIT 1").get(contentRow.id);
      base = path.resolve(firstEp && firstEp.video_path ? path.dirname(firstEp.video_path) : path.join(SERIES_DIR, safeRel(contentRow.title) || ('title-' + contentRow.id)));
    } else if (contentRow) {
      base = path.resolve(contentRow.video_path ? path.dirname(contentRow.video_path) : path.join(MOVIES_DIR, safeRel(contentRow.title) || ('title-' + contentRow.id)));
    } else {
      base = path.resolve(req.body.target === 'series' ? SERIES_DIR : MOVIES_DIR);
    }
    let uploaded = 0;
    const skipped = [];
    const savedVideos = [];
    (req.files || []).forEach((f, i) => {
      const drop = () => { try { fs.unlinkSync(f.path); } catch {} };
      const rel = safeRel(paths[i] || f.originalname);
      const ext = path.extname(rel).toLowerCase();
      if (!rel || !UPLOAD_EXTS.includes(ext)) { skipped.push(paths[i] || f.originalname || 'unknown'); drop(); return; }
      const dest = path.resolve(base, rel);
      if (!dest.startsWith(base + path.sep)) { skipped.push(rel); drop(); return; }
      try {
        fs.mkdirSync(path.dirname(dest), { recursive: true });
        if (fs.existsSync(dest)) fs.unlinkSync(dest); // re-drop overwrites
        fs.renameSync(f.path, dest);
        uploaded++;
        if (VIDEO_EXTS.includes(ext)) savedVideos.push({ rel, abs: dest });
      } catch (e) { skipped.push(rel); drop(); }
    });
    let episodes_added = 0;
    let linked_video = null;
    if (contentRow && contentRow.type === 'series') {
      for (const v of savedVideos) {
        if (db.prepare('SELECT 1 FROM episodes WHERE content_id=? AND video_path=?').get(contentRow.id, v.abs)) continue;
        const parsed = parseEpisodeFile(path.basename(v.rel));
        let s = parsed.season || 1;
        let e = parsed.episode;
        if (!e) e = (db.prepare('SELECT MAX(episode) m FROM episodes WHERE content_id=? AND season=?').get(contentRow.id, s).m || 0) + 1;
        if (db.prepare('SELECT 1 FROM episodes WHERE content_id=? AND season=? AND episode=?').get(contentRow.id, s, e)) continue;
        db.prepare('INSERT INTO episodes (content_id,season,episode,title,video_path) VALUES (?,?,?,?,?)')
          .run(contentRow.id, s, e, parsed.title || path.parse(v.rel).name, v.abs);
        episodes_added++;
      }
    } else if (contentRow && savedVideos.length) {
      const attach = req.body.attach === 'replace' ? 'replace' : 'auto';
      if (attach === 'replace' || (!contentRow.video_path && !contentRow.remote_url)) {
        db.prepare('UPDATE content SET video_path=?, remote_url=? WHERE id=?').run(savedVideos[0].abs, '', contentRow.id);
        linked_video = savedVideos[0].abs;
      }
    }
    const scan = scanMedia(false);
    res.json({ ok: true, uploaded, skipped, episodes_added, linked_video, files: savedVideos.map(v => v.abs), ...scan });
  });
}

module.exports = { registerApi };
