package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"lanflix/internal/db"
)

func getServerInfo(w http.ResponseWriter, r *http.Request) {
	J(w, 200, map[string]any{
		"ips":    lanIPs(),
		"port":   serverPort(),
		"movies": db.Count(`SELECT COUNT(*) FROM content WHERE type='movie'`),
		"series": db.Count(`SELECT COUNT(*) FROM content WHERE type='series'`),
		"name":   "LanFlix",
	})
}

func getFile(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		E(w, 400, "path required")
		return
	}
	if strings.HasPrefix(p, "http") {
		http.Redirect(w, r, p, http.StatusFound)
		return
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		E(w, 400, "bad path")
		return
	}
	allowed := []string{db.MediaRoot(), db.PublicDir(), os.TempDir()}
	ok := false
	for _, a := range allowed {
		aa, _ := filepath.Abs(a)
		if abs == aa || strings.HasPrefix(abs, aa+string(os.PathSeparator)) {
			ok = true
			break
		}
	}
	if !ok {
		E(w, 403, "Forbidden")
		return
	}
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		E(w, 404, "Not found")
		return
	}
	http.ServeFile(w, r, abs)
}

func getGenres(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.QueryMap(`SELECT genres FROM content`)
	set := map[string]bool{}
	for _, row := range rows {
		for _, g := range parseStrArr(str(row["genres"])) {
			set[g] = true
		}
	}
	out := []string{}
	for g := range set {
		out = append(out, g)
	}
	// sort
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if out == nil {
		out = []string{}
	}
	J(w, 200, map[string]any{"genres": out})
}

func getContentList(w http.ResponseWriter, r *http.Request) {
	pid, ok := optionalProfile(w, r, nil)
	if !ok {
		return
	}
	where := "1=1" + kidsClause(pid)
	var args []any
	if s := qStr(r, "search"); s != "" {
		where += " AND (title LIKE ? OR description LIKE ? OR director LIKE ?)"
		args = append(args, "%"+s+"%", "%"+s+"%", "%"+s+"%")
	}
	if g := qStr(r, "genre"); g != "" {
		where += " AND genres LIKE ?"
		args = append(args, "%"+g+"%")
	}
	if t := qStr(r, "type"); t != "" {
		where += " AND type=?"
		args = append(args, t)
	}
	order := "added_at DESC"
	switch qStr(r, "sort") {
	case "trending":
		order = "views_count DESC"
	case "year":
		order = "year DESC"
	case "title":
		order = "title ASC"
	}
	total := db.Count(`SELECT COUNT(*) FROM content WHERE `+where, args...)
	lim := qInt(r, "limit", 48)
	off := qInt(r, "offset", 0)
	rows, _ := db.QueryMap(`SELECT * FROM content WHERE `+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, lim, off)...)
	items := []map[string]any{}
	for _, c := range rows {
		items = append(items, rowContent(c))
	}
	J(w, 200, map[string]any{"total": total, "items": items})
}

func contentQuery(kf string, extra, order string, lim int) []map[string]any {
	rows, _ := db.QueryMap(`SELECT * FROM content WHERE 1=1 `+kf+` `+extra+` ORDER BY `+order+` LIMIT ?`, lim)
	out := []map[string]any{}
	for _, c := range rows {
		out = append(out, rowContent(c))
	}
	return out
}

