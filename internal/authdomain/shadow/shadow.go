// Package shadow implements the inert Authorization Domains D1/I1a policy
// compiler, store, evaluator, and differential export. A permit returned by
// this package is evidence only: this package has no production PEP or binding.
package shadow

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	SchemaVersion   = "authorization-domains/v1"
	RegistryVersion = "1"
	ActionRead      = "read"
	ReplaySchema    = "gatekeeper.auth-domains.replay/v1"
)

type ExactSelector struct {
	Kind     string `json:"kind"`
	ObjectID string `json:"object_id"`
}

type ProtectedBlock struct {
	ID             string        `json:"id"`
	ObjectSelector ExactSelector `json:"object_selector"`
	Actions        []string      `json:"actions"`
	Reason         string        `json:"reason"`
	Owner          string        `json:"owner"`
	AuditPolicy    string        `json:"audit_policy"`
	CreatedAt      time.Time     `json:"created_at"`
	ExpiresAt      *time.Time    `json:"expires_at,omitempty"`
}

type Lease struct {
	NotAfter            time.Time `json:"not_after"`
	MaxMaterializations uint64    `json:"max_materializations"`
}

type BlockException struct {
	ID             string        `json:"id"`
	BlockID        string        `json:"block_id"`
	PrincipalID    string        `json:"principal_id"`
	WorkerID       string        `json:"worker_id"`
	Actions        []string      `json:"actions"`
	ObjectSelector ExactSelector `json:"object_selector"`
	DomainID       string        `json:"domain_id"`
	SessionID      string        `json:"session_id"`
	IssuedBy       string        `json:"issued_by"`
	IssuedAt       time.Time     `json:"issued_at"`
	ExpiresAt      time.Time     `json:"expires_at"`
	Lease          Lease         `json:"lease"`
}

// PolicyGeneration is immutable after Compile. Digest covers the canonical
// document with Digest omitted.
type PolicyGeneration struct {
	SchemaVersion   string           `json:"schema_version"`
	Generation      uint64           `json:"generation"`
	ParentDigest    *string          `json:"parent_digest"`
	Digest          string           `json:"digest"`
	RegistryVersion string           `json:"registry_version"`
	Blocks          []ProtectedBlock `json:"blocks"`
	Exceptions      []BlockException `json:"exceptions"`
	CreatedAt       time.Time        `json:"created_at"`
}

type policyDigestDocument struct {
	SchemaVersion   string           `json:"schema_version"`
	Generation      uint64           `json:"generation"`
	ParentDigest    *string          `json:"parent_digest"`
	RegistryVersion string           `json:"registry_version"`
	Blocks          []ProtectedBlock `json:"blocks"`
	Exceptions      []BlockException `json:"exceptions"`
	CreatedAt       time.Time        `json:"created_at"`
}

