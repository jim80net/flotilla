package cos

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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

// newNonce returns a fresh 128-bit random identity as lowercase hex — unique per clamped
// ledger line regardless of concurrency, and safe as a filename (hex only, no separators).
func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// isNonce reports whether s is a well-formed nonce (non-empty lowercase hex). The reader
// validates a nonce parsed from a ledger line before using it in a path, so a malformed or
// tampered token can never traverse out of the bodies dir.
func isNonce(s string) bool {
	if s == "" {
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
	if !isNonce(nonce) {
		return os.ErrInvalid
	}
	dir := BodiesDir(ledgerPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, nonce+".txt"), []byte(strings.TrimSpace(body)), 0o600)
}

// LookupBody returns the full body for a ledger entry by its nonce — an EXACT identity match
// (read <bodies>/<nonce>.txt), never a content scan. It returns ok=false for an empty or
// malformed nonce (e.g. a pre-#407 line carries none), or when no companion file exists (a
// best-effort write that failed) — every miss falls back cleanly to the clamped audit gist.
func LookupBody(ledgerPath, nonce string) (string, bool) {
	if !isNonce(nonce) {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(BodiesDir(ledgerPath), nonce+".txt"))
	if err != nil {
		return "", false
	}
	return string(b), true
}
