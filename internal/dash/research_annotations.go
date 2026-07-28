package dash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/dash/control"
	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/researchannotation"
)

const researchAnnotationAuthor = "operator"

type researchAnnotationCreateRequest struct {
	Generation     *uint64                    `json:"generation"`
	DocumentDigest string                     `json:"document_digest"`
	Anchor         *researchannotation.Anchor `json:"anchor,omitempty"`
	Comment        string                     `json:"comment"`
}

type researchAnnotationRouteRequest struct {
	AnnotationID string `json:"annotation_id"`
	Generation   uint64 `json:"generation"`
}

type researchAnnotationView struct {
	researchannotation.Annotation
	AnchorResolution *researchannotation.Resolution `json:"anchor_resolution,omitempty"`
}

type researchAnnotationsResponse struct {
	Schema         int                      `json:"schema"`
	DocumentID     string                   `json:"document_id"`
	DocumentDigest string                   `json:"document_digest"`
	Generation     uint64                   `json:"generation"`
	Annotations    []researchAnnotationView `json:"annotations"`
	Created        *researchAnnotationView  `json:"created,omitempty"`
}

type researchAnnotationAuditEvent struct {
	DocumentID   string
	AnnotationID string
	Author       string
	Action       string
	Result       string
	Digest       string
}

func defaultResearchAnnotationAudit(event researchAnnotationAuditEvent) {
	fmt.Fprint(os.Stderr, formatResearchAnnotationAudit(event))
}

func formatResearchAnnotationAudit(event researchAnnotationAuditEvent) string {
	return fmt.Sprintf("flotilla dash: research annotation action=%s result=%s document=%q annotation=%q author=%q digest=%q\n",
		event.Action, event.Result, event.DocumentID, event.AnnotationID, event.Author, event.Digest)
}

func (s *Server) handleResearchAnnotations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	document, found, err := readResearchDocument(s.cfg.ResearchPath, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research document could not be read")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "research document not found")
		return
	}
	stored, err := researchannotation.LoadDocument(s.cfg.ResearchAnnotationsPath, document.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research annotations could not be read")
		return
	}
	writeJSON(w, annotationResponse(document, stored, nil))
}

func (s *Server) handleResearchAnnotationCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("id")
	document, found, err := readResearchDocument(s.cfg.ResearchPath, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research document could not be read")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "research document not found")
		return
	}
	var req researchAnnotationCreateRequest
	if !decodeJSON(w, r, &req) {
		s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, Author: researchAnnotationAuthor, Action: "create", Result: "invalid_request", Digest: document.Digest})
		return
	}
	if req.Generation == nil {
		writeError(w, http.StatusBadRequest, "generation is required")
		s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, Author: researchAnnotationAuthor, Action: "create", Result: "invalid_generation", Digest: document.Digest})
		return
	}
	if req.DocumentDigest != document.Digest {
		current, loadErr := researchannotation.LoadDocument(s.cfg.ResearchAnnotationsPath, document.ID)
		if loadErr != nil {
			writeError(w, http.StatusInternalServerError, "the research annotations could not be read")
			return
		}
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"error": "the research document changed; reload before annotating", "generation": current.Generation, "document_digest": document.Digest,
		})
		s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, Author: researchAnnotationAuthor, Action: "create", Result: "digest_conflict", Digest: document.Digest})
		return
	}
	anchor := req.Anchor
	if anchor != nil {
		if err := researchannotation.ValidateAnchor(*anchor); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, Author: researchAnnotationAuthor, Action: "create", Result: "invalid_anchor", Digest: document.Digest})
			return
		}
		resolution := researchannotation.Resolve(document.Markdown, *anchor)
		if resolution.State != researchannotation.AnchorAttached {
			writeError(w, http.StatusBadRequest, "the selected quote and context do not uniquely match this document")
			s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, Author: researchAnnotationAuthor, Action: "create", Result: "ambiguous_anchor", Digest: document.Digest})
			return
		}
		normalized := *anchor
		normalized.Start, normalized.End = resolution.Start, resolution.End
		anchor = &normalized
	}
	stored, created, err := researchannotation.Create(s.cfg.ResearchAnnotationsPath, researchannotation.CreateInput{
		DocumentID: document.ID, DocumentDigest: document.Digest, ExpectedGeneration: *req.Generation,
		Anchor: anchor, Author: researchAnnotationAuthor, Comment: req.Comment, Now: s.now(),
	})
	if errors.Is(err, researchannotation.ErrConflict) {
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"error": "the research annotations changed; reload before saving", "generation": stored.Generation, "document_digest": document.Digest,
		})
		s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, Author: researchAnnotationAuthor, Action: "create", Result: "generation_conflict", Digest: document.Digest})
		return
	}
	if errors.Is(err, researchannotation.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, Author: researchAnnotationAuthor, Action: "create", Result: "invalid_annotation", Digest: document.Digest})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research annotation could not be saved")
		s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, Author: researchAnnotationAuthor, Action: "create", Result: "storage_error", Digest: document.Digest})
		return
	}
	stored, created = s.routeResearchAnnotation(r.Context(), document, stored, created)
	response := annotationResponse(document, stored, &created)
	writeJSONStatus(w, http.StatusCreated, response)
	s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, AnnotationID: created.ID, Author: researchAnnotationAuthor, Action: "create", Result: "created_" + created.Routing.State, Digest: document.Digest})
}

