package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/roster"
)

const cadenceManifestVersion = 1

var cadenceNoncePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type cadenceStatusArgs struct {
	nonce        string
	rosterPath   string
	manifestPath string
	asJSON       bool
}

// cadenceManifest is the durable ceremony contract written when a recursive
// cadence is dispatched. It binds prose-free completion checks to the exact
// coordinators, delivery identities, artifact paths, and wall-clock window.
type cadenceManifest struct {
	Version   int                     `json:"version"`
	Nonce     string                  `json:"nonce"`
	StartedAt string                  `json:"started_at"`
	DueAt     string                  `json:"due_at"`
	Members   []cadenceManifestMember `json:"members"`
}

type cadenceManifestMember struct {
	Coordinator   string `json:"coordinator"`
	DispatchNonce string `json:"dispatch_nonce"`
	ArtifactPath  string `json:"artifact_path"`
}

type cadenceStatusDoc struct {
	Nonce                  string                   `json:"nonce"`
	GeneratedAt            string                   `json:"generated_at"`
	StartedAt              string                   `json:"started_at"`
	DueAt                  string                   `json:"due_at"`
	ExpectedCoordinators   []string                 `json:"expected_coordinators"`
	DispatchReceipts       []cadenceDispatchReceipt `json:"dispatch_receipts"`
	RecursiveArtifactPaths []cadenceArtifactStatus  `json:"recursive_artifact_paths"`
	OverdueMembers         []string                 `json:"overdue_members"`
	CompletionBar          cadenceCompletionBar     `json:"completion_bar"`
}

type cadenceDispatchReceipt struct {
	Coordinator       string `json:"coordinator"`
	Nonce             string `json:"nonce"`
	Disposition       string `json:"disposition"`
	LedgerDisposition string `json:"ledger_disposition,omitempty"`
	Sender            string `json:"sender,omitempty"`
	Recipient         string `json:"recipient,omitempty"`
	Reason            string `json:"reason,omitempty"`
	ID                string `json:"id,omitempty"`
	Detail            string `json:"detail,omitempty"`
}

type cadenceArtifactStatus struct {
	Coordinator string `json:"coordinator"`
	Path        string `json:"path"`
	Present     bool   `json:"present"`
	NonEmpty    bool   `json:"non_empty"`
	Current     bool   `json:"current"`
	ModifiedAt  string `json:"modified_at,omitempty"`
}

type cadenceCompletionBar struct {
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Remaining int    `json:"remaining"`
	Percent   int    `json:"percent"`
	State     string `json:"state"`
}

func cmdCadence(args []string) error {
	if len(args) == 0 || args[0] != "status" {
		return fmt.Errorf("usage: flotilla cadence status <nonce> [--json] [--roster <path>] [--manifest <path>]")
	}
	opts, err := parseCadenceStatusArgs(args[1:])
	if err != nil {
		return err
	}
	rp, err := resolveRosterPath(opts.rosterPath)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(rp)
	if err != nil {
		return err
	}
	rosterDir := filepath.Dir(rp)
	manifestPath := opts.manifestPath
	if manifestPath == "" {
		manifestPath = filepath.Join(rosterDir, "cadences", opts.nonce+".json")
	} else if !filepath.IsAbs(manifestPath) {
		manifestPath = filepath.Join(rosterDir, manifestPath)
	}
	manifest, err := loadCadenceManifest(manifestPath, opts.nonce, cfg)
	if err != nil {
		return err
	}
	doc, err := buildCadenceStatus(manifest, cfg, rosterDir, time.Now().UTC())
	if err != nil {
		return err
	}
	if opts.asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(doc)
	}
	fmt.Printf("cadence %s: %d/%d complete (%s)", doc.Nonce, doc.CompletionBar.Completed, doc.CompletionBar.Total, doc.CompletionBar.State)
	if len(doc.OverdueMembers) > 0 {
		fmt.Printf("; overdue: %s", strings.Join(doc.OverdueMembers, ", "))
	}
	fmt.Println()
	return nil
}

// parseCadenceStatusArgs accepts flags before or after the nonce, matching the
// command shape operators use: `cadence status <nonce> --json`.
func parseCadenceStatusArgs(args []string) (cadenceStatusArgs, error) {
	opts := cadenceStatusArgs{rosterPath: rosterDefault()}
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--json":
			opts.asJSON = true
		case arg == "--roster" || arg == "--manifest":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("cadence status: %s requires a path", arg)
			}
			i++
			if arg == "--roster" {
				opts.rosterPath = args[i]
			} else {
				opts.manifestPath = args[i]
			}
		case strings.HasPrefix(arg, "--roster="):
			opts.rosterPath = strings.TrimPrefix(arg, "--roster=")
		case strings.HasPrefix(arg, "--manifest="):
			opts.manifestPath = strings.TrimPrefix(arg, "--manifest=")
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("cadence status: unknown flag %q", arg)
		case opts.nonce == "":
			opts.nonce = arg
		default:
			return opts, fmt.Errorf("usage: flotilla cadence status <nonce> [--json] [--roster <path>] [--manifest <path>]")
		}
	}
	if !cadenceNoncePattern.MatchString(opts.nonce) {
		return opts, fmt.Errorf("cadence status: invalid nonce %q", opts.nonce)
	}
	return opts, nil
}