func getBrowseRows(w http.ResponseWriter, r *http.Request) {
	pid, ok := optionalProfile(w, r, nil)
	if !ok {
		return
	}
	kf := kidsClause(pid)
	q := func(extra, order string, lim int) []map[string]any { return contentQuery(kf, extra, order, lim) }

	continueItems := []map[string]any{}
	if pid != 0 {
		rows, _ := db.QueryMap(`
			SELECT c.*, w.position_sec, w.duration_sec, w.episode_id, w.updated_at FROM watch_history w
			JOIN content c ON c.id = w.content_id
			WHERE w.profile_id=? AND w.completed=0 AND w.position_sec > 5
			ORDER BY w.updated_at DESC LIMIT 12`, pid)
		for _, rr := range rows {
			c := rowContent(rr)
			c["resume_at"] = intVal(rr["position_sec"])
			c["resume_of"] = intVal(rr["duration_sec"])
			c["resume_episode"] = rr["episode_id"]
			continueItems = append(continueItems, c)
		}
	}
	myList := []map[string]any{}
	if pid != 0 {
		rows, _ := db.QueryMap(`SELECT c.* FROM my_list m JOIN content c ON c.id=m.content_id WHERE m.profile_id=? ORDER BY m.added_at DESC LIMIT 24`, pid)
		for _, c := range rows {
			myList = append(myList, rowContent(c))
		}
	}
	hero := q("AND is_featured=1", "views_count DESC, added_at DESC", 5)
	if len(hero) == 0 {
		hero = q("", "views_count DESC", 5)
	}
	trending := q("", "views_count DESC", 12)
	top10 := trending
	if len(top10) > 10 {
		top10 = top10[:10]
	}
	genreRows := []map[string]any{}
	for _, g := range []string{"Drama", "Comedy", "Action", "Sci-Fi", "Thriller", "Romance", "Animation"} {
		rows, _ := db.QueryMap(`SELECT * FROM content WHERE genres LIKE ? `+kf+` ORDER BY views_count DESC LIMIT 10`, "%"+g+"%")
		if len(rows) >= 3 {
			items := []map[string]any{}
			for _, c := range rows {
				items = append(items, rowContent(c))
			}
			genreRows = append(genreRows, map[string]any{"title": g, "genre": g, "items": items})
		}
	}
	J(w, 200, map[string]any{
		"hero": hero, "continue": continueItems, "myList": myList,
		"trending": trending, "top10": top10,
		"fresh": q("", "added_at DESC", 12),
		"movies": q("AND type='movie'", "views_count DESC", 12),
		"series": q("AND type='series'", "views_count DESC", 12),
		"genreRows": genreRows,
	})
}

func getContentDetail(w http.ResponseWriter, r *http.Request) {
	pid, ok := optionalProfile(w, r, nil)
	if !ok {
		return
	}
	c, good := getContent(w, pathID(r))
	if !good {
		return
	}
	out := rowContent(c)
	eps, _ := db.QueryMap(`SELECT * FROM episodes WHERE content_id=? ORDER BY season,episode`, intVal(c["id"]))
	if eps == nil {
		eps = []map[string]any{}
	}
	if pid != 0 {
		out["in_list"] = db.Count(`SELECT COUNT(*) FROM my_list WHERE profile_id=? AND content_id=?`, pid, intVal(c["id"])) > 0
		out["my_rating"] = 0
		if rr, err := db.QueryOne(`SELECT value FROM ratings WHERE profile_id=? AND content_id=?`, pid, intVal(c["id"])); err == nil {
			out["my_rating"] = intVal(rr["value"])
		}
		wmap := map[string]map[string]any{}
		wrows, _ := db.QueryMap(`SELECT episode_id, position_sec, duration_sec, completed, updated_at FROM watch_history WHERE profile_id=? AND content_id=?`, pid, intVal(c["id"]))
		for _, wr := range wrows {
			if wr["episode_id"] == nil {
				wmap["m"] = wr
			} else {
				wmap[strconv.FormatInt(intVal(wr["episode_id"]), 10)] = wr
			}
		}
		if m, found := wmap["m"]; found {
			out["progress"] = m
		} else {
			out["progress"] = nil
		}
		for i, e := range eps {
			key := strconv.FormatInt(intVal(e["id"]), 10)
			if m, found := wmap[key]; found {
				e["watch"] = m
			} else {
				e["watch"] = nil
			}
			eps[i] = e
		}
	}
	out["episodes"] = eps
	J(w, 200, map[string]any{"item": out})
}

func getEpisodes(w http.ResponseWriter, r *http.Request) {
	eps, _ := db.QueryMap(`SELECT * FROM episodes WHERE content_id=? ORDER BY season,episode`, pathID(r))
	if eps == nil {
		eps = []map[string]any{}
	}
	J(w, 200, map[string]any{"episodes": eps})
}

func postContentView(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	p, ok := ownProfileID(w, num(b["profileId"], 0), uid, role)
	if !ok {
		return
	}
	cid := pathID(r)
	if _, good := getContent(w, cid); !good {
		return
	}
	epid := num(b["episodeId"], 0)
	if epid != 0 {
		if db.Count(`SELECT COUNT(*) FROM episodes WHERE id=?`, epid) == 0 {
			E(w, 404, "Episode not found")
			return
		}
	}
	db.Conn.Exec(`UPDATE content SET views_count = views_count + 1 WHERE id=?`, cid)
	var epidArg any
	if epid != 0 {
		epidArg = epid
	}
	db.Conn.Exec(`INSERT INTO views_log (profile_id,content_id,episode_id,event) VALUES (?,?,?,?)`, intVal(p["id"]), cid, epidArg, "play")
	J(w, 200, map[string]any{"ok": true})
}

