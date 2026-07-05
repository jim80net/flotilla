package cos

import (
	"crypto/sha256"
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
// The fix is separation of concerns: keep the bounded atomic audit line UNCHANGED, and
// persist the FULL body in a loopback-only companion store the dash can render from. Each
// clamped message's full body is written to ONE file (a single os.WriteFile — never an
// append, so there is no cross-process interleaving to guard against, unlike the shared
// ledger line). The store is keyed so the dash can find a ledger entry's full body from the
// entry's own (timestamp, from, to) plus the clamped gist as a disambiguating prefix.
//
// The store is PRIVATE (loopback-only, under the roster dir alongside the ledger) and is
// never published — like the ledger itself.

const clampMarker = "…"

// BodiesDir is the companion store directory for a given ledger path: a "bodies"
// subdirectory alongside the ledger file.
func BodiesDir(ledgerPath string) string {
	return filepath.Join(filepath.Dir(ledgerPath), "bodies")
}

// WillClamp reports whether Line would clamp this gist (i.e. the full body is longer than
// the audit line can carry, so a companion body is needed). It mirrors clampGist's decision
// exactly — TrimSpace, then compare the rune count to maxGistRunes — so the writer and the
// clamp never disagree.
func WillClamp(gist string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(gist)) > maxGistRunes
}

// bodyKey is the stable per-exchange key: the first 16 hex of sha256(ts | from | to). It is
// computed identically by the writer (WriteBody, from Entry) and the reader (LookupBody,
// from the parsed ledger entry), so a body written for an exchange is findable from that
// exchange's rendered ledger line. NUL separators prevent field-boundary collisions.
func bodyKey(ts, from, to string) string {
	sum := sha256.Sum256([]byte(ts + "\x00" + from + "\x00" + to))
	return hex.EncodeToString(sum[:])[:16]
}

// bodyFileName is the companion file name for a specific body: <key>-<bodyhash12>.txt. The
// body-hash suffix keeps two same-second, same-parties messages in DISTINCT files (the key
// alone would collide); the reader disambiguates by the clamped-gist prefix.
func bodyFileName(ts, from, to, body string) string {
	sum := sha256.Sum256([]byte(body))
	return bodyKey(ts, from, to) + "-" + hex.EncodeToString(sum[:])[:12] + ".txt"
}

// WriteBody persists the FULL (whitespace-trimmed) body of one clamped exchange to the
// companion store, best-effort. It writes ONE file with a single os.WriteFile (no append →
// no interleaving), creating the bodies dir on demand. Callers invoke it only when
// WillClamp(e.Gist) is true. The trimmed body is stored so the clamped gist (which is
// TrimSpace(body)[:maxGistRunes] + clampMarker) is a genuine prefix of the stored content —
// the reader's disambiguation contract.
func WriteBody(ledgerPath string, e Entry) error {
	dir := BodiesDir(ledgerPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ts := e.Time.UTC().Format(time.RFC3339)
	body := strings.TrimSpace(e.Gist)
	name := bodyFileName(ts, e.From, e.To, body)
	return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600)
}

// LookupBody returns the full body for a rendered ledger entry, if the companion store holds
// one. clampedGist is the entry's gist as parsed from the ledger (it ends with clampMarker
// when the body was clamped). It returns ok=false when the gist was not clamped, when no
// companion file exists (e.g. a pre-#407 line, or a body-write that failed best-effort), or
// on any read error — every miss falls back cleanly to the clamped gist. When several files
// share the (ts,from,to) key, the one whose content begins with the de-marked gist prefix is
// the exact match.
func LookupBody(ledgerPath, ts, from, to, clampedGist string) (string, bool) {
	if !strings.HasSuffix(clampedGist, clampMarker) {
		return "", false
	}
	prefix := strings.TrimSuffix(clampedGist, clampMarker)
	matches, err := filepath.Glob(filepath.Join(BodiesDir(ledgerPath), bodyKey(ts, from, to)+"-*.txt"))
	if err != nil {
		return "", false
	}
	var fallback string
	var haveFallback bool
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		content := string(b)
		if strings.HasPrefix(content, prefix) {
			return content, true
		}
		if !haveFallback {
			fallback, haveFallback = content, true
		}
	}
	// A single keyed file that does not prefix-match (e.g. the gist ended in a real "…"
	// coincidence) is still a better full copy than the clamped line; but only trust it when
	// it is the SOLE candidate, so an ambiguous multi-file key never renders the wrong body.
	if haveFallback && len(matches) == 1 {
		return fallback, true
	}
	return "", false
}