func loadCadenceManifest(path, nonce string, cfg *roster.Config) (cadenceManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cadenceManifest{}, fmt.Errorf("cadence status: read manifest %q: %w", path, err)
	}
	var manifest cadenceManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return cadenceManifest{}, fmt.Errorf("cadence status: parse manifest %q: %w", path, err)
	}
	if manifest.Version != cadenceManifestVersion {
		return cadenceManifest{}, fmt.Errorf("cadence status: manifest %q version=%d, want %d", path, manifest.Version, cadenceManifestVersion)
	}
	if manifest.Nonce != nonce {
		return cadenceManifest{}, fmt.Errorf("cadence status: manifest nonce %q does not match requested nonce %q", manifest.Nonce, nonce)
	}
	started, err := time.Parse(time.RFC3339Nano, manifest.StartedAt)
	if err != nil {
		return cadenceManifest{}, fmt.Errorf("cadence status: invalid started_at %q", manifest.StartedAt)
	}
	due, err := time.Parse(time.RFC3339Nano, manifest.DueAt)
	if err != nil || !due.After(started) {
		return cadenceManifest{}, fmt.Errorf("cadence status: due_at %q must be after started_at", manifest.DueAt)
	}
	if len(manifest.Members) == 0 {
		return cadenceManifest{}, fmt.Errorf("cadence status: manifest has no expected coordinators")
	}
	seen := make(map[string]bool, len(manifest.Members))
	seenDispatch := make(map[string]string, len(manifest.Members))
	for _, member := range manifest.Members {
		if seen[member.Coordinator] {
			return cadenceManifest{}, fmt.Errorf("cadence status: duplicate coordinator %q", member.Coordinator)
		}
		seen[member.Coordinator] = true
		if !cfg.IsCoordinator(member.Coordinator) {
			return cadenceManifest{}, fmt.Errorf("cadence status: %q is not a roster coordinator", member.Coordinator)
		}
		if inbound.ParseDispatchNonce(member.DispatchNonce) != member.DispatchNonce {
			return cadenceManifest{}, fmt.Errorf("cadence status: coordinator %q has invalid dispatch_nonce %q", member.Coordinator, member.DispatchNonce)
		}
		if prior := seenDispatch[member.DispatchNonce]; prior != "" {
			return cadenceManifest{}, fmt.Errorf("cadence status: coordinators %q and %q share dispatch_nonce %q", prior, member.Coordinator, member.DispatchNonce)
		}
		seenDispatch[member.DispatchNonce] = member.Coordinator
		if strings.TrimSpace(member.ArtifactPath) == "" {
			return cadenceManifest{}, fmt.Errorf("cadence status: coordinator %q has no artifact_path", member.Coordinator)
		}
	}
	return manifest, nil
}

