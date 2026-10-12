package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"lanflix/internal/db"
)

var videoExts = map[string]bool{".mp4": true, ".webm": true, ".mkv": true, ".mov": true, ".avi": true, ".m4v": true}
var imgExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
var subExts = map[string]bool{".vtt": true, ".srt": true}

func MoviesDir() string { return filepath.Join(db.MediaRoot(), "movies") }
func SeriesDir() string { return filepath.Join(db.MediaRoot(), "series") }

var movieRe = regexp.MustCompile(`^(.*?)\s*\((\d{4})\)\s*$`)
var epRe1 = regexp.MustCompile(`[Ss](\d{1,2})\s*[Ee](\d{1,3})`)
var epRe2 = regexp.MustCompile(`(\d{1,2})x(\d{1,3})`)

// Standalone episode markers without a season, e.g.
// "The.Pacific.E03.720p.mkv", "Show - E04 - Title.mp4", "Episode 5.mp4".
var epRe3 = regexp.MustCompile(`(?:^|[\.\s_\-])[Ee](\d{1,3})(?:[\.\s_\-]|$)`)
var epRe4 = regexp.MustCompile(`(?i)episode\s*(\d{1,3})`)

func ParseMovieFolder(name string) (string, *int) {
	if m := movieRe.FindStringSubmatch(name); m != nil {
		y, _ := strconv.Atoi(m[2])
		return strings.TrimSpace(m[1]), &y
	}
	return strings.TrimSpace(name), nil
}

type epParsed struct {
	Season  int
	Episode *int
	Title   string
}

func ParseEpisodeFile(fname string) epParsed {
	base := strings.TrimSuffix(fname, filepath.Ext(fname))
	var s, e *int
	token := ""
	if m := epRe1.FindStringSubmatch(base); m != nil {
		si, _ := strconv.Atoi(m[1])
		ei, _ := strconv.Atoi(m[2])
		s, e = &si, &ei
		token = epRe1.FindString(base)
	} else if m := epRe2.FindStringSubmatch(base); m != nil {
		si, _ := strconv.Atoi(m[1])
		ei, _ := strconv.Atoi(m[2])
		s, e = &si, &ei
		token = epRe2.FindString(base)
	} else if m := epRe3.FindStringSubmatch(base); m != nil {
		ei, _ := strconv.Atoi(m[1])
		e = &ei
		token = epRe3.FindString(base)
	} else if m := epRe4.FindStringSubmatch(base); m != nil {
		ei, _ := strconv.Atoi(m[1])
		e = &ei
		token = epRe4.FindString(base)
	}
	t := base
	if e != nil {
		if token != "" {
			t = strings.Replace(base, token, " ", 1)
		}
		// Normalise separators so "The.Pacific.E03.720p" -> "The Pacific 720p"
		t = strings.ReplaceAll(t, ".", " ")
		t = strings.ReplaceAll(t, "_", " ")
		t = strings.Join(strings.Fields(strings.Trim(t, "-_. ")), " ")
		if t == "" {
			t = base
		}
	}
	si := 1
	if s != nil {
		si = *s
	}
	return epParsed{Season: si, Episode: e, Title: t}
}

func filesInDir(dir string, exts map[string]bool) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if exts[strings.ToLower(filepath.Ext(e.Name()))] {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func pickPoster(files []string, want string) string {
	for _, f := range files {
		b := strings.ToLower(filepath.Base(f))
		if strings.Contains(b, want) {
			return f
		}
	}
	if len(files) > 0 {
		return files[0]
	}
	return ""
}

type fileMeta struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Year        *int     `json:"year"`
	Genres      []string `json:"genres"`
	Cast        []string `json:"cast"`
	Director    string   `json:"director"`
	Maturity    string   `json:"maturity_rating"`
	DurationSec int      `json:"duration_sec"`
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	if b == nil {
		return "[]"
	}
	return string(b)
}

type Result struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Healed  int `json:"healed"`
}

