package api

import (
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"lanflix/internal/auth"
	"lanflix/internal/db"
)

var J = auth.WriteJSON
var E = auth.WriteErr

func decode(r *http.Request, v any) map[string]any {
	m := map[string]any{}
	if r.Body == nil {
		return m
	}
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	_ = dec.Decode(&m)
	if v != nil {
		if b, err := json.Marshal(m); err == nil {
			_ = json.Unmarshal(b, v)
		}
	}
	return m
}

func qStr(r *http.Request, k string) string { return r.URL.Query().Get(k) }

func qInt(r *http.Request, k string, def int64) int64 {
	s := r.URL.Query().Get(k)
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func num(v any, def int64) int64 {
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
			return i
		}
	}
	return def
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func strArr(v any) []string {
	switch t := v.(type) {
	case nil:
		return []string{}
	case string:
		var out []string
		for _, s := range strings.Split(t, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := []string{}
		for _, x := range t {
			if s, ok := x.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		return []string{}
	}
}

func parseStrArr(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		return []string{}
	}
	return out
}

// fileURL mirrors the Node convention: remote URLs pass through,
// local absolute paths are served via /api/file?path=.
func fileURL(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http") {
		return p
	}
	return "/api/file?path=" + url.QueryEscape(p)
}

func rowContent(m map[string]any) map[string]any {
	genres := parseStrArr(str(m["genres"]))
	cast := parseStrArr(str(m["cast"]))
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	out["genres"] = genres
	out["cast"] = cast
	out["poster"] = fileURL(str(m["poster_path"]))
	out["backdrop"] = fileURL(str(m["backdrop_path"]))
	var sum, cnt float64
	switch s := m["rating_sum"].(type) {
	case int64:
		sum = float64(s)
	}
	switch c := m["rating_count"].(type) {
	case int64:
		cnt = float64(c)
	}
	if cnt > 0 {
		out["rating_avg"] = sum / cnt
	} else {
		out["rating_avg"] = 0
	}
	return out
}

func intVal(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// kidsClause returns the SQL filter for kids profiles (empty for others).
func kidsClause(profileID int64) string {
	if profileID == 0 {
		return ""
	}
	p, err := db.QueryOne(`SELECT * FROM profiles WHERE id=?`, profileID)
	if err != nil {
		return ""
	}
	if intVal(p["is_kids"]) == 1 {
		return ` AND maturity_rating NOT IN ('A 18+','U/A 16+')`
	}
	return ""
}

// ownProfileID enforces that uid owns pid (admins bypass). Returns the profile.
func ownProfileID(w http.ResponseWriter, pid, uid int64, role string) (map[string]any, bool) {
	p, err := db.QueryOne(`SELECT * FROM profiles WHERE id=?`, pid)
	if err == sql.ErrNoRows || err != nil {
		E(w, 404, "Profile not found")
		return nil, false
	}
	if intVal(p["user_id"]) != uid && role != "admin" {
		E(w, 403, "Not your profile")
		return nil, false
	}
	return p, true
}

// ownProfileQuery is the strict variant: profileId required in the query string.
func ownProfileQuery(w http.ResponseWriter, r *http.Request, uid int64, role string) (map[string]any, bool) {
	pid := qInt(r, "profileId", 0)
	if pid == 0 {
		E(w, 400, "profileId required")
		return nil, false
	}
	return ownProfileID(w, pid, uid, role)
}

// optionalProfile: if a profileId is supplied it must be owned (auth required),
// otherwise the route stays public.
func optionalProfile(w http.ResponseWriter, r *http.Request, body map[string]any) (int64, bool) {
	var pid int64
	if s := qStr(r, "profileId"); s != "" {
		pid = qInt(r, "profileId", 0)
	} else if body != nil {
		pid = num(body["profileId"], 0)
	}
	if pid == 0 {
		return 0, true
	}
	tok := r.Header.Get("Authorization")
	id, _, role, ok := auth.ParseToken(trimBearer(tok))
	if !ok {
		// fall through to auth-style errors
		if tok == "" {
			E(w, 401, "No token")
		} else {
			E(w, 401, "Invalid token")
		}
		return 0, false
	}
	if _, good := ownProfileID(w, pid, id, role); !good {
		return 0, false
	}
	return pid, true
}

func trimBearer(h string) string {
	return strings.TrimPrefix(h, "Bearer ")
}

func getContent(w http.ResponseWriter, id int64) (map[string]any, bool) {
	c, err := db.QueryOne(`SELECT * FROM content WHERE id=?`, id)
	if err != nil {
		E(w, 404, "Title not found")
		return nil, false
	}
	return c, true
}

func pathID(r *http.Request) int64 {
	return num(r.PathValue("id"), 0)
}

func lanIPs() []string {
	ips := []string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				ips = append(ips, v4.String())
			}
		}
	}
	return ips
}

func serverPort() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}
