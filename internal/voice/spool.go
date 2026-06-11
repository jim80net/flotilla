package voice

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The outbound spool is the XO→voice transport for `flotilla speak` (design:
// openspec/changes/discord-voice §P1-3). It is a plain file-drop directory, chosen for
// MAXIMAL DECOUPLING: `speak` writes a file and returns, and the (separate, possibly
// down) `flotilla voice` process watches→consumes→deletes those files. Two invariants are
// load-bearing and must hold even when the voice process is dead:
//
//   - NEVER-BLOCKS-THE-TURN: `flotilla speak` MUST NOT block on, or fail because of, the
//     voice process's liveness. The XO calls `speak` from inside a turn; coupling that
//     turn to voice being up (a socket, an ack, a refuse-on-full) would let a down voice
//     process fail the XO's turn. Writing a file never waits on a reader, so it can't.
//
//   - DROP-OLDEST, NEVER REFUSE-NEW (the bound): a down voice process must never grow the
//     spool without limit, so the spool is capped (SpoolMaxFiles). On overflow the OLDEST
//     entries are deleted, NEVER the just-written one — refusing a new write would surface
//     an error into `speak` and thereby fail the XO turn, which the first invariant
//     forbids. A speak is always more current than what it evicts, so dropping the oldest
//     loses the least-relevant spoken line.
//
// Filenames are `<zero-padded-unix-nanos>-<random>.txt`: the fixed-width nanosecond prefix
// makes a lexical sort equal a chronological sort (so "oldest-first" is a cheap string
// sort, and the bound's eviction is deterministic), and the random suffix keeps two writes
// in the same nanosecond from colliding onto one file (an overwrite would silently lose a
// spoken line). The file BODY is exactly the text to speak.

const (
	// SpoolMaxFiles bounds the outbound spool. A live voice process drains it continuously,
	// so this only bites when voice is DOWN; 64 buffers a healthy burst of spoken replies
	// while a brief voice restart completes, yet caps a permanently-dead voice process at a
	// trivial on-disk footprint (64 short text files). On overflow the oldest are dropped.
	SpoolMaxFiles = 64

	// spoolEntryExt is the spool-file extension. Entries are plain UTF-8 text (the body is
	// the line to speak); the extension is purely a readable marker — listing filters on it
	// so a stray non-entry file in the dir is ignored rather than consumed as speech.
	spoolEntryExt = ".txt"

	// spoolNanoWidth zero-pads the unix-nanosecond filename prefix to a fixed width so a
	// lexical sort is a chronological sort. 19 digits covers unix-nanos through the year
	// 2262 (math.MaxInt64 ns ≈ 2262-04-11); every realistic timestamp is exactly 19 digits.
	spoolNanoWidth = 19

	// spoolRandHexLen is the length of the random hex suffix that disambiguates writes
	// sharing a nanosecond. 8 hex chars = 32 bits of entropy — collision-free in practice
	// for the at-most-a-handful of speaks that could ever share one nanosecond.
	spoolRandHexLen = 8

	// stateRootEnv overrides the project state root (for tests and non-standard layouts),
	// mirroring workspace.go's FLOTILLA_WORKSPACE_ROOT env-override idiom.
	stateRootEnv = "FLOTILLA_STATE_ROOT"
)

// SpoolDir returns the outbound spool directory, `<state-root>/voice/outbound`. The state
// root is $FLOTILLA_STATE_ROOT when set, else the relative `state` directory — the same
// cwd-relative project-state convention the roster default (`flotilla.json`) and
// `state/voice.env` already use. Both the producer (`flotilla speak`) and the consumer
// (`flotilla voice`) run from the same project working directory, so they resolve the same
// dir. The directory is created lazily on the first write (see WriteSpeak), never here, so
// merely asking for the path has no filesystem side effect.
func SpoolDir() string {
	root := os.Getenv(stateRootEnv)
	if root == "" {
		root = "state"
	}
	return filepath.Join(root, "voice", "outbound")
}

// WriteSpeak drops one spoken line onto the outbound spool and returns the path written.
// It is the writer behind `flotilla speak`: it creates the spool dir lazily (MkdirAll),
// writes the text as a single timestamped, collision-safe file, then enforces the bound by
// dropping the OLDEST entries — never the file it just wrote. It does NOT wait for, nor
// depend on, the voice process; a down voice process changes nothing about its success.
//
// A failure to enforce the bound (e.g. a racing reader already deleted an old entry) is
// NOT propagated: the write itself succeeded and that is the contract `speak` relies on —
// surfacing a trim error would fail the XO turn for a non-essential cleanup, violating the
// never-blocks-the-turn invariant. The cap is a soft ceiling, not a hard gate.
func WriteSpeak(text string) (string, error) {
	dir := SpoolDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("voice: create spool dir: %w", err)
	}
	name, err := spoolName(time.Now())
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	// O_EXCL: never silently overwrite — the random suffix already makes a same-nanosecond
	// collision astronomically unlikely, and O_EXCL turns the residual case into an error
	// rather than a lost spoken line.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("voice: write spool entry: %w", err)
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return "", fmt.Errorf("voice: write spool entry: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("voice: close spool entry: %w", err)
	}
	// Enforce the bound: drop oldest-first down to the cap. Best-effort by design — the
	// write already succeeded, so a trim hiccup must not fail speak (see the doc comment).
	trimSpool(dir, SpoolMaxFiles)
	return path, nil
}