func decodeStrict(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

// Compile validates a candidate without mutating any store.
func Compile(data []byte, now time.Time) (*PolicyGeneration, error) {
	var p PolicyGeneration
	if err := decodeStrict(data, &p); err != nil {
		return nil, fmt.Errorf("compile policy: %w", err)
	}
	if err := validatePolicy(&p, now); err != nil {
		return nil, err
	}
	want, err := policyDigest(p)
	if err != nil {
		return nil, err
	}
	if p.Digest != want {
		return nil, fmt.Errorf("compile policy: digest mismatch: got %q want %q", p.Digest, want)
	}
	return clonePolicy(&p), nil
}

func policyDigest(p PolicyGeneration) (string, error) {
	d := policyDigestDocument{p.SchemaVersion, p.Generation, p.ParentDigest, p.RegistryVersion, p.Blocks, p.Exceptions, p.CreatedAt}
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

// SealCandidate is a test/tooling helper that computes the canonical digest;
// the result still must pass Compile before publication.
func SealCandidate(p PolicyGeneration) ([]byte, error) {
	p.Digest = ""
	d, err := policyDigest(p)
	if err != nil {
		return nil, err
	}
	p.Digest = d
	return json.Marshal(p)
}

func validatePolicy(p *PolicyGeneration, now time.Time) error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("compile policy: unknown schema %q", p.SchemaVersion)
	}
	if p.RegistryVersion != RegistryVersion {
		return fmt.Errorf("compile policy: unknown registry %q", p.RegistryVersion)
	}
	if p.Generation == 0 {
		return errors.New("compile policy: generation must be positive")
	}
	if p.CreatedAt.IsZero() {
		return errors.New("compile policy: created_at is required")
	}
	ids, blocks := map[string]bool{}, map[string]ProtectedBlock{}
	selectors := map[string]string{}
	for _, b := range p.Blocks {
		if b.ID == "" || ids[b.ID] {
			return fmt.Errorf("compile policy: duplicate or empty block id %q", b.ID)
		}
		ids[b.ID], blocks[b.ID] = true, b
		if b.ObjectSelector.Kind != "exact" || !canonicalObject(b.ObjectSelector.ObjectID) {
			return fmt.Errorf("compile policy: block %q has non-canonical selector", b.ID)
		}
		if len(b.Actions) == 0 || !onlyRead(b.Actions) {
			return fmt.Errorf("compile policy: block %q has unsupported actions", b.ID)
		}
		if b.Reason == "" || b.Owner == "" || b.AuditPolicy != "durable_before_effect" || b.CreatedAt.IsZero() {
			return fmt.Errorf("compile policy: block %q is incomplete", b.ID)
		}
		if b.ExpiresAt != nil && !now.Before(*b.ExpiresAt) {
			return fmt.Errorf("compile policy: block %q is expired", b.ID)
		}
		key := b.ObjectSelector.ObjectID + "\x00" + strings.Join(sorted(b.Actions), ",")
		if prior := selectors[key]; prior != "" {
			return fmt.Errorf("compile policy: ambiguous blocks %q and %q", prior, b.ID)
		}
		selectors[key] = b.ID
	}
	seenExceptions := map[string]bool{}
	for _, e := range p.Exceptions {
		if e.ID == "" || seenExceptions[e.ID] {
			return fmt.Errorf("compile policy: duplicate or empty exception id %q", e.ID)
		}
		seenExceptions[e.ID] = true
		b, ok := blocks[e.BlockID]
		if !ok {
			return fmt.Errorf("compile policy: exception %q has dangling block", e.ID)
		}
		if e.PrincipalID == "" || e.WorkerID == "" || e.DomainID == "" || e.SessionID == "" || e.IssuedBy == "" || e.IssuedAt.IsZero() || e.ExpiresAt.IsZero() {
			return fmt.Errorf("compile policy: exception %q is incomplete", e.ID)
		}
		if !now.Before(e.ExpiresAt) || !now.Before(e.Lease.NotAfter) || e.Lease.MaxMaterializations != 1 {
			return fmt.Errorf("compile policy: exception %q has invalid lease or expiry", e.ID)
		}
		if e.ObjectSelector != b.ObjectSelector || len(e.Actions) == 0 || !subset(e.Actions, b.Actions) {
			return fmt.Errorf("compile policy: exception %q widens block", e.ID)
		}
	}
	return nil
}

func canonicalObject(v string) bool {
	return v != "" && !strings.ContainsAny(v, " \t\r\n") && strings.Contains(v, "://")
}
func onlyRead(v []string) bool { return len(v) == 1 && v[0] == ActionRead }
func subset(a, b []string) bool {
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}
func contains(v []string, x string) bool {
	for _, y := range v {
		if x == y {
			return true
		}
	}
	return false
}
func sorted(v []string) []string { out := append([]string(nil), v...); sort.Strings(out); return out }
func clonePolicy(p *PolicyGeneration) *PolicyGeneration {
	b, _ := json.Marshal(p)
	var out PolicyGeneration
	_ = json.Unmarshal(b, &out)
	return &out
}

type PublishResult struct {
	Generation uint64
	Digest     string
	Idempotent bool
}
type idempotencyRecord struct {
	Digest     string `json:"digest"`
	Generation uint64 `json:"generation"`
}
type headFile struct {
	Generation  uint64                       `json:"generation"`
	Digest      string                       `json:"digest"`
	Idempotency map[string]idempotencyRecord `json:"idempotency"`
}

type Store struct {
	dir         string
	mu          sync.RWMutex
	lastGood    *PolicyGeneration
	idempotency map[string]idempotencyRecord
}

func OpenStore(dir string, now time.Time) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, idempotency: map[string]idempotencyRecord{}}
	if err := s.recover(now); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) LastGood() *PolicyGeneration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePolicy(s.lastGood)
}

