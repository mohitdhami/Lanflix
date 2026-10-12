package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"lanflix/internal/db"
	"lanflix/internal/imdb"
	"lanflix/internal/scanner"
)

var uploadExts = map[string]bool{
	".mp4": true, ".webm": true, ".mkv": true, ".mov": true, ".avi": true, ".m4v": true,
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".vtt": true, ".srt": true, ".json": true,
}

var videoExts = map[string]bool{".mp4": true, ".webm": true, ".mkv": true, ".mov": true, ".avi": true, ".m4v": true}

func safeRel(p string) string {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
	var clean []string
	for _, s := range parts {
		if s == "" || s == "." || s == ".." {
			continue
		}
		s = strings.TrimLeft(s, ".")
		var b strings.Builder
		for _, r := range s {
			if r == '<' || r == '>' || r == ':' || r == '"' || r == '|' || r == '?' || r == '*' || r < 0x20 {
				b.WriteRune('_')
			} else {
				b.WriteRune(r)
			}
		}
		s = strings.TrimSpace(b.String())
		if s != "" {
			clean = append(clean, s)
		}
	}
	return strings.Join(clean, "/")
}

func saveUploaded(src io.Reader, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dest); err == nil {
		_ = os.Remove(dest)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, src)
	return err
}

func postAdminPoster(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	cid := pathID(r)
	if db.Count(`SELECT COUNT(*) FROM content WHERE id=?`, cid) == 0 {
		E(w, 404, "Not found")
		return
	}
	if err := r.ParseMultipartForm(200 << 20); err != nil {
		E(w, 400, "Could not parse upload")
		return
	}
	dir := filepath.Join(db.PublicDir(), "posters")
	_ = os.MkdirAll(dir, 0o755)
	for _, field := range []string{"poster", "backdrop"} {
		f, hdr, err := r.FormFile(field)
		if err != nil {
			continue
		}
		ext := strings.ToLower(filepath.Ext(hdr.Filename))
		if ext == "" {
			ext = ".jpg"
		}
		dest := filepath.Join(dir, "c"+itoa64(int(cid))+"-"+field+ext)
		if err := saveUploaded(f, dest); err == nil {
			if field == "poster" {
				db.Conn.Exec(`UPDATE content SET poster_path=? WHERE id=?`, dest, cid)
			} else {
				db.Conn.Exec(`UPDATE content SET backdrop_path=? WHERE id=?`, dest, cid)
			}
		}
		f.Close()
	}
	J(w, 200, map[string]any{"ok": true})
}

func postAdminUpload(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		E(w, 400, "Could not parse upload")
		return
	}
	var paths []string
	if raw := r.FormValue("paths"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &paths)
	}
	// Attach-to-title mode?
	var contentRow map[string]any
	if cid := num(r.FormValue("content_id"), 0); cid != 0 {
		c, err := db.QueryOne(`SELECT * FROM content WHERE id=?`, cid)
		if err != nil {
			E(w, 404, "Title not found")
			return
		}
		contentRow = c
	}
	var base string
	if contentRow != nil && str(contentRow["type"]) == "series" {
		base = titleFolder(true, contentRow)
	} else if contentRow != nil {
		base = titleFolder(false, contentRow)
	} else if r.FormValue("target") == "series" {
		base = scanner.SeriesDir()
	} else {
		base = scanner.MoviesDir()
	}
	base, _ = filepath.Abs(base)
	uploaded := 0
	skipped := []string{}
	var savedVideos []struct{ rel, abs string }
	files := r.MultipartForm.File["files"]
	for i, hdr := range files {
		rel := ""
		if i < len(paths) {
			rel = safeRel(paths[i])
		}
		if rel == "" {
			rel = safeRel(hdr.Filename)
		}
		ext := strings.ToLower(filepath.Ext(rel))
		if rel == "" || !uploadExts[ext] {
			name := hdr.Filename
			if i < len(paths) && paths[i] != "" {
				name = paths[i]
			}
			skipped = append(skipped, name)
			continue
		}
		dest, _ := filepath.Abs(filepath.Join(base, rel))
		if dest != base && !strings.HasPrefix(dest, base+string(os.PathSeparator)) {
			skipped = append(skipped, rel)
			continue
		}
		src, err := hdr.Open()
		if err != nil {
			skipped = append(skipped, rel)
			continue
		}
		if err := saveUploaded(src, dest); err != nil {
			skipped = append(skipped, rel)
			src.Close()
			continue
		}
		src.Close()
		uploaded++
		if videoExts[ext] {
			savedVideos = append(savedVideos, struct{ rel, abs string }{rel, dest})
		}
	}
	episodesAdded := 0
	var linkedVideo any
	if contentRow != nil && str(contentRow["type"]) == "series" {
		cid := intVal(contentRow["id"])
		for _, v := range savedVideos {
			if db.Count(`SELECT COUNT(*) FROM episodes WHERE content_id=? AND video_path=?`, cid, v.abs) > 0 {
				continue
			}
			parsed := scanner.ParseEpisodeFile(filepath.Base(v.rel))
			s := int64(parsed.Season)
			var e int64
			if parsed.Episode != nil {
				e = int64(*parsed.Episode)
			} else {
				e = int64(db.Count(`SELECT COALESCE(MAX(episode),0) FROM episodes WHERE content_id=? AND season=?`, cid, s)) + 1
			}
			if db.Count(`SELECT COUNT(*) FROM episodes WHERE content_id=? AND season=? AND episode=?`, cid, s, e) > 0 {
				continue
			}
			title := parsed.Title
			if title == "" {
				title = "Episode " + itoa64(int(e))
			}
			db.Conn.Exec(`INSERT INTO episodes (content_id,season,episode,title,video_path) VALUES (?,?,?,?,?)`, cid, s, e, title, v.abs)
			episodesAdded++
		}
	} else if contentRow != nil && len(savedVideos) > 0 {
		attach := r.FormValue("attach")
		if attach == "replace" || (str(contentRow["video_path"]) == "" && str(contentRow["remote_url"]) == "") {
			db.Conn.Exec(`UPDATE content SET video_path=?, remote_url=? WHERE id=?`, savedVideos[0].abs, "", intVal(contentRow["id"]))
			linkedVideo = savedVideos[0].abs
		}
	}
	res := scanner.ScanMedia()
	filesOut := []string{}
	for _, v := range savedVideos {
		filesOut = append(filesOut, v.abs)
	}
	if skipped == nil {
		skipped = []string{}
	}
	J(w, 200, map[string]any{"ok": true, "uploaded": uploaded, "skipped": skipped,
		"episodes_added": episodesAdded, "linked_video": linkedVideo, "files": filesOut,
		"added": res.Added, "updated": res.Updated})
}

