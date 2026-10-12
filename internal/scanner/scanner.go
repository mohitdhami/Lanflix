package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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
	if m := epRe1.FindStringSubmatch(base); m != nil {
		si, _ := strconv.Atoi(m[1])
		ei, _ := strconv.Atoi(m[2])
		s, e = &si, &ei
	} else if m := epRe2.FindStringSubmatch(base); m != nil {
		si, _ := strconv.Atoi(m[1])
		ei, _ := strconv.Atoi(m[2])
		s, e = &si, &ei
	}
	t := base
	if s != nil && e != nil {
		// strip the matched token for a cleaner title
		token := epRe1.FindString(base)
		if token == "" {
			token = epRe2.FindString(base)
		}
		t = strings.Trim(strings.Replace(base, token, "", 1), "-_. ")
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
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Year          *int     `json:"year"`
	Genres        []string `json:"genres"`
	Cast          []string `json:"cast"`
	Director      string   `json:"director"`
	Maturity      string   `json:"maturity_rating"`
	DurationSec   int      `json:"duration_sec"`
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
}

func ScanMedia() Result {
	res := Result{}
	moviesDir, seriesDir := MoviesDir(), SeriesDir()
	os.MkdirAll(moviesDir, 0o755)
	os.MkdirAll(seriesDir, 0o755)

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
			if r, err := db.QueryOne(`SELECT * FROM content WHERE video_path = ?`, videoPath); err == nil {
				existing = r
			} else {
				// same folder = same movie
				rows, _ := db.QueryMap(`SELECT * FROM content WHERE type='movie'`)
				for _, r := range rows {
					if vp, _ := r["video_path"].(string); vp != "" && filepath.Dir(vp) == folder {
						existing = r
						break
					}
				}
			}
			if existing != nil {
				id := existing["id"]
				keep := videoPath
				if vp, _ := existing["video_path"].(string); vp != "" {
					if _, err := os.Stat(vp); err == nil {
						keep = vp
					}
				}
				var y any
				if year != nil {
					y = int64(*year)
				}
				db.Conn.Exec(`UPDATE content SET title=?, year=?, poster_path=?, backdrop_path=?, video_path=? WHERE id=?`, title, y, poster, backdrop, keep, id)
				res.Updated++
			} else {
				var y any
				if year != nil {
					y = int64(*year)
				}
				db.Conn.Exec(`INSERT INTO content (type,title,description,year,genres,cast,director,maturity_rating,duration_sec,poster_path,backdrop_path,video_path)
					VALUES ('movie',?,?,?,?,?,?,?,?,?,?,?)`,
					title, meta.Description, y, mustJSON(genres), mustJSON(meta.Cast), meta.Director, maturity, int64(meta.DurationSec), poster, backdrop, videoPath)
				res.Added++
			}
		}
		// loose files directly in movies/
		for _, f := range filesInDir(moviesDir, videoExts) {
			base := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			title, year := ParseMovieFolder(base)
			if _, err := db.QueryOne(`SELECT * FROM content WHERE video_path = ?`, f); err != nil {
				var y any
				if year != nil {
					y = int64(*year)
				}
				db.Conn.Exec(`INSERT INTO content (type,title,year,genres,video_path) VALUES ('movie',?,?,?,?)`, title, y, `["Drama"]`, f)
				res.Added++
			}
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
			showRow, err := db.QueryOne(`SELECT * FROM content WHERE type='series' AND title=?`, sh.Name())
			if err != nil {
				_, _ = db.Conn.Exec(`INSERT INTO content (type,title,description,year,genres,poster_path,backdrop_path) VALUES ('series',?,?,NULL,?,?,?)`, sh.Name(), "", `["Drama"]`, poster, backdrop)
				showRow, _ = db.QueryOne(`SELECT * FROM content WHERE type='series' AND title=?`, sh.Name())
				res.Added++
			} else {
				db.Conn.Exec(`UPDATE content SET poster_path=?, backdrop_path=? WHERE id=?`, poster, backdrop, showRow["id"])
			}
			// sort for stable numbering
			for i := 0; i < len(files); i++ {
				for j := i + 1; j < len(files); j++ {
					if files[j] < files[i] {
						files[i], files[j] = files[j], files[i]
					}
				}
			}
			epNum := 1
			for _, fp := range files {
				parsed := ParseEpisodeFile(filepath.Base(fp))
				epNo := epNum
				if parsed.Episode != nil {
					epNo = *parsed.Episode
				} else {
					epNum++
				}
				sid := showRow["id"]
				if _, err := db.QueryOne(`SELECT * FROM episodes WHERE content_id=? AND season=? AND episode=?`, sid, int64(parsed.Season), int64(epNo)); err != nil {
					if _, err := db.QueryOne(`SELECT * FROM episodes WHERE video_path=?`, fp); err != nil {
						title := parsed.Title
						if title == "" {
							title = "Episode " + strconv.Itoa(epNo)
						}
						db.Conn.Exec(`INSERT INTO episodes (content_id,season,episode,title,video_path) VALUES (?,?,?,?,?)`, sid, int64(parsed.Season), int64(epNo), title, fp)
						res.Added++
					}
				} else {
					// keep row, refresh path if renamed
					db.Conn.Exec(`UPDATE episodes SET video_path=? WHERE content_id=? AND season=? AND episode=? AND video_path != ?`, fp, sid, int64(parsed.Season), int64(epNo), fp)
				}
			}
		}
	}
	return res
}