// PolicyFor is the worker-facing read seam. It accepts the complete minted
// context and never a parallel caller-selected domain.
func (s *Store) PolicyFor(context DomainContext, now time.Time) (*PolicyGeneration, error) {
	if !validContext(context, now) {
		return nil, errors.New("policy store: invalid domain context")
	}
	return s.LastGood(), nil
}

// Publish atomically adopts a fully compiled immutable successor. Rejections
// leave the complete last-good snapshot untouched.
func (s *Store) Publish(candidate []byte, expected uint64, key string, now time.Time) (PublishResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == "" {
		return PublishResult{}, errors.New("publish policy: idempotency key is required")
	}
	p, err := Compile(candidate, now)
	if err != nil {
		return PublishResult{}, err
	}
	if prior, ok := s.idempotency[key]; ok {
		if prior.Digest != p.Digest {
			return PublishResult{}, errors.New("publish policy: idempotency key reused with different digest")
		}
		return PublishResult{prior.Generation, prior.Digest, true}, nil
	}
	current := uint64(0)
	var parent *string
	if s.lastGood != nil {
		current, parent = s.lastGood.Generation, &s.lastGood.Digest
	}
	if expected != current {
		return PublishResult{}, fmt.Errorf("publish policy: stale expected generation %d; current is %d", expected, current)
	}
	if p.Generation != current+1 {
		return PublishResult{}, fmt.Errorf("publish policy: successor generation must be %d", current+1)
	}
	if (parent == nil) != (p.ParentDigest == nil) || parent != nil && *parent != *p.ParentDigest {
		return PublishResult{}, errors.New("publish policy: parent digest mismatch")
	}
	genPath := s.generationPath(p.Generation)
	if _, err := os.Stat(genPath); err == nil {
		return PublishResult{}, errors.New("publish policy: immutable generation already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return PublishResult{}, err
	}
	if err := atomicWriteNew(genPath, candidate); err != nil {
		return PublishResult{}, err
	}
	nextIDs := cloneIDs(s.idempotency)
	nextIDs[key] = idempotencyRecord{p.Digest, p.Generation}
	if err := s.writeHead(headFile{p.Generation, p.Digest, nextIDs}); err != nil {
		return PublishResult{}, err
	}
	s.lastGood, s.idempotency = p, nextIDs
	return PublishResult{p.Generation, p.Digest, false}, nil
}

func (s *Store) generationPath(g uint64) string {
	return filepath.Join(s.dir, fmt.Sprintf("generation-%020d.json", g))
}
func cloneIDs(in map[string]idempotencyRecord) map[string]idempotencyRecord {
	out := make(map[string]idempotencyRecord, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func atomicWriteNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	ok = true
	return syncDir(filepath.Dir(path))
}

func (s *Store) writeHead(h headFile) error {
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, fmt.Sprintf(".head-%d.tmp", time.Now().UnixNano()))
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, 0)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	err = f.Sync()
	_ = f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(s.dir, "head.json")); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(s.dir)
}
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// recover adopts only the generation named by the committed head after
// validating its complete predecessor chain. Uncommitted successor files are
// orphans and can never become authoritative merely because they exist.
func (s *Store) recover(now time.Time) error {
	var h headFile
	b, err := os.ReadFile(filepath.Join(s.dir, "head.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recover policy: read committed head: %w", err)
	}
	if err := decodeStrict(b, &h); err != nil {
		return fmt.Errorf("recover policy: invalid committed head: %w", err)
	}
	if h.Generation == 0 || h.Digest == "" {
		return errors.New("recover policy: invalid committed head identity")
	}
	chain := make(map[uint64]*PolicyGeneration, h.Generation)
	for generation := uint64(1); generation <= h.Generation; generation++ {
		raw, err := os.ReadFile(s.generationPath(generation))
		if err != nil {
			return fmt.Errorf("recover policy: committed generation %d unavailable: %w", generation, err)
		}
		var envelope PolicyGeneration
		if err := decodeStrict(raw, &envelope); err != nil {
			return fmt.Errorf("recover policy: committed generation %d malformed: %w", generation, err)
		}
		// Validate expiration at admission time. Restart time cannot rewrite
		// immutable policy history merely because wall time advanced.
		p, err := Compile(raw, envelope.CreatedAt)
		if err != nil {
			return fmt.Errorf("recover policy: committed generation %d invalid: %w", generation, err)
		}
		if p.Generation != generation {
			return fmt.Errorf("recover policy: generation file %d contains generation %d", generation, p.Generation)
		}
		if generation == 1 {
			if p.ParentDigest != nil {
				return errors.New("recover policy: genesis has a parent digest")
			}
		} else {
			prior := chain[generation-1]
			if p.ParentDigest == nil || *p.ParentDigest != prior.Digest {
				return fmt.Errorf("recover policy: generation %d predecessor digest mismatch", generation)
			}
		}
		chain[generation] = p
	}
	committed := chain[h.Generation]
	if committed.Digest != h.Digest {
		return errors.New("recover policy: committed head digest mismatch")
	}
	for key, record := range h.Idempotency {
		p := chain[record.Generation]
		if key == "" || p == nil || p.Digest != record.Digest {
			return fmt.Errorf("recover policy: invalid idempotency record %q", key)
		}
	}
	s.lastGood = committed
	s.idempotency = cloneIDs(h.Idempotency)
	return nil
}