// ReconcilePaths heals stale ABSOLUTE paths left over from a previous
// checkout / restart (e.g. DB points at /old/dir/media/... while the files
// now live under the current Root/media/...). It also normalises legacy
// absolute values that are still valid into Root-relative form so the next
// move/restart is a no-op. Returns number of rows healed.
func ReconcilePaths() int {
	healed := 0
	healOne := func(table, col string, id any, stored string) {
		if stored == "" {
			return
		}
		lower := strings.ToLower(stored)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
			return
		}
		resolved := db.ResolvePath(stored)
		if st, err := os.Stat(resolved); err != nil || st.IsDir() {
			return // truly missing — leave row so admin can see it
		}
		normalised := db.StoredPath(resolved)
		if normalised != stored {
			db.Conn.Exec(`UPDATE `+table+` SET `+col+`=? WHERE id=?`, normalised, id)
			healed++
		}
	}
	contents, _ := db.QueryMap(`SELECT id, video_path, poster_path, backdrop_path FROM content`)
	for _, c := range contents {
		id := c["id"]
		if s, _ := c["video_path"].(string); s != "" {
			healOne("content", "video_path", id, s)
		}
		if s, _ := c["poster_path"].(string); s != "" {
			healOne("content", "poster_path", id, s)
		}
		if s, _ := c["backdrop_path"].(string); s != "" {
			healOne("content", "backdrop_path", id, s)
		}
	}
	eps, _ := db.QueryMap(`SELECT id, video_path, thumbnail_path FROM episodes`)
	for _, e := range eps {
		id := e["id"]
		if s, _ := e["video_path"].(string); s != "" {
			healOne("episodes", "video_path", id, s)
		}
		if s, _ := e["thumbnail_path"].(string); s != "" {
			if s != "" {
				healOne("episodes", "thumbnail_path", id, s)
			}
		}
	}
	return healed
}

