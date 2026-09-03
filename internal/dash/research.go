package dash

// The Research library is a read-only private-dash view over operator-facing
// Markdown. The configured root is the publication boundary: only regular,
// non-hidden .md files discovered beneath it enter the index, and the body route
// applies the same file rules to exact IDs. Symlinks and arbitrary
// request-derived paths never become readable host files.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/sys/unix"
)

const maxResearchDocumentBytes = 4 << 20

const researchPresentationProbeMarker = `data-flotilla-presentation-probe`

type ResearchEntry struct {
	ID                string               `json:"id"`
	Title             string               `json:"title"`
	Summary           string               `json:"summary,omitempty"`
	UpdatedAt         string               `json:"updated_at"`
	Status            string               `json:"status"`
	Tags              []string             `json:"tags"`
	Decision          bool                 `json:"decision"`
	Archival          bool                 `json:"archival"`
	Publication       ResearchPublication  `json:"publication"`
	PublicationValid  bool                 `json:"publication_valid"`
	Diagnostics       []ResearchDiagnostic `json:"diagnostics"`
	PublicationState  string               `json:"publication_state"`
	PresentationReady bool                 `json:"presentation_ready"`
	PresentationURL   string               `json:"presentation_url,omitempty"`
	LearnReady        bool                 `json:"learn_ready"`
}

// ResearchPublication is explicit author-owned publication metadata. It is read
// from a leading HTML comment so the Markdown remains portable:
//
//	<!-- flotilla-publication
//	classification: research|decision|archival
//	reader-action: What the operator should decide, do, or retain.
//	support: material|text-only
//	support-rationale: Why a text-only publication is sufficient.
//	-->
//
// A decision classification places a paper on the existing decision shelf; it
// never means GO and never changes Authorization Domains state.
type ResearchPublication struct {
	Classification   string `json:"classification,omitempty"`
	ReaderAction     string `json:"reader_action,omitempty"`
	Support          string `json:"support,omitempty"`
	SupportRationale string `json:"support_rationale,omitempty"`
	Explicit         bool   `json:"explicit"`
}

type ResearchDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ResearchDiagnosticsSummary struct {
	Documents      int            `json:"documents"`
	NeedsAttention int            `json:"needs_attention"`
	Valid          int            `json:"valid"`
	Showpieces     int            `json:"showpieces"`
	SourceOnly     int            `json:"source_only"`
	ByCode         map[string]int `json:"by_code"`
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

// publishableResearchID is the product boundary between operator-facing research
// and operational artifacts that happen to be Markdown. Content quality is
// handled by readiness diagnostics; these known artifact classes never enter the
// library or its body route.
func publishableResearchID(id string) bool {
	if !validResearchID(id) {
		return false
	}
	parts := strings.Split(strings.ToLower(id), "/")
	base := parts[len(parts)-1]
	stem := strings.TrimSuffix(base, path.Ext(base))
	if base == "findings.md" || strings.Contains(stem, "scorecard") ||
		strings.Contains(stem, "process-dump") || strings.Contains(stem, "process_dump") ||
		strings.Contains(stem, "process-snapshot") || strings.Contains(stem, "process_snapshot") ||
		strings.Contains(stem, "session-dump") || strings.Contains(stem, "session_dump") ||
		strings.Contains(stem, "pane-dump") || strings.Contains(stem, "pane_dump") {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "walk" || part == "walks" || strings.HasPrefix(part, "walk-") ||
			strings.HasSuffix(part, "-walk") || strings.Contains(part, "-walk-") ||
			strings.Contains(part, "walk-package") || strings.Contains(part, "session-mirror") ||
			strings.Contains(part, "process-dump") || strings.Contains(part, "process_snapshot") {
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

func validResearchPresentationID(id string) bool {
	if id == "" || strings.ContainsRune(id, '\x00') || strings.Contains(id, `\`) ||
		strings.HasPrefix(id, "/") || path.Clean(id) != id {
		return false
	}
	parts := strings.Split(id, "/")
	presentationAt := -1
	for i, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
		if part == "presentation" {
			presentationAt = i
		}
	}
	if path.Base(id) == "SOURCE.md" && path.Dir(id) != "." {
		return true
	}
	if presentationAt < 1 || presentationAt == len(parts)-1 {
		return false
	}
	switch strings.ToLower(path.Ext(id)) {
	case ".html", ".css", ".js", ".json", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif",
		".mp4", ".webm", ".ogv", ".woff", ".woff2":
		return true
	default:
		return false
	}
}

func openResearchPresentation(root, id string) (*os.File, os.FileInfo, bool, error) {
	if !validResearchPresentationID(id) {
		return nil, nil, false, nil
	}
	sourceID := id
	if path.Base(id) != "SOURCE.md" {
		presentationAt := strings.Index(id, "/presentation/")
		if presentationAt < 1 {
			return nil, nil, false, nil
		}
		sourceID = id[:presentationAt] + "/SOURCE.md"
	}
	if !publishableResearchID(sourceID) {
		return nil, nil, false, nil
	}
	source, _, found, err := openResearchRegularNoFollow(root, sourceID)
	if source != nil {
		_ = source.Close()
	}
	if err != nil || !found {
		return nil, nil, false, err
	}
	return openResearchRegularNoFollow(root, id)
}

func openResearchRegularNoFollow(root, id string) (*os.File, os.FileInfo, bool, error) {
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || err == unix.ELOOP || err == unix.ENOTDIR {
			return nil, nil, false, nil
		}
		return nil, nil, false, err
	}
	currentFD := rootFD
	parts := strings.Split(id, "/")
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		nextFD, openErr := unix.Openat(currentFD, part, flags, 0)
		if currentFD != rootFD {
			_ = unix.Close(currentFD)
		}
		if openErr != nil {
			_ = unix.Close(rootFD)
			if errors.Is(openErr, os.ErrNotExist) || openErr == unix.ELOOP || openErr == unix.ENOTDIR {
				return nil, nil, false, nil
			}
			return nil, nil, false, openErr
		}
		currentFD = nextFD
	}
	_ = unix.Close(rootFD)
	file := os.NewFile(uintptr(currentFD), filepath.Base(id))
	if file == nil {
		_ = unix.Close(currentFD)
		return nil, nil, false, fmt.Errorf("open research presentation")
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

func researchPresentation(root, sourceID string) (string, bool) {
	if path.Base(sourceID) != "SOURCE.md" || path.Dir(sourceID) == "." {
		return "", false
	}
	presentationID := path.Join(path.Dir(sourceID), "presentation", "index.html")
	file, info, found, err := openResearchPresentation(root, presentationID)
	if file != nil {
		defer file.Close()
	}
	if err != nil || !found || !usableResearchPresentation(file, info) {
		return "", false
	}
	return "/research-presentations/" + presentationID, true
}

// usableResearchPresentation is the package admission boundary for an embedded
// preview. A successful iframe load is only transport evidence: empty HTML and
// non-rendering DOM also load, but render the blank slab this gate exists to
// prevent. Parse the HTML tree and require substantive rendered text inside a
// presentation-shaped region before advertising it as ready.
func usableResearchPresentation(file *os.File, info os.FileInfo) bool {
	if file == nil || info == nil || info.Size() <= 0 || info.Size() > maxResearchDocumentBytes {
		return false
	}
	doc, err := html.Parse(io.LimitReader(file, maxResearchDocumentBytes+1))
	if err != nil {
		return false
	}
	return len([]rune(strings.Join(strings.Fields(researchPresentationVisibleText(doc, false)), " "))) >= 8
}

func researchPresentationVisibleText(node *html.Node, inPresentation bool) string {
	if node.Type == html.ElementNode {
		// Non-HTML namespaces need their own rendering model (for example SVG
		// defs versus text). Do not guess that their text nodes paint pixels.
		if node.Namespace != "" || researchPresentationNodeHidden(node) {
			return ""
		}
		switch node.Data {
		case "main", "section", "article":
			inPresentation = true
		}
	}
	if node.Type == html.TextNode {
		if inPresentation {
			return node.Data
		}
		return ""
	}
	var text strings.Builder
	closedDetails := node.Type == html.ElementNode && node.Data == "details" && !researchPresentationHasAttr(node, "open", "")
	visibleSummarySeen := false
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if closedDetails {
			if child.Type != html.ElementNode || child.Data != "summary" || visibleSummarySeen {
				continue
			}
			visibleSummarySeen = true
		}
		text.WriteString(researchPresentationVisibleText(child, inPresentation))
		text.WriteByte(' ')
	}
	return text.String()
}

func researchPresentationNodeHidden(node *html.Node) bool {
	switch node.Data {
	case "head", "title", "script", "style", "template", "noscript", "noembed", "noframes", "datalist",
		"iframe", "object", "embed", "canvas", "audio", "video", "source", "track", "param", "rp":
		return true
	case "dialog":
		if !researchPresentationHasAttr(node, "open", "") {
			return true
		}
	case "input":
		if researchPresentationHasAttr(node, "type", "hidden") {
			return true
		}
	}
	for _, attr := range node.Attr {
		if attr.Key == "hidden" {
			return true
		}
		if attr.Key == "style" {
			if researchPresentationInlineStyleHidden(attr.Val) {
				return true
			}
		}
	}
	return false
}

func researchPresentationInlineStyleHidden(style string) bool {
	for _, declaration := range strings.Split(style, ";") {
		property, value, ok := strings.Cut(declaration, ":")
		if !ok {
			continue
		}
		property = strings.ToLower(strings.TrimSpace(property))
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.TrimSpace(strings.TrimSuffix(value, "!important"))
		switch property {
		case "display":
			if value == "none" {
				return true
			}
		case "visibility":
			if value == "hidden" || value == "collapse" {
				return true
			}
		case "content-visibility":
			if value == "hidden" {
				return true
			}
		case "opacity":
			percent := strings.HasSuffix(value, "%")
			if percent {
				value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
			}
			opacity, err := strconv.ParseFloat(value, 64)
			if percent {
				opacity /= 100
			}
			if err == nil && opacity <= 0 {
				return true
			}
		}
	}
	return false
}

func researchPresentationHasAttr(node *html.Node, key, value string) bool {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) && (value == "" || strings.EqualFold(strings.TrimSpace(attr.Val), value)) {
			return true
		}
	}
	return false
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
		// Catalog cards render summaries as plain text. Remove strong delimiters
		// before the byte limit can separate an opener from its closer.
		line = strings.NewReplacer("**", "", "__", "").Replace(line)
		const max = 220
		if len(line) > max {
			line = strings.TrimSpace(line[:max-1]) + "…"
		}
		return line
	}
	return ""
}

func parseResearchPublication(markdown string) (ResearchPublication, []ResearchDiagnostic) {
	const opener = "<!-- flotilla-publication"
	trimmed := strings.TrimLeft(markdown, "\ufeff \t\r\n")
	if !strings.HasPrefix(trimmed, opener) {
		return ResearchPublication{}, nil
	}
	start := len(markdown) - len(trimmed)
	endOffset := strings.Index(markdown[start+len(opener):], "-->")
	if endOffset < 0 {
		return ResearchPublication{Explicit: true}, []ResearchDiagnostic{{
			Code: "metadata.malformed", Message: "Publication directive is not closed with -->.",
		}}
	}
	body := markdown[start+len(opener) : start+len(opener)+endOffset]
	publication := ResearchPublication{Explicit: true}
	diagnostics := []ResearchDiagnostic{}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			diagnostics = append(diagnostics, ResearchDiagnostic{Code: "metadata.malformed", Message: "Publication directive lines must use key: value."})
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "classification":
			publication.Classification = strings.ToLower(value)
		case "reader-action":
			publication.ReaderAction = value
		case "support":
			publication.Support = strings.ToLower(value)
		case "support-rationale":
			publication.SupportRationale = value
		default:
			diagnostics = append(diagnostics, ResearchDiagnostic{Code: "metadata.unknown", Message: "Unknown publication directive: " + key + "."})
		}
	}
	if publication.Classification != "" && publication.Classification != "research" &&
		publication.Classification != "decision" && publication.Classification != "archival" {
		diagnostics = append(diagnostics, ResearchDiagnostic{Code: "metadata.classification", Message: "Classification must be research, decision, or archival."})
	}
	if publication.Support != "" && publication.Support != "material" && publication.Support != "text-only" {
		diagnostics = append(diagnostics, ResearchDiagnostic{Code: "metadata.support", Message: "Support must be material or text-only."})
	}
	return publication, diagnostics
}

func withoutResearchPublicationDirective(markdown string) string {
	const opener = "<!-- flotilla-publication"
	trimmed := strings.TrimLeft(markdown, "\ufeff \t\r\n")
	if !strings.HasPrefix(trimmed, opener) {
		return markdown
	}
	start := len(markdown) - len(trimmed)
	endOffset := strings.Index(markdown[start+len(opener):], "-->")
	if endOffset < 0 {
		return markdown[:start]
	}
	end := start + len(opener) + endOffset + len("-->")
	return markdown[:start] + markdown[end:]
}

func researchContentDiagnostic(markdown string) *ResearchDiagnostic {
	body := strings.TrimSpace(withoutResearchPublicationDirective(markdown))
	if body == "" {
		return &ResearchDiagnostic{Code: "content.empty", Message: "Document is empty."}
	}
	lines := strings.Split(body, "\n")
	content := []string{}
	titleRemoved := false
	for _, raw := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if !titleRemoved && strings.HasPrefix(line, "# ") {
			titleRemoved = true
			continue
		}
		lower := strings.ToLower(line)
		if line == "" || line == "---" || strings.HasPrefix(lower, "status:") || strings.HasPrefix(lower, "state:") {
			continue
		}
		line = strings.Trim(line, "#>*_`- |0123456789.\t")
		if line != "" {
			content = append(content, line)
		}
	}
	if len(content) == 0 {
		return &ResearchDiagnostic{Code: "content.title_only", Message: "Document contains a title or metadata but no substantive body."}
	}
	normalized := strings.ToLower(strings.Join(content, " "))
	words := strings.Fields(normalized)
	boilerplate := strings.Contains(normalized, "lorem ipsum") || strings.Contains(normalized, "coming soon") ||
		strings.Contains(normalized, "placeholder") || strings.Contains(normalized, "todo") || strings.Contains(normalized, "tbd")
	if boilerplate && len(words) <= 24 {
		return &ResearchDiagnostic{Code: "content.boilerplate", Message: "Document body appears to contain only boilerplate or placeholder text."}
	}
	return nil
}

func researchHasSupportingMaterial(markdown string) bool {
	body := withoutResearchPublicationDirective(markdown)
	lines := strings.Split(body, "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.Contains(line, "](") || strings.HasPrefix(line, "![") {
			return true
		}
		if strings.Contains(line, "|") && i+1 < len(lines) {
			delimiter := strings.TrimSpace(lines[i+1])
			if strings.Contains(delimiter, "|") && strings.Contains(delimiter, "---") {
				return true
			}
		}
	}
	return false
}

func researchPublicationDiagnostics(markdown string, publication ResearchPublication, metadataDiagnostics []ResearchDiagnostic) []ResearchDiagnostic {
	diagnostics := append([]ResearchDiagnostic{}, metadataDiagnostics...)
	if content := researchContentDiagnostic(markdown); content != nil {
		diagnostics = append(diagnostics, *content)
	}
	if strings.TrimSpace(publication.ReaderAction) == "" {
		diagnostics = append(diagnostics, ResearchDiagnostic{
			Code: "action.missing", Message: "Add one explicit reader action: a decision, next step, or archival reason.",
		})
	}
	hasMaterial := researchHasSupportingMaterial(markdown)
	textOnly := publication.Support == "text-only" && strings.TrimSpace(publication.SupportRationale) != ""
	if !hasMaterial && !textOnly {
		diagnostics = append(diagnostics, ResearchDiagnostic{
			Code: "support.missing", Message: "Add supporting material or declare text-only with a rationale.",
		})
	}
	return diagnostics
}

func researchStatus(title, markdown string, publication ResearchPublication) (string, []string, bool, bool) {
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
		plain := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(line, "**", ""), "`", ""))
		if key, value, ok := strings.Cut(plain, ":"); ok {
			key = strings.TrimSpace(strings.TrimLeft(key, "-* "))
			if key == "status" || key == "state" {
				markers = append(markers, strings.TrimSpace(value))
			}
		}
		if strings.Contains(line, "[awaiting-auth]") || strings.HasPrefix(line, "# ") {
			markers = append(markers, line)
		}
	}
	sample := strings.Join(markers, "\n")
	status, tags, decision := "research", []string{"research"}, false
	switch {
	case containsResearchToken(sample, "awaiting-auth") || containsResearchToken(sample, "awaiting auth"):
		status, tags, decision = "awaiting-auth", []string{"decision", "awaiting-auth"}, true
	case strings.Contains(sample, "design only") || strings.Contains(sample, "awaiting go") || strings.Contains(sample, "awaiting-go"):
		status, tags, decision = "design-only", []string{"decision", "design-only"}, true
	case strings.Contains(sample, "operator review") || strings.Contains(sample, "operator-review"):
		status, tags, decision = "operator-review", []string{"decision", "operator-review"}, true
	}
	switch publication.Classification {
	case "archival":
		return "archival", []string{"research", "archival"}, false, true
	case "decision":
		if decision {
			// Preserve the existing awaiting/design-only/operator-review state.
			// The publication directive classifies; it never upgrades to GO.
			return status, tags, true, false
		}
		return "decision", []string{"decision"}, true, false
	case "research":
		if decision {
			return status, tags, true, false
		}
		return "research", []string{"research"}, false, false
	}
	return status, tags, decision, false
}

func containsResearchToken(value, token string) bool {
	for offset := 0; offset <= len(value)-len(token); {
		i := strings.Index(value[offset:], token)
		if i < 0 {
			return false
		}
		i += offset
		beforeOK := i == 0 || !researchTokenChar(value[i-1])
		after := i + len(token)
		afterOK := after == len(value) || !researchTokenChar(value[after])
		if beforeOK && afterOK {
			return true
		}
		offset = i + 1
	}
	return false
}

func researchTokenChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
}