type Resolution struct {
	Source          string `json:"source"`
	ResolverVersion string `json:"resolver_version"`
	EvidenceDigest  string `json:"evidence_digest"`
}
type RuntimeIdentity struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
}
type DomainContext struct {
	SchemaVersion   string          `json:"schema_version"`
	ContextID       string          `json:"context_id"`
	DomainID        string          `json:"domain_id"`
	Resolution      Resolution      `json:"resolution"`
	PrincipalID     string          `json:"principal_id"`
	WorkerID        string          `json:"worker_id"`
	SessionID       string          `json:"session_id"`
	RuntimeIdentity RuntimeIdentity `json:"runtime_identity"`
	IsolationClaim  string          `json:"isolation_claim"`
	IssuedAt        time.Time       `json:"issued_at"`
	ExpiresAt       time.Time       `json:"expires_at"`
	MintAuthority   string          `json:"mint_authority"`
	ClaimedDomainID string          `json:"claimed_domain_id,omitempty"`
	seal            [32]byte
}

func (c DomainContext) StorageKey(objectID string) (string, error) {
	if !sealedContext(c) || c.DomainID == "" || !canonicalObject(objectID) {
		return "", errors.New("storage key: invalid domain context or object")
	}
	return c.DomainID + "\x00" + objectID, nil
}

func (c DomainContext) ReplayKey(decisionID, pepID string, ordinal uint64) (string, error) {
	if !sealedContext(c) || c.DomainID == "" || c.ContextID == "" || decisionID == "" || pepID == "" || ordinal == 0 {
		return "", errors.New("replay key: incomplete context or binding")
	}
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", c.DomainID, c.ContextID, decisionID, pepID, ordinal), nil
}

type MintInput struct {
	ResolvedDomainID, PrincipalID, WorkerID, SessionID, RuntimeKind, RuntimeSubject, ResolverVersion, EvidenceDigest, ClaimedDomainID string
	IsolationClaim                                                                                                                    string
	TTL                                                                                                                               time.Duration
}

const serverMintAuthority = "flotilla.authdomain.context-mint/v1"

// contextMinter is the package-owned constructor capability. Shadow ingress
// can mint; external callers cannot obtain or implement the capability.
type contextMinter struct{ enabled bool }

func newServerContextMinter() contextMinter { return contextMinter{enabled: true} }

