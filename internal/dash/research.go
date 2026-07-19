package dash

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const researchBodyLimit = 2 << 20

var researchIDRE = regexp.MustCompile(`^[a-f0-9]{24}$`)

type researchEntry struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Path       string    `json:"path"`
	State      string    `json:"state"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
	Body       string    `json:"body,omitempty"`
	rawBody    string
}

func researchID(rel string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(rel)))
	return hex.EncodeToString(sum[:12])
}

func researchState(body string) string {
	if len(body) > 4096 {
		body = body[:4096]
	}
	for _, raw := range strings.Split(strings.ToLower(body), "\n") {
		line := strings.Trim(strings.TrimSpace(raw), "*_`>#- ")
		isStatus := strings.HasPrefix(line, "status:") || strings.HasPrefix(line, "status ")
		if (isStatus || strings.HasPrefix(line, "design only")) && strings.Contains(line, "design only") {
			return "design_only"
		}
		if (isStatus || strings.HasPrefix(line, "awaiting authority") || strings.HasPrefix(line, "awaiting-auth")) &&
			(strings.Contains(line, "awaiting authority") || strings.Contains(line, "awaiting-auth")) {
			return "awaiting_authority"
		}
	}
	return "reference"
}

func researchTitle(rel, body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			if title := strings.TrimSpace(strings.TrimPrefix(line, "# ")); title != "" {
				return title
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	return strings.ReplaceAll(base, "-", " ")
}

func readResearch(dir string) ([]researchEntry, error) {
	entries := []researchEntry{}
	root, err := os.OpenRoot(dir)
	if os.IsNotExist(err) {
		return entries, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	err = fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			if !d.IsDir() {
				return errors.New("research library root is not a directory")
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > researchBodyLimit {
			return nil
		}
		body, err := root.ReadFile(path)
		if err != nil {
			return err
		}
		rel := filepath.FromSlash(path)
		if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return errors.New("research library path escaped its root")
		}
		entries = append(entries, researchEntry{
			ID: researchID(rel), Title: researchTitle(rel, string(body)), Path: filepath.ToSlash(rel),
			State: researchState(string(body)), ModifiedAt: info.ModTime().UTC(), Size: info.Size(), rawBody: string(body),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	priority := func(state string) int {
		switch state {
		case "design_only":
			return 0
		case "awaiting_authority":
			return 1
		default:
			return 2
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if priority(entries[i].State) != priority(entries[j].State) {
			return priority(entries[i].State) < priority(entries[j].State)
		}
		if !entries[i].ModifiedAt.Equal(entries[j].ModifiedAt) {
			return entries[i].ModifiedAt.After(entries[j].ModifiedAt)
		}
		return entries[i].Path < entries[j].Path
	})
	return entries, nil
}

func (s *Server) handleResearchIndex(w http.ResponseWriter, _ *http.Request) {
	entries, err := readResearch(s.cfg.ResearchPath)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "the research library could not be read"})
		return
	}
	writeJSON(w, map[string]any{"documents": entries, "count": len(entries)})
}

func (s *Server) handleResearchDocument(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !researchIDRE.MatchString(id) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid research document id"})
		return
	}
	entries, err := readResearch(s.cfg.ResearchPath)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "the research library could not be read"})
		return
	}
	for _, entry := range entries {
		if entry.ID != id {
			continue
		}
		entry.Body = entry.rawBody
		writeJSON(w, map[string]any{"document": entry})
		return
	}
	writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "research document not found"})
}

func (s *Server) handleResearchPage(w http.ResponseWriter, _ *http.Request) {
	body, err := assetsFS.ReadFile("assets/research.html")
	if err != nil {
		http.Error(w, "research page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}