func (s *Server) handleResearchAnnotationRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	document, found, err := readResearchDocument(s.cfg.ResearchPath, r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research document could not be read")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "research document not found")
		return
	}
	var req researchAnnotationRouteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	stored, err := researchannotation.LoadDocument(s.cfg.ResearchAnnotationsPath, document.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the research annotations could not be read")
		return
	}
	var annotation *researchannotation.Annotation
	for i := range stored.Annotations {
		if stored.Annotations[i].ID == req.AnnotationID {
			annotation = &stored.Annotations[i]
			break
		}
	}
	if annotation == nil {
		writeError(w, http.StatusNotFound, "research annotation not found")
		return
	}
	if req.Generation == 0 || annotation.Routing.Generation != req.Generation ||
		annotation.Routing.Key != researchannotation.RouteKey(annotation.ID, req.Generation) {
		writeError(w, http.StatusConflict, "the research annotation route changed; reload before retrying")
		return
	}
	stored, routed := s.routeResearchAnnotation(r.Context(), document, stored, *annotation)
	writeJSON(w, annotationResponse(document, stored, &routed))
	s.auditResearchAnnotation(researchAnnotationAuditEvent{
		DocumentID: document.ID, AnnotationID: routed.ID, Author: researchAnnotationAuthor,
		Action: "route_retry", Result: routed.Routing.State, Digest: document.Digest,
	})
}

func (s *Server) routeResearchAnnotation(ctx context.Context, document ResearchDocument, stored researchannotation.Document, annotation researchannotation.Annotation) (researchannotation.Document, researchannotation.Annotation) {
	if annotation.Routing.State == researchannotation.RouteQueued || annotation.Routing.State == researchannotation.RouteDelivered {
		return stored, annotation
	}
	target := strings.TrimSpace(s.roster.CosAgent)
	if target == "" {
		target = strings.TrimSpace(s.xo)
	}
	if target == "" {
		return stored, annotation
	}
	message := researchAnnotationRouteMessage(document, annotation)
	result, err := s.control.Route(ctx, target, message)
	if err != nil || strings.TrimSpace(result.Target) == "" {
		return stored, annotation
	}
	next := annotation.Routing
	next.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
	switch result.Outcome {
	case control.OutcomeDelivered:
		next.State = researchannotation.RouteDelivered
		next.Target = result.Target
		next.QueuedID = ""
	default:
		queuedID, _, queueErr := outbox.Enqueue(filepath.Dir(s.cfg.RosterPath), respondSender, result.Target, message)
		if queueErr != nil {
			return stored, annotation
		}
		next.State = researchannotation.RouteQueued
		next.Target = result.Target
		next.QueuedID = queuedID
	}
	updated, routed, updateErr := researchannotation.UpdateRouting(
		s.cfg.ResearchAnnotationsPath, document.ID, annotation.ID, annotation.Routing.Generation, next,
	)
	if updateErr != nil {
		return stored, annotation
	}
	return updated, routed
}

func researchAnnotationRouteMessage(document ResearchDocument, annotation researchannotation.Annotation) string {
	comment := ""
	if len(annotation.Comments) > 0 {
		comment = annotation.Comments[0].Text
	}
	anchor := map[string]string{"scope": "whole_document"}
	if annotation.Anchor != nil {
		anchor = map[string]string{
			"scope": "passage", "quote": annotation.Anchor.Quote,
			"prefix": annotation.Anchor.Prefix, "suffix": annotation.Anchor.Suffix,
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"schema": "flotilla.research_annotation_route/v1", "route_key": annotation.Routing.Key,
		"document_id": document.ID, "document_title": document.Title, "paper": "/research/" + document.ID,
		"annotation_id": annotation.ID, "anchor": anchor, "author": annotation.Author,
		"created_at": annotation.CreatedAt, "comment": comment,
	})
	return "[Jim left an R&D annotation for your fleet]\n" + string(payload) +
		"\nThis is saved feedback, not assigned work. Review what Jim wrote, then explicitly assign or respond; delivery alone does not assign ownership."
}

func annotationResponse(document ResearchDocument, stored researchannotation.Document, created *researchannotation.Annotation) researchAnnotationsResponse {
	views := make([]researchAnnotationView, 0, len(stored.Annotations))
	for _, annotation := range stored.Annotations {
		views = append(views, annotationView(document, annotation))
	}
	response := researchAnnotationsResponse{
		Schema: researchannotation.Schema, DocumentID: document.ID, DocumentDigest: document.Digest,
		Generation: stored.Generation, Annotations: views,
	}
	if created != nil {
		view := annotationView(document, *created)
		response.Created = &view
	}
	return response
}

func annotationView(document ResearchDocument, annotation researchannotation.Annotation) researchAnnotationView {
	view := researchAnnotationView{Annotation: annotation}
	if annotation.Anchor != nil {
		resolution := researchannotation.Resolve(document.Markdown, *annotation.Anchor)
		if annotation.DocumentDigest != document.Digest {
			resolution = researchannotation.Reanchor(document.Markdown, *annotation.Anchor)
		}
		view.AnchorResolution = &resolution
	}
	return view
}

func (s *Server) auditResearchAnnotation(event researchAnnotationAuditEvent) {
	if s.researchAnnotationAudit != nil {
		s.researchAnnotationAudit(event)
	}
}
