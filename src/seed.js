const { db } = require('./db');
const { hashPassword } = require('./auth');

const SAMPLE_VIDEOS = [
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/BigBuckBunny.mp4',
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ElephantsDream.mp4',
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerBlazes.mp4',
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerEscapes.mp4',
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerFun.mp4',
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerJoyrides.mp4',
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/Sintel.mp4',
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/TearsOfSteel.mp4',
];

function seedUsers() {
  // admin + demo user (idempotent)
  const adminExists = db.prepare('SELECT * FROM users WHERE email=?').get('admin@lan.local');
  if (!adminExists) {
    const r = db.prepare('INSERT INTO users (email,password_hash,name,role) VALUES (?,?,?,?)')
      .run('admin@lan.local', hashPassword('admin123'), 'Admin', 'admin');
    db.prepare('INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)').run(r.lastInsertRowid, 'Admin', '👑', '#E50914');
    db.prepare('INSERT INTO profiles (user_id,name,avatar,color,is_kids) VALUES (?,?,?,?,?)').run(r.lastInsertRowid, 'Kids', '🦊', '#46d369', 1);
    console.log('seed: admin@lan.local / admin123');
  }
  const demoExists = db.prepare('SELECT * FROM users WHERE email=?').get('demo@lan.local');
  if (!demoExists) {
    const r = db.prepare('INSERT INTO users (email,password_hash,name,role) VALUES (?,?,?,?)')
      .run('demo@lan.local', hashPassword('demo1234'), 'Demo Family', 'user');
    const p1 = db.prepare('INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)').run(r.lastInsertRowid, 'Mohit', '😎', '#0071eb');
    db.prepare('INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)').run(r.lastInsertRowid, 'Guest', '🎬', '#f5a623');
  }
}

function seedCatalog() {
  // Demo catalog. Skips titles that already exist (safe to re-run for restore).

  const demo = [
    { type: 'movie', title: 'Big Buck Bunny', year: 2008, genres: ['Animation', 'Comedy', 'Family'], desc: 'A giant rabbit takes revenge on three bullying rodents in this classic open-source comedy.', director: 'Sacha Goedegebure', cast: ['Blender Foundation'], maturity: 'U', dur: 596, featured: 1 },
    { type: 'movie', title: 'Sintel', year: 2010, genres: ['Fantasy', 'Adventure', 'Drama'], desc: 'A lonely young woman searches for the dragon she once befriended. A Blender Foundation epic.', director: 'Colin Levy', cast: ['Halina Reijn'], maturity: 'U/A 13+', dur: 888, featured: 1 },
    { type: 'movie', title: 'Tears of Steel', year: 2012, genres: ['Sci-Fi', 'Action', 'Drama'], desc: 'Forty years after a breakup in Amsterdam, a group of warriors save the world from killer robots.', director: 'Ian Hubert', cast: ['Derek de Lint'], maturity: 'U/A 16+', dur: 734, featured: 1 },
    { type: 'movie', title: 'Elephants Dream', year: 2006, genres: ['Sci-Fi', 'Animation', 'Mystery'], desc: 'Two travelers explore a strange infinite machine. The first open-source film ever made.', director: 'Bassam Kurdali', cast: ['Tygo Gernandt'], maturity: 'U', dur: 653, featured: 0 },
    { type: 'series', title: 'Blender Shorts', year: 2023, genres: ['Comedy', 'Animation'], desc: 'A collection of bite-sized open movies. Perfect to test episodes, autoplay and continue-watching.', maturity: 'U', featured: 1, eps: [
      { t: 'For Bigger Blazes', d: 'Chromecast promo action short.', dur: 15 },
      { t: 'For Bigger Escapes', d: 'An escape story in 15 seconds.', dur: 15 },
      { t: 'For Bigger Fun', d: 'Fun in every frame.', dur: 60 },
      { t: 'For Bigger Joyrides', d: 'A joyride you will not forget.', dur: 15 },
    ]},
    { type: 'movie', title: 'Neon City Nights', year: 2024, genres: ['Thriller', 'Crime', 'Drama'], desc: 'A cab driver witnesses something he should not have on the last night before the city power grid resets.', director: 'Local Studio', cast: ['A. Sharma', 'R. Verma'], maturity: 'A 18+', dur: 5400, featured: 0 },
    { type: 'movie', title: 'Monsoon Wedding Blues', year: 2023, genres: ['Romance', 'Comedy', 'Drama'], desc: 'Two families, one flooded venue, and a wedding planner who refuses to give up.', director: 'Local Studio', cast: ['P. Iyer', 'K. Nair'], maturity: 'U/A 13+', dur: 7200, featured: 0 },
    { type: 'series', title: 'LAN Detectives', year: 2024, genres: ['Mystery', 'Comedy'], desc: 'Two hostel roommates solve absurd neighbourhood mysteries using a router log and pure confidence.', maturity: 'U/A 13+', featured: 0, eps: [
      { t: 'The Missing Remote', d: 'S1 E1 — The TV remote vanishes during finals week.', dur: 1200 },
      { t: 'Buffering', d: 'S1 E2 — The Wi-Fi slows every night at 9 PM. Coincidence?', dur: 1300 },
      { t: 'The 4K Heist', d: 'S1 E3 — Somebody is hogging the bandwidth.', dur: 1400 },
    ]},
  ];

  const poster = (seed) => `https://picsum.photos/seed/${encodeURIComponent(seed)}/400/600`;
  const backdrop = (seed) => `https://picsum.photos/seed/${encodeURIComponent(seed)}-bg/1280/720`;

  let vi = 0, inserted = 0;
  const ins = db.prepare(`INSERT INTO content (type,title,description,year,genres,cast,director,maturity_rating,duration_sec,poster_path,backdrop_path,remote_url,is_featured)
    VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`);
  const insEp = db.prepare(`INSERT INTO episodes (content_id,season,episode,title,description,duration_sec,remote_url) VALUES (?,?,?,?,?,?,?)`);

  for (const d of demo) {
    if (db.prepare('SELECT 1 FROM content WHERE type=? AND title=?').get(d.type, d.title)) continue;
    const remote = d.type === 'movie' ? SAMPLE_VIDEOS[vi++ % SAMPLE_VIDEOS.length] : '';
    const r = ins.run(d.type, d.title, d.desc, d.year, JSON.stringify(d.genres), JSON.stringify(d.cast || []), d.director || '', d.maturity, d.dur || 3600, poster(d.title), backdrop(d.title), remote, d.featured ? 1 : 0);
    inserted++;
    if (d.type === 'series' && d.eps) {
      d.eps.forEach((e, i) => {
        insEp.run(r.lastInsertRowid, 1, i + 1, e.t, e.d, e.dur, SAMPLE_VIDEOS[(vi + i) % SAMPLE_VIDEOS.length]);
      });
      vi += d.eps.length;
    }
  }
  console.log(`seed: demo catalog inserted (${inserted} titles)`);
  return { inserted };
}

function seed(force = false) {
  const userCount = db.prepare('SELECT COUNT(*) c FROM users').get().c;
  if (userCount > 0 && !force) { console.log('seed: users exist, skipping'); return; }
  if (force) {
    try { require('./db').resetDb(); } catch {}
  }
  seedUsers();
  seedCatalog();
}

module.exports = { seed, seedUsers, seedCatalog };
