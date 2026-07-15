package dash

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/dash/control"
	"github.com/jim80net/flotilla/internal/outbox"
)

const (
	paradeConversationSchema = 1
	paradeMessageTextCap     = 4_000
)

var (
	errParadeDate      = errors.New("invalid parade date")
	errParadeNotFound  = errors.New("parade not found")
	errParadeSlide     = errors.New("invalid parade slide")
	paradeConvoLocks   sync.Map // conversations.json path → *sync.Mutex
	paradeMessageKinds = map[string]bool{"kudos": true, "invest": true, "feedback": true, "note": true}
)

// ParadeConversationMessage is one operator-authored reaction to a parade slide.
type ParadeConversationMessage struct {
	ID     string `json:"id"`
	TS     string `json:"ts"`
	Author string `json:"author"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
}

// ParadeSlideConversation stores the latest title snapshot alongside the messages
// keyed to a slide index. Reordering slides may orphan a thread in MVP; the snapshot
// preserves the human context.
type ParadeSlideConversation struct {
	Title    string                      `json:"title"`
	Messages []ParadeConversationMessage `json:"messages"`
}

// ParadeConversations is the archive-local conversations.json document.
type ParadeConversations struct {
	Schema int                                `json:"schema"`
	Slides map[string]ParadeSlideConversation `json:"slides"`
}

type paradeMessageRequest struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

type paradeMessageResponse struct {
	Slide    ParadeSlideConversation `json:"slide"`
	Delivery string                  `json:"delivery"` // delivered | queued
	QueuedID string                  `json:"queued_id,omitempty"`
}

func emptyParadeConversations() ParadeConversations {
	return ParadeConversations{Schema: paradeConversationSchema, Slides: map[string]ParadeSlideConversation{}}
}

func loadParadeConversations(path string) (ParadeConversations, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyParadeConversations(), nil
		}
		return ParadeConversations{}, fmt.Errorf("read parade conversations: %w", err)
	}
	var doc ParadeConversations
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ParadeConversations{}, fmt.Errorf("decode parade conversations: %w", err)
	}
	if doc.Schema != paradeConversationSchema {
		return ParadeConversations{}, fmt.Errorf("parade conversations: unsupported schema %d", doc.Schema)
	}
	if doc.Slides == nil {
		doc.Slides = map[string]ParadeSlideConversation{}
	}
	return doc, nil
}

func appendParadeConversation(path string, index int, title string, message ParadeConversationMessage) (ParadeSlideConversation, error) {
	v, _ := paradeConvoLocks.LoadOrStore(path, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	doc, err := loadParadeConversations(path)
	if err != nil {
		return ParadeSlideConversation{}, err
	}
	key := strconv.Itoa(index)
	thread := doc.Slides[key]
	thread.Title = title
	thread.Messages = append(thread.Messages, message)
	doc.Slides[key] = thread
	if err := writeParadeConversationsAtomic(path, doc); err != nil {
		return ParadeSlideConversation{}, err
	}
	return thread, nil
}

func writeParadeConversationsAtomic(path string, doc ParadeConversations) error {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode parade conversations: %w", err)
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".conversations-*.tmp")
	if err != nil {
		return fmt.Errorf("create parade conversations temp file: %w", err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod parade conversations temp file: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write parade conversations temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync parade conversations temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close parade conversations temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace parade conversations: %w", err)
	}
	ok = true
	return nil
}

func paradeArchiveDir(root, date string) (string, error) {
	if !paradeDateRe.MatchString(date) {
		return "", errParadeDate
	}
	dir := filepath.Join(root, date)
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errParadeNotFound
		}
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errParadeNotFound
	}
	return dir, nil
}

func paradeSlideTitles(dir, fallbackTitle string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "slides.md"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if os.IsNotExist(err) {
		raw, err = os.ReadFile(filepath.Join(dir, "report.md"))
	}
	if err != nil {
		if os.IsNotExist(err) {
			return []string{fallbackTitle}, nil
		}
		return nil, err
	}
	normalized := strings.ReplaceAll(string(raw), "\r\n", "\n")
	chunks := regexpSlideSeparator.Split(normalized, -1)
	titles := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		line := strings.SplitN(chunk, "\n", 2)[0]
		titles = append(titles, strings.TrimSpace(strings.TrimLeft(line, "#")))
	}
	if len(titles) == 0 {
		return []string{fallbackTitle}, nil
	}
	return titles, nil
}

var regexpSlideSeparator = regexp.MustCompile(`(?m)^---\s*$`)

func newParadeMessageID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate parade message id: %w", err)
	}
	return "pm-" + hex.EncodeToString(raw[:]), nil
}

func (s *Server) handleParadeConversations(w http.ResponseWriter, r *http.Request) {
	dir, err := paradeArchiveDir(s.cfg.ParadesPath, r.PathValue("date"))
	if err != nil {
		writeParadePathError(w, err)
		return
	}
	doc, err := loadParadeConversations(filepath.Join(dir, "conversations.json"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the parade conversation archive could not be read")
		return
	}
	writeJSON(w, doc)
}

func (s *Server) handleParadeMessage(w http.ResponseWriter, r *http.Request) {
	date := r.PathValue("date")
	dir, err := paradeArchiveDir(s.cfg.ParadesPath, date)
	if err != nil {
		writeParadePathError(w, err)
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		writeError(w, http.StatusBadRequest, errParadeSlide.Error())
		return
	}
	titles, err := paradeSlideTitles(dir, date)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the parade slides could not be read")
		return
	}
	if index >= len(titles) {
		writeError(w, http.StatusNotFound, errParadeSlide.Error())
		return
	}
	cosTarget := strings.TrimSpace(s.roster.CosAgent)
	if cosTarget == "" {
		writeError(w, http.StatusServiceUnavailable, "parade conversations require a configured cos_agent")
		return
	}
	var req paradeMessageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	if req.Kind == "" {
		req.Kind = "note"
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "message text is required")
		return
	}
	if utf8.RuneCountInString(req.Text) > paradeMessageTextCap {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("message text exceeds %d characters", paradeMessageTextCap))
		return
	}
	if !paradeMessageKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "message kind must be kudos, invest, feedback, or note")
		return
	}
	id, err := newParadeMessageID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the parade message could not be created")
		return
	}
	message := ParadeConversationMessage{
		ID: id, TS: s.now().UTC().Format(time.RFC3339), Author: "operator", Kind: req.Kind, Text: req.Text,
	}
	thread, err := appendParadeConversation(filepath.Join(dir, "conversations.json"), index, titles[index], message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the parade message could not be saved")
		return
	}
	routed := fmt.Sprintf("[parade %s · slide %d/%d · %s]\nkind=%s\ntext: %s", date, index+1, len(titles), titles[index], req.Kind, req.Text)
	res, routeErr := s.control.Route(r.Context(), cosTarget, routed)
	if routeErr == nil && res.Outcome == control.OutcomeDelivered {
		writeJSONStatus(w, http.StatusCreated, paradeMessageResponse{Slide: thread, Delivery: "delivered"})
		return
	}
	queuedID, _, queueErr := outbox.Enqueue(filepath.Dir(s.cfg.RosterPath), respondSender, cosTarget, routed)
	if queueErr != nil {
		writeError(w, http.StatusBadGateway, "message was saved, but CoS delivery could not be queued")
		return
	}
	writeJSONStatus(w, http.StatusAccepted, paradeMessageResponse{Slide: thread, Delivery: "queued", QueuedID: queuedID})
}

func writeParadePathError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errParadeDate):
		writeError(w, http.StatusBadRequest, errParadeDate.Error())
	case errors.Is(err, errParadeNotFound):
		writeError(w, http.StatusNotFound, errParadeNotFound.Error())
	default:
		writeError(w, http.StatusInternalServerError, "the parade archive could not be read")
	}
}