// ListSpool returns the spool entry filenames (not full paths) oldest-first — the order the
// `flotilla voice` consumer drains them in. Because filenames carry a fixed-width
// nanosecond prefix, a lexical sort IS chronological order. Non-entry files (anything not
// ending in spoolEntryExt) are ignored. A missing spool dir is not an error: it just means
// nothing has been spoken yet, so the result is empty.
func ListSpool() ([]string, error) {
	dir := SpoolDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("voice: read spool dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), spoolEntryExt) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // fixed-width nanos prefix ⇒ lexical sort == oldest-first
	return names, nil
}

// ReadSpool reads one spool entry's body (the text to speak) by its filename (as returned
// by ListSpool). It does NOT delete the entry — the consumer reads, acts, then DeleteSpools
// so a crash between read and delete re-delivers rather than silently drops.
func ReadSpool(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(SpoolDir(), name))
	if err != nil {
		return "", fmt.Errorf("voice: read spool entry %q: %w", name, err)
	}
	return string(b), nil
}

// DeleteSpool removes one consumed spool entry by its filename. An already-absent entry is
// not an error (idempotent): a concurrent drop-oldest trim or a double-consume must not
// fail the consumer.
func DeleteSpool(name string) error {
	if err := os.Remove(filepath.Join(SpoolDir(), name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("voice: delete spool entry %q: %w", name, err)
	}
	return nil
}

// spoolName builds a spool filename for a write at t: a fixed-width unix-nanosecond prefix
// (lexical-sort == chronological-sort) plus a random hex suffix (same-nanosecond
// collision-safety), with the entry extension. The randomness is the only thing standing
// between two simultaneous speaks and a silent overwrite, so a CSPRNG read failure is a
// hard error rather than a degraded-but-colliding fallback.
func spoolName(t time.Time) (string, error) {
	var b [spoolRandHexLen / 2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("voice: spool name entropy: %w", err)
	}
	return fmt.Sprintf("%0*d-%s%s", spoolNanoWidth, t.UnixNano(), hex.EncodeToString(b[:]), spoolEntryExt), nil
}

// trimSpool enforces the drop-oldest bound: if the spool holds more than max entries, it
// deletes the oldest (count-max) of them. It is best-effort — list/remove errors are
// swallowed because trimming runs AFTER a successful write and must never fail speak (the
// never-blocks-the-turn invariant). The newest entries (including the just-written one,
// which sorts last) are always kept.
func trimSpool(dir string, max int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), spoolEntryExt) {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) <= max {
		return
	}
	sort.Strings(names) // oldest-first
	for _, name := range names[:len(names)-max] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}