func researchEntry(id, markdown string, modTime time.Time) ResearchEntry {
	title := researchTitle(id, markdown)
	publication, metadataDiagnostics := parseResearchPublication(markdown)
	status, tags, decision, archival := researchStatus(title, markdown, publication)
	diagnostics := researchPublicationDiagnostics(markdown, publication, metadataDiagnostics)
	return ResearchEntry{
		ID:               id,
		Title:            title,
		Summary:          researchSummary(withoutResearchPublicationDirective(markdown)),
		UpdatedAt:        modTime.UTC().Format(time.RFC3339),
		Status:           status,
		Tags:             tags,
		Decision:         decision,
		Archival:         archival,
		Publication:      publication,
		PublicationValid: len(diagnostics) == 0,
		Diagnostics:      diagnostics,
		PublicationState: "source-only",
	}
}

func applyResearchPresentationReadiness(root string, entry *ResearchEntry) {
	if presentationURL, ready := researchPresentation(root, entry.ID); ready {
		entry.PublicationState = "showpiece"
		entry.PresentationReady = true
		entry.PresentationURL = presentationURL
		entry.PublicationValid = len(entry.Diagnostics) == 0
		entry.LearnReady = educationalResearchReady(*entry)
		return
	}
	if !entry.Archival {
		entry.Diagnostics = append(entry.Diagnostics, ResearchDiagnostic{
			Code:    "presentation.missing",
			Message: "Add a self-contained HTML5 presentation package; the source remains visible but is not showpiece-ready.",
		})
	}
	entry.PublicationValid = len(entry.Diagnostics) == 0
	entry.LearnReady = educationalResearchReady(*entry)
}

