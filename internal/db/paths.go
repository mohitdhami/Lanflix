package db

import (
	"os"
	"path/filepath"
	"strings"
)

// Path handling: LanFlix used to store ABSOLUTE filesystem paths
// (e.g. /Users/x/ghhhg/media/movies/...) in video_path / poster_path.
// Those break on server restart whenever the project folder is renamed,
// moved, checked out elsewhere, or the binary runs from a different cwd
// (Root changes, old absolute no longer exists) — even though the files
// are still present under the current ./media tree.
//
// Going forward we store paths RELATIVE to the project Root
// (e.g. "media/movies/Foo/foo.mp4", "public/posters/x.jpg") so they
// survive restarts / moves. All readers resolve via ResolvePath, which
// accepts legacy absolute values, current relative values, and heals
// stale absolute values by re-locating the file under the current tree.

func isRemote(p string) bool {
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
}

// StoredPath converts an on-disk absolute path to the DB representation.
// Paths inside the project tree become Root-relative; everything else
// (remote URLs, already-relative values, outside-tree absolutes) passes
// through (cleaned).
func StoredPath(abs string) string {
	if abs == "" || isRemote(abs) {
		return abs
	}
	if !filepath.IsAbs(abs) {
		return filepath.Clean(abs)
	}
	if Root != "" {
		if rel, err := filepath.Rel(Root, abs); err == nil && rel != "" &&
			rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return abs
}

// NormalizeStored is used for admin-supplied paths (PUT/POST): absolute
// paths inside the tree become relative, relative paths are cleaned,
// URLs and empty values pass through.
func NormalizeStored(p string) string {
	if p == "" || isRemote(p) {
		return p
	}
	return StoredPath(p)
}

// ResolvePath maps a stored DB value to an absolute on-disk path.
// It handles: relative (current format), absolute (legacy + still valid),
// and stale absolute (project moved/renamed) via suffix + basename search.
func ResolvePath(stored string) string {
	if stored == "" || isRemote(stored) {
		return stored
	}
	if filepath.IsAbs(stored) {
		if st, err := os.Stat(stored); err == nil && !st.IsDir() {
			return stored
		}
		if rel, ok := relocateAbsolute(stored); ok {
			return rel
		}
		return stored
	}
	abs := filepath.Join(Root, stored)
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		return abs
	}
	// Relative value missing (e.g. folder renamed): try suffix + basename.
	if rel, ok := relocateAbsolute(abs); ok {
		return rel
	}
	if found, ok := FindMediaFile(filepath.Base(stored)); ok {
		return found
	}
	return abs
}

// ExistsLocal reports whether a stored path refers to a file present on disk.
// Remote URLs and empty values return false.
func ExistsLocal(stored string) bool {
	if stored == "" || isRemote(stored) {
		return false
	}
	abs := ResolvePath(stored)
	st, err := os.Stat(abs)
	return err == nil && !st.IsDir()
}

// relocateAbsolute handles stale absolute paths from a previous checkout,
// e.g. /Users/x/ghhhg/media/series/Foo/bar.mkv when Root is now
// /Users/x/LANFLIX. It extracts the "media/..." / "public/..." suffix and
// re-roots it, falling back to a basename search.
func relocateAbsolute(abs string) (string, bool) {
	// Normalise separators for suffix search.
	slash := filepath.ToSlash(abs)
	lower := strings.ToLower(slash)
	for _, marker := range []string{"/media/", "/public/"} {
		if i := strings.LastIndex(lower, marker); i >= 0 {
			// keep original case suffix
			suffix := slash[i+1:] // "media/..." without leading slash
			candidate := filepath.Join(Root, filepath.FromSlash(suffix))
			if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
				return candidate, true
			}
			// Suffix exists in tree layout but file itself moved/renamed:
			// fall through to basename search.
			break
		}
	}
	if found, ok := FindMediaFile(filepath.Base(abs)); ok {
		return found, true
	}
	return "", false
}

// FindMediaFile searches the current media + posters trees for a basename.
// Returns the first match. Used only for healing missing references, so a
// full walk on a miss is acceptable (hits are O(1) stat calls).
func FindMediaFile(base string) (string, bool) {
	if base == "" || base == "." || base == "/" {
		return "", false
	}
	roots := []string{MediaRoot(), filepath.Join(PublicDir(), "posters"), PublicDir()}
	var found string
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || found != "" {
				return nil
			}
			if !d.IsDir() && d.Name() == base {
				found = p
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found, true
		}
	}
	return "", false
}
