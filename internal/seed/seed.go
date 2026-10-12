package seed

import (
	"encoding/json"
	"net/url"

	"lanflix/internal/auth"
	"lanflix/internal/db"
)

var sampleVideos = []string{
	"https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/BigBuckBunny.mp4",
	"https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ElephantsDream.mp4",
	"https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerBlazes.mp4",
	"https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerEscapes.mp4",
	"https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerFun.mp4",
	"https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerJoyrides.mp4",
	"https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/Sintel.mp4",
	"https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/TearsOfSteel.mp4",
}

type demoEp struct {
	T string
	D string
	Dur int
}

type demoItem struct {
	Type     string
	Title    string
	Year     *int
	Genres   []string
	Desc     string
	Director string
	Cast     []string
	Maturity string
	Dur      int
	Featured bool
	Eps      []demoEp
}

func yr(y int) *int { return &y }

func demoCatalog() []demoItem {
	return []demoItem{
		{Type: "movie", Title: "Big Buck Bunny", Year: yr(2008), Genres: []string{"Animation", "Comedy", "Family"}, Desc: "A giant rabbit takes revenge on three bullying rodents in this classic open-source comedy.", Director: "Sacha Goedegebure", Cast: []string{"Blender Foundation"}, Maturity: "U", Dur: 596, Featured: true},
		{Type: "movie", Title: "Sintel", Year: yr(2010), Genres: []string{"Fantasy", "Adventure", "Drama"}, Desc: "A lonely young woman searches for the dragon she once befriended. A Blender Foundation epic.", Director: "Colin Levy", Cast: []string{"Halina Reijn"}, Maturity: "U/A 13+", Dur: 888, Featured: true},
		{Type: "movie", Title: "Tears of Steel", Year: yr(2012), Genres: []string{"Sci-Fi", "Action", "Drama"}, Desc: "Forty years after a breakup in Amsterdam, a group of warriors save the world from killer robots.", Director: "Ian Hubert", Cast: []string{"Derek de Lint"}, Maturity: "U/A 16+", Dur: 734, Featured: true},
		{Type: "movie", Title: "Elephants Dream", Year: yr(2006), Genres: []string{"Sci-Fi", "Animation", "Mystery"}, Desc: "Two travelers explore a strange infinite machine. The first open-source film ever made.", Director: "Bassam Kurdali", Cast: []string{"Tygo Gernandt"}, Maturity: "U", Dur: 653},
		{Type: "series", Title: "Blender Shorts", Year: yr(2023), Genres: []string{"Comedy", "Animation"}, Desc: "A collection of bite-sized open movies. Perfect to test episodes, autoplay and continue-watching.", Maturity: "U", Featured: true, Eps: []demoEp{
			{"For Bigger Blazes", "Chromecast promo action short.", 15},
			{"For Bigger Escapes", "An escape story in 15 seconds.", 15},
			{"For Bigger Fun", "Fun in every frame.", 60},
			{"For Bigger Joyrides", "A joyride you will not forget.", 15},
		}},
		{Type: "movie", Title: "Neon City Nights", Year: yr(2024), Genres: []string{"Thriller", "Crime", "Drama"}, Desc: "A cab driver witnesses something he should not have on the last night before the city power grid resets.", Director: "Local Studio", Cast: []string{"A. Sharma", "R. Verma"}, Maturity: "A 18+", Dur: 5400},
		{Type: "movie", Title: "Monsoon Wedding Blues", Year: yr(2023), Genres: []string{"Romance", "Comedy", "Drama"}, Desc: "Two families, one flooded venue, and a wedding planner who refuses to give up.", Director: "Local Studio", Cast: []string{"P. Iyer", "K. Nair"}, Maturity: "U/A 13+", Dur: 7200},
		{Type: "series", Title: "LAN Detectives", Year: yr(2024), Genres: []string{"Mystery", "Comedy"}, Desc: "Two hostel roommates solve absurd neighbourhood mysteries using a router log and pure confidence.", Maturity: "U/A 13+", Eps: []demoEp{
			{"The Missing Remote", "S1 E1 — The TV remote vanishes during finals week.", 1200},
			{"Buffering", "S1 E2 — The Wi-Fi slows every night at 9 PM. Coincidence?", 1300},
			{"The 4K Heist", "S1 E3 — Somebody is hogging the bandwidth.", 1400},
		}},
	}
}

