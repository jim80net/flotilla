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
// desk via the confirmed-delivery library, serialized by the cross-process pane
// transaction lock). Returns the typed outcome (delivered/busy/crashed/…) at 200;
// a hard failure (unknown target/surface, pane-resolution) is an error status.
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
// the resume recipe path). Still gated (503, ErrResumeUnavailable) until the
// resume orchestration is extracted from package main into a reusable library.
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
// JSON message (always surfaced). ErrResumeUnavailable (resume not yet wired) and
// ErrWebhookMissing are 503; an unknown target/agent is 404; bad input (empty/
// over-length) is 400; a downstream delivery/post failure is 502.
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