func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func ScanMedia() Result {
	res := Result{}
	moviesDir, seriesDir := MoviesDir(), SeriesDir()
	os.MkdirAll(moviesDir, 0o755)
	os.MkdirAll(seriesDir, 0o755)

	// Heal moved-project references FIRST so the duplicate checks below
	// (which compare stored paths) see the current locations instead of
	// inserting duplicate rows for the same files.
	res.Healed = ReconcilePaths()

	resolveDir := func(stored string) string {
		if stored == "" {
			return ""
		}
		return filepath.Dir(db.ResolvePath(stored))
	}

	// ---- movies ----
	if ents, err := os.ReadDir(moviesDir); err == nil {
		for _, ent := range ents {
			if !ent.IsDir() {
				continue
			}
			folder := filepath.Join(moviesDir, ent.Name())
			title, year := ParseMovieFolder(ent.Name())
			videos := filesInDir(folder, videoExts)
			if len(videos) == 0 {
				continue
			}
			videoPath := videos[0]
			storedVideo := db.StoredPath(videoPath)
			imgs := filesInDir(folder, imgExts)
			poster, backdrop := "", ""
			for _, f := range imgs {
				b := strings.ToLower(filepath.Base(f))
				if poster == "" && (strings.Contains(b, "poster") || strings.Contains(b, "cover") || strings.Contains(b, "folder")) {
					poster = f
				}
				if backdrop == "" && (strings.Contains(b, "backdrop") || strings.Contains(b, "banner") || strings.Contains(b, "fanart")) {
					backdrop = f
				}
			}
			if poster == "" && len(imgs) > 0 {
				poster = imgs[0]
			}
			if backdrop == "" && len(imgs) > 1 {
				backdrop = imgs[1]
			}
			storedPoster, storedBackdrop := "", ""
			if poster != "" {
				storedPoster = db.StoredPath(poster)
			}
			if backdrop != "" {
				storedBackdrop = db.StoredPath(backdrop)
			}
			meta := fileMeta{}
			if b, err := os.ReadFile(filepath.Join(folder, "metadata.json")); err == nil {
				_ = json.Unmarshal(b, &meta)
			}
			if meta.Title != "" {
				title = meta.Title
			}
			if meta.Year != nil {
				year = meta.Year
			}
			genres := meta.Genres
			if genres == nil {
				genres = []string{"Drama"}
			}
			maturity := meta.Maturity
			if maturity == "" {
				maturity = "U/A 13+"
			}
			var existing map[string]any
			if r, err := db.QueryOne(`SELECT * FROM content WHERE video_path = ?`, storedVideo); err == nil {
				existing = r
			} else if r, err := db.QueryOne(`SELECT * FROM content WHERE video_path = ?`, videoPath); err == nil {
				// legacy absolute row not yet normalised
				existing = r
			} else {
				// same folder = same movie (compare resolved dirs so
				// moves/renames of the project root still match)
				rows, _ := db.QueryMap(`SELECT * FROM content WHERE type='movie'`)
				for _, r := range rows {
					if vp := strVal(r["video_path"]); vp != "" && resolveDir(vp) == folder {
						existing = r
						break
					}
				}
			}
			if existing != nil {
				id := existing["id"]
				keep := storedVideo
				if vp := strVal(existing["video_path"]); vp != "" {
					if db.ExistsLocal(vp) {
						keep = vp
						// normalise legacy absolute -> relative while we're here
						if norm := db.StoredPath(db.ResolvePath(vp)); norm != vp {
							keep = norm
						}
					}
				}
				// Never wipe art the folder doesn't have: IMDb downloads,
				// poster uploads and manual picks live outside media/ and
				// would otherwise vanish on every restart/rescan.
				keepPoster, keepBackdrop := storedPoster, storedBackdrop
				if keepPoster == "" {
					keepPoster = strVal(existing["poster_path"])
				}
				if keepBackdrop == "" {
					keepBackdrop = strVal(existing["backdrop_path"])
				}
				var y any
				if year != nil {
					y = int64(*year)
				}
				db.Conn.Exec(`UPDATE content SET title=?, year=?, poster_path=?, backdrop_path=?, video_path=? WHERE id=?`, title, y, keepPoster, keepBackdrop, keep, id)
				res.Updated++
			} else {
				var y any
				if year != nil {
					y = int64(*year)
				}
				db.Conn.Exec(`INSERT INTO content (type,title,description,year,genres,cast,director,maturity_rating,duration_sec,poster_path,backdrop_path,video_path)
					VALUES ('movie',?,?,?,?,?,?,?,?,?,?,?)`,
					title, meta.Description, y, mustJSON(genres), mustJSON(meta.Cast), meta.Director, maturity, int64(meta.DurationSec), storedPoster, storedBackdrop, storedVideo)
				res.Added++
			}
		}
		// loose files directly in movies/
		for _, f := range filesInDir(moviesDir, videoExts) {
			stored := db.StoredPath(f)
			base := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			title, year := ParseMovieFolder(base)
			if _, err := db.QueryOne(`SELECT * FROM content WHERE video_path = ?`, stored); err == nil {
				continue
			}
			if _, err := db.QueryOne(`SELECT * FROM content WHERE video_path = ?`, f); err == nil {
				// legacy absolute exists — normalise it
				db.Conn.Exec(`UPDATE content SET video_path=? WHERE video_path=?`, stored, f)
				res.Updated++
				continue
			}
			var y any
			if year != nil {
				y = int64(*year)
			}
			db.Conn.Exec(`INSERT INTO content (type,title,year,genres,video_path) VALUES ('movie',?,?,?,?)`, title, y, `["Drama"]`, stored)
			res.Added++
		}
	}

	// ---- series ----
	if shows, err := os.ReadDir(seriesDir); err == nil {
		for _, sh := range shows {
			if !sh.IsDir() {
				continue
			}
			showDir := filepath.Join(seriesDir, sh.Name())
			var files []string
			ents, _ := os.ReadDir(showDir)
			for _, e := range ents {
				p := filepath.Join(showDir, e.Name())
				if e.IsDir() {
					sub, _ := os.ReadDir(p)
					for _, f := range sub {
						if !f.IsDir() && videoExts[strings.ToLower(filepath.Ext(f.Name()))] {
							files = append(files, filepath.Join(p, f.Name()))
						}
					}
				} else if videoExts[strings.ToLower(filepath.Ext(e.Name()))] {
					files = append(files, p)
				}
			}
			if len(files) == 0 {
				continue
			}
			imgs := filesInDir(showDir, imgExts)
			poster := pickPoster(imgs, "poster")
			if poster == "" {
				poster = pickPoster(imgs, "cover")
			}
			if poster == "" && len(imgs) > 0 {
				poster = imgs[0]
			}
			backdrop := pickPoster(imgs, "backdrop")
			if backdrop == "" && len(imgs) > 1 {
				backdrop = imgs[1]
			}
			storedPoster, storedBackdrop := "", ""
			if poster != "" {
				storedPoster = db.StoredPath(poster)
			}
			if backdrop != "" {
				storedBackdrop = db.StoredPath(backdrop)
			}
			showRow, err := db.QueryOne(`SELECT * FROM content WHERE type='series' AND title=?`, sh.Name())
			if err != nil {
				_, _ = db.Conn.Exec(`INSERT INTO content (type,title,description,year,genres,poster_path,backdrop_path) VALUES ('series',?,?,NULL,?,?,?)`, sh.Name(), "", `["Drama"]`, storedPoster, storedBackdrop)
				showRow, _ = db.QueryOne(`SELECT * FROM content WHERE type='series' AND title=?`, sh.Name())
				res.Added++
			} else {
				// Same rule as movies: keep existing art when the folder
				// has none, so IMDb/poster-upload art survives restarts.
				keepPoster, keepBackdrop := storedPoster, storedBackdrop
				if keepPoster == "" {
					keepPoster = strVal(showRow["poster_path"])
				}
				if keepBackdrop == "" {
					keepBackdrop = strVal(showRow["backdrop_path"])
				}
				db.Conn.Exec(`UPDATE content SET poster_path=?, backdrop_path=? WHERE id=?`, keepPoster, keepBackdrop, showRow["id"])
			}
			sort.Strings(files)
			epNum := 1
			for _, fp := range files {
				stored := db.StoredPath(fp)
				parsed := ParseEpisodeFile(filepath.Base(fp))
				epNo := epNum
				if parsed.Episode != nil {
					epNo = *parsed.Episode
				} else {
					epNum++
				}
				sid := showRow["id"]
				title := parsed.Title
				if title == "" {
					title = "Episode " + strconv.Itoa(epNo)
				}
				// 1) Same file already linked (by stored or legacy absolute
				// path)? The row is authoritative for S/E/title (it may
				// carry user-set bulk-upload values) — only normalise a
				// legacy absolute path, never clobber metadata from the
				// filename.
				var byPath map[string]any
				if r, err := db.QueryOne(`SELECT * FROM episodes WHERE content_id=? AND video_path=?`, sid, stored); err == nil {
					byPath = r
				} else if r, err := db.QueryOne(`SELECT * FROM episodes WHERE content_id=? AND video_path=?`, sid, fp); err == nil {
					byPath = r
				} else if r, err := db.QueryOne(`SELECT * FROM episodes WHERE video_path=?`, stored); err == nil {
					// File moved across shows: adopt it if its S/E slot is free.
					if int64Val(r["content_id"]) == int64Val(sid) {
						byPath = r
					} else if db.Count(`SELECT COUNT(*) FROM episodes WHERE content_id=? AND season=? AND episode=?`, sid, int64(parsed.Season), int64(epNo)) == 0 {
						db.Conn.Exec(`UPDATE episodes SET content_id=?, season=?, episode=?, video_path=? WHERE id=?`, sid, int64(parsed.Season), int64(epNo), stored, r["id"])
						res.Updated++
						continue
					}
				}
				if byPath != nil {
					if strVal(byPath["video_path"]) != stored {
						db.Conn.Exec(`UPDATE episodes SET video_path=? WHERE id=?`, stored, byPath["id"])
						res.Updated++
					}
					continue
				}
				// 2) Same S/E slot exists but points at a different file?
				// Repoint only if the slot's current file is gone (rename /
				// replacement). If the current file is still present, leave
				// both alone — silently stealing the slot would orphan the
				// existing episode and flip-flop on every scan.
				if cur, err := db.QueryOne(`SELECT * FROM episodes WHERE content_id=? AND season=? AND episode=?`, sid, int64(parsed.Season), int64(epNo)); err != nil {
					db.Conn.Exec(`INSERT INTO episodes (content_id,season,episode,title,video_path) VALUES (?,?,?,?,?)`, sid, int64(parsed.Season), int64(epNo), title, stored)
					res.Added++
				} else if curStored := strVal(cur["video_path"]); curStored != stored && !db.ExistsLocal(curStored) {
					db.Conn.Exec(`UPDATE episodes SET video_path=? WHERE content_id=? AND season=? AND episode=?`, stored, sid, int64(parsed.Season), int64(epNo))
					res.Updated++
				}
			}
		}
	}
	return res
}

func int64Val(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
