package roster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/org"
)

// PR3: AgentsAbove/AgentsBelow/OwningXO read the compiled org DAG after Load.

func TestAgentsAboveBelow_UseFileOrgDAG(t *testing.T) {
	rp, _ := writePair(t, federatedRoster, agreeingOrg)
	cfg, err := Load(rp)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Org().Source != org.SourceFile {
		t.Fatalf("source=%s", cfg.Org().Source)
	}
	// File says backend → alpha-xo → xo
	if got := cfg.AgentsAbove("backend"); !sortedEqual(got, []string{"alpha-xo"}) {
		t.Errorf("AgentsAbove(backend)=%v want [alpha-xo] from org file", got)
	}
	if got := cfg.AgentsAbove("alpha-xo"); !sortedEqual(got, []string{"xo"}) {
		t.Errorf("AgentsAbove(alpha-xo)=%v want [xo]", got)
	}
	if got := cfg.AgentsBelow("xo"); !sortedEqual(got, []string{"alpha-xo"}) {
		// file DAG children of xo is only alpha-xo (backend reports to alpha-xo)
		t.Errorf("AgentsBelow(xo)=%v want [alpha-xo]", got)
	}
	if got := cfg.AgentsBelow("alpha-xo"); !sortedEqual(got, []string{"backend"}) {
		t.Errorf("AgentsBelow(alpha-xo)=%v want [backend]", got)
	}
	if got := cfg.OwningXO("backend", "xo"); got != "alpha-xo" {
		t.Errorf("OwningXO(backend)=%q want alpha-xo from PrimaryParent", got)
	}
}

func TestAgentsAboveBelow_DerivedParityStillHolds(t *testing.T) {
	// No org file — derived snapshot; channel rules via DAG must match pre-PR3 expectations.
	cfg := loadLiveShape(t)
	if cfg.Org() == nil || cfg.Org().Source != org.SourceDerived {
		t.Fatal("want derived org")
	}
	for _, a := range cfg.Agents {
		// Channel path during Snapshot populated Parents/Children; AgentsAbove now reads DAG.
		// Inverse still holds on the DAG.
		for _, below := range cfg.AgentsBelow(a.Name) {
			if !containsStr(cfg.AgentsAbove(below), a.Name) {
				t.Errorf("inverse: %q below %q but not above", below, a.Name)
			}
		}
	}
	if got := cfg.OwningXO("alpha-be", "meta"); got != "alpha-xo" {
		t.Errorf("OwningXO(alpha-be)=%q", got)
	}
}

func TestLoadWith_OrgCompileFailureIsFatal(t *testing.T) {
	// Malformed org (cycle) must refuse LoadWith — watch cmd returns this error
	// before starting the detector (org-truth watch spec: fatal start).
	dir := t.TempDir()
	rp := filepath.Join(dir, "flotilla.json")
	op := filepath.Join(dir, "fleet-org.yaml")
	if err := os.WriteFile(rp, []byte(federatedRoster), 0o600); err != nil {
		t.Fatal(err)
	}
	cycleOrg := `version: 1
root: xo
nodes:
  - id: xo
    kind: coordinator
    reports_to: backend
  - id: backend
    kind: desk
    reports_to: xo
`
	if err := os.WriteFile(op, []byte(cycleOrg), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadWith(rp, LoadOptions{OrgFile: op})
	if err == nil {
		t.Fatal("expected org cycle refuse")
	}
	if !strings.Contains(err.Error(), "cycle") && !strings.Contains(err.Error(), "org") {
		t.Errorf("want org/cycle in error: %v", err)
	}
}

func TestLoadWith_DisagreementFatalForWatchPath(t *testing.T) {
	rp, _ := writePair(t, federatedRoster, disagreeingOrg)
	_, err := Load(rp)
	if err == nil {
		t.Fatal("expected disagreement refuse (watch would not start)")
	}
	if !strings.Contains(err.Error(), "disagrees") && !strings.Contains(err.Error(), "reports_to") {
		t.Errorf("got: %v", err)
	}
}

func containsStr(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}
