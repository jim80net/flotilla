package dash

import (
	"fmt"
	"path/filepath"
	"sort"
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
	if err := validateExampleSnapshot(rc, snapshot); err != nil {
		t.Fatal(err)
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

func TestCommittedExampleWalkFixtureRejectsGhostSeatAndCollapsedStates(t *testing.T) {
	root := filepath.Join("..", "..")
	rc, err := roster.Load(filepath.Join(root, "flotilla.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := watch.LoadSnapshot(filepath.Join(root, "flotilla-detector-state.example.json"))
	if !ok {
		t.Fatal("example detector snapshot did not load")
	}
	mutated := make(map[string]surface.State, len(snapshot.DeskStates))
	for name := range snapshot.DeskStates {
		mutated[name] = surface.StateWorking
	}
	delete(mutated, "alpha-adj")
	mutated["ghost-seat"] = surface.StateWorking
	mutated["data"] = surface.StateErrored
	snapshot.DeskStates = mutated

	err = validateExampleSnapshot(rc, snapshot)
	if err == nil {
		t.Fatal("ghost seat and collapsed advertised states passed fixture validation")
	}
	message := err.Error()
	for _, want := range []string{
		"missing roster seats: alpha-adj",
		"extra snapshot seats: ghost-seat",
		"missing advertised states: idle, awaiting-input, awaiting-approval",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("fixture discriminator error %q missing %q", message, want)
		}
	}
}

func validateExampleSnapshot(rc *roster.Config, snapshot watch.Snapshot) error {
	rosterNames := make(map[string]bool, len(rc.Agents))
	for _, agent := range rc.Agents {
		rosterNames[agent.Name] = true
	}
	var missing, extra []string
	for name := range rosterNames {
		if _, ok := snapshot.DeskStates[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range snapshot.DeskStates {
		if !rosterNames[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	required := []surface.State{
		surface.StateIdle,
		surface.StateWorking,
		surface.StateAwaitingInput,
		surface.StateAwaitingApproval,
		surface.StateErrored,
	}
	present := make(map[surface.State]bool, len(required))
	for _, state := range snapshot.DeskStates {
		present[state] = true
	}
	var missingStates []string
	for _, state := range required {
		if !present[state] {
			missingStates = append(missingStates, state.String())
		}
	}

	var problems []string
	if len(missing) != 0 {
		problems = append(problems, "missing roster seats: "+strings.Join(missing, ", "))
	}
	if len(extra) != 0 {
		problems = append(problems, "extra snapshot seats: "+strings.Join(extra, ", "))
	}
	if len(missingStates) != 0 {
		problems = append(problems, "missing advertised states: "+strings.Join(missingStates, ", "))
	}
	if len(problems) != 0 {
		return fmt.Errorf("example detector snapshot invalid: %s", strings.Join(problems, "; "))
	}
	return nil
}
