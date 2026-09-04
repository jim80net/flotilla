package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/roster"
)

func coordinatorFlag(value bool) *bool { return &value }

func cadenceFixture(t *testing.T) (cadenceManifest, *roster.Config, string, time.Time) {
	t.Helper()
	rosterDir := t.TempDir()
	backendDir := t.TempDir()
	frontendDir := t.TempDir()
	cfg := &roster.Config{
		XOAgent: "xo",
		Agents: []roster.Agent{
			{Name: "xo"},
			{Name: "backend", Coordinator: coordinatorFlag(true), WorktreePath: backendDir},
			{Name: "frontend", Coordinator: coordinatorFlag(true), WorktreePath: frontendDir},
		},
	}
	started := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	manifest := cadenceManifest{
		Version: cadenceManifestVersion, Nonce: "walk-2026-09-04",
		StartedAt: started.Format(time.RFC3339Nano), DueAt: started.Add(time.Hour).Format(time.RFC3339Nano),
		Members: []cadenceManifestMember{
			{Coordinator: "backend", DispatchNonce: "flotilla-dispatch-aabbccdd", ArtifactPath: "state/retros/inputs/backend.md"},
			{Coordinator: "frontend", DispatchNonce: "flotilla-dispatch-eeff0011", ArtifactPath: "state/retros/inputs/frontend.md"},
		},
	}
	return manifest, cfg, rosterDir, started
}

func writeCadenceArtifact(t *testing.T, root, rel string, modified time.Time) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("complete\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

func TestCadenceStatusReportsReceiptsArtifactsAndCompleteBar(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	for _, member := range manifest.Members {
		agent, err := cfg.Agent(member.Coordinator)
		if err != nil {
			t.Fatal(err)
		}
		writeCadenceArtifact(t, agent.WorktreePath, member.ArtifactPath, started.Add(10*time.Minute))
		if _, err := dispatch.Consume(rosterDir, dispatch.ConsumedEntry{
			Nonce: member.DispatchNonce, PayloadHash: member.Coordinator + "-payload", ConsumedAt: started.Add(5 * time.Minute),
			Reason: dispatch.ReasonDurableAck, Sender: "xo", Recipient: member.Coordinator,
		}); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.ExpectedCoordinators, []string{"backend", "frontend"}) {
		t.Fatalf("expected coordinators = %v", doc.ExpectedCoordinators)
	}
	if len(doc.DispatchReceipts) != 2 || doc.DispatchReceipts[0].Disposition != "consumed" || doc.DispatchReceipts[1].Disposition != "consumed" {
		t.Fatalf("dispatch receipts = %+v", doc.DispatchReceipts)
	}
	if len(doc.RecursiveArtifactPaths) != 2 || !doc.RecursiveArtifactPaths[0].Current || !doc.RecursiveArtifactPaths[1].Current {
		t.Fatalf("recursive artifacts = %+v", doc.RecursiveArtifactPaths)
	}
	if len(doc.OverdueMembers) != 0 {
		t.Fatalf("overdue members = %v", doc.OverdueMembers)
	}
	if doc.CompletionBar != (cadenceCompletionBar{Completed: 2, Total: 2, Remaining: 0, Percent: 100, State: "complete"}) {
		t.Fatalf("completion bar = %+v", doc.CompletionBar)
	}
}

func TestCadenceStatusNamesOverdueMissingMember(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	backend, _ := cfg.Agent("backend")
	writeCadenceArtifact(t, backend.WorktreePath, manifest.Members[0].ArtifactPath, started.Add(10*time.Minute))
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.OverdueMembers, []string{"frontend"}) {
		t.Fatalf("overdue members = %v, want frontend", doc.OverdueMembers)
	}
	if doc.DispatchReceipts[1].Disposition != "unknown" {
		t.Fatalf("frontend receipt = %+v, want unknown", doc.DispatchReceipts[1])
	}
	if doc.CompletionBar != (cadenceCompletionBar{Completed: 1, Total: 2, Remaining: 1, Percent: 50, State: "overdue"}) {
		t.Fatalf("completion bar = %+v", doc.CompletionBar)
	}
}

