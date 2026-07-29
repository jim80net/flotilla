package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/researchannotation"
)

func TestResearchReplyRecordsOwnerResponse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "research-annotations")
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, created, err := researchannotation.Create(root, researchannotation.CreateInput{
		DocumentID: "notes/example.md", DocumentDigest: digest, Author: "operator",
		Comment: "Please turn this finding into work.", Now: time.Date(2026, 7, 29, 4, 31, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}

	args := []string{
		"--store", root,
		"--document", "notes/example.md",
		"--annotation", created.ID,
		"--from", "flotilla-dev",
		"--resolve",
		"Accepted. Work is tracked in #900.",
	}
	if err := cmdResearchReply(args); err != nil {
		t.Fatalf("cmdResearchReply: %v", err)
	}
	doc, err := researchannotation.LoadDocument(root, "notes/example.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Generation != 2 || len(doc.Annotations) != 1 {
		t.Fatalf("document = %#v, want generation 2 and one annotation", doc)
	}
	annotation := doc.Annotations[0]
	if !annotation.Resolved || len(annotation.Comments) != 2 {
		t.Fatalf("annotation = %#v, want resolved two-comment thread", annotation)
	}
	reply := annotation.Comments[1]
	if reply.Author != "flotilla-dev" || reply.Text != "Accepted. Work is tracked in #900." {
		t.Fatalf("reply = %#v", reply)
	}

	if err := cmdResearchReply(args); err != nil {
		t.Fatalf("idempotent cmdResearchReply: %v", err)
	}
	again, err := researchannotation.LoadDocument(root, "notes/example.md")
	if err != nil {
		t.Fatal(err)
	}
	if again.Generation != 2 || len(again.Annotations[0].Comments) != 2 {
		t.Fatalf("idempotent retry changed state: %#v", again)
	}
}
