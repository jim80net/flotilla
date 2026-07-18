package dash

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jim80net/flotilla/internal/dash/control"
	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/paradeconversation"
)

const (
	paradeConversationSchema = paradeconversation.Schema
	paradeMessageTextCap     = paradeconversation.MaxTextRunes
)

var (
	errParadeDate     = errors.New("invalid parade date")
	errParadeNotFound = errors.New("parade not found")
	errParadeSlide    = errors.New("invalid parade slide")
)

type ParadeConversationMessage = paradeconversation.Message
type ParadeSlideConversation = paradeconversation.Thread
type ParadeConversations = paradeconversation.Document

type paradeMessageRequest struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

type paradeMessageResponse struct {
	Slide    ParadeSlideConversation `json:"slide"`
	Delivery string                  `json:"delivery"` // delivered | queued
	Target   string                  `json:"target"`
	QueuedID string                  `json:"queued_id,omitempty"`
}

type paradeConversationMeta struct {
	Schema     int                          `json:"schema"`
	Parades    map[string]map[string]string `json:"parades"`
	Unanswered map[string]int               `json:"unanswered_operator"`
}

func emptyParadeConversations() ParadeConversations {
	return paradeconversation.Empty()
}

func loadParadeConversations(path string) (ParadeConversations, error) {
	return paradeconversation.Load(path)
}

func appendParadeConversation(path string, index int, title string, message ParadeConversationMessage) (ParadeSlideConversation, error) {
	return paradeconversation.Append(path, index, title, message)
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
	return paradeconversation.SlideTitles(dir, fallbackTitle)
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

func readParadeConversationMeta(root string) (paradeConversationMeta, error) {
	meta := paradeConversationMeta{
		Schema: paradeConversationSchema, Parades: map[string]map[string]string{}, Unanswered: map[string]int{},
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return meta, nil
		}
		return paradeConversationMeta{}, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !paradeDateRe.MatchString(entry.Name()) {
			continue
		}
		doc, err := paradeconversation.Load(filepath.Join(root, entry.Name(), "conversations.json"))
		if err != nil {
			return paradeConversationMeta{}, err
		}
		meta.Parades[entry.Name()] = paradeconversation.LatestAgentReplies(doc)
		if pending := paradeconversation.UnansweredOperatorMessages(doc); len(pending) > 0 {
			meta.Unanswered[entry.Name()] = len(pending)
		}
	}
	return meta, nil
}

func (s *Server) handleParadeConversationMeta(w http.ResponseWriter, _ *http.Request) {
	meta, err := readParadeConversationMeta(s.cfg.ParadesPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the parade conversation metadata could not be read")
		return
	}
	writeJSON(w, meta)
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
	if !paradeconversation.ValidKind(req.Kind) {
		writeError(w, http.StatusBadRequest, "message kind must be kudos, invest, feedback, or note")
		return
	}
	message, err := paradeconversation.NewMessage("operator", req.Kind, req.Text, s.now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the parade message could not be created")
		return
	}
	thread, err := appendParadeConversation(filepath.Join(dir, "conversations.json"), index, titles[index], message)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the parade message could not be saved")
		return
	}
	// The persisted message keeps the operator's exact multiline text. The routed
	// envelope is deliberately one structural text line so authored newlines cannot
	// masquerade as another kind=/text: field in the CoS instruction.
	routeText := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(req.Text)
	routed := fmt.Sprintf("[parade reply requested · %s · slide %d/%d · %s]\nkind=%s\ntext: %s\nreply on the parade page with: flotilla parade reply --date %s --slide %d --text \"your reply\"", date, index+1, len(titles), titles[index], req.Kind, routeText, date, index+1)
	targets := []string{}
	if preferred := strings.TrimSpace(s.roster.ParadeAgent); preferred != "" && preferred != cosTarget {
		targets = append(targets, preferred)
	}
	targets = append(targets, cosTarget)
	for _, target := range targets {
		res, routeErr := s.control.Route(r.Context(), target, routed)
		if routeErr == nil && res.Outcome == control.OutcomeDelivered {
			writeJSONStatus(w, http.StatusCreated, paradeMessageResponse{Slide: thread, Delivery: "delivered", Target: target})
			return
		}
	}
	queuedID, _, queueErr := outbox.Enqueue(filepath.Dir(s.cfg.RosterPath), respondSender, cosTarget, routed)
	if queueErr != nil {
		writeError(w, http.StatusBadGateway, "message was saved, but CoS delivery could not be queued")
		return
	}
	writeJSONStatus(w, http.StatusAccepted, paradeMessageResponse{Slide: thread, Delivery: "queued", Target: cosTarget, QueuedID: queuedID})
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
