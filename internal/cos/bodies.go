package cos

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// The companion full-body store (#407).
//
// The ledger LINE is a bounded, atomic, PIPE_BUF-safe audit record: its gist is clamped
// to maxGistRunes so a single O_APPEND stays atomic across the several appender processes
// (see Line / maxLineBytes). That clamp is CORRECT for the who-knows-what audit, but it
// means a message longer than the clamp is truncated at write time — and any surface that
// renders the clamped gist AS the message (the dash conversation thread) shows the operator
// a destroyed copy of his own words (#407).
//
// The fix is separation of concerns: keep the bounded atomic audit line UNCHANGED except for
// a small identity token, and persist the FULL body in a loopback-only companion store the
// dash can render from. Each clamped message gets a unique NONCE, written into BOTH the audit
// line (` #<nonce>` after the gist) and the companion filename (`<nonce>.txt`). Lookup is an
// EXACT match by that identity — never a content/prefix scan — so two same-second, same-party
// messages that happen to share their first maxGistRunes runes still resolve to their OWN
// bodies (cubic #422: prefix disambiguation could not tell them apart; the nonce can).
//
// Each body is one os.WriteFile (never an append → no cross-process interleaving, unlike the
// shared ledger line). The store is PRIVATE (loopback-only, under the roster dir alongside
// the ledger) and is never published — like the ledger itself.

const clampMarker = "…"

// BodiesDir is the companion store directory for a given ledger path: a "bodies"
// subdirectory alongside the ledger file.
func BodiesDir(ledgerPath string) string {
	return filepath.Join(filepath.Dir(ledgerPath), "bodies")
}

// WillClamp reports whether Line would clamp this gist (i.e. the full body is longer than
// the audit line can carry, so a companion body — and its nonce — is needed). It mirrors
// clampGist's decision exactly (TrimSpace, then compare the rune count to maxGistRunes) so
// the writer and the clamp never disagree.
func WillClamp(gist string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(gist)) > maxGistRunes
}

// nonceBytes is the companion nonce's entropy; NonceHexLen is its exact rendered width.
const (
	nonceBytes  = 16
	NonceHexLen = 2 * nonceBytes // 32 lowercase hex chars
)

// newNonce returns a fresh 128-bit random identity as lowercase hex — unique per clamped
// ledger line regardless of concurrency, and safe as a filename (hex only, no separators).
func newNonce() (string, error) {
	var b [nonceBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// IsNonce reports whether s is a well-formed companion nonce: EXACTLY NonceHexLen lowercase
// hex chars. It is the single shape check used everywhere a nonce crosses a trust boundary —
// before any filesystem use (path safety, so a tampered token cannot traverse out of the
// bodies dir) AND by the dash's ledger parser, which accepts a trailing token ONLY when it is
// a genuine nonce and otherwise falls the whole line back to raw (never mis-structures junk).
func IsNonce(s string) bool {
	if len(s) != NonceHexLen {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// WriteBody persists the FULL (whitespace-trimmed) body of one clamped exchange to the
// companion store under its nonce, best-effort. It writes ONE file with a single os.WriteFile
// (no append → no interleaving), creating the bodies dir on demand.
func WriteBody(ledgerPath, nonce, body string) error {
	if !IsNonce(nonce) {
		return os.ErrInvalid
	}
	dir := BodiesDir(ledgerPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, nonce+".txt"), []byte(strings.TrimSpace(body)), 0o600)
}

// BodyRetention bounds the companion store (#423): a body older than this is pruned, and
// the dash's lookup for that entry falls back to the clamped audit gist — the documented
// miss path, honest by design. 30 days comfortably exceeds the operator's thread-reading
// horizon while keeping the store from growing without bound over long operation. The
// audit LINE itself is never touched — retention applies only to the companion bodies.
const BodyRetention = 30 * 24 * time.Hour

// PruneBodies removes companion bodies whose file mtime is older than BodyRetention.
// Best-effort, like every store operation: an unreadable dir or a failed remove is
// silently skipped (a stale body that survives one pass is caught by the next). Only
// well-formed `<nonce>.txt` names are considered — anything else in the dir is not ours
// to delete. Called by Append after it writes a companion body, so unclamped appends
// (the common case) pay nothing and the scan is bounded by the store the pruning itself
// keeps small.
func PruneBodies(ledgerPath string, now time.Time) {
	dir := BodiesDir(ledgerPath)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-BodyRetention)
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".txt") || !IsNonce(strings.TrimSuffix(name, ".txt")) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// LookupBody returns the full body for a ledger entry by its nonce — an EXACT identity match
// (read <bodies>/<nonce>.txt), never a content scan. It returns ok=false for an empty or
// malformed nonce (e.g. a pre-#407 line carries none), or when no companion file exists (a
// best-effort write that failed) — every miss falls back cleanly to the clamped audit gist.
func LookupBody(ledgerPath, nonce string) (string, bool) {
	if !IsNonce(nonce) {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(BodiesDir(ledgerPath), nonce+".txt"))
	if err != nil {
		return "", false
	}
	return string(b), true
}
