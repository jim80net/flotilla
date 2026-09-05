package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
		PackagePath: "state/retros/cos-2026-09-04.md",
		Members: []cadenceManifestMember{
			{Coordinator: "backend", DispatchNonce: "flotilla-dispatch-aabbccdd", ArtifactPath: "state/retros/inputs/backend.md"},
			{Coordinator: "frontend", DispatchNonce: "flotilla-dispatch-eeff0011", ArtifactPath: "state/retros/inputs/frontend.md"},
		},
	}
	writeCadenceArtifact(t, rosterDir, manifest.PackagePath, started.Add(20*time.Minute))
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
	if doc.CompletionBar != (cadenceCompletionBar{Completed: 3, Total: 3, Remaining: 0, Percent: 100, State: "complete"}) {
		t.Fatalf("completion bar = %+v", doc.CompletionBar)
	}
}

func TestCadenceStatusNamesOverdueMissingMember(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	backend, _ := cfg.Agent("backend")
	writeCadenceArtifact(t, backend.WorktreePath, manifest.Members[0].ArtifactPath, started.Add(10*time.Minute))
	if _, err := dispatch.Consume(rosterDir, dispatch.ConsumedEntry{
		Nonce: manifest.Members[0].DispatchNonce, PayloadHash: "backend-payload", ConsumedAt: started.Add(5 * time.Minute),
		Reason: dispatch.ReasonDurableAck, Sender: "xo", Recipient: "backend",
	}); err != nil {
		t.Fatal(err)
	}
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
	if doc.CompletionBar != (cadenceCompletionBar{Completed: 2, Total: 3, Remaining: 1, Percent: 66, State: "overdue"}) {
		t.Fatalf("completion bar = %+v", doc.CompletionBar)
	}
}

func TestCadenceStatusDoesNotCompleteWithoutConsumedReceipt(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	manifest.Members = manifest.Members[:1]
	backend, _ := cfg.Agent("backend")
	writeCadenceArtifact(t, backend.WorktreePath, manifest.Members[0].ArtifactPath, started.Add(10*time.Minute))
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !doc.RecursiveArtifactPaths[0].Current || doc.DispatchReceipts[0].Disposition != "unknown" {
		t.Fatalf("evidence = artifact %+v receipt %+v", doc.RecursiveArtifactPaths[0], doc.DispatchReceipts[0])
	}
	if doc.CompletionBar != (cadenceCompletionBar{Completed: 1, Total: 2, Remaining: 1, Percent: 50, State: "in_progress"}) {
		t.Fatalf("completion bar = %+v", doc.CompletionBar)
	}
}

func TestCadenceStatusDoesNotCompleteWithoutSynthesisPackage(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	if err := os.Remove(filepath.Join(rosterDir, filepath.FromSlash(manifest.PackagePath))); err != nil {
		t.Fatal(err)
	}
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
	if doc.CompletionBar.State == "complete" || doc.CompletionBar.Completed != 2 || doc.CompletionBar.Total != 3 {
		t.Fatalf("missing synthesis package completion = %+v", doc.CompletionBar)
	}
	if doc.SynthesisPackage.Present || doc.SynthesisPackage.Current {
		t.Fatalf("missing synthesis package = %+v", doc.SynthesisPackage)
	}
}

func TestCadenceStatusDoesNotCompleteWithStaleSynthesisPackage(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	writeCadenceArtifact(t, rosterDir, manifest.PackagePath, started.Add(-time.Minute))
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if doc.SynthesisPackage.Current || doc.CompletionBar.State == "complete" {
		t.Fatalf("stale synthesis package completed cadence: package=%+v bar=%+v", doc.SynthesisPackage, doc.CompletionBar)
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

func TestCadenceStatusSynthesisCollisionUsesCoordinatorNeutralWording(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	manifest.PackagePath = filepath.Join(cfg.Agents[1].WorktreePath, filepath.FromSlash(manifest.Members[0].ArtifactPath))
	_, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(10*time.Minute))
	if err == nil || !strings.Contains(err.Error(), `artifact owners "synthesis package" and "backend"`) {
		t.Fatalf("collision error = %v, want coordinator-neutral owners", err)
	}
	if strings.Contains(err.Error(), "coordinators") {
		t.Fatalf("collision mislabels synthesis package as coordinator: %v", err)
	}
}

func TestCadenceStatusRejectsSharedArtifactIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alias string
	}{
		{name: "same-resolved-path"},
		{name: "symlink-alias", alias: "symlink"},
		{name: "hard-link-alias", alias: "hardlink"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest, cfg, rosterDir, started := cadenceFixture(t)
			backend, _ := cfg.Agent("backend")
			manifest.Members[1].ArtifactPath = manifest.Members[0].ArtifactPath
			writeCadenceArtifact(t, backend.WorktreePath, manifest.Members[0].ArtifactPath, started.Add(time.Minute))
			switch tc.alias {
			case "symlink":
				alias := filepath.Join(t.TempDir(), "shared-worktree")
				if err := os.Symlink(backend.WorktreePath, alias); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				cfg.Agents[2].WorktreePath = alias
			case "hardlink":
				frontend, _ := cfg.Agent("frontend")
				source := filepath.Join(backend.WorktreePath, filepath.FromSlash(manifest.Members[0].ArtifactPath))
				target := filepath.Join(frontend.WorktreePath, filepath.FromSlash(manifest.Members[1].ArtifactPath))
				if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(source, target); err != nil {
					t.Skipf("hard link unavailable: %v", err)
				}
			default:
				cfg.Agents[2].WorktreePath = backend.WorktreePath
			}
			_, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(10*time.Minute))
			if err == nil || !strings.Contains(err.Error(), "same artifact") {
				t.Fatalf("shared artifact error = %v", err)
			}
		})
	}
}

