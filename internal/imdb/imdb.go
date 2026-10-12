package imdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ttRe = regexp.MustCompile(`tt\d{7,9}`)

type Info struct {
	ID          string   `json:"imdb_id"`
	Type        string   `json:"type"` // movie | series
	Title       string   `json:"title"`
	Year        *int     `json:"year"`
	Description string   `json:"description"`
	Genres      []string `json:"genres"`
	Cast        []string `json:"cast"`
	Director    string   `json:"director"`
	Maturity    string   `json:"maturity_rating"`
	DurationSec int      `json:"duration_sec"`
	PosterURL   string   `json:"poster_url"`
	BackdropURL string   `json:"backdrop_url"`
	ImdbRating  string   `json:"imdb_rating"`
}

// ExtractID pulls tt1234567 out of a full IMDb link or bare id.
func ExtractID(input string) (string, bool) {
	m := ttRe.FindString(input)
	if m == "" {
		return "", false
	}
	return m, true
}

var client = &http.Client{Timeout: 30 * time.Second}

func getJSON(url string, v any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "LanFlix/1.0 (local media server)")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("metadata service returned %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// FetchMeta resolves an IMDb id to full metadata. Both the movie and the
// series catalogs are consulted because miniseries are sometimes mistyped
// on one side; records whose own id doesn't match the request are rejected,
// and of the survivors the richer, type-consistent record wins.
func FetchMeta(tt string) (*Info, error) {
	type result struct {
		asked string
		meta  *cineMeta
	}
	results := make([]result, 0, 2)
	for _, t := range []string{"movie", "series"} {
		var doc struct {
			Meta *cineMeta `json:"meta"`
		}
		if err := getJSON("https://v3-cinemeta.strem.io/meta/"+t+"/"+tt+".json", &doc); err != nil {
			continue
		}
		if doc.Meta == nil || !strings.EqualFold(doc.Meta.ID, tt) || strings.TrimSpace(doc.Meta.Name) == "" {
			continue
		}
		results = append(results, result{t, doc.Meta})
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no metadata found for %s (check the IMDb link)", tt)
	}
	best := results[0]
	bestScore := -1
	for _, r := range results {
		score := 0
		if r.meta.Poster != "" {
			score++
		}
		if len(r.meta.Genres) > 0 {
			score++
		}
		if len(r.meta.Cast) > 0 {
			score++
		}
		if r.meta.Type == r.asked {
			score++
		}
		if score > bestScore {
			bestScore = score
			best = r
		}
	}
	return best.meta.info(tt), nil
}

type cineMeta struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Year         string   `json:"year"`
	Poster       string   `json:"poster"`
	Background   string   `json:"background"`
	Description  string   `json:"description"`
	Runtime      string   `json:"runtime"`
	ReleaseInfo  string   `json:"releaseInfo"`
	Genres       []string `json:"genre"`
	Cast         []string `json:"cast"`
	Director     []string `json:"director"`
	Writer       []string `json:"writer"`
	ImdbRating   string   `json:"imdbRating"`
	Cert         *string  `json:"certification"`
}

func (m *cineMeta) info(tt string) *Info {
	out := &Info{ID: tt, Title: m.Name, Description: m.Description, Genres: m.Genres, Cast: m.Cast, PosterURL: m.Poster, BackdropURL: m.Background, ImdbRating: m.ImdbRating}
	if out.Genres == nil {
		out.Genres = []string{}
	}
	if out.Cast == nil {
		out.Cast = []string{}
	}
	if m.Type == "series" {
		out.Type = "series"
	} else {
		out.Type = "movie"
	}
	// year: "1999" or "2008–2013"
	yf := strings.FieldsFunc(m.Year, func(r rune) bool { return r < '0' || r > '9' })
	if len(yf) > 0 {
		if y, err := strconv.Atoi(yf[0]); err == nil {
			out.Year = &y
		}
	}
	if len(m.Director) > 0 {
		out.Director = strings.Join(m.Director, ", ")
	} else if len(m.Writer) > 0 {
		out.Director = strings.Join(m.Writer, ", ")
	}
	// runtime: "136 min"
	rf := strings.Fields(m.Runtime)
	if len(rf) > 0 {
		if n, err := strconv.Atoi(rf[0]); err == nil {
			out.DurationSec = n * 60
		}
	}
	out.Maturity = "U/A 13+"
	if m.Cert != nil && *m.Cert != "" {
		out.Maturity = *m.Cert
	}
	return out
}

// DownloadArt fetches an image and stores it under public/posters,
// returning the absolute path. Content sniffing picks the extension.
func DownloadArt(publicDir, tt, kind, artURL string) (string, error) {
	if artURL == "" {
		return "", fmt.Errorf("no image url")
	}
	req, err := http.NewRequest("GET", artURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "LanFlix/1.0 (local media server)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("image returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return "", err
	}
	ext := ".jpg"
	switch http.DetectContentType(data) {
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	dir := filepath.Join(publicDir, "posters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "imdb-"+tt+"-"+kind+ext)
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return "", err
	}
	return dest, nil
}