func buildCadenceStatus(manifest cadenceManifest, cfg *roster.Config, rosterDir string, now time.Time) (cadenceStatusDoc, error) {
	started, err := time.Parse(time.RFC3339Nano, manifest.StartedAt)
	if err != nil {
		return cadenceStatusDoc{}, err
	}
	due, err := time.Parse(time.RFC3339Nano, manifest.DueAt)
	if err != nil {
		return cadenceStatusDoc{}, err
	}
	doc := cadenceStatusDoc{
		Nonce: manifest.Nonce, GeneratedAt: now.UTC().Format(time.RFC3339Nano),
		StartedAt: started.UTC().Format(time.RFC3339Nano), DueAt: due.UTC().Format(time.RFC3339Nano),
		ExpectedCoordinators:   make([]string, 0, len(manifest.Members)),
		DispatchReceipts:       make([]cadenceDispatchReceipt, 0, len(manifest.Members)),
		RecursiveArtifactPaths: make([]cadenceArtifactStatus, 0, len(manifest.Members)),
		OverdueMembers:         []string{},
	}
	type resolvedMember struct {
		manifest cadenceManifestMember
		artifact cadenceArtifactStatus
	}
	resolved := make([]resolvedMember, 0, len(manifest.Members))
	artifactOwners := make(map[string]string, len(manifest.Members))
	type existingArtifact struct {
		owner string
		path  string
		info  os.FileInfo
	}
	existingArtifacts := make([]existingArtifact, 0, len(manifest.Members))
	for _, member := range manifest.Members {
		artifactPath, err := resolveCadenceArtifactPath(cfg, rosterDir, member)
		if err != nil {
			return cadenceStatusDoc{}, err
		}
		identity, err := canonicalCadenceArtifactPath(artifactPath)
		if err != nil {
			return cadenceStatusDoc{}, fmt.Errorf("cadence status: resolve artifact for %q: %w", member.Coordinator, err)
		}
		if prior := artifactOwners[identity]; prior != "" {
			return cadenceStatusDoc{}, fmt.Errorf("cadence status: coordinators %q and %q resolve to the same artifact %q", prior, member.Coordinator, identity)
		}
		artifactOwners[identity] = member.Coordinator
		artifact, info, err := inspectCadenceArtifact(member.Coordinator, artifactPath, started)
		if err != nil {
			return cadenceStatusDoc{}, fmt.Errorf("cadence status: inspect artifact for %q: %w", member.Coordinator, err)
		}
		if info != nil {
			for _, prior := range existingArtifacts {
				if os.SameFile(prior.info, info) {
					return cadenceStatusDoc{}, fmt.Errorf("cadence status: coordinators %q and %q resolve to the same artifact file %q and %q", prior.owner, member.Coordinator, prior.path, artifactPath)
				}
			}
			existingArtifacts = append(existingArtifacts, existingArtifact{owner: member.Coordinator, path: artifactPath, info: info})
		}
		resolved = append(resolved, resolvedMember{manifest: member, artifact: artifact})
	}

	completed := 0
	for _, resolvedMember := range resolved {
		member := resolvedMember.manifest
		doc.ExpectedCoordinators = append(doc.ExpectedCoordinators, member.Coordinator)
		status := dispatch.LookupNonce(rosterDir, member.DispatchNonce, now.UTC())
		receipt := cadenceDispatchReceipt{
			Coordinator: member.Coordinator, Nonce: member.DispatchNonce,
			Disposition: string(status.Disposition), Sender: status.Sender, Recipient: status.Recipient,
			Reason: status.Reason, ID: status.ID, Detail: status.Detail,
		}
		if status.Disposition != dispatch.DispositionUnknown && !cadenceReceiptRecipientAllowed(cfg, member.Coordinator, status.Recipient) {
			receipt.LedgerDisposition = receipt.Disposition
			receipt.Disposition = "recipient_mismatch"
			receipt.Detail = fmt.Sprintf("durable recipient %q does not match expected coordinator %q or its adjutant", status.Recipient, member.Coordinator)
		}
		doc.DispatchReceipts = append(doc.DispatchReceipts, receipt)
		artifact := resolvedMember.artifact
		doc.RecursiveArtifactPaths = append(doc.RecursiveArtifactPaths, artifact)
		if artifact.Current {
			completed++
		} else if !now.Before(due) {
			doc.OverdueMembers = append(doc.OverdueMembers, member.Coordinator)
		}
	}
	sort.Strings(doc.OverdueMembers)
	total := len(manifest.Members)
	state := "in_progress"
	if completed == total {
		state = "complete"
	} else if len(doc.OverdueMembers) > 0 {
		state = "overdue"
	}
	doc.CompletionBar = cadenceCompletionBar{
		Completed: completed, Total: total, Remaining: total - completed,
		Percent: completed * 100 / total, State: state,
	}
	return doc, nil
}

func resolveCadenceArtifactPath(cfg *roster.Config, rosterDir string, member cadenceManifestMember) (string, error) {
	artifactPath := strings.TrimSpace(member.ArtifactPath)
	if filepath.IsAbs(artifactPath) {
		return filepath.Clean(artifactPath), nil
	}
	base := rosterDir
	agent, err := cfg.Agent(member.Coordinator)
	if err != nil {
		return "", err
	}
	if agent.WorktreePath != "" {
		base = agent.WorktreePath
	}
	return filepath.Clean(filepath.Join(base, artifactPath)), nil
}

func canonicalCadenceArtifactPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(abs)
	suffix := ""
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if suffix != "" {
				resolved = filepath.Join(resolved, suffix)
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(abs), nil
		}
		if suffix == "" {
			suffix = filepath.Base(current)
		} else {
			suffix = filepath.Join(filepath.Base(current), suffix)
		}
		current = parent
	}
}

func cadenceReceiptRecipientAllowed(cfg *roster.Config, coordinator, recipient string) bool {
	if recipient == coordinator {
		return true
	}
	for _, agent := range cfg.Agents {
		adjutantFor := agent.AdjutantFor
		if adjutantFor == "" {
			adjutantFor = agent.AssistantFor
		}
		if agent.Name == recipient && adjutantFor == coordinator {
			return true
		}
	}
	return false
}

func inspectCadenceArtifact(coordinator, path string, started time.Time) (cadenceArtifactStatus, os.FileInfo, error) {
	result := cadenceArtifactStatus{Coordinator: coordinator, Path: path}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return result, nil, nil
	}
	if err != nil {
		return result, nil, err
	}
	if !info.Mode().IsRegular() {
		return result, info, nil
	}
	result.Present = true
	result.NonEmpty = info.Size() > 0
	result.ModifiedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	result.Current = result.NonEmpty && !info.ModTime().Before(started)
	return result, info, nil
}
