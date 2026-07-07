package dash

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/dash/control"
	"github.com/jim80net/flotilla/internal/outbox"
)

// --- request shapes (control writes arrive as JSON) ---

type routeReq struct {
	Target  string `json:"target"`
	Message string `json:"message"`
}

// respondReq is one operator decision response (#501): target is the owning desk;
// goal_id (+ the optional work-item label) is the decision's identity, composed into
// the delivered body so the receiving desk knows exactly which decision was answered.
type respondReq struct {
	Target  string `json:"target"`
	GoalID  string `json:"goal_id"`
	Item    string `json:"item"`
	Message string `json:"message"`
}

// respondDoc is the honest outcome the decisions UI renders: "delivered" (turn
// confirmed), or "queued" with the durable outbox id (at-least-once — the watch
// sweep delivers when the desk is idle). Never a silent drop, never a fake success.
type respondDoc struct {
	Outcome  string `json:"outcome"` // delivered | queued
	Target   string `json:"target"`
	Detail   string `json:"detail,omitempty"`
	QueuedID string `json:"queued_id,omitempty"`
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

// respondSender is the durable-outbox sender identity for operator decision responses.
// It is the OPERATOR's message (typed in the dash), not the XO's — the outbox file,
// the sweep's inbound tracking, and any stale escalation all carry that provenance.
const respondSender = "operator"

// handleControlRespond serves POST /api/control/respond — the #501 decision-response
// loop's delivery leg. It composes a self-describing body (which decision, whose
// words), attempts LIVE confirmed delivery via the same Route path the thread
// composer uses (pane txn lock, typed outcomes), and on ANY not-delivered outcome
// (busy/transient/crashed/input-blocked/unconfirmed) enqueues the response to the
// durable operator outbox so it is AT-LEAST-ONCE: the watch sweep delivers it when
// the desk can receive. The operator sees exactly what happened — delivered now, or
// queued with the id.
func (s *Server) handleControlRespond(w http.ResponseWriter, r *http.Request) {
	var req respondReq
	if !decodeJSON(w, r, &req) {
		return
	}
	// Guard the OPERATOR's text, not the composed wrapper — the wrapper would make an
	// empty response look non-empty and deliver a contentless decision answer.
	if strings.TrimSpace(req.Message) == "" {
		writeControlError(w, control.ErrEmptyMessage)
		return
	}
	// Fast-fail an empty target with the same 404 the resolver would eventually map —
	// no wasted resolution, and a clear error for a hand-built API request (OCR #505).
	if strings.TrimSpace(req.Target) == "" {
		writeControlError(w, control.ErrUnknownTarget)
		return
	}
	ref := strings.TrimSpace(req.GoalID)
	if item := strings.TrimSpace(req.Item); item != "" {
		if ref != "" {
			ref += " / " + item
		} else {
			ref = item // an item-only reference never renders a dangling " / " (OCR #505)
		}
	}
	msg := req.Message
	if ref != "" {
		msg = fmt.Sprintf("[operator decision response — %s] %s", ref, req.Message)
	}
	res, err := s.control.Route(r.Context(), req.Target, msg)
	if err != nil {
		writeControlError(w, err)
		return
	}
	if res.Outcome == control.OutcomeDelivered {
		writeJSON(w, respondDoc{Outcome: "delivered", Target: res.Target})
		return
	}
	// Not delivered live — make it durable. Recipient is the CANONICAL agent name the
	// route resolved (res.Target), so the sweep and the UI name the same desk.
	id, deduped, qerr := outbox.Enqueue(filepath.Dir(s.cfg.RosterPath), respondSender, res.Target, msg)
	if qerr != nil {
		// Both legs failed: the live outcome AND the durable enqueue. Surface both —
		// the operator must know the response did NOT take.
		writeError(w, http.StatusBadGateway, fmt.Sprintf("%s; durable outbox enqueue also failed: %v", res.Detail, qerr))
		return
	}
	detail := res.Detail
	if deduped {
		detail = "an identical response is already queued — no duplicate added"
	}
	writeJSON(w, respondDoc{Outcome: "queued", Target: res.Target, Detail: detail, QueuedID: id})
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
		errors.Is(err, control.ErrOverLength),
		errors.Is(err, control.ErrAmbiguousTarget):
		status = http.StatusBadRequest
	}
	writeError(w, status, err.Error())
}