func TestCadenceStatusRejectsDanglingArtifactSymlink(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	frontend, _ := cfg.Agent("frontend")
	aliasPath := filepath.Join(frontend.WorktreePath, filepath.FromSlash(manifest.Members[1].ArtifactPath))
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0o700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(cfg.Agents[1].WorktreePath, filepath.FromSlash(manifest.Members[0].ArtifactPath))
	if err := os.Symlink(targetPath, aliasPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(10*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "unresolved symlink") {
		t.Fatalf("dangling artifact symlink error = %v", err)
	}
}

func TestCadenceStatusFailsClosedOnWrongReceiptRecipient(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	if _, err := dispatch.Consume(rosterDir, dispatch.ConsumedEntry{
		Nonce: manifest.Members[0].DispatchNonce, PayloadHash: "wrong-recipient", ConsumedAt: started.Add(time.Minute),
		Reason: dispatch.ReasonDurableAck, Sender: "xo", Recipient: "frontend",
	}); err != nil {
		t.Fatal(err)
	}
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	receipt := doc.DispatchReceipts[0]
	if receipt.Disposition != "recipient_mismatch" || receipt.LedgerDisposition != "consumed" || receipt.Recipient != "frontend" {
		t.Fatalf("wrong-recipient receipt = %+v", receipt)
	}
}

func TestCadenceStatusAcceptsCoordinatorAdjutantReceipt(t *testing.T) {
	manifest, cfg, rosterDir, started := cadenceFixture(t)
	cfg.Agents = append(cfg.Agents, roster.Agent{Name: "backend-adj", AdjutantFor: "backend"})
	if _, err := dispatch.Consume(rosterDir, dispatch.ConsumedEntry{
		Nonce: manifest.Members[0].DispatchNonce, PayloadHash: "adjutant-recipient", ConsumedAt: started.Add(time.Minute),
		Reason: dispatch.ReasonDurableAck, Sender: "xo", Recipient: "backend-adj",
	}); err != nil {
		t.Fatal(err)
	}
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, started.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.DispatchReceipts[0]; got.Disposition != "consumed" || got.LedgerDisposition != "" || got.Recipient != "backend-adj" {
		t.Fatalf("adjutant receipt = %+v", got)
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
	for _, field := range []string{"expected_coordinators", "dispatch_receipts", "recursive_artifact_paths", "synthesis_package", "overdue_members", "completion_bar"} {
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
		PackagePath: "state/retros/cos-2026-09-04.md",
		Members: []cadenceManifestMember{{
			Coordinator: "backend", DispatchNonce: "flotilla-dispatch-1234abcd",
			ArtifactPath: "state/retros/inputs/backend.md",
		}},
	}
	writeCadenceArtifact(t, rosterDir, manifest.PackagePath, started.Add(45*time.Second))
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
	if _, err := dispatch.Consume(rosterDir, dispatch.ConsumedEntry{
		Nonce: manifest.Members[0].DispatchNonce, PayloadHash: "backend-payload", ConsumedAt: started.Add(15 * time.Second),
		Reason: dispatch.ReasonDurableAck, Sender: "xo", Recipient: "backend",
	}); err != nil {
		t.Fatal(err)
	}

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

func TestLoadCadenceManifestRequiresSynthesisPackagePath(t *testing.T) {
	manifest, cfg, _, _ := cadenceFixture(t)
	manifest.PackagePath = ""
	path := filepath.Join(t.TempDir(), "manifest.json")
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCadenceManifest(path, manifest.Nonce, cfg); err == nil || !strings.Contains(err.Error(), "package_path") {
		t.Fatalf("missing package path error = %v", err)
	}
}

func TestLoadCadenceManifestRejectsDuplicateDispatchNonce(t *testing.T) {
	manifest, cfg, _, _ := cadenceFixture(t)
	manifest.Members[1].DispatchNonce = manifest.Members[0].DispatchNonce
	path := filepath.Join(t.TempDir(), "manifest.json")
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCadenceManifest(path, manifest.Nonce, cfg); err == nil || !strings.Contains(err.Error(), "share dispatch_nonce") {
		t.Fatalf("duplicate dispatch nonce error = %v", err)
	}
}