// Mint accepts resolved server observations. Caller-supplied community
// overrides must be rejected by ingress before this seam.
func (m contextMinter) Mint(in MintInput, now time.Time) (DomainContext, error) {
	if !m.enabled {
		return DomainContext{}, errors.New("mint context: server minter is required")
	}
	if in.ResolvedDomainID == "" || in.PrincipalID == "" || in.WorkerID == "" || in.SessionID == "" || in.ResolverVersion == "" || in.EvidenceDigest == "" || in.TTL <= 0 {
		return DomainContext{}, errors.New("mint context: incomplete server observation")
	}
	if in.RuntimeKind != "linux_user" && in.RuntimeKind != "container" {
		return DomainContext{}, errors.New("mint context: invalid runtime kind")
	}
	if in.IsolationClaim != "unproved" && in.IsolationClaim != "proved_linux_user" && in.IsolationClaim != "proved_container" {
		return DomainContext{}, errors.New("mint context: invalid isolation claim")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return DomainContext{}, err
	}
	c := DomainContext{SchemaVersion: SchemaVersion, ContextID: hex.EncodeToString(idBytes), DomainID: in.ResolvedDomainID, Resolution: Resolution{"server_observed_host", in.ResolverVersion, in.EvidenceDigest}, PrincipalID: in.PrincipalID, WorkerID: in.WorkerID, SessionID: in.SessionID, RuntimeIdentity: RuntimeIdentity{in.RuntimeKind, in.RuntimeSubject}, IsolationClaim: in.IsolationClaim, IssuedAt: now, ExpiresAt: now.Add(in.TTL), MintAuthority: serverMintAuthority, ClaimedDomainID: in.ClaimedDomainID}
	c.seal = contextSeal(c)
	return c, nil
}

func contextSeal(c DomainContext) [32]byte {
	payload := struct {
		SchemaVersion, ContextID, DomainID string
		Resolution                         Resolution
		PrincipalID, WorkerID, SessionID   string
		RuntimeIdentity                    RuntimeIdentity
		IsolationClaim                     string
		IssuedAt, ExpiresAt                time.Time
		MintAuthority, ClaimedDomainID     string
	}{c.SchemaVersion, c.ContextID, c.DomainID, c.Resolution, c.PrincipalID, c.WorkerID, c.SessionID, c.RuntimeIdentity, c.IsolationClaim, c.IssuedAt, c.ExpiresAt, c.MintAuthority, c.ClaimedDomainID}
	b, _ := json.Marshal(payload)
	return sha256.Sum256(append([]byte("authdomain-server-context-v1\x00"), b...))
}

type AuthzRequest struct {
	SchemaVersion           string
	RequestID               string
	DomainContext           DomainContext
	Action                  string
	ObjectID                string
	CanonicalizationVersion string
	PolicyGeneration        uint64
	ClassifierVersion       string
	RequestedAt             time.Time
}
type Outcome string

const (
	PermitUnblocked Outcome = "permit_unblocked"
	PermitException Outcome = "permit_exception"
	DenyBlocked     Outcome = "deny_blocked"
)
const (
	ReasonUnprotected            = "unprotected"
	ReasonProtectedBlock         = "protected_block"
	ReasonException              = "exact_exception"
	ReasonStaleGeneration        = "stale_generation"
	ReasonInvalidContext         = "invalid_context"
	ReasonUnknownProtectedAction = "unknown_protected_action"
	ReasonPolicyUnavailable      = "policy_unavailable"
	ReasonContextSubstitution    = "context_substitution"
)

type AuthzDecision struct {
	SchemaVersion        string
	RequestID            string
	Decision             Outcome
	ReasonCode           string
	PolicyGeneration     uint64
	BlockIDs             []string
	ExceptionID          string
	EvaluatedConstraints []string
	LeaseNotAfter        *time.Time
	DecisionID           string
	DecidedAt            time.Time
}