func TestCadenceStatusRejectsStaleArtifact(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	backend, _ := cfg.Agent("backend")
	writeCadenceArtifact(t, backend.WorktreePath, manifest.Members[0].ArtifactPath, started.Add(-time.Minute))
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	artifact := doc.RecursiveArtifactPaths[0]
	if !artifact.Present || !artifact.NonEmpty || artifact.Current {
		t.Fatalf("stale artifact = %+v, want present/non-empty but not current", artifact)
	}
	if doc.CompletionBar.State != "in_progress" || len(doc.OverdueMembers) != 0 {
		t.Fatalf("pre-deadline status = bar %+v overdue %v", doc.CompletionBar, doc.OverdueMembers)
	}
}

func TestCadenceStatusJSONContract(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, started)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"expected_coordinators", "dispatch_receipts", "recursive_artifact_paths", "overdue_members", "completion_bar"} {
		if !json.Valid(raw) || !containsJSONField(raw, field) {
			t.Fatalf("JSON missing %q: %s", field, raw)
		}
	}
}

func TestCmdCadenceStatusJSONReadsDefaultManifest(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	rosterDir := t.TempDir()
	backendDir := t.TempDir()
	cfg := roster.Config{
		XOAgent: "xo",
		Agents: []roster.Agent{
			{Name: "xo"},
			{Name: "backend", Coordinator: coordinatorFlag(true), WorktreePath: backendDir},
		},
	}
	rawRoster, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rosterPath := filepath.Join(rosterDir, "flotilla.json")
	if err := os.WriteFile(rosterPath, rawRoster, 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute)
	manifest := cadenceManifest{
		Version: cadenceManifestVersion, Nonce: "retro-2026-09-04",
		StartedAt: started.Format(time.RFC3339Nano), DueAt: started.Add(time.Hour).Format(time.RFC3339Nano),
		Members: []cadenceManifestMember{{
			Coordinator: "backend", DispatchNonce: "flotilla-dispatch-1234abcd",
			ArtifactPath: "state/retros/inputs/backend.md",
		}},
	}
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestDir := filepath.Join(rosterDir, "cadences")
	if err := os.MkdirAll(manifestDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, manifest.Nonce+".json"), rawManifest, 0o600); err != nil {
		t.Fatal(err)
	}
	writeCadenceArtifact(t, backendDir, manifest.Members[0].ArtifactPath, started.Add(30*time.Second))

	var commandErr error
	stdout, _ := captureStdoutStderr(t, func() {
		commandErr = cmdCadence([]string{"status", manifest.Nonce, "--json", "--roster", rosterPath})
	})
	if commandErr != nil {
		t.Fatal(commandErr)
	}
	var doc cadenceStatusDoc
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode command JSON: %v\n%s", err, stdout)
	}
	if doc.Nonce != manifest.Nonce || doc.CompletionBar.State != "complete" {
		t.Fatalf("command status = %+v", doc)
	}
}

func containsJSONField(raw []byte, field string) bool {
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	_, ok := doc[field]
	return ok
}

func TestParseCadenceStatusArgsAllowsNonceBeforeJSON(t *testing.T) {
	opts, err := parseCadenceStatusArgs([]string{"walk-2026-09-04", "--json", "--roster", "/tmp/flotilla.json"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.nonce != "walk-2026-09-04" || !opts.asJSON || opts.rosterPath != "/tmp/flotilla.json" {
		t.Fatalf("options = %+v", opts)
	}
	if _, err := parseCadenceStatusArgs([]string{"../escape", "--json"}); err == nil {
		t.Fatal("path-traversing nonce accepted")
	}
}

func TestLoadCadenceManifestRequiresRosterCoordinators(t *testing.T) {
	manifest, cfg, _, _ := cadenceFixture(t)
	manifest.Members[0].Coordinator = "missing"
	path := filepath.Join(t.TempDir(), "manifest.json")
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCadenceManifest(path, manifest.Nonce, cfg); err == nil {
		t.Fatal("non-coordinator member accepted")
	}
}
