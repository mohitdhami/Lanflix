package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"lanflix/internal/auth"
	"lanflix/internal/db"
	"lanflix/internal/scanner"
	"lanflix/internal/seed"
)

func isAdmin(uid int64, role string) bool { return role == "admin" }

func adminOnly(next func(http.ResponseWriter, *http.Request, int64, string)) http.HandlerFunc {
	return auth.RequireAuth(func(w http.ResponseWriter, r *http.Request, uid int64, role string) {
		if role != "admin" {
			E(w, 403, "Admin only")
			return
		}
		next(w, r, uid, role)
	})
}

func authed(next func(http.ResponseWriter, *http.Request, int64, string)) http.HandlerFunc {
	return auth.RequireAuth(next)
}

func getAdminStats(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	top, _ := db.QueryMap(`SELECT id,title,type,views_count FROM content ORDER BY views_count DESC LIMIT 8`)
	last7, _ := db.QueryMap(`SELECT date(started_at) d, COUNT(*) c FROM views_log WHERE started_at >= date('now','-7 days') GROUP BY d ORDER BY d`)
	recent, _ := db.QueryMap(`SELECT v.*, c.title, p.name as profile FROM views_log v LEFT JOIN content c ON c.id=v.content_id LEFT JOIN profiles p ON p.id=v.profile_id ORDER BY v.started_at DESC LIMIT 20`)
	var bytes int64
	_ = filepath.WalkDir(db.MediaRoot(), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				bytes += fi.Size()
			}
		}
		return nil
	})
	if top == nil {
		top = []map[string]any{}
	}
	if last7 == nil {
		last7 = []map[string]any{}
	}
	if recent == nil {
		recent = []map[string]any{}
	}
	J(w, 200, map[string]any{
		"users": db.Count(`SELECT COUNT(*) FROM users`), "profiles": db.Count(`SELECT COUNT(*) FROM profiles`),
		"movies": db.Count(`SELECT COUNT(*) FROM content WHERE type='movie'`), "series": db.Count(`SELECT COUNT(*) FROM content WHERE type='series'`),
		"episodes": db.Count(`SELECT COUNT(*) FROM episodes`), "views": db.Count(`SELECT COUNT(*) FROM views_log`),
		"top": top, "last7": last7, "recent": recent, "storageBytes": bytes,
	})
}

func getAdminUsers(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	users, _ := db.QueryMap(`SELECT id,email,name,role,is_active,created_at FROM users ORDER BY id`)
	out := []map[string]any{}
	for _, u := range users {
		uid2 := intVal(u["id"])
		profs, _ := db.QueryMap(`SELECT * FROM profiles WHERE user_id=?`, uid2)
		if profs == nil {
			profs = []map[string]any{}
		}
		u["profiles"] = profs
		u["watch_count"] = db.Count(`SELECT COUNT(*) FROM views_log v JOIN profiles p ON p.id=v.profile_id WHERE p.user_id=?`, uid2)
		out = append(out, u)
	}
	J(w, 200, map[string]any{"users": out})
}

func postAdminUser(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	email := strings.ToLower(strings.TrimSpace(str(b["email"])))
	pw := str(b["password"])
	if email == "" || pw == "" {
		E(w, 400, "email + password required")
		return
	}
	ur := "user"
	if str(b["role"]) == "admin" {
		ur = "admin"
	}
	h, _ := auth.HashPassword(pw)
	res, err := db.Conn.Exec(`INSERT INTO users (email,password_hash,name,role) VALUES (?,?,?,?)`, email, h, str(b["name"]), ur)
	if err != nil {
		E(w, 400, "Email already exists")
		return
	}
	nid, _ := res.LastInsertId()
	name := str(b["name"])
	if name == "" {
		name = "User"
	}
	first := name
	if i := strings.Index(name, " "); i > 0 {
		first = name[:i]
	}
	db.Conn.Exec(`INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)`, nid, first, "😎", "#0071eb")
	J(w, 200, map[string]any{"ok": true, "id": nid})
}

