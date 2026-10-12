const fs = require('fs');
const path = require('path');
const { db } = require('./db');

const MEDIA_ROOT = path.join(__dirname, '..', 'media');
const MOVIES_DIR = path.join(MEDIA_ROOT, 'movies');
const SERIES_DIR = path.join(MEDIA_ROOT, 'series');
const POSTERS_DIR = path.join(__dirname, '..', 'public', 'posters');

for (const d of [MEDIA_ROOT, MOVIES_DIR, SERIES_DIR, POSTERS_DIR]) {
  if (!fs.existsSync(d)) fs.mkdirSync(d, { recursive: true });
}

const VIDEO_EXTS = ['.mp4', '.webm', '.mkv', '.mov', '.avi', '.m4v'];
const IMG_EXTS = ['.jpg', '.jpeg', '.png', '.webp'];
const SUB_EXTS = ['.vtt', '.srt'];

function parseMovieFolder(name) {
  // "Dune Part Two (2024)" -> { title, year }
  const m = name.match(/^(.*?)\s*\((\d{4})\)\s*$/);
  if (m) return { title: m[1].trim(), year: parseInt(m[2]) };
  return { title: name.trim(), year: null };
}

function parseEpisodeFile(fname) {
  // S01E01, 1x02, S1 E2 patterns
  const base = path.parse(fname).name;
  let m = base.match(/[Ss](\d{1,2})\s*[Ee](\d{1,3})/) || base.match(/(\d{1,2})x(\d{1,3})/);
  if (m) return { season: parseInt(m[1]), episode: parseInt(m[2]), title: base.replace(m[0], '').replace(/^[-_. ]+|[-_. ]+$/g, '').trim() || base };
  return { season: 1, episode: null, title: base };
}

function findInDir(dir, exts) {
  if (!fs.existsSync(dir)) return [];
  return fs.readdirSync(dir).filter(f => exts.includes(path.extname(f).toLowerCase())).map(f => path.join(dir, f));
}

