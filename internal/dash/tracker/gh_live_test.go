package tracker

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLive_ReadPath exercises the REAL execRunner against a live, authenticated
// `gh` and a real repo — proving the argv my code builds parses correctly and
// the pinned --json shape unmarshals into Issue. It is SKIPPED by default (it
// needs network + gh auth, so it must never gate CI); run it locally with:
//
//	FLOTILLA_GH_LIVE=1 FLOTILLA_GH_REPO=jim80net/flotilla go test ./internal/dash/tracker/ -run Live -v
//
// It is read-only: it lists and views, never creates/comments/closes (so it
// cannot pollute the repo).
func TestLive_ReadPath(t *testing.T) {
	if os.Getenv("FLOTILLA_GH_LIVE") == "" {
		t.Skip("set FLOTILLA_GH_LIVE=1 (and FLOTILLA_GH_REPO) to run the live gh integration test")
	}
	repo := os.Getenv("FLOTILLA_GH_REPO")
	if repo == "" {
		repo = "jim80net/flotilla"
	}
	g, err := NewGH(repo)
	if err != nil {
		t.Fatalf("NewGH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	issues, err := g.List(ctx, ListFilter{Limit: 3})
	if err != nil {
		t.Fatalf("live List: %v", err)
	}
	t.Logf("live List returned %d issues", len(issues))
	if len(issues) == 0 {
		return // an empty (but successful) list is still a pass
	}
	// Detail of the first issue must parse with body + the pinned fields.
	issue, err := g.Get(ctx, issues[0].Number)
	if err != nil {
		t.Fatalf("live Get(%d): %v", issues[0].Number, err)
	}
	if issue.Number != issues[0].Number || issue.Title == "" {
		t.Fatalf("live Get returned %+v", issue)
	}
	t.Logf("live Get(#%d) title=%q labels=%d comments=%d", issue.Number, issue.Title, len(issue.Labels), len(issue.Comments))
}
