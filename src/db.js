const path = require('path');
const fs = require('fs');

const DATA_DIR = path.join(__dirname, '..', 'data');
if (!fs.existsSync(DATA_DIR)) fs.mkdirSync(DATA_DIR, { recursive: true });

const DB_PATH = process.env.LANFLIX_DB || path.join(DATA_DIR, 'lanflix.db');

// Built-in SQLite (Node 22.5+). No native builds, no npm failures.
const { DatabaseSync } = require('node:sqlite');
const db = new DatabaseSync(DB_PATH);
db.exec(`PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON;`);

function initDb() {
  db.exec(`
  CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    name TEXT DEFAULT '',
    role TEXT DEFAULT 'user' CHECK(role IN ('admin','user')),
    is_active INTEGER DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now'))
  );
  CREATE TABLE IF NOT EXISTS profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    avatar TEXT DEFAULT '🎬',
    color TEXT DEFAULT '#E50914',
    is_kids INTEGER DEFAULT 0,
    pin TEXT DEFAULT NULL,
    created_at TEXT DEFAULT (datetime('now'))
  );
  CREATE TABLE IF NOT EXISTS content (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT DEFAULT 'movie' CHECK(type IN ('movie','series')),
    title TEXT NOT NULL,
    description TEXT DEFAULT '',
    year INTEGER,
    genres TEXT DEFAULT '[]',
    cast TEXT DEFAULT '[]',
    director TEXT DEFAULT '',
    maturity_rating TEXT DEFAULT 'U/A 13+',
    duration_sec INTEGER DEFAULT 0,
    poster_path TEXT DEFAULT '',
    backdrop_path TEXT DEFAULT '',
    trailer_url TEXT DEFAULT '',
    video_path TEXT DEFAULT '',
    remote_url TEXT DEFAULT '',
    views_count INTEGER DEFAULT 0,
    rating_sum INTEGER DEFAULT 0,
    rating_count INTEGER DEFAULT 0,
    is_featured INTEGER DEFAULT 0,
    added_at TEXT DEFAULT (datetime('now'))
  );
  CREATE TABLE IF NOT EXISTS episodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    content_id INTEGER NOT NULL REFERENCES content(id) ON DELETE CASCADE,
    season INTEGER DEFAULT 1,
    episode INTEGER DEFAULT 1,
    title TEXT DEFAULT '',
    description TEXT DEFAULT '',
    duration_sec INTEGER DEFAULT 0,
    video_path TEXT DEFAULT '',
    remote_url TEXT DEFAULT '',
    thumbnail_path TEXT DEFAULT ''
  );
  CREATE TABLE IF NOT EXISTS my_list (
    profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    content_id INTEGER NOT NULL REFERENCES content(id) ON DELETE CASCADE,
    added_at TEXT DEFAULT (datetime('now')),
    PRIMARY KEY (profile_id, content_id)
  );
  CREATE TABLE IF NOT EXISTS ratings (
    profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    content_id INTEGER NOT NULL REFERENCES content(id) ON DELETE CASCADE,
    value INTEGER NOT NULL CHECK(value IN (-1,1)),
    updated_at TEXT DEFAULT (datetime('now')),
    PRIMARY KEY (profile_id, content_id)
  );
  CREATE TABLE IF NOT EXISTS watch_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id INTEGER NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    content_id INTEGER NOT NULL REFERENCES content(id) ON DELETE CASCADE,
    episode_id INTEGER REFERENCES episodes(id) ON DELETE SET NULL,
    position_sec INTEGER DEFAULT 0,
    duration_sec INTEGER DEFAULT 0,
    completed INTEGER DEFAULT 0,
    updated_at TEXT DEFAULT (datetime('now')),
    UNIQUE(profile_id, content_id, episode_id)
  );
  CREATE TABLE IF NOT EXISTS views_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_id INTEGER REFERENCES profiles(id) ON DELETE SET NULL,
    content_id INTEGER REFERENCES content(id) ON DELETE CASCADE,
    episode_id INTEGER REFERENCES episodes(id) ON DELETE SET NULL,
    event TEXT DEFAULT 'play',
    started_at TEXT DEFAULT (datetime('now'))
  );
  CREATE INDEX IF NOT EXISTS idx_content_type ON content(type);
  CREATE INDEX IF NOT EXISTS idx_watch_profile ON watch_history(profile_id);
  CREATE INDEX IF NOT EXISTS idx_ep_content ON episodes(content_id);
  `);
}

function resetDb() {
  try { db.exec(`DROP TABLE IF EXISTS views_log; DROP TABLE IF EXISTS watch_history; DROP TABLE IF EXISTS ratings; DROP TABLE IF EXISTS my_list; DROP TABLE IF EXISTS episodes; DROP TABLE IF EXISTS content; DROP TABLE IF EXISTS profiles; DROP TABLE IF EXISTS users;`); } catch {}
  initDb();
}

initDb();

module.exports = { db, DB_PATH, DATA_DIR };