func getMyList(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	p, ok := ownProfileQuery(w, r, uid, role)
	if !ok {
		return
	}
	rows, _ := db.QueryMap(`SELECT c.* FROM my_list m JOIN content c ON c.id=m.content_id WHERE m.profile_id=? ORDER BY m.added_at DESC`, intVal(p["id"]))
	items := []map[string]any{}
	for _, c := range rows {
		items = append(items, rowContent(c))
	}
	J(w, 200, map[string]any{"items": items})
}

func postMyList(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	p, ok := ownProfileID(w, num(b["profileId"], 0), uid, role)
	if !ok {
		return
	}
	cid := num(b["contentId"], 0)
	if cid == 0 {
		E(w, 400, "contentId required")
		return
	}
	if _, good := getContent(w, cid); !good {
		return
	}
	db.Conn.Exec(`INSERT OR IGNORE INTO my_list (profile_id,content_id) VALUES (?,?)`, intVal(p["id"]), cid)
	J(w, 200, map[string]any{"ok": true})
}

func deleteMyList(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	p, ok := ownProfileQuery(w, r, uid, role)
	if !ok {
		return
	}
	db.Conn.Exec(`DELETE FROM my_list WHERE profile_id=? AND content_id=?`, intVal(p["id"]), num(r.PathValue("contentId"), 0))
	J(w, 200, map[string]any{"ok": true})
}

func postRate(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	p, ok := ownProfileID(w, num(b["profileId"], 0), uid, role)
	if !ok {
		return
	}
	pid := intVal(p["id"])
	cid := num(b["contentId"], 0)
	v := num(b["value"], 0)
	if cid == 0 || (v != 1 && v != -1) {
		E(w, 400, "contentId, value (1|-1) required")
		return
	}
	if _, good := getContent(w, cid); !good {
		return
	}
	prev, err := db.QueryOne(`SELECT value FROM ratings WHERE profile_id=? AND content_id=?`, pid, cid)
	if err != nil {
		db.Conn.Exec(`INSERT INTO ratings (profile_id,content_id,value) VALUES (?,?,?)`, pid, cid, v)
		db.Conn.Exec(`UPDATE content SET rating_sum = rating_sum + ?, rating_count = rating_count + 1 WHERE id=?`, v, cid)
	} else if intVal(prev["value"]) == v {
		db.Conn.Exec(`DELETE FROM ratings WHERE profile_id=? AND content_id=?`, pid, cid)
		db.Conn.Exec(`UPDATE content SET rating_sum = rating_sum - ?, rating_count = rating_count - 1 WHERE id=?`, v, cid)
	} else {
		db.Conn.Exec(`UPDATE ratings SET value=? WHERE profile_id=? AND content_id=?`, v, pid, cid)
		db.Conn.Exec(`UPDATE content SET rating_sum = rating_sum + ? WHERE id=?`, v-intVal(prev["value"]), cid)
	}
	J(w, 200, map[string]any{"ok": true})
}

