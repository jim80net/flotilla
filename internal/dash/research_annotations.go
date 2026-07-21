package dash

import (
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/jim80net/flotilla/internal/researchannotation"
)

const researchAnnotationAuthor = "operator"

type researchAnnotationCreateRequest struct {
	Generation     *uint64                    `json:"generation"`
	DocumentDigest string                     `json:"document_digest"`
	Anchor         *researchannotation.Anchor `json:"anchor,omitempty"`
	Comment        string                     `json:"comment"`
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
	response := annotationResponse(document, stored, &created)
	writeJSONStatus(w, http.StatusCreated, response)
	s.auditResearchAnnotation(researchAnnotationAuditEvent{DocumentID: document.ID, AnnotationID: created.ID, Author: researchAnnotationAuthor, Action: "create", Result: "created", Digest: document.Digest})
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