// educationalResearchReady is the operator-facing Learn admission boundary.
// A plausible title, a Markdown file, or presentation assets alone are never
// enough: the author must explicitly publish teaching-quality research and the
// complete HTML5 showpiece must pass every publication check.
func educationalResearchReady(entry ResearchEntry) bool {
	return entry.Publication.Explicit &&
		entry.Publication.Classification == "research" &&
		entry.PublicationValid &&
		entry.PresentationReady &&
		!entry.Decision &&
		!entry.Archival
}

func summarizeResearchDiagnostics(entries []ResearchEntry) ResearchDiagnosticsSummary {
	summary := ResearchDiagnosticsSummary{Documents: len(entries), ByCode: map[string]int{}}
	for _, entry := range entries {
		if entry.PresentationReady {
			summary.Showpieces++
		} else {
			summary.SourceOnly++
		}
		if len(entry.Diagnostics) == 0 {
			summary.Valid++
			continue
		}
		summary.NeedsAttention++
		for _, diagnostic := range entry.Diagnostics {
			summary.ByCode[diagnostic.Code]++
		}
	}
	return summary
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
		if !publishableResearchID(id) {
			return nil
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		entry := researchEntry(id, string(body), info.ModTime())
		applyResearchPresentationReadiness(root, &entry)
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return entries, nil
		}
		return nil, err
	}
	showpiecePackages := map[string]bool{}
	for _, entry := range entries {
		if entry.PresentationReady && path.Base(entry.ID) == "SOURCE.md" {
			showpiecePackages[path.Dir(entry.ID)] = true
		}
	}
	filtered := entries[:0]
	for _, entry := range entries {
		legacyStem := strings.TrimSuffix(entry.ID, path.Ext(entry.ID))
		if path.Base(entry.ID) != "SOURCE.md" && showpiecePackages[legacyStem] {
			continue
		}
		filtered = append(filtered, entry)
	}
	entries = filtered
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Decision != entries[j].Decision {
			return entries[i].Decision
		}
		if entries[i].PresentationReady != entries[j].PresentationReady {
			return entries[i].PresentationReady
		}
		if entries[i].UpdatedAt != entries[j].UpdatedAt {
			return entries[i].UpdatedAt > entries[j].UpdatedAt
		}
		return entries[i].ID < entries[j].ID
	})
	return entries, nil
}

