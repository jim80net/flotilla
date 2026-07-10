package roster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/org"
)

// federatedRoster is a small generic federation matching fleet-org.example shape
// (xo / alpha-xo / backend) with agreeing channel parents.
const federatedRoster = `{
  "operator_user_id":"U",
  "xo_agent":"xo",
  "agents":[{"name":"xo"},{"name":"alpha-xo"},{"name":"backend"}],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"xo","role":"fleet-command",
     "members":["xo","alpha-xo","backend"]},
    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["xo"]},
    {"channel_id":"C_BE","xo_agent":"backend","members":["alpha-xo"]}]}`

const agreeingOrg = `version: 1
root: xo
nodes:
  - id: xo
    kind: coordinator
  - id: alpha-xo
    kind: coordinator
    reports_to: xo
    home_channel_id: "C_ALPHA"
  - id: backend
    kind: desk
    reports_to: alpha-xo
    home_channel_id: "C_BE"
`

const disagreeingOrg = `version: 1
root: xo
nodes:
  - id: xo
    kind: coordinator
  - id: alpha-xo
    kind: coordinator
    reports_to: xo
    home_channel_id: "C_ALPHA"
  - id: backend
    kind: desk
    reports_to: xo
    home_channel_id: "C_BE"
`

func writePair(t *testing.T, rosterBody, orgBody string) (rosterPath, orgPath string) {
	t.Helper()
	dir := t.TempDir()
	rosterPath = filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if orgBody != "" {
		orgPath = filepath.Join(dir, "fleet-org.yaml")
		if err := os.WriteFile(orgPath, []byte(orgBody), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return rosterPath, orgPath
}

func TestLoad_OrgFileAgree(t *testing.T) {
	rp, _ := writePair(t, federatedRoster, agreeingOrg)
	cfg, err := Load(rp) // default discovery of fleet-org.yaml beside roster
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.Org()
	if d == nil || d.Source != org.SourceFile {
		t.Fatalf("want file-sourced org DAG, got %+v", d)
	}
	if d.PrimaryParent("backend") != "alpha-xo" {
		t.Errorf("parent=%q", d.PrimaryParent("backend"))
	}
}

func TestLoad_OrgFileDisagree(t *testing.T) {
	rp, _ := writePair(t, federatedRoster, disagreeingOrg)
	_, err := Load(rp)
	if err == nil {
		t.Fatal("expected agreement refuse")
	}
	if !strings.Contains(err.Error(), "disagrees") && !strings.Contains(err.Error(), "reports_to") {
		t.Errorf("want disagreement wording: %v", err)
	}
	if !strings.Contains(err.Error(), "backend") {
		t.Errorf("want agent named: %v", err)
	}
}

func TestLoad_OrgFileExplicitPath(t *testing.T) {
	dir := t.TempDir()
	rp := filepath.Join(dir, "flotilla.json")
	op := filepath.Join(dir, "custom-org.yaml")
	if err := os.WriteFile(rp, []byte(federatedRoster), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(op, []byte(agreeingOrg), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWith(rp, LoadOptions{OrgFile: op})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Org().Source != org.SourceFile {
		t.Fatal("want file source")
	}
}

func TestLoad_OrgFileExplicitMissing(t *testing.T) {
	rp, _ := writePair(t, federatedRoster, "")
	_, err := LoadWith(rp, LoadOptions{OrgFile: filepath.Join(t.TempDir(), "missing.yaml")})
	if err == nil {
		t.Fatal("explicit missing org file must refuse")
	}
}

func TestLoad_DuplicateHomeUndeclared(t *testing.T) {
	// alpha-xo owns two non-fleet homes; org present without home_channel_id.
	rosterBody := `{
  "operator_user_id":"U","xo_agent":"xo",
  "agents":[{"name":"xo"},{"name":"alpha-xo"}],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"xo","role":"fleet-command","members":["xo","alpha-xo"]},
    {"channel_id":"C_A1","xo_agent":"alpha-xo","members":["xo"]},
    {"channel_id":"C_A2","xo_agent":"alpha-xo","members":["xo"]}]}`
	orgBody := `version: 1
root: xo
nodes:
  - id: xo
    kind: coordinator
  - id: alpha-xo
    kind: coordinator
    reports_to: xo
`
	rp, _ := writePair(t, rosterBody, orgBody)
	_, err := Load(rp)
	if err == nil {
		t.Fatal("expected multi-home refuse")
	}
	if !strings.Contains(err.Error(), "multiple home") {
		t.Errorf("got: %v", err)
	}
}

func TestLoad_DuplicateHomeDeclared(t *testing.T) {
	rosterBody := `{
  "operator_user_id":"U","xo_agent":"xo",
  "agents":[{"name":"xo"},{"name":"alpha-xo"}],
  "channels":[
    {"channel_id":"C_CMD","xo_agent":"xo","role":"fleet-command","members":["xo","alpha-xo"]},
    {"channel_id":"C_A1","xo_agent":"alpha-xo","members":["xo"]},
    {"channel_id":"C_A2","xo_agent":"alpha-xo","members":["xo"]}]}`
	orgBody := `version: 1
root: xo
nodes:
  - id: xo
    kind: coordinator
  - id: alpha-xo
    kind: coordinator
    reports_to: xo
    home_channel_id: "C_A1"
`
	rp, _ := writePair(t, rosterBody, orgBody)
	cfg, err := Load(rp)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Org().Source != org.SourceFile {
		t.Fatal("want file")
	}
}

func TestLoad_MutualHomeStillRefused(t *testing.T) {
	// Cycle is refused before org attach (synthesis). Org file absent.
	_, err := Load(writeRoster(t, `{
	  "operator_user_id":"U",
	  "agents":[{"name":"x"},{"name":"y"}],
	  "channels":[{"channel_id":"CX","xo_agent":"x","members":["y"]},
	              {"channel_id":"CY","xo_agent":"y","members":["x"]}]}`))
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("want cycle refuse, got %v", err)
	}
}

func TestLoad_AbsentOrgDerives(t *testing.T) {
	rp, _ := writePair(t, federatedRoster, "")
	cfg, err := Load(rp)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Org().Source != org.SourceDerived {
		t.Fatalf("want derived, got %s", cfg.Org().Source)
	}
}