// Evaluate is pure and deterministic. DecidedAt is supplied explicitly and
// DecisionID is derived from inputs and output; it performs no admission/effect.
func Evaluate(p *PolicyGeneration, req AuthzRequest, now time.Time) AuthzDecision {
	d := AuthzDecision{SchemaVersion: SchemaVersion, RequestID: req.RequestID, Decision: DenyBlocked, ReasonCode: ReasonPolicyUnavailable, DecidedAt: now}
	if p == nil {
		return finishDecision(d, req)
	}
	d.PolicyGeneration = p.Generation
	matching := []ProtectedBlock{}
	for _, b := range p.Blocks {
		if b.ObjectSelector.ObjectID == req.ObjectID {
			matching = append(matching, b)
			d.BlockIDs = append(d.BlockIDs, b.ID)
		}
	}
	if len(matching) == 0 {
		d.Decision = PermitUnblocked
		d.ReasonCode = ReasonUnprotected
		return finishDecision(d, req)
	}
	if req.PolicyGeneration != p.Generation {
		d.ReasonCode = ReasonStaleGeneration
		return finishDecision(d, req)
	}
	if !validContext(req.DomainContext, now) {
		d.ReasonCode = ReasonInvalidContext
		return finishDecision(d, req)
	}
	if req.Action != ActionRead {
		d.ReasonCode = ReasonUnknownProtectedAction
		return finishDecision(d, req)
	}
	for _, e := range p.Exceptions {
		if exactException(e, matching, req, now) {
			d.Decision = PermitException
			d.ReasonCode = ReasonException
			d.ExceptionID = e.ID
			d.LeaseNotAfter = &e.Lease.NotAfter
			d.EvaluatedConstraints = []string{"domain", "principal", "worker", "session", "object", "action", "lease"}
			return finishDecision(d, req)
		}
	}
	for _, e := range p.Exceptions {
		if exceptionTargets(e, matching, req) {
			d.ReasonCode = ReasonContextSubstitution
			return finishDecision(d, req)
		}
	}
	d.ReasonCode = ReasonProtectedBlock
	return finishDecision(d, req)
}
func exceptionTargets(e BlockException, blocks []ProtectedBlock, r AuthzRequest) bool {
	if e.ObjectSelector.ObjectID != r.ObjectID || !contains(e.Actions, r.Action) {
		return false
	}
	for _, b := range blocks {
		if e.BlockID == b.ID {
			return true
		}
	}
	return false
}
func validContext(c DomainContext, now time.Time) bool {
	return sealedContext(c) && c.SchemaVersion == SchemaVersion && c.ContextID != "" && c.DomainID != "" && c.PrincipalID != "" && c.WorkerID != "" && c.SessionID != "" && c.Resolution.Source == "server_observed_host" && !now.Before(c.IssuedAt) && now.Before(c.ExpiresAt)
}
func sealedContext(c DomainContext) bool {
	want := contextSeal(c)
	return subtle.ConstantTimeCompare(c.seal[:], want[:]) == 1 && c.MintAuthority == serverMintAuthority
}
func exactException(e BlockException, blocks []ProtectedBlock, r AuthzRequest, now time.Time) bool {
	if e.DomainID != r.DomainContext.DomainID || e.PrincipalID != r.DomainContext.PrincipalID || e.WorkerID != r.DomainContext.WorkerID || e.SessionID != r.DomainContext.SessionID || e.ObjectSelector.ObjectID != r.ObjectID || !contains(e.Actions, r.Action) || !now.Before(e.ExpiresAt) || !now.Before(e.Lease.NotAfter) {
		return false
	}
	for _, b := range blocks {
		if e.BlockID != b.ID {
			return false
		}
	}
	return true
}
func finishDecision(d AuthzDecision, r AuthzRequest) AuthzDecision {
	b, _ := json.Marshal(struct {
		Request    AuthzRequest
		Decision   Outcome
		Reason     string
		Generation uint64
		Blocks     []string
		Exception  string
	}{r, d.Decision, d.ReasonCode, d.PolicyGeneration, d.BlockIDs, d.ExceptionID})
	h := sha256.Sum256(b)
	d.DecisionID = hex.EncodeToString(h[:])
	return d
}

type ReplayContext struct {
	ContextID string `json:"context_id"`
	WorkerID  string `json:"worker_id"`
	SessionID string `json:"session_id"`
	DomainID  string `json:"domain_id"`
	MintedBy  string `json:"minted_by"`
}
type ReplayCoverage struct {
	Name     string `json:"name"`
	Critical bool   `json:"critical"`
	Traced   bool   `json:"traced"`
}
type ReplayRequest struct {
	Class  string `json:"class"`
	Action string `json:"action"`
	Object string `json:"object"`
}
type ReplayDecision struct {
	Outcome           string `json:"outcome"`
	Reason            string `json:"reason"`
	ContextSource     string `json:"context_source"`
	ResolvedContextID string `json:"resolved_context_id"`
}
type ReplayRecord struct {
	ID              string         `json:"id"`
	Seam            string         `json:"seam"`
	Request         ReplayRequest  `json:"request"`
	ClaimedContext  *ReplayContext `json:"claimed_context"`
	ResolvedContext *ReplayContext `json:"resolved_context"`
	Decision        ReplayDecision `json:"decision"`
}
type ReplayExport struct {
	Schema                  string           `json:"schema"`
	LifecycleContractSHA256 string           `json:"lifecycle_contract_sha256"`
	Coverage                []ReplayCoverage `json:"coverage"`
	Records                 []ReplayRecord   `json:"records"`
	Probes                  []any            `json:"probes"`
}

