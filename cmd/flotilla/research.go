package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/researchannotation"
)

func researchUsage() {
	fmt.Println(`flotilla research — maintain private R&D response threads

usage:
  flotilla research reply --document <id> --annotation <id> --from <owner>
                          [--resolve] [--store <dir>] [--file <path|-> | <message>]`)
}

func cmdResearch(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		researchUsage()
		return nil
	}
	switch args[0] {
	case "reply":
		return cmdResearchReply(args[1:])
	default:
		return fmt.Errorf("unknown research subcommand %q (try: reply)", args[0])
	}
}

func cmdResearchReply(args []string) error {
	fs := flag.NewFlagSet("research reply", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path (used to resolve the default private annotation store)")
	storePath := fs.String("store", os.Getenv("FLOTILLA_RESEARCH_ANNOTATIONS_PATH"), "private annotation store directory (default <roster-dir>/research-annotations)")
	documentID := fs.String("document", "", "research document id (required)")
	annotationID := fs.String("annotation", "", "annotation id (required)")
	author := fs.String("from", "", "responding owner/desk (required)")
	filePath := fs.String("file", "", "response body file ('-' reads stdin)")
	resolve := fs.Bool("resolve", false, "mark the annotation addressed after recording the response")
	jsonOutput := fs.Bool("json", false, "print the durable response receipt as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*storePath) == "" {
		*storePath = filepath.Join(filepath.Dir(*rosterPath), "research-annotations")
	}
	message, err := resolveMessage(*filePath, fs.Args(), os.Stdin)
	if err != nil {
		return fmt.Errorf("research reply: %w", err)
	}
	current, err := researchannotation.LoadDocument(*storePath, *documentID)
	if err != nil {
		return fmt.Errorf("research reply: load annotation document: %w", err)
	}
	updated, annotation, comment, created, err := researchannotation.Reply(*storePath, researchannotation.ReplyInput{
		DocumentID: *documentID, AnnotationID: *annotationID, ExpectedGeneration: current.Generation,
		Author: strings.TrimSpace(*author), Comment: message, Resolve: *resolve, Now: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("research reply: %w", err)
	}
	receipt := struct {
		DocumentID   string `json:"document_id"`
		AnnotationID string `json:"annotation_id"`
		CommentID    string `json:"comment_id"`
		Owner        string `json:"owner"`
		Resolved     bool   `json:"resolved"`
		Generation   uint64 `json:"generation"`
		Created      bool   `json:"created"`
	}{
		DocumentID: updated.DocumentID, AnnotationID: annotation.ID, CommentID: comment.ID,
		Owner: comment.Author, Resolved: annotation.Resolved, Generation: updated.Generation, Created: created,
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(receipt)
	}
	action := "already recorded"
	if created {
		action = "recorded"
	}
	fmt.Printf("research: response %s annotation=%s owner=%s resolved=%t generation=%d\n",
		action, receipt.AnnotationID, receipt.Owner, receipt.Resolved, receipt.Generation)
	return nil
}
