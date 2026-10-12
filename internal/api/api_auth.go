package api

import (
	"net/http"
	"strings"

	"lanflix/internal/auth"
	"lanflix/internal/db"
)

func postSignup(w http.ResponseWriter, r *http.Request) {
	b := decode(r, nil)
	email := strings.ToLower(strings.TrimSpace(str(b["email"])))
	pw := str(b["password"])
	name := str(b["name"])
	if email == "" || len(pw) < 4 {		E(w, 400, "Email + password (min 4 chars) required")
		return
	}
	if _, err := db.QueryOne(`SELECT * FROM users WHERE email=?`, email); err == nil {
		E(w, 400, "Email already registered. Please log in.")
		return
	}
	role := "user"
	if db.Count(`SELECT COUNT(*) FROM users`) == 0 {
		role = "admin"
	}
	if name == "" {
		name = email
		if i := strings.Index(email, "@"); i > 0 {
			name = email[:i]
		}
	}
	h, _ := auth.HashPassword(pw)
	res, err := db.Conn.Exec(`INSERT INTO users (email,password_hash,name,role) VALUES (?,?,?,?)`, email, h, name, role)
	if err != nil {
		E(w, 400, "Could not create account")
		return
	}
	uid, _ := res.LastInsertId()
	first := name
	if i := strings.Index(name, " "); i > 0 {
		first = name[:i]
	}
	db.Conn.Exec(`INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)`, uid, first, "😎", "#0071eb")
	tok, _ := auth.SignToken(uid, email, role)
	J(w, 200, map[string]any{"token": tok, "user": map[string]any{"id": uid, "email": email, "name": name, "role": role}})
}

func postLogin(w http.ResponseWriter, r *http.Request) {
	b := decode(r, nil)
	email := strings.ToLower(strings.TrimSpace(str(b["email"])))
	pw := str(b["password"])
	if email == "" || pw == "" {
		E(w, 400, "Email + password required")
		return
	}
	u, err := db.QueryOne(`SELECT * FROM users WHERE email=?`, email)
	if err != nil {
		E(w, 401, "Invalid email or password")
		return
	}
	if !auth.VerifyPassword(pw, str(u["password_hash"])) {
		E(w, 401, "Invalid email or password")
		return
	}
	if intVal(u["is_active"]) == 0 {
		E(w, 403, "Account disabled. Contact admin.")
		return
	}
	uid := intVal(u["id"])
	role := str(u["role"])
	tok, _ := auth.SignToken(uid, email, role)
	J(w, 200, map[string]any{"token": tok, "user": map[string]any{"id": uid, "email": email, "name": str(u["name"]), "role": role}})
}

func getMe(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	u, err := db.QueryOne(`SELECT id,email,name,role,created_at FROM users WHERE id=?`, uid)
	if err != nil {
		E(w, 401, "Invalid token")
		return
	}
	J(w, 200, map[string]any{"user": u})
}

func getProfiles(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	target := uid
	if qStr(r, "userId") != "" && role == "admin" {
		target = qInt(r, "userId", uid)
	}
	rows, _ := db.QueryMap(`SELECT * FROM profiles WHERE user_id=? ORDER BY id`, target)
	if rows == nil {
		rows = []map[string]any{}
	}
	J(w, 200, map[string]any{"profiles": rows})
}

func postProfile(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	b := decode(r, nil)
	name := str(b["name"])
	if name == "" {
		E(w, 400, "Name required")
		return
	}
	target := uid
	if num(b["userId"], 0) != 0 && role == "admin" {
		target = num(b["userId"], 0)
	}
	if db.Count(`SELECT COUNT(*) FROM profiles WHERE user_id=?`, target) >= 5 {
		E(w, 400, "Max 5 profiles per account")
		return
	}
	avatar := str(b["avatar"])
	if avatar == "" {
		avatar = "🎬"
	}
	color := str(b["color"])
	if color == "" {
		color = "#E50914"
	}
	kids := 0
	if bv, ok := b["is_kids"].(bool); ok && bv {
		kids = 1
	} else if num(b["is_kids"], 0) == 1 {
		kids = 1
	}
	var pin any
	if str(b["pin"]) != "" {
		pin = str(b["pin"])
	}
	res, err := db.Conn.Exec(`INSERT INTO profiles (user_id,name,avatar,color,is_kids,pin) VALUES (?,?,?,?,?,?)`, target, name, avatar, color, kids, pin)
	if err != nil {
		E(w, 400, "Could not create profile")
		return
	}
	id, _ := res.LastInsertId()
	p, _ := db.QueryOne(`SELECT * FROM profiles WHERE id=?`, id)
	J(w, 200, map[string]any{"profile": p})
}

func putProfile(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	p, err := db.QueryOne(`SELECT * FROM profiles WHERE id=?`, pathID(r))
	if err != nil {
		E(w, 404, "Not found")
		return
	}
	if intVal(p["user_id"]) != uid && role != "admin" {
		E(w, 403, "Forbidden")
		return
	}
	b := decode(r, nil)
	name := str(b["name"])
	if name == "" {
		name = str(p["name"])
	}
	avatar := str(b["avatar"])
	if avatar == "" {
		avatar = str(p["avatar"])
	}
	color := str(b["color"])
	if color == "" {
		color = str(p["color"])
	}
	kids := intVal(p["is_kids"])
	if _, present := b["is_kids"]; present {
		kids = 0
		if bv, ok := b["is_kids"].(bool); (ok && bv) || num(b["is_kids"], 0) == 1 {
			kids = 1
		}
	}
	var pin any
	if pv, present := b["pin"]; present {
		if str(pv) == "" {
			pin = nil
		} else {
			pin = str(pv)
		}
	} else {
		pin = p["pin"]
	}
	db.Conn.Exec(`UPDATE profiles SET name=?,avatar=?,color=?,is_kids=?,pin=? WHERE id=?`, name, avatar, color, kids, pin, intVal(p["id"]))
	np, _ := db.QueryOne(`SELECT * FROM profiles WHERE id=?`, intVal(p["id"]))
	J(w, 200, map[string]any{"profile": np})
}

func deleteProfile(w http.ResponseWriter, r *http.Request, uid int64, role string) {
	p, err := db.QueryOne(`SELECT * FROM profiles WHERE id=?`, pathID(r))
	if err != nil {
		E(w, 404, "Not found")
		return
	}
	if intVal(p["user_id"]) != uid && role != "admin" {
		E(w, 403, "Forbidden")
		return
	}
	db.Conn.Exec(`DELETE FROM profiles WHERE id=?`, intVal(p["id"]))
	J(w, 200, map[string]any{"ok": true})
}
