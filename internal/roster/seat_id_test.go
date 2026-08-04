package roster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredRosterRoundTripAndAbsentFieldCompatibility942(t *testing.T) {
	path := writeRoster(t, `{
  "xo_agent": "lead",
  "agents": [
    {"seat_id":"0102030405060708","name":"lead","coordinator":true},
    {"seat_id":"1112131415161718","parent":"0102030405060708","name":"builder"},
    {"name":"legacy-observer"}
  ]
}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Org() == nil || cfg.Org().Source != "roster" || cfg.Org().PrimaryParent("builder") != "lead" {
		t.Fatalf("roster DAG = %+v", cfg.Org())
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Config
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.Agents[1].Parent != "0102030405060708" || roundTrip.Agents[2].SeatID != "" || roundTrip.Agents[2].Parent != "" {
		t.Fatalf("round-trip agents = %+v", roundTrip.Agents)
	}

	legacy := writeRoster(t, `{"agents":[{"name":"lead"},{"name":"builder"}]}`)
	if _, err := Load(legacy); err != nil {
		t.Fatalf("absent seat_id/parent must remain valid: %v", err)
	}
	partial := writeRoster(t, `{"agents":[{"name":"lead","seat_id":"0102030405060708"},{"name":"builder"}]}`)
	partialCfg, err := Load(partial)
	if err != nil || partialCfg.HasStructuredHierarchy() || partialCfg.Org().Source != "derived" {
		t.Fatalf("partial provisioning must retain legacy view until migration: cfg=%+v err=%v", partialCfg, err)
	}
}

func TestStructuredRosterRetiresInterimOrgFile942(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	rosterBody := `{
  "xo_agent":"lead",
  "agents":[
    {"name":"lead","seat_id":"0102030405060708","coordinator":true},
    {"name":"builder","seat_id":"1112131415161718","parent":"0102030405060708"}
  ]
}`
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	// Deliberately stale: once roster edges are crowned this file is not a second
	// source whose disagreement can replace or reject the roster.
	orgBody := "version: 1\nroot: builder\nnodes:\n  - id: builder\n    kind: coordinator\n  - id: lead\n    kind: desk\n    reports_to: builder\n"
	if err := os.WriteFile(filepath.Join(dir, "fleet-org.yaml"), []byte(orgBody), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Org().Source != "roster" || cfg.Org().PrimaryParent("builder") != "lead" {
		t.Fatalf("crowned org = %+v", cfg.Org())
	}
}

func TestStructuredRosterRejectsDuplicateAndDanglingSeatIDs942(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate": `{"agents":[{"name":"lead","seat_id":"0102030405060708"},{"name":"builder","seat_id":"0102030405060708"}]}`,
		"dangling":  `{"agents":[{"name":"lead","seat_id":"0102030405060708"},{"name":"builder","seat_id":"1112131415161718","parent":"ffffffffffffffff"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeRoster(t, body))
			if err == nil {
				t.Fatal("expected load error")
			}
			if name == "dangling" && (!strings.Contains(err.Error(), `agent "builder"`) || !strings.Contains(err.Error(), "ffffffffffffffff")) {
				t.Fatalf("dangling error must name seat and parent: %v", err)
			}
		})
	}
}

func TestStructuredRosterChecksChannelParentAgreement942(t *testing.T) {
	path := writeRoster(t, `{
  "xo_agent":"lead",
  "agents":[
    {"name":"lead","seat_id":"0102030405060708","coordinator":true},
    {"name":"other","seat_id":"1112131415161718","coordinator":true},
    {"name":"builder","seat_id":"2122232425262728","parent":"0102030405060708"}
  ],
  "channels":[{"channel_id":"C_OTHER","xo_agent":"other","members":["builder"],"role":"project"}]
}`)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "parent/channel disagreement") || !strings.Contains(err.Error(), `agent "builder"`) {
		t.Fatalf("agreement error = %v", err)
	}
}

func TestEnsureSeatIDAssignsOnceAndRejectsSymlink942(t *testing.T) {
	path := writeRoster(t, `{"cos_agent":"builder","future_field":{"keep":true},"agents":[{"name":"builder"}]}`)
	id, assigned, err := EnsureSeatID(path, "builder")
	if err != nil || !assigned || len(id) != 16 {
		t.Fatalf("first assignment = (%q, %v, %v)", id, assigned, err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("written roster does not load: %v", err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `"future_field"`) || strings.Contains(string(written), "context-ledger.md") {
		t.Fatalf("seat patch lost authored fields or wrote runtime defaults: %s", written)
	}
	again, assigned, err := EnsureSeatID(path, "builder")
	if err != nil || assigned || again != id {
		t.Fatalf("second assignment = (%q, %v, %v), want unchanged %q", again, assigned, err, id)
	}

	link := filepath.Join(t.TempDir(), "roster-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureSeatID(link, "builder"); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink assignment error = %v", err)
	}
}
