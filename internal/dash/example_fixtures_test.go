package dash

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestCommittedExampleWalkFixturesLoadAsPopulatedState(t *testing.T) {
	root := filepath.Join("..", "..")
	rc, err := roster.Load(filepath.Join(root, "flotilla.example.json"))
	if err != nil {
		t.Fatalf("load example roster: %v", err)
	}

	snapshot, ok := watch.LoadSnapshot(filepath.Join(root, "flotilla-detector-state.example.json"))
	if !ok {
		t.Fatal("example detector snapshot did not load")
	}
	if len(snapshot.DeskStates) != len(rc.Agents) {
		t.Fatalf("example snapshot covers %d desks, want all %d roster seats", len(snapshot.DeskStates), len(rc.Agents))
	}
	if snapshot.DeskStates["backend"] != surface.StateWorking || snapshot.DeskStates["data"] != surface.StateErrored {
		t.Fatalf("example snapshot must retain working and error controls: %+v", snapshot.DeskStates)
	}

	parades, err := readParades(filepath.Join(root, "parades.example"))
	if err != nil {
		t.Fatalf("read example parade: %v", err)
	}
	if len(parades) != 1 || !strings.Contains(parades[0].Slides, "intentionally long closing sentence") {
		t.Fatalf("example parade is not populated with long-content control: %+v", parades)
	}

	research, err := readResearchIndex(filepath.Join(root, "research.example"))
	if err != nil {
		t.Fatalf("read example research: %v", err)
	}
	if len(research) != 1 || research[0].Title != "Example authorization review — operator review" || !research[0].Decision {
		t.Fatalf("example research is not a populated decision document: %+v", research)
	}
	document, found, err := readResearchDocument(filepath.Join(root, "research.example"), research[0].ID)
	if err != nil || !found || !strings.Contains(document.Markdown, "deliberately extended paragraph") {
		t.Fatalf("example research long-content document missing: found=%t err=%v", found, err)
	}
}
