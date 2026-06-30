package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/readermap"
)

// The I/O half of the partition firewall (Pillar D). The PURE detector
// (readermap.Check / readermap.TermSet) lives in internal/readermap; this file reads
// the deployment's gitignored private term sources from disk/env and compiles the
// TermSet once. It deliberately reads the SAME sources as scripts/check-private-boundary.sh
// — the runtime guard and the static CI guard share their DATA (not their regex code,
// since Go is RE2 and the bash guard is PCRE), and a conformance test pins the verdicts.

const (
	denylistEnv     = "FLOTILLA_PRIVATE_DENYLIST"      // a ready alternation: "term1|term2|..."
	denylistFileEnv = "FLOTILLA_PRIVATE_DENYLIST_FILE" // an explicit path override
	denylistDefault = ".flotilla/private-denylist"     // the default gitignored path (relative to cwd)
	warnlistEnv     = "FLOTILLA_PRIVATE_WARNLIST"
	warnlistFileEnv = "FLOTILLA_PRIVATE_WARNLIST_FILE"
	warnlistDefault = ".flotilla/private-warnlist"
)

// LoadFirewall builds the runtime firewall TermSet from the deployment's gitignored
// private term sources (the same sources the static guard reads). It NEVER hard-codes
// deployment vocabulary. With no sources configured the deploy lists are empty and only
// the built-in generic + canonical patterns apply (mirroring the bash guard's
// generic-always / deployment-only-if-configured model). A malformed term list
// (uncompilable regex) is a hard error — a silently-dropped denylist term is a silent
// partition hole, the exact failure this guard exists to prevent.
func LoadFirewall() (*readermap.TermSet, error) {
	deny, err := loadTermList(denylistEnv, denylistFileEnv, denylistDefault)
	if err != nil {
		return nil, fmt.Errorf("flotilla firewall: loading denylist: %w", err)
	}
	warn, err := loadTermList(warnlistEnv, warnlistFileEnv, warnlistDefault)
	if err != nil {
		return nil, fmt.Errorf("flotilla firewall: loading warnlist: %w", err)
	}
	return readermap.NewTermSet(deny, warn)
}

// firewallBounce formats the REFUSE bounce for a CLI egress (e.g. notify): the
// offending token + its generic abstraction, as a suggestion the desk applies
// in-context. It returns nil unless r is a Refuse, so a clean (OK/Warn) check leaves
// the success path byte-identical. NEVER rewrites the message — it only suggests.
func firewallBounce(verb string, r readermap.FirewallResult) error {
	if r.Decision != readermap.FirewallRefuse {
		return nil
	}
	return fmt.Errorf("%s REFUSED: the message contains a possible private leak %q — rewrite it to its generic abstraction (%s). Nothing was posted", verb, r.Token, r.Abstraction)
}

// loadTermList resolves one private term list in the SAME priority order as the bash
// guard: (1) the env alternation var (a single "a|b|c" string, used by CI from a repo
// secret so the list is never committed); (2) an explicit FILE-path env override; (3)
// the default gitignored path relative to cwd. A missing default file is NOT an error
// (an unconfigured deployment runs the generic patterns only); an explicitly-pointed
// FILE that cannot be read IS an error (the operator asked for it).
func loadTermList(envVar, fileEnvVar, defaultPath string) ([]string, error) {
	if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
		// Already an alternation — pass it through as a single regex fragment.
		return []string{v}, nil
	}
	path := defaultPath
	explicit := false
	if p := strings.TrimSpace(os.Getenv(fileEnvVar)); p != "" {
		path, explicit = p, true
	}
	terms, err := readTermFile(path)
	if err != nil {
		if os.IsNotExist(err) && !explicit {
			return nil, nil // unconfigured default → no deployment list
		}
		return nil, err
	}
	return terms, nil
}

// readTermFile reads one term per line, stripping blank lines and #-comments — the
// same line format the bash guard parses (grep -vE '^[[:space:]]*(#|$)').
func readTermFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var terms []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		terms = append(terms, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return terms, nil
}
