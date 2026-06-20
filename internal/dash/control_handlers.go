package dash

import (
	"errors"
	"net/http"

	"github.com/jim80net/flotilla/internal/dash/control"
)

// --- request shapes (control writes arrive as JSON) ---

type routeReq struct {
	Target  string `json:"target"`
	Message string `json:"message"`
}

type notifyReq struct {
	Message string `json:"message"`
}

type resumeReq struct {
	Agent string `json:"agent"`
}

// handleControlRoute serves POST /api/control/route (deliver an instruction to a
// desk via the confirmed-delivery library). Gated on the cross-process pane lock
// (returns 503 until it lands).
func (s *Server) handleControlRoute(w http.ResponseWriter, r *http.Request) {
	var req routeReq
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.control.Route(r.Context(), req.Target, req.Message)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, res)
}

// handleControlNotify serves POST /api/control/notify (post an operator note to
// the fleet channel via discord.Post). Live now (no pane driven).
func (s *Server) handleControlNotify(w http.ResponseWriter, r *http.Request) {
	var req notifyReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.control.Notify(r.Context(), req.Message); err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, okDoc{OK: true})
}

// handleControlResume serves POST /api/control/resume (restart a crashed desk via
// the resume recipe path). Gated on the cross-process pane lock (503 until it lands).
func (s *Server) handleControlResume(w http.ResponseWriter, r *http.Request) {
	var req resumeReq
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.control.Resume(r.Context(), req.Agent)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, res)
}

// writeControlError maps a control typed error onto an HTTP status + an honest
// JSON message (always surfaced). ErrControlUnavailable (the pane-lock gate) is a
// 503 so the UI can show "control coming with the pane lock" distinctly; bad
// input is 400; a webhook gap is 503; a downstream post failure is 502.
func writeControlError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, control.ErrResumeUnavailable),
		errors.Is(err, control.ErrWebhookMissing):
		status = http.StatusServiceUnavailable
	case errors.Is(err, control.ErrUnknownTarget),
		errors.Is(err, control.ErrUnknownAgent):
		status = http.StatusNotFound
	case errors.Is(err, control.ErrEmptyMessage),
		errors.Is(err, control.ErrOverLength):
		status = http.StatusBadRequest
	}
	writeError(w, status, err.Error())
}