func putAdminUser(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	tid := pathID(r)
	u, err := db.QueryOne(`SELECT * FROM users WHERE id=?`, tid)
	if err != nil {
		E(w, 404, "Not found")
		return
	}
	b := decode(r, nil)
	newRole := str(u["role"])
	if rv, present := b["role"]; present {
		newRole = "user"
		if str(rv) == "admin" {
			newRole = "admin"
		}
	}
	if tid == uid {
		if newRole != "admin" {
			E(w, 400, "You cannot demote yourself")
			return
		}
		if av, present := b["is_active"]; present && (num(av, 1) == 0) {
			E(w, 400, "You cannot disable yourself")
			return
		}
	}
	name := str(u["name"])
	if nv, present := b["name"]; present {
		name = str(nv)
	}
	active := intVal(u["is_active"])
	if av, present := b["is_active"]; present {
		active = 0
		if bv, ok := av.(bool); (ok && bv) || num(av, 0) == 1 {
			active = 1
		}
	}
	db.Conn.Exec(`UPDATE users SET name=?, role=?, is_active=? WHERE id=?`, name, newRole, active, tid)
	if pw := str(b["password"]); pw != "" {
		h, _ := auth.HashPassword(pw)
		db.Conn.Exec(`UPDATE users SET password_hash=? WHERE id=?`, h, tid)
	}
	J(w, 200, map[string]any{"ok": true})
}

func deleteAdminUser(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	tid := pathID(r)
	if tid == uid {
		E(w, 400, "Cannot delete yourself")
		return
	}
	db.Conn.Exec(`DELETE FROM users WHERE id=?`, tid)
	J(w, 200, map[string]any{"ok": true})
}

func getAdminContent(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	where := "1=1"
	var args []any
	if t := qStr(r, "type"); t != "" {
		where += " AND type=?"
		args = append(args, t)
	}
	if s := qStr(r, "search"); s != "" {
		where += " AND title LIKE ?"
		args = append(args, "%"+s+"%")
	}
	rows, _ := db.QueryMap(`SELECT * FROM content WHERE `+where+` ORDER BY added_at DESC LIMIT 200`, args...)
	items := []map[string]any{}
	for _, c := range rows {
		o := rowContent(c)
		o["episode_count"] = db.Count(`SELECT COUNT(*) FROM episodes WHERE content_id=?`, intVal(c["id"]))
		items = append(items, o)
	}
	J(w, 200, map[string]any{"items": items})
}

func arrField(v any, fallback string) string {
	if v == nil {
		return fallback
	}
	b, _ := json.Marshal(strArr(v))
	return string(b)
}