function scanMedia(verbose = false) {
  const log = (...a) => { if (verbose) console.log('[scan]', ...a); };
  let added = 0, updated = 0;

  // ---- MOVIES: media/movies/<Folder>/<video> ----
  if (fs.existsSync(MOVIES_DIR)) {
    for (const folder of fs.readdirSync(MOVIES_DIR)) {
      const full = path.join(MOVIES_DIR, folder);
      if (!fs.statSync(full).isDirectory()) continue;
      const { title, year } = parseMovieFolder(folder);
      const videos = findInDir(full, VIDEO_EXTS);
      if (!videos.length) continue;
      const videoPath = videos[0];
      const posters = findInDir(full, IMG_EXTS);
      const poster = posters.find(p => /poster|cover|folder/i.test(path.basename(p))) || posters[0] || '';
      const backdrop = posters.find(p => /backdrop|banner|fanart/i.test(path.basename(p))) || posters[1] || '';
      const subs = findInDir(full, SUB_EXTS);
      // sidecar metadata.json
      let meta = {};
      const metaPath = path.join(full, 'metadata.json');
      if (fs.existsSync(metaPath)) { try { meta = JSON.parse(fs.readFileSync(metaPath, 'utf8')); } catch {} }

      const existing = db.prepare('SELECT * FROM content WHERE video_path = ?').get(videoPath)
        // same folder = same movie (a second file may sort before the linked one)
        || db.prepare("SELECT * FROM content WHERE type='movie'").all().find(c => c.video_path && path.dirname(c.video_path) === full);
      if (existing) {
        const keepPath = existing.video_path && fs.existsSync(existing.video_path) ? existing.video_path : videoPath;
        db.prepare(`UPDATE content SET title=?, year=?, poster_path=?, backdrop_path=?, video_path=? WHERE id=?`)
          .run(meta.title || title, meta.year || year, poster, backdrop, keepPath, existing.id);
        updated++;
      } else {
        db.prepare(`INSERT INTO content (type,title,description,year,genres,cast,director,maturity_rating,duration_sec,poster_path,backdrop_path,video_path)
          VALUES ('movie',?,?,?,?,?,?,?,?,?,?,?)`).run(
          meta.title || title,
          meta.description || '',
          meta.year || year,
          JSON.stringify(meta.genres || ['Drama']),
          JSON.stringify(meta.cast || []),
          meta.director || '',
          meta.maturity_rating || 'U/A 13+',
          meta.duration_sec || 0,
          poster, backdrop, videoPath
        );
        added++;
        log('movie +', title);
      }
      // store subs path in a side lookup (served by stream route via same folder)
    }
    // also loose files directly in movies/
    for (const f of findInDir(MOVIES_DIR, VIDEO_EXTS)) {
      const base = path.parse(f).name;
      const { title, year } = parseMovieFolder(base);
      const existing = db.prepare('SELECT * FROM content WHERE video_path = ?').get(f);
      if (!existing) {
        db.prepare(`INSERT INTO content (type,title,year,genres,video_path) VALUES ('movie',?,?,?,?)`)
          .run(title, year, JSON.stringify(['Drama']), f);
        added++;
      }
    }
  }

  // ---- SERIES: media/series/<Show>/[Season 1/]video ----
  if (fs.existsSync(SERIES_DIR)) {
    for (const show of fs.readdirSync(SERIES_DIR)) {
      const showDir = path.join(SERIES_DIR, show);
      if (!fs.statSync(showDir).isDirectory()) continue;
      // collect videos recursively one level (Season subfolders or flat)
      let files = [];
      for (const entry of fs.readdirSync(showDir)) {
        const p = path.join(showDir, entry);
        if (fs.statSync(p).isDirectory()) {
          for (const f of fs.readdirSync(p)) {
            const fp = path.join(p, f);
            if (!fs.statSync(fp).isDirectory() && VIDEO_EXTS.includes(path.extname(f).toLowerCase())) files.push(fp);
          }
        } else if (VIDEO_EXTS.includes(path.extname(entry).toLowerCase())) files.push(p);
      }
      if (!files.length) continue;
      const posters = findInDir(showDir, IMG_EXTS);
      const poster = posters.find(p => /poster|cover|folder/i.test(path.basename(p))) || posters[0] || '';
      const backdrop = posters.find(p => /backdrop|banner|fanart/i.test(path.basename(p))) || posters[1] || '';
      let showRow = db.prepare("SELECT * FROM content WHERE type='series' AND title=?").get(show);
      if (!showRow) {
        const r = db.prepare(`INSERT INTO content (type,title,description,year,genres,poster_path,backdrop_path) VALUES ('series',?,?,?,?,?,?)`)
          .run(show, '', null, JSON.stringify(['Drama']), poster, backdrop);
        showRow = db.prepare('SELECT * FROM content WHERE id=?').get(r.lastInsertRowid);
        added++;
        log('series +', show);
      } else {
        db.prepare('UPDATE content SET poster_path=?, backdrop_path=? WHERE id=?').run(poster, backdrop, showRow.id);
      }
      let epNum = 1;
      // sort for stable episode numbering
      files.sort();
      for (const fp of files) {
        const parsed = parseEpisodeFile(path.basename(fp));
        const epNo = parsed.episode || epNum++;
        const seasonNo = parsed.season || 1;
        const exists = db.prepare('SELECT * FROM episodes WHERE content_id=? AND season=? AND episode=?').get(showRow.id, seasonNo, epNo);
        if (!exists) {
          // check by video_path too (renamed)
          const byPath = db.prepare('SELECT * FROM episodes WHERE video_path=?').get(fp);
          if (!byPath) {
            db.prepare('INSERT INTO episodes (content_id,season,episode,title,video_path) VALUES (?,?,?,?,?)')
              .run(showRow.id, seasonNo, epNo, parsed.title || `Episode ${epNo}`, fp);
            added++;
          }
        } else if (exists.video_path !== fp) {
          db.prepare('UPDATE episodes SET video_path=?, title=? WHERE id=?').run(fp, parsed.title || exists.title, exists.id);
          updated++;
        }
      }
    }
  }

  // NOTE: the scanner never deletes catalog rows. If a file goes missing the
  // title/episode stays listed (CRM shows it as missing) and only an explicit
  // admin Delete removes it — scans must never destroy metadata, ratings,
  // watch history or manually created entries.
  if (verbose) console.log(`[scan] done. added=${added} updated=${updated}`);
  return { added, updated };
}

module.exports = { scanMedia, MOVIES_DIR, SERIES_DIR, parseEpisodeFile };
