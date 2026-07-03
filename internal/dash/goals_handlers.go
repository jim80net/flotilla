package dash

import (
	"net/http"

	"github.com/jim80net/flotilla/internal/dash/goals"
)

// handleGoals serves GET /api/goals — the GoalsDoc { tree, rollups, generated_at }
// parsed fresh from fleet-goals.yaml (design §6.1). A missing goals file is an
// empty tree (honest "no goals yet"), never an error; a malformed one is a typed
// 502 surfaced to the UI (never a silent empty map masquerading as "no goals").
func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	doc, err := goals.Load(s.cfg.GoalsPath)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not read fleet goals: "+err.Error())
		return
	}
	writeJSON(w, doc)
}

// handleGoalDetail serves GET /api/goals/{id} — one node + its work items + owner
// desk hints (design §6.1). An unknown id is a 404; a malformed goals file is a 502.
func (s *Server) handleGoalDetail(w http.ResponseWriter, r *http.Request) {
	doc, err := goals.Load(s.cfg.GoalsPath)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not read fleet goals: "+err.Error())
		return
	}
	detail, ok := doc.Detail(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no goal with id "+r.PathValue("id"))
		return
	}
	writeJSON(w, detail)
}