func postAdminContent(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	title := strings.TrimSpace(str(b["title"]))
	if title == "" {
		E(w, 400, "Title required")
		return
	}
	ctype := "movie"
	if str(b["type"]) == "series" {
		ctype = "series"
	}
	feat := 0
	if bv, ok := b["is_featured"].(bool); (ok && bv) || num(b["is_featured"], 0) == 1 {
		feat = 1
	}
	var year any
	if y, present := b["year"]; present && num(y, 0) != 0 {
		year = num(y, 0)
	}
	res, err := db.Conn.Exec(`INSERT INTO content (type,title,description,year,genres,cast,director,maturity_rating,duration_sec,poster_path,backdrop_path,trailer_url,video_path,remote_url,is_featured,imdb_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ctype, title, str(b["description"]), year, arrField(b["genres"], "[]"), arrField(b["cast"], "[]"),
		str(b["director"]), strOr(str(b["maturity_rating"]), "U/A 13+"), num(b["duration_sec"], 0),
		str(b["poster_path"]), str(b["backdrop_path"]), str(b["trailer_url"]), str(b["video_path"]), str(b["remote_url"]),
		feat, str(b["imdb_id"]))
	if err != nil {
		E(w, 400, "Could not create title")
		return
	}
	id, _ := res.LastInsertId()
	J(w, 200, map[string]any{"ok": true, "id": id})
}

func strOr(s, fb string) string {
	if s == "" {
		return fb
	}
	return s
}

func putAdminContent(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	c, err := db.QueryOne(`SELECT * FROM content WHERE id=?`, pathID(r))
	if err != nil {
		E(w, 404, "Not found")
		return
	}
	b := decode(r, nil)
	cid := intVal(c["id"])
	title := str(c["title"])
	if v, p := b["title"]; p {
		title = str(v)
	}
	desc := str(c["description"])
	if v, p := b["description"]; p {
		desc = str(v)
	}
	var year any = c["year"]
	if v, p := b["year"]; p {
		if num(v, 0) == 0 {
			year = nil
		} else {
			year = num(v, 0)
		}
	}
	genres := arrField(b["genres"], str(c["genres"]))
	if genres == "null" {
		genres = "[]"
	}
	cast := arrField(b["cast"], str(c["cast"]))
	if cast == "null" {
		cast = "[]"
	}
	keep := func(k string) string {
		if v, p := b[k]; p {
			return str(v)
		}
		return str(c[k])
	}
	dur := intVal(c["duration_sec"])
	if v, p := b["duration_sec"]; p {
		dur = num(v, 0)
	}
	feat := intVal(c["is_featured"])
	if v, p := b["is_featured"]; p {
		feat = 0
		if bv, ok := v.(bool); (ok && bv) || num(v, 0) == 1 {
			feat = 1
		}
	}
	db.Conn.Exec(`UPDATE content SET title=?,description=?,year=?,genres=?,cast=?,director=?,maturity_rating=?,duration_sec=?,poster_path=?,backdrop_path=?,trailer_url=?,video_path=?,remote_url=?,is_featured=?,imdb_id=? WHERE id=?`,
		title, desc, year, genres, cast, keep("director"), keep("maturity_rating"), dur,
		keep("poster_path"), keep("backdrop_path"), keep("trailer_url"), keep("video_path"), keep("remote_url"), feat, keep("imdb_id"), cid)
	J(w, 200, map[string]any{"ok": true})
}

func deleteAdminContent(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	db.Conn.Exec(`DELETE FROM content WHERE id=?`, pathID(r))
	J(w, 200, map[string]any{"ok": true})
}

func postAdminConvert(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	c, err := db.QueryOne(`SELECT * FROM content WHERE id=?`, pathID(r))
	if err != nil {
		E(w, 404, "Not found")
		return
	}
	b := decode(r, nil)
	to := str(b["type"])
	if to != "movie" && to != "series" {
		E(w, 400, "type must be movie or series")
		return
	}
	if to == str(c["type"]) {
		J(w, 200, map[string]any{"ok": true, "unchanged": true})
		return
	}
	if str(c["type"]) == "series" && to == "movie" {
		n := db.Count(`SELECT COUNT(*) FROM episodes WHERE content_id=?`, intVal(c["id"]))
		if n > 0 {
			E(w, 400, "Delete its "+itoa64(n)+" episode(s) first")
			return
		}
	}
	db.Conn.Exec(`UPDATE content SET type=? WHERE id=?`, to, intVal(c["id"]))
	J(w, 200, map[string]any{"ok": true, "from": str(c["type"]), "to": to})
}

func itoa64(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func postAdminEpisode(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	cid := num(b["content_id"], 0)
	title := str(b["title"])
	if cid == 0 || title == "" {
		E(w, 400, "content_id + title required")
		return
	}
	if db.Count(`SELECT COUNT(*) FROM content WHERE id=? AND type='series'`, cid) == 0 {
		E(w, 404, "Series not found")
		return
	}
	s := num(b["season"], 1)
	if s == 0 {
		s = 1
	}
	e := num(b["episode"], 0)
	if e == 0 {
		e = int64(db.Count(`SELECT COALESCE(MAX(episode),0) FROM episodes WHERE content_id=? AND season=?`, cid, s)) + 1
	}
	if db.Count(`SELECT COUNT(*) FROM episodes WHERE content_id=? AND season=? AND episode=?`, cid, s, e) > 0 {
		E(w, 400, "S"+itoa64(int(s))+" E"+itoa64(int(e))+" already exists")
		return
	}
	res, err := db.Conn.Exec(`INSERT INTO episodes (content_id,season,episode,title,description,duration_sec,remote_url,video_path) VALUES (?,?,?,?,?,?,?,?)`,
		cid, s, e, title, str(b["description"]), num(b["duration_sec"], 0), str(b["remote_url"]), str(b["video_path"]))
	if err != nil {
		E(w, 400, "Could not add episode")
		return
	}
	id, _ := res.LastInsertId()
	J(w, 200, map[string]any{"ok": true, "id": id})
}

func deleteAdminEpisode(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	if db.Count(`SELECT COUNT(*) FROM episodes WHERE id=?`, pathID(r)) == 0 {
		E(w, 404, "Not found")
		return
	}
	db.Conn.Exec(`DELETE FROM episodes WHERE id=?`, pathID(r))
	J(w, 200, map[string]any{"ok": true})
}

func postAdminScan(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	res := scanner.ScanMedia()
	J(w, 200, map[string]any{"ok": true, "added": res.Added, "updated": res.Updated})
}

func postAdminSeedDemo(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	J(w, 200, map[string]any{"ok": true, "inserted": seed.SeedCatalog()})
}

func getAdminViews(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	rows, _ := db.QueryMap(`SELECT v.*, c.title, p.name as profile FROM views_log v LEFT JOIN content c ON c.id=v.content_id LEFT JOIN profiles p ON p.id=v.profile_id ORDER BY v.started_at DESC LIMIT 100`)
	if rows == nil {
		rows = []map[string]any{}
	}
	J(w, 200, map[string]any{"items": rows})
}
