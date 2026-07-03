package dash

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

// loadSessionMirror reads the agent's session-mirror ledger fresh and builds the
// history document via the pure sessionmirror builder. A missing ledger is an
// honest empty document — the dash never fabricates entries.
func (s *Server) loadSessionMirror(agent string, limit int) sessionmirror.HistoryDoc {
	path := sessionmirror.LedgerPath(filepath.Dir(s.cfg.RosterPath), agent)
	b, err := os.ReadFile(path)
	if err != nil {
		return sessionmirror.BuildHistory(agent, nil, limit)
	}
	return sessionmirror.BuildHistory(agent, b, limit)
}

func (s *Server) handleSessionMirror(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	if agent == "" {
		http.Error(w, "agent query parameter is required", http.StatusBadRequest)
		return
	}
	if _, err := s.roster.Agent(agent); err != nil {
		http.Error(w, "unknown agent", http.StatusBadRequest)
		return
	}
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			http.Error(w, "limit must be a non-negative integer", http.StatusBadRequest)
			return
		}
		limit = n
	}
	writeJSON(w, s.loadSessionMirror(agent, limit))
}