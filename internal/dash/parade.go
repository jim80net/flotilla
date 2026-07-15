package dash

// The Parade page — a dash-served archive of the fleet's periodic "parade" reports
// (accomplished / next / learned / need / demo). Convention: a parade directory holds
// one dated subdir per parade — <ParadesPath>/<YYYY-MM-DD>/{report.md, assets/…} — and
// the page lists them NEWEST-FIRST so the progression over time reads top-to-bottom.
// The dash reads the authored deck + images without modifying them. The only parade write
// is the separate conversations.json sidecar owned by parade_conversations.go. Reports
// render client-side via the same escape-then-markdown pipeline the decision brief uses
// (assets/parade.js), so no raw HTML from a report ever reaches the DOM.

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ParadeEntry is one archived parade: its date (the dir name), the raw slides.md deck
// source (POWERPOINT-style: "---"-separated slides, first line per slide = title, image
// refs render large), and the image asset filenames under assets/ (served via
// /parade-assets/<date>/<file>). slides.md is preferred; a legacy report.md is read as a
// fallback so an older parade still renders (as a single-deck source).
type ParadeEntry struct {
	Date   string   `json:"date"`
	Slides string   `json:"slides"`
	Assets []string `json:"assets"`
}

// paradeDateRe bounds a parade dir/date to an ISO calendar day — this is also the
// path-traversal guard for the asset route (a date can only be 10 safe chars).
var paradeDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// readParades lists the parade archive newest-first. A MISSING directory is an honest empty
// list (the page shows its empty state); any OTHER read error (bad perms, not-a-dir) is
// RETURNED so the handler surfaces an error state rather than a silent "no parades" (same
// honest-error discipline as #363). ISO dates sort lexically, so a reverse sort IS newest-first.
func readParades(dir string) ([]ParadeEntry, error) {
	out := []ParadeEntry{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil // absent archive ⇒ honest empty, not an error
		}
		return out, err
	}
	var dates []string
	for _, e := range ents {
		if e.IsDir() && paradeDateRe.MatchString(e.Name()) {
			dates = append(dates, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	for _, d := range dates {
		pd := filepath.Join(dir, d)
		slides := ""
		if b, err := os.ReadFile(filepath.Join(pd, "slides.md")); err == nil {
			slides = string(b)
		} else if b, err := os.ReadFile(filepath.Join(pd, "report.md")); err == nil {
			slides = string(b) // legacy fallback — rendered as a single-deck source
		}
		assets := []string{}
		if aents, err := os.ReadDir(filepath.Join(pd, "assets")); err == nil {
			for _, a := range aents {
				if !a.IsDir() && isParadeImage(a.Name()) {
					assets = append(assets, a.Name())
				}
			}
			sort.Strings(assets)
		}
		out = append(out, ParadeEntry{Date: d, Slides: slides, Assets: assets})
	}
	return out, nil
}

// isParadeImage gates which asset files the gallery serves — RASTER images only, so the
// route can never be pointed at report.md, an arbitrary file, or an SVG (an active
// same-origin document that can carry script; dropped to keep the gallery inert — cubic #373).
func isParadeImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

// handleParades serves the parade archive as JSON (newest-first) for parade.js. A non-absent
// read error surfaces as an "error" field so the page shows an error state, not a false empty.
func (s *Server) handleParades(w http.ResponseWriter, r *http.Request) {
	parades, err := readParades(s.cfg.ParadesPath)
	resp := map[string]any{"parades": parades}
	if err != nil {
		resp["error"] = "the parade archive could not be read"
	}
	writeJSON(w, resp)
}

// handleParadePage serves the parade page's static chrome (data arrives via /api/parades).
func (s *Server) handleParadePage(w http.ResponseWriter, r *http.Request) {
	b, err := assetsFS.ReadFile("assets/parade.html")
	if err != nil {
		http.Error(w, "parade page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

// handleParadeAsset serves one image from a parade's assets/ dir. Defence-in-depth against
// path traversal AND symlink escape: the date must be an ISO day; the file must be a bare
// image basename (no separators, no ".."); and — because a symlink dropped in assets/ could
// otherwise point at an arbitrary host file — BOTH the assets dir and the target are resolved
// with EvalSymlinks and the RESOLVED target must stay inside the RESOLVED assets dir (cubic
// #373 P1). Anything else (including a symlink that escapes, or a missing file) is a 404.
func (s *Server) handleParadeAsset(w http.ResponseWriter, r *http.Request) {
	date := r.PathValue("date")
	file := r.PathValue("file")
	if !paradeDateRe.MatchString(date) || file == "" || file != filepath.Base(file) ||
		strings.Contains(file, "..") || !isParadeImage(file) {
		http.NotFound(w, r)
		return
	}
	base := filepath.Join(s.cfg.ParadesPath, date, "assets") // Join cleans; file is a bare basename
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	realFull, err := filepath.EvalSymlinks(filepath.Join(base, file))
	if err != nil || !strings.HasPrefix(realFull, realBase+string(os.PathSeparator)) {
		http.NotFound(w, r) // missing, or a symlink escaping the resolved assets dir
		return
	}
	http.ServeFile(w, r, realFull)
}