const NeutralFixtureObject = "fixture://authorization-domains/protected/exact-read-object"
const LifecycleContractSHA256 = "4a5d12ff96b136db5bd7e78c9467a222c242be99c060d5a17fe267725bc9caff"

// AdaptNeutralDecision performs the deliberately narrow I1a projection. It
// cannot represent an exception permit and therefore rejects one.
func AdaptNeutralDecision(id, seam, class string, req AuthzRequest, decision AuthzDecision) (ReplayRecord, error) {
	if req.RequestID == "" || decision.RequestID != req.RequestID {
		return ReplayRecord{}, errors.New("replay adapter: request and decision are not correlated")
	}
	if decision.Decision == PermitException {
		return ReplayRecord{}, errors.New("replay adapter: permit_exception is not representable")
	}
	record := ReplayRecord{ID: id, Seam: seam, Request: ReplayRequest{Class: class, Action: req.Action, Object: req.ObjectID}}
	if decision.Decision == PermitUnblocked {
		record.Decision = ReplayDecision{Outcome: "allow", Reason: "unprotected", ContextSource: "none"}
		return record, nil
	}
	c := req.DomainContext
	if !validContext(c, decision.DecidedAt) {
		return ReplayRecord{}, errors.New("replay adapter: invalid server domain context")
	}
	record.ResolvedContext = &ReplayContext{ContextID: c.ContextID, WorkerID: c.WorkerID, SessionID: c.SessionID, DomainID: c.DomainID, MintedBy: c.MintAuthority}
	if c.ClaimedDomainID != "" {
		record.ClaimedContext = &ReplayContext{ContextID: "untrusted-claim", WorkerID: c.WorkerID, SessionID: c.SessionID, DomainID: c.ClaimedDomainID, MintedBy: "caller-claim"}
	}
	record.Decision = ReplayDecision{Outcome: "deny", Reason: "protected_block", ContextSource: "server_resolved", ResolvedContextID: c.ContextID}
	return record, nil
}

// ExportNeutral projects only the contract's supported allow-unprotected and
// deny-protected outcomes. Exception permits are deliberately unrepresentable.
func ExportNeutral(lifecycleDigest string, records []ReplayRecord) ([]byte, error) {
	if lifecycleDigest != LifecycleContractSHA256 {
		return nil, errors.New("replay export: lifecycle digest does not match pinned contract")
	}
	allowedSeams := map[string]bool{"ordinary-work": true, "protected-read-pep": true, "protected-read-audit": true}
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, r := range records {
		if !allowedSeams[r.Seam] {
			return nil, fmt.Errorf("replay export: unknown seam %q", r.Seam)
		}
		seen[r.Seam] = true
		counts[r.Seam]++
		switch r.Seam {
		case "ordinary-work":
			if r.Decision.Outcome != "allow" || r.Decision.Reason != "unprotected" || r.Decision.ContextSource != "none" || r.Decision.ResolvedContextID != "" || r.Request.Object == NeutralFixtureObject || r.ClaimedContext != nil || r.ResolvedContext != nil {
				return nil, errors.New("replay export: invalid ordinary-work projection")
			}
		case "protected-read-pep", "protected-read-audit":
			if r.Request.Action != ActionRead || r.Request.Object != NeutralFixtureObject || r.Decision.Outcome != "deny" || r.Decision.Reason != "protected_block" || r.ResolvedContext == nil || r.Decision.ContextSource != "server_resolved" || r.Decision.ResolvedContextID != r.ResolvedContext.ContextID {
				return nil, errors.New("replay export: invalid protected context projection")
			}
		}
	}
	for _, s := range []string{"ordinary-work", "protected-read-pep", "protected-read-audit"} {
		if !seen[s] || counts[s] != 1 {
			return nil, fmt.Errorf("replay export: seam %q must appear exactly once", s)
		}
	}
	e := ReplayExport{Schema: ReplaySchema, LifecycleContractSHA256: lifecycleDigest, Coverage: []ReplayCoverage{{"ordinary-work", false, true}, {"protected-read-pep", true, true}, {"protected-read-audit", true, true}}, Records: records, Probes: []any{}}
	return json.Marshal(e)
}