func titleFolder(isSeries bool, c map[string]any) string {
	if isSeries {
		if ep, err := db.QueryOne(`SELECT video_path FROM episodes WHERE content_id=? AND video_path != '' LIMIT 1`, intVal(c["id"])); err == nil {
			if vp := str(ep["video_path"]); vp != "" {
				return filepath.Dir(vp)
			}
		}
		name := safeRel(str(c["title"]))
		if name == "" {
			name = "title-" + itoa64(int(intVal(c["id"])))
		}
		return filepath.Join(scanner.SeriesDir(), name)
	}
	if vp := str(c["video_path"]); vp != "" {
		return filepath.Dir(vp)
	}
	name := safeRel(str(c["title"]))
	if name == "" {
		name = "title-" + itoa64(int(intVal(c["id"])))
	}
	return filepath.Join(scanner.MoviesDir(), name)
}

// ---------- IMDb ----------

func postAdminImdbFetch(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	tt, ok := imdb.ExtractID(str(b["input"]) + " " + str(b["url"]) + " " + str(b["id"]))
	if !ok {
		E(w, 400, "Give an IMDb link or id like tt0133093")
		return
	}
	info, err := imdb.FetchMeta(tt)
	if err != nil {
		E(w, 502, err.Error())
		return
	}
	posterPath, backdropPath := "", ""
	if p, err := imdb.DownloadArt(db.PublicDir(), tt, "poster", info.PosterURL); err == nil {
		posterPath = p
	}
	if b, err := imdb.DownloadArt(db.PublicDir(), tt, "backdrop", info.BackdropURL); err == nil {
		backdropPath = b
	}
	J(w, 200, map[string]any{
		"imdb_id": info.ID, "type": info.Type, "title": info.Title, "year": info.Year,
		"description": info.Description, "genres": info.Genres, "cast": info.Cast,
		"director": info.Director, "maturity_rating": info.Maturity, "duration_sec": info.DurationSec,
		"poster_path": posterPath, "backdrop_path": backdropPath,
		"poster_url": fileURL(posterPath), "backdrop_url": fileURL(backdropPath),
		"imdb_rating": info.ImdbRating,
	})
}

func postAdminImdbImport(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	tt, ok := imdb.ExtractID(str(b["input"]) + " " + str(b["url"]) + " " + str(b["id"]))
	if !ok {
		E(w, 400, "Give an IMDb link or id like tt0133093")
		return
	}
	if ex, err := db.QueryOne(`SELECT id FROM content WHERE imdb_id=? AND imdb_id != ''`, tt); err == nil {
		J(w, 200, map[string]any{"ok": true, "id": intVal(ex["id"]), "existed": true})
		return
	}
	info, err := imdb.FetchMeta(tt)
	if err != nil {
		E(w, 502, err.Error())
		return
	}
	if ex, err := db.QueryOne(`SELECT id FROM content WHERE type=? AND title=?`, info.Type, info.Title); err == nil {
		db.Conn.Exec(`UPDATE content SET imdb_id=? WHERE id=?`, tt, intVal(ex["id"]))
		J(w, 200, map[string]any{"ok": true, "id": intVal(ex["id"]), "existed": true})
		return
	}
	posterPath, _ := imdb.DownloadArt(db.PublicDir(), tt, "poster", info.PosterURL)
	backdropPath, _ := imdb.DownloadArt(db.PublicDir(), tt, "backdrop", info.BackdropURL)
	var year any
	if info.Year != nil {
		year = int64(*info.Year)
	}
	feat := 0
	if bv, ok := b["is_featured"].(bool); ok && bv {
		feat = 1
	}
	gb, _ := json.Marshal(info.Genres)
	cb, _ := json.Marshal(info.Cast)
	res, err := db.Conn.Exec(`INSERT INTO content (type,title,description,year,genres,cast,director,maturity_rating,duration_sec,poster_path,backdrop_path,remote_url,is_featured,imdb_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		info.Type, info.Title, info.Description, year, gb, cb, info.Director, info.Maturity,
		int64(info.DurationSec), posterPath, backdropPath, "", feat, tt)
	if err != nil {
		E(w, 400, "Could not import title")
		return
	}
	id, _ := res.LastInsertId()
	J(w, 200, map[string]any{"ok": true, "id": id, "existed": false})
}