func readResearchDocument(root, id string) (ResearchDocument, bool, error) {
	if !publishableResearchID(id) {
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
	applyResearchPresentationReadiness(root, &entry)
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
	writeJSON(w, map[string]any{"research": entries, "diagnostics": summarizeResearchDiagnostics(entries)})
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

func (s *Server) handleResearchPresentation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	file, info, found, err := openResearchPresentation(s.cfg.ResearchPath, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research presentation could not be read")
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
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(info.Name()))); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if strings.EqualFold(filepath.Ext(info.Name()), ".html") {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'none'; frame-ancestors 'self'; base-uri 'none'; form-action 'none'")
	}
	if path.Base(id) == "index.html" && info.Size() <= maxResearchDocumentBytes {
		body, readErr := io.ReadAll(io.LimitReader(file, maxResearchDocumentBytes+1))
		if readErr != nil || len(body) > maxResearchDocumentBytes {
			writeError(w, http.StatusInternalServerError, "the research presentation could not be read")
			return
		}
		probe, probeErr := assetsFS.ReadFile("assets/research-presentation-ready.js")
		if probeErr != nil {
			writeError(w, http.StatusInternalServerError, "the research presentation readiness probe is unavailable")
			return
		}
		body = injectResearchPresentationProbe(body, probe)
		http.ServeContent(w, r, info.Name(), info.ModTime(), bytes.NewReader(body))
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func injectResearchPresentationProbe(body, script []byte) []byte {
	probe := make([]byte, 0, len(script)+64)
	probe = append(probe, `<script data-flotilla-presentation-probe>`...)
	probe = append(probe, script...)
	probe = append(probe, `</script>`...)
	at := researchPresentationProbeInsertion(body)
	if at < 0 || at > len(body) {
		at = 0
	}
	result := make([]byte, 0, len(body)+len(probe))
	result = append(result, body[:at]...)
	result = append(result, probe...)
	result = append(result, body[at:]...)
	return result
}

// researchPresentationProbeInsertion keeps a leading HTML5 doctype (and any
// preceding whitespace/comments) ahead of the inspector so standards mode is
// preserved. Otherwise the inspector is byte zero. In both cases the bridge is
// ready before package startup and before the outer viewer sends its probe.
func researchPresentationProbeInsertion(body []byte) int {
	offset := 0
	if bytes.HasPrefix(body, []byte{0xef, 0xbb, 0xbf}) {
		offset = 3
	}
	for offset < len(body) {
		for offset < len(body) && strings.ContainsRune(" \t\r\n\f", rune(body[offset])) {
			offset++
		}
		if !bytes.HasPrefix(body[offset:], []byte("<!--")) {
			break
		}
		end := bytes.Index(body[offset+4:], []byte("-->"))
		if end < 0 {
			return 0
		}
		offset += 4 + end + 3
	}
	const doctype = "<!doctype"
	if len(body)-offset < len(doctype) || !bytes.EqualFold(body[offset:offset+len(doctype)], []byte(doctype)) {
		return 0
	}
	end := bytes.IndexByte(body[offset+len(doctype):], '>')
	if end < 0 {
		return 0
	}
	return offset + len(doctype) + end + 1
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