func SeedUsers() {
	if _, err := db.QueryOne(`SELECT * FROM users WHERE email=?`, "admin@lan.local"); err != nil {
		h, _ := auth.HashPassword("admin123")
		r, _ := db.Conn.Exec(`INSERT INTO users (email,password_hash,name,role) VALUES (?,?,?,?)`, "admin@lan.local", h, "Admin", "admin")
		if id, err := r.LastInsertId(); err == nil {
			db.Conn.Exec(`INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)`, id, "Admin", "👑", "#E50914")
			db.Conn.Exec(`INSERT INTO profiles (user_id,name,avatar,color,is_kids) VALUES (?,?,?,?,?)`, id, "Kids", "🦊", "#46d369", 1)
		}
	}
	if _, err := db.QueryOne(`SELECT * FROM users WHERE email=?`, "demo@lan.local"); err != nil {
		h, _ := auth.HashPassword("demo1234")
		r, _ := db.Conn.Exec(`INSERT INTO users (email,password_hash,name,role) VALUES (?,?,?,?)`, "demo@lan.local", h, "Demo Family", "user")
		if id, err := r.LastInsertId(); err == nil {
			db.Conn.Exec(`INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)`, id, "Mohit", "😎", "#0071eb")
			db.Conn.Exec(`INSERT INTO profiles (user_id,name,avatar,color) VALUES (?,?,?,?)`, id, "Guest", "🎬", "#f5a623")
		}
	}
}

// SeedCatalog inserts missing demo titles; returns count inserted.
func SeedCatalog() int {
	inserted := 0
	vi := 0
	for _, d := range demoCatalog() {
		if _, err := db.QueryOne(`SELECT 1 FROM content WHERE type=? AND title=?`, d.Type, d.Title); err == nil {
			continue
		}
		remote := ""
		if d.Type == "movie" {
			remote = sampleVideos[vi%len(sampleVideos)]
			vi++
		}
		g, _ := json.Marshal(d.Genres)
		cj, _ := json.Marshal(d.Cast)
		if cj == nil {
			cj = []byte("[]")
		}
		dur := d.Dur
		if dur == 0 {
			dur = 3600
		}
		feat := 0
		if d.Featured {
			feat = 1
		}
		var y any
		if d.Year != nil {
			y = int64(*d.Year)
		}
		r, err := db.Conn.Exec(`INSERT INTO content (type,title,description,year,genres,cast,director,maturity_rating,duration_sec,poster_path,backdrop_path,remote_url,is_featured)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			d.Type, d.Title, d.Desc, y, string(g), string(cj), d.Director, d.Maturity, int64(dur),
			"https://picsum.photos/seed/"+url.PathEscape(d.Title)+"/400/600", "https://picsum.photos/seed/"+url.PathEscape(d.Title+"-bg")+"/1280/720", remote, feat)
		if err != nil {
			continue
		}
		inserted++
		if id, err := r.LastInsertId(); err == nil {
			for i, e := range d.Eps {
				db.Conn.Exec(`INSERT INTO episodes (content_id,season,episode,title,description,duration_sec,remote_url) VALUES (?,?,?,?,?,?,?)`,
					id, 1, i+1, e.T, e.D, e.Dur, sampleVideos[(vi+i)%len(sampleVideos)])
			}
			vi += len(d.Eps)
		}
	}
	return inserted
}

func Seed(force bool) {
	if db.Count(`SELECT COUNT(*) FROM users`) > 0 && !force {
		return
	}
	if force {
		_ = db.Reset()
	}
	SeedUsers()
	SeedCatalog()
}
