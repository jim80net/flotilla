package dash

// The Research library is a read-only private-dash view over operator-facing
// Markdown. The configured root is the publication boundary: only regular,
// non-hidden .md files discovered beneath it enter the index, and the body route
// applies the same file rules to exact IDs. Symlinks and arbitrary
// request-derived paths never become readable host files.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxResearchDocumentBytes = 4 << 20

type ResearchEntry struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Summary   string   `json:"summary,omitempty"`
	UpdatedAt string   `json:"updated_at"`
	Status    string   `json:"status"`
	Tags      []string `json:"tags"`
	Decision  bool     `json:"decision"`
}

type ResearchDocument struct {
	ResearchEntry
	Markdown string `json:"markdown"`
	Digest   string `json:"digest"`
}

func researchDigest(markdown string) string {
	sum := sha256.Sum256([]byte(markdown))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validResearchID(id string) bool {
	if id == "" || strings.ContainsRune(id, '\x00') || strings.Contains(id, `\`) ||
		strings.HasPrefix(id, "/") || path.Clean(id) != id || !strings.EqualFold(path.Ext(id), ".md") {
		return false
	}
	for _, part := range strings.Split(id, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func validResearchVideoID(id string) bool {
	if id == "" || strings.ContainsRune(id, '\x00') || strings.Contains(id, `\`) ||
		strings.HasPrefix(id, "/") || path.Clean(id) != id {
		return false
	}
	switch strings.ToLower(path.Ext(id)) {
	case ".mp4", ".webm", ".ogv":
	default:
		return false
	}
	for _, part := range strings.Split(id, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func openResearchVideo(root, id string) (*os.File, os.FileInfo, bool, error) {
	if !validResearchVideoID(id) {
		return nil, nil, false, nil
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	if !rootInfo.IsDir() {
		return nil, nil, false, fmt.Errorf("research root is not a directory")
	}
	full := root
	parts := strings.Split(id, "/")
	for i, part := range parts {
		full = filepath.Join(full, part)
		info, statErr := os.Lstat(full)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return nil, nil, false, nil
			}
			return nil, nil, false, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return nil, nil, false, nil
		}
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, false, err
	}
	fullReal, err := filepath.EvalSymlinks(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	rel, err := filepath.Rel(rootReal, fullReal)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, nil, false, nil
	}
	file, err := os.Open(fullReal)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, false, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, false, nil
	}
	return file, info, true, nil
}

func researchTitle(id, markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(line, "# ") {
			if title := strings.TrimSpace(strings.TrimPrefix(line, "# ")); title != "" {
				return title
			}
		}
	}
	base := strings.TrimSuffix(path.Base(id), path.Ext(id))
	return strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(base))
}

func researchSummary(markdown string) string {
	inFence := false
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		if inFence || line == "" || line == "---" || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "|") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") ||
			strings.HasPrefix(line, ">") || (line[0] >= '0' && line[0] <= '9') {
			continue
		}
		line = strings.Trim(line, "*_` ")
		if line == "" || strings.Contains(line, ":**") {
			continue
		}
		const max = 220
		if len(line) > max {
			line = strings.TrimSpace(line[:max-1]) + "…"
		}
		return line
	}
	return ""
}

func researchStatus(title, markdown string) (string, []string, bool) {
	// Decision classification is metadata-shaped, not a full-text search. A paper
	// that merely discusses "operator review" later in its body must not land on
	// the waiting shelf. Accept the title plus explicit status/state markers near
	// the top of the document (including awaiting-auth tokens).
	markers := []string{strings.ToLower(title)}
	lines := strings.Split(markdown, "\n")
	if len(lines) > 40 {
		lines = lines[:40]
	}
	for _, line := range lines {
		line = strings.ToLower(strings.TrimSpace(line))
		if strings.Contains(line, "status:") || strings.Contains(line, "state:") ||
			strings.Contains(line, "awaiting-auth") || strings.HasPrefix(line, "# ") {
			markers = append(markers, line)
		}
	}
	sample := strings.Join(markers, "\n")
	switch {
	case strings.Contains(sample, "awaiting-auth") || strings.Contains(sample, "awaiting auth"):
		return "awaiting-auth", []string{"decision", "awaiting-auth"}, true
	case strings.Contains(sample, "design only") || strings.Contains(sample, "awaiting go") || strings.Contains(sample, "awaiting-go"):
		return "design-only", []string{"decision", "design-only"}, true
	case strings.Contains(sample, "operator review") || strings.Contains(sample, "operator-review"):
		return "operator-review", []string{"decision", "operator-review"}, true
	default:
		return "research", []string{"research"}, false
	}
}

func researchEntry(id, markdown string, modTime time.Time) ResearchEntry {
	title := researchTitle(id, markdown)
	status, tags, decision := researchStatus(title, markdown)
	return ResearchEntry{
		ID:        id,
		Title:     title,
		Summary:   researchSummary(markdown),
		UpdatedAt: modTime.UTC().Format(time.RFC3339),
		Status:    status,
		Tags:      tags,
		Decision:  decision,
	}
}

func readResearchIndex(root string) ([]ResearchEntry, error) {
	entries := []ResearchEntry{}
	rootInfo, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return entries, nil
		}
		return nil, err
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("research root is not a directory")
	}
	err = filepath.WalkDir(root, func(file string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if file != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > maxResearchDocumentBytes {
			return nil
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		id := filepath.ToSlash(rel)
		if !validResearchID(id) {
			return nil
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		entries = append(entries, researchEntry(id, string(body), info.ModTime()))
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return entries, nil
		}
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Decision != entries[j].Decision {
			return entries[i].Decision
		}
		if entries[i].UpdatedAt != entries[j].UpdatedAt {
			return entries[i].UpdatedAt > entries[j].UpdatedAt
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func readResearchDocument(root, id string) (ResearchDocument, bool, error) {
	if !validResearchID(id) {
		return ResearchDocument{}, false, nil
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ResearchDocument{}, false, nil
		}
		return ResearchDocument{}, false, err
	}
	if !rootInfo.IsDir() {
		return ResearchDocument{}, false, fmt.Errorf("research root is not a directory")
	}
	// Reject a symlink in every request-derived path component, even when it
	// resolves back inside the root. This keeps the body route identical to the
	// index's regular-file-only publication rule.
	full := root
	parts := strings.Split(id, "/")
	for i, part := range parts {
		full = filepath.Join(full, part)
		info, statErr := os.Lstat(full)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return ResearchDocument{}, false, nil
			}
			return ResearchDocument{}, false, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return ResearchDocument{}, false, nil
		}
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ResearchDocument{}, false, err
	}
	fullReal, err := filepath.EvalSymlinks(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ResearchDocument{}, false, nil
		}
		return ResearchDocument{}, false, err
	}
	rel, err := filepath.Rel(rootReal, fullReal)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ResearchDocument{}, false, nil
	}
	info, err := os.Stat(fullReal)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxResearchDocumentBytes {
		return ResearchDocument{}, false, err
	}
	body, err := os.ReadFile(fullReal)
	if err != nil {
		return ResearchDocument{}, false, err
	}
	entry := researchEntry(id, string(body), info.ModTime())
	markdown := string(body)
	return ResearchDocument{ResearchEntry: entry, Markdown: markdown, Digest: researchDigest(markdown)}, true, nil
}

func (s *Server) handleResearchIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	entries, err := readResearchIndex(s.cfg.ResearchPath)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"research": []ResearchEntry{}, "error": "the research library could not be read"})
		return
	}
	writeJSON(w, map[string]any{"research": entries})
}

func (s *Server) handleResearchDocument(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	doc, found, err := readResearchDocument(s.cfg.ResearchPath, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research document could not be read")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "research document not found")
		return
	}
	writeJSON(w, doc)
}

func (s *Server) handleResearchVideo(w http.ResponseWriter, r *http.Request) {
	file, info, found, err := openResearchVideo(s.cfg.ResearchPath, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research video could not be read")
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch strings.ToLower(filepath.Ext(info.Name())) {
	case ".mp4":
		w.Header().Set("Content-Type", "video/mp4")
	case ".webm":
		w.Header().Set("Content-Type", "video/webm")
	case ".ogv":
		w.Header().Set("Content-Type", "video/ogg")
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleResearchPage(w http.ResponseWriter, _ *http.Request) {
	b, err := assetsFS.ReadFile("assets/research.html")
	if err != nil {
		http.Error(w, "research page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}
