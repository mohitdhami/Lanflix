package api

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
			stored := db.StoredPath(dest)
			if field == "poster" {
				db.Conn.Exec(`UPDATE content SET poster_path=? WHERE id=?`, stored, cid)
			} else {
				db.Conn.Exec(`UPDATE content SET backdrop_path=? WHERE id=?`, stored, cid)
			}
		}
		f.Close()
	}
	J(w, 200, map[string]any{"ok": true})
}

// epMetaIn carries optional per-file episode metadata for bulk series
// uploads. The frontend's bulk staging UI sends one entry per dropped file
// (matched by its drop-relative path); Season/Episode nil or <=0 means
// "auto" (parse from filename, else next free number).
type epMetaIn struct {
	Path    string `json:"path"`
	Season  *int64 `json:"season"`
	Episode *int64 `json:"episode"`
	Title   string `json:"title"`
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
	var epMetas []epMetaIn
	if raw := r.FormValue("episodes"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &epMetas)
	}
	metaByRel := map[string]epMetaIn{}
	for _, m := range epMetas {
		if m.Path == "" {
			continue
		}
		metaByRel[safeRel(m.Path)] = m
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

	// Validate + resolve every part first (no disk writes yet).
	type upItem struct {
		hdr  *multipart.FileHeader
		rel  string
		ext  string
		dest string
	}
	var items []upItem
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
		items = append(items, upItem{hdr: hdr, rel: rel, ext: ext, dest: dest})
	}

	// saveItem writes one validated part to disk (counts + tracks videos).
	saveItem := func(it upItem) bool {
		src, err := it.hdr.Open()
		if err != nil {
			skipped = append(skipped, it.rel)
			return false
		}
		defer src.Close()
		if err := saveUploaded(src, it.dest); err != nil {
			skipped = append(skipped, it.rel)
			return false
		}
		uploaded++
		if videoExts[it.ext] {
			savedVideos = append(savedVideos, struct{ rel, abs string }{it.rel, it.dest})
		}
		return true
	}
	episodesAdded := 0
	var linkedVideo any
	var details []map[string]any
	if contentRow != nil && str(contentRow["type"]) == "series" {
		cid := intVal(contentRow["id"])
		// S/E slots already taken. A slot whose file is gone counts as
		// free (its orphaned row gets repointed rather than skipped).
		used := map[string]bool{}
		slotRow := map[string]map[string]any{}
		if rows, _ := db.QueryMap(`SELECT id, season, episode, video_path FROM episodes WHERE content_id=?`, cid); rows != nil {
			for _, rr := range rows {
				key := fmt.Sprintf("%d:%d", intVal(rr["season"]), intVal(rr["episode"]))
				vp, _ := rr["video_path"].(string)
				if vp == "" || db.ExistsLocal(vp) {
					used[key] = true
				}
				slotRow[key] = rr
			}
		}
		nextAuto := map[int64]int64{}
		if rows, _ := db.QueryMap(`SELECT season, COALESCE(MAX(episode),0) AS m FROM episodes WHERE content_id=? GROUP BY season`, cid); rows != nil {
			for _, rr := range rows {
				nextAuto[intVal(rr["season"])] = intVal(rr["m"]) + 1
			}
		}
		// Plan every video BEFORE touching disk: explicit S/E first
		// (sorted), then autos — so a mixed bulk drop ("S02E01.mp4" +
		// "Finale.mp4" with E blank) never lets an auto pick collide with
		// an explicit slot, and colliding files never land in the show
		// folder (the next scan would otherwise adopt them into wrong
		// slots).
		type pendingEp struct {
			it            upItem
			stored, title string
			s, e          int64
			isAuto        bool
		}
		var pend []pendingEp
		seenDest := map[string]bool{}
		for _, it := range items {
			if !videoExts[it.ext] {
				continue
			}
			if seenDest[it.dest] {
				skipped = append(skipped, it.rel)
				details = append(details, map[string]any{"file": it.rel, "status": "skipped", "reason": "duplicate file in this upload"})
				continue
			}
			seenDest[it.dest] = true
			stored := db.StoredPath(it.dest)
			parsed := scanner.ParseEpisodeFile(filepath.Base(it.rel))
			s := int64(parsed.Season)
			var e int64
			if parsed.Episode != nil {
				e = int64(*parsed.Episode)
			}
			title := parsed.Title
			explicitEp := e > 0
			if m, ok := metaByRel[it.rel]; ok {
				if m.Season != nil && *m.Season > 0 {
					s = *m.Season
				}
				if m.Episode != nil && *m.Episode > 0 {
					e = *m.Episode
					explicitEp = true
				} else {
					e = 0
					explicitEp = false
				}
				if strings.TrimSpace(m.Title) != "" {
					title = strings.TrimSpace(m.Title)
				}
			}
			if s <= 0 {
				s = 1
			}
			if title == "" && explicitEp {
				title = "Episode " + itoa64(int(e))
			}
			pend = append(pend, pendingEp{it: it, stored: stored, title: title, s: s, e: e, isAuto: !explicitEp})
		}
		sort.Slice(pend, func(i, j int) bool {
			if pend[i].isAuto != pend[j].isAuto {
				return !pend[i].isAuto // explicit first
			}
			if pend[i].s != pend[j].s {
				return pend[i].s < pend[j].s
			}
			if pend[i].e != pend[j].e {
				return pend[i].e < pend[j].e
			}
			return pend[i].it.rel < pend[j].it.rel
		})
		for _, p := range pend {
			s, e, title := p.s, p.e, p.title
			if p.isAuto {
				// next free episode number in this season
				cand := nextAuto[s]
				if cand <= 0 {
					cand = 1
				}
				for used[fmt.Sprintf("%d:%d", s, cand)] {
					cand++
				}
				e = cand
				if title == "" {
					title = "Episode " + itoa64(int(e))
				}
			} else if db.Count(`SELECT COUNT(*) FROM episodes WHERE content_id=? AND video_path=?`, cid, p.stored) > 0 {
				// Same file again: refresh it on disk, no new row.
				if saveItem(p.it) {
					details = append(details, map[string]any{"file": p.it.rel, "status": "skipped", "reason": "already linked — file refreshed", "season": s, "episode": e, "title": title})
				} else {
					details = append(details, map[string]any{"file": p.it.rel, "status": "error", "reason": "could not save file"})
				}
				continue
			}
			key := fmt.Sprintf("%d:%d", s, e)
			if used[key] {
				// Slot taken by a present file: keep the folder clean by
				// NOT saving the colliding file.
				skipped = append(skipped, p.it.rel)
				details = append(details, map[string]any{
					"file": p.it.rel, "status": "skipped",
					"reason": fmt.Sprintf("S%d E%d already exists", s, e),
					"season": s, "episode": e, "title": title,
				})
				continue
			}
			if !saveItem(p.it) {
				details = append(details, map[string]any{"file": p.it.rel, "status": "error", "reason": "could not save file"})
				continue
			}
			if prev, ok := slotRow[key]; ok {
				// Orphaned slot (its file is gone): repoint, don't insert.
				db.Conn.Exec(`UPDATE episodes SET video_path=?, title=? WHERE id=?`, p.stored, title, prev["id"])
			} else {
				db.Conn.Exec(`INSERT INTO episodes (content_id,season,episode,title,video_path) VALUES (?,?,?,?,?)`, cid, s, e, title, p.stored)
			}
			used[key] = true
			if nextAuto[s] <= e {
				nextAuto[s] = e + 1
			}
			episodesAdded++
			details = append(details, map[string]any{
				"file": p.it.rel, "status": "added",
				"season": s, "episode": e, "title": title,
			})
		}
		// Non-video sidecars (subs, posters, metadata) save as before.
		for _, it := range items {
			if videoExts[it.ext] {
				continue
			}
			saveItem(it)
		}
	} else {
		for _, it := range items {
			saveItem(it)
		}
		if contentRow != nil && len(savedVideos) > 0 {
			attach := r.FormValue("attach")
			if attach == "replace" || (str(contentRow["video_path"]) == "" && str(contentRow["remote_url"]) == "") {
				stored := db.StoredPath(savedVideos[0].abs)
				db.Conn.Exec(`UPDATE content SET video_path=?, remote_url=? WHERE id=?`, stored, "", intVal(contentRow["id"]))
				linkedVideo = stored
			}
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
	if details == nil {
		details = []map[string]any{}
	}
	J(w, 200, map[string]any{"ok": true, "uploaded": uploaded, "skipped": skipped,
		"episodes_added": episodesAdded, "linked_video": linkedVideo, "files": filesOut,
		"details": details,
		"added": res.Added, "updated": res.Updated, "healed": res.Healed})
}

func titleFolder(isSeries bool, c map[string]any) string {
	if isSeries {
		if ep, err := db.QueryOne(`SELECT video_path FROM episodes WHERE content_id=? AND video_path != '' LIMIT 1`, intVal(c["id"])); err == nil {
			if vp := str(ep["video_path"]); vp != "" {
				return filepath.Dir(db.ResolvePath(vp))
			}
		}
		name := safeRel(str(c["title"]))
		if name == "" {
			name = "title-" + itoa64(int(intVal(c["id"])))
		}
		return filepath.Join(scanner.SeriesDir(), name)
	}
	if vp := str(c["video_path"]); vp != "" {
		abs := db.ResolvePath(vp)
		if abs != "" {
			return filepath.Dir(abs)
		}
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
		posterPath = db.StoredPath(p)
	}
	if b, err := imdb.DownloadArt(db.PublicDir(), tt, "backdrop", info.BackdropURL); err == nil {
		backdropPath = db.StoredPath(b)
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
	posterPath = db.StoredPath(posterPath)
	backdropPath = db.StoredPath(backdropPath)
	var year any
	if info.Year != nil {
		year = int64(*info.Year)
	}
	feat := 0
	if bv, ok := b["is_featured"].(bool); ok && bv {
		feat = 1
	}
	if feat == 1 && featuredFull(0) {
		E(w, 400, "Featured is full (20/20) — unfeature a title first")
		return
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
