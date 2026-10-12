// LanFlix — local-network Netflix clone. Go backend.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"lanflix/internal/api"
	"lanflix/internal/auth"
	"lanflix/internal/db"
	"lanflix/internal/scanner"
	"lanflix/internal/seed"
)

func main() {
	reset := flag.Bool("reset", false, "wipe the database and reseed demo data")
	flag.Parse()
	if err := db.Open(); err != nil {
		fmt.Println("db error:", err)
		os.Exit(1)
	}
	if *reset {
		if err := db.Reset(); err != nil {
			fmt.Println("reset error:", err)
			os.Exit(1)
		}
		seed.SeedUsers()
		seed.SeedCatalog()
		fmt.Println("database wiped and reseeded (admin@lan.local / admin123)")
	}
	for _, d := range []string{filepath.Join(db.Root, "media", "movies"), filepath.Join(db.Root, "media", "series"), filepath.Join(db.Root, "public", "posters"), db.DataDir} {
		_ = os.MkdirAll(d, 0o755)
	}
	seed.Seed(false)
	scanner.ScanMedia()

	mux := http.NewServeMux()
	reg := func(method, pattern string, h http.HandlerFunc) {
		mux.HandleFunc(method+" "+pattern, h)
	}
	// public
	reg("GET", "/api/server-info", api.GetServerInfo)
	reg("GET", "/api/file", api.GetFile)
	reg("POST", "/api/auth/signup", api.PostSignup)
	reg("POST", "/api/auth/login", api.PostLogin)
	reg("GET", "/api/genres", api.GetGenres)
	reg("GET", "/api/content", api.GetContentList)
	reg("GET", "/api/browse/rows", api.GetBrowseRows)
	reg("GET", "/api/content/{id}", api.GetContentDetail)
	reg("GET", "/api/content/{id}/episodes", api.GetEpisodes)
	reg("GET", "/api/stream/movie/{id}", api.StreamMovie)
	reg("GET", "/api/stream/episode/{id}", api.StreamEpisode)
	reg("GET", "/api/subs/movie/{id}", api.SubsMovie)
	reg("GET", "/api/subs/episode/{id}", api.SubsEpisode)
	// authed (profile-scoped)
	reg("GET", "/api/auth/me", api.Authed(api.GetMe))
	reg("GET", "/api/profiles", api.Authed(api.GetProfiles))
	reg("POST", "/api/profiles", api.Authed(api.PostProfile))
	reg("PUT", "/api/profiles/{id}", api.Authed(api.PutProfile))
	reg("DELETE", "/api/profiles/{id}", api.Authed(api.DeleteProfile))
	reg("POST", "/api/content/{id}/view", api.Authed(api.PostContentView))
	reg("GET", "/api/mylist", api.Authed(api.GetMyList))
	reg("POST", "/api/mylist", api.Authed(api.PostMyList))
	reg("DELETE", "/api/mylist/{contentId}", api.Authed(api.DeleteMyList))
	reg("POST", "/api/rate", api.Authed(api.PostRate))
	reg("POST", "/api/progress", api.Authed(api.PostProgress))
	reg("GET", "/api/history", api.Authed(api.GetHistory))
	// admin
	reg("GET", "/api/admin/stats", api.AdminOnly(api.GetAdminStats))
	reg("GET", "/api/admin/users", api.AdminOnly(api.GetAdminUsers))
	reg("POST", "/api/admin/users", api.AdminOnly(api.PostAdminUser))
	reg("PUT", "/api/admin/users/{id}", api.AdminOnly(api.PutAdminUser))
	reg("DELETE", "/api/admin/users/{id}", api.AdminOnly(api.DeleteAdminUser))
	reg("GET", "/api/admin/content", api.AdminOnly(api.GetAdminContent))
	reg("POST", "/api/admin/content", api.AdminOnly(api.PostAdminContent))
	reg("PUT", "/api/admin/content/{id}", api.AdminOnly(api.PutAdminContent))
	reg("DELETE", "/api/admin/content/{id}", api.AdminOnly(api.DeleteAdminContent))
	reg("POST", "/api/admin/content/{id}/convert", api.AdminOnly(api.PostAdminConvert))
	reg("POST", "/api/admin/episodes", api.AdminOnly(api.PostAdminEpisode))
	reg("POST", "/api/admin/episodes/reorder", api.AdminOnly(api.PostAdminEpisodesReorder))
	reg("PUT", "/api/admin/episodes/{id}", api.AdminOnly(api.PutAdminEpisode))
	reg("POST", "/api/admin/episodes/{id}/move", api.AdminOnly(api.PostAdminEpisodeMove))
	reg("DELETE", "/api/admin/episodes/{id}", api.AdminOnly(api.DeleteAdminEpisode))
	reg("POST", "/api/admin/scan", api.AdminOnly(api.PostAdminScan))
	reg("POST", "/api/admin/seed-demo", api.AdminOnly(api.PostAdminSeedDemo))
	reg("GET", "/api/admin/views", api.AdminOnly(api.GetAdminViews))
	reg("POST", "/api/admin/content/{id}/poster", api.AdminOnly(api.PostAdminPoster))
	reg("POST", "/api/admin/upload", api.AdminOnly(api.PostAdminUpload))
	reg("POST", "/api/admin/imdb", api.AdminOnly(api.PostAdminImdbFetch))
	reg("POST", "/api/admin/imdb/import", api.AdminOnly(api.PostAdminImdbImport))

	// static frontend + SPA fallback; unknown /api/* -> JSON 404
	fileSrv := http.FileServer(http.Dir(filepath.Join(db.Root, "public")))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			auth.WriteErr(w, 404, "Unknown API endpoint")
			return
		}
		p := filepath.Join(db.Root, "public", filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(p); err == nil && !st.IsDir() && p != filepath.Join(db.Root, "public") {
			fileSrv.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(db.Root, "public", "index.html"))
	})

	handler := withMiddleware(mux)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: "0.0.0.0:" + port, Handler: handler}
	fmt.Print("\n  🎬 LanFlix running! (Go)\n\n")
	fmt.Println("  Local:   http://localhost:" + port)
	for _, ip := range api.LanIPs() {
		fmt.Println("  LAN:     http://" + ip + ":" + port + "   <-- open this on TV / phones")
	}
	fmt.Println("\n  Default admin: admin@lan.local / admin123")
	fmt.Println("  Demo user:   demo@lan.local / demo1234")
	fmt.Print("\n  Put video files in ./media/movies and ./media/series, then Admin → Rescan.\n\n")
	if err := srv.ListenAndServe(); err != nil {
		if strings.Contains(err.Error(), "address already in use") {
			fmt.Printf("\n  ❌ Port %s is already in use — LanFlix is already running in another terminal.\n", port)
			fmt.Print("  Stop the other one, or use a different port:  PORT=9000 go run .\n\n")
			os.Exit(1)
		}
		fmt.Println("server error:", err)
		os.Exit(1)
	}
}

func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			if r.Method == "OPTIONS" {
				w.WriteHeader(204)
				return
			}
		}
		defer func() {
			if rec := recover(); rec != nil {
				fmt.Println("[api panic]", r.Method, r.URL.Path, "-", rec)
				auth.WriteErr(w, 500, "Server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
