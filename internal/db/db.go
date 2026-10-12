package db

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var Conn *sql.DB
var DBPath string
var DataDir string
var Root string

func Open() error {
	cwd, _ := os.Getwd()
	root := cwd
	// A compiled binary may run outside the source tree; prefer the
	// executable's dir when it holds the project layout.
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != cwd {
			if _, err := os.Stat(filepath.Join(dir, "data")); err == nil {
				root = dir
			} else if _, err := os.Stat(filepath.Join(dir, "public")); err == nil {
				root = dir
			}
		}
	}
	if p := os.Getenv("LANFLIX_DB"); p != "" {
		DBPath = p
		DataDir = filepath.Dir(p)
	} else {
		DataDir = filepath.Join(root, "data")
		DBPath = filepath.Join(DataDir, "lanflix.db")
	}
	Root = root
	if err := os.MkdirAll(DataDir, 0o755); err != nil {
		return err
	}
	c, err := sql.Open("sqlite", DBPath)
	if err != nil {
		return err
	}
	c.SetMaxOpenConns(1)
	if _, err := c.Exec(`PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		return err
	}
	Conn = c
	if err := initSchema(); err != nil {
		return err
	}
	return migrate()
}

func initSchema() error {
	_, err := Conn.Exec(`
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
		imdb_id TEXT DEFAULT '',
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
	`)
	return err
}

// migrate adds columns introduced after the first release.
func migrate() error {
	rows, err := Conn.Query(`PRAGMA table_info(content)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols := map[string]bool{}
	var cid int
	var name, ctype string
	var notnull, pk int
	var dflt sql.NullString
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if !cols["imdb_id"] {
		if _, err := Conn.Exec(`ALTER TABLE content ADD COLUMN imdb_id TEXT DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func Reset() error {
	for _, t := range []string{"views_log", "watch_history", "ratings", "my_list", "episodes", "content", "profiles", "users"} {
		if _, err := Conn.Exec(`DROP TABLE IF EXISTS ` + t); err != nil {
			return err
		}
	}
	return initSchema()
}

// Row helpers: scan a row into map[string]any with []byte -> string normalization.
func ScanMap(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			case int64:
				m[c] = v
			case float64:
				m[c] = v
			case nil:
				m[c] = nil
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func QueryMap(q string, args ...any) ([]map[string]any, error) {
	rows, err := Conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMap(rows)
}

func QueryOne(q string, args ...any) (map[string]any, error) {
	rows, err := QueryMap(q, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return rows[0], nil
}

func Count(q string, args ...any) int {
	var n int
	_ = Conn.QueryRow(q, args...).Scan(&n)
	return n
}

func MediaRoot() string {
	// media/ lives in the project tree, even when LANFLIX_DB points elsewhere.
	return filepath.Join(Root, "media")
}

func PublicDir() string {
	return filepath.Join(Root, "public")
}