func postProgress(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	p, ok := ownProfileID(w, num(b["profileId"], 0), uid, role)
	if !ok {
		return
	}
	pid := intVal(p["id"])
	cid := num(b["contentId"], 0)
	if cid == 0 {
		E(w, 400, "contentId required")
		return
	}
	if _, good := getContent(w, cid); !good {
		return
	}
	epid := num(b["episodeId"], 0)
	if epid != 0 && db.Count(`SELECT COUNT(*) FROM episodes WHERE id=? AND content_id=?`, epid, cid) == 0 {
		E(w, 404, "Episode not found for this title")
		return
	}
	pos := num(b["position_sec"], 0)
	dur := num(b["duration_sec"], 0)
	if dur <= 0 || pos < 0 {
		// Unknown/unplayable media or noise: acknowledge but store nothing,
		// so a real position is never clobbered by 0.
		J(w, 200, map[string]any{"ok": true, "completed": false, "ignored": true})
		return
	}
	completed := 0
	if dur > 0 && pos >= int64(float64(dur)*0.95) {
		completed = 1
	}
	var upd int64
	if epid != 0 {
		r, _ := db.Conn.Exec(`UPDATE watch_history SET position_sec=?, duration_sec=?, completed=?, updated_at=datetime('now') WHERE profile_id=? AND content_id=? AND episode_id=?`, pos, dur, completed, pid, cid, epid)
		upd, _ = r.RowsAffected()
	} else {
		r, _ := db.Conn.Exec(`UPDATE watch_history SET position_sec=?, duration_sec=?, completed=?, updated_at=datetime('now') WHERE profile_id=? AND content_id=? AND episode_id IS NULL`, pos, dur, completed, pid, cid)
		upd, _ = r.RowsAffected()
	}
	if upd == 0 {
		var epidArg any
		if epid != 0 {
			epidArg = epid
		}
		db.Conn.Exec(`INSERT INTO watch_history (profile_id,content_id,episode_id,position_sec,duration_sec,completed,updated_at) VALUES (?,?,?,?,?,?,datetime('now'))`, pid, cid, epidArg, pos, dur, completed)
	}
	J(w, 200, map[string]any{"ok": true, "completed": completed == 1})
}

func getHistory(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	p, ok := ownProfileQuery(w, r, uid, role)
	if !ok {
		return
	}
	rows, _ := db.QueryMap(`
		SELECT w.*, c.title, c.type, e.title as ep_title, e.season, e.episode FROM watch_history w
		JOIN content c ON c.id=w.content_id LEFT JOIN episodes e ON e.id=w.episode_id
		WHERE w.profile_id=? ORDER BY w.updated_at DESC LIMIT ?`, intVal(p["id"]), qInt(r, "limit", 30))
	if rows == nil {
		rows = []map[string]any{}
	}
	J(w, 200, map[string]any{"items": rows})
}

func streamMovie(w http.ResponseWriter, r *http.Request) { streamByID(w, r, true) }
func streamEpisode(w http.ResponseWriter, r *http.Request) { streamByID(w, r, false) }
func subsMovie(w http.ResponseWriter, r *http.Request)    { subsByID(w, r, true) }
func subsEpisode(w http.ResponseWriter, r *http.Request)  { subsByID(w, r, false) }

func streamByID(w http.ResponseWriter, r *http.Request, isMovie bool) {
	var vp, remote string
	if isMovie {
		c, good := getContent(w, pathID(r))
		if !good {
			return
		}
		vp, remote = str(c["video_path"]), str(c["remote_url"])
	} else {
		e, err := db.QueryOne(`SELECT * FROM episodes WHERE id=?`, pathID(r))
		if err != nil {
			E(w, 404, "Not found")
			return
		}
		vp, remote = str(e["video_path"]), str(e["remote_url"])
	}
	if vp != "" {
		if st, err := os.Stat(vp); err == nil && !st.IsDir() {
			f, err := os.Open(vp)
			if err != nil {
				E(w, 404, "File not found")
				return
			}
			defer f.Close()
			http.ServeContent(w, r, filepath.Base(vp), st.ModTime(), f)
			return
		}
	}
	if remote != "" {
		http.Redirect(w, r, remote, http.StatusFound)
		return
	}
	E(w, 404, "No video file. Add files to media/ and re-scan.")
}

func subsByID(w http.ResponseWriter, r *http.Request, isMovie bool) {
	var vp string
	if isMovie {
		c, good := getContent(w, pathID(r))
		if !good {
			return
		}
		vp = str(c["video_path"])
	} else {
		e, err := db.QueryOne(`SELECT * FROM episodes WHERE id=?`, pathID(r))
		if err != nil {
			E(w, 404, "Not found")
			return
		}
		vp = str(e["video_path"])
	}
	out := []map[string]any{}
	if vp != "" {
		dir := filepath.Dir(vp)
		base := strings.TrimSuffix(filepath.Base(vp), filepath.Ext(vp))
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(n), ".vtt") {
				continue
			}
			if strings.HasPrefix(n, base) || strings.Contains(strings.ToLower(n), "sub") {
				out = append(out, map[string]any{
					"lang": strings.TrimSuffix(n, filepath.Ext(n)),
					"url":  "/api/file?path=" + urlQueryEscape(filepath.Join(dir, n)),
				})
			}
		}
	}
	J(w, 200, map[string]any{"subs": out})
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}
