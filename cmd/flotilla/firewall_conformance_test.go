package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/readermap"
)

// The Go runtime firewall (RE2, internal/readermap) and the bash static guard
// (scripts/check-private-boundary.sh, PCRE) CANNOT share regex code — they share the
// gitignored TERM-LIST data instead. This conformance test is the actual "never
// diverge" guarantee: it feeds a shared fixture corpus through BOTH engines with the
// SAME deny/warn term lists and fails on any verdict mismatch.
//
// Scope: the SHARED surface — the deployment denylist, the advisory warnlist, and the
// built-in deployment-agnostic generic patterns (home paths, secret shapes). The
// canonical `<prefix>:<n>.<m>` pattern is Go-OWNED in P2 (the bash static guard mirrors
// it under #202, which extends this corpus then); fixtures here avoid that shape so the
// two engines are compared only where both currently implement the rule.

// the shared term lists fed to BOTH engines.
var conformanceDeny = []string{"acme-desk", "AcmeCorp"}
var conformanceWarn = []string{"flatten(ed|s|ing)?", "the special daemon"}

type conformanceCase struct {
	name string
	text string
	want readermap.FirewallDecision
}

// Leak-shaped fixtures are assembled from fragments so the literal pattern is never
// contiguous in this committed file (the static tree-scan guard would otherwise flag
// the conformance corpus itself). Both engines see the assembled whole.
var conformanceCorpus = []conformanceCase{
	{"clean prose", "the backfill finished and the gap is closed", readermap.FirewallOK},
	{"deny term", "the acme-desk reported in", readermap.FirewallRefuse},
	{"deny org", "deployed for AcmeCorp today", readermap.FirewallRefuse},
	{"generic home leak", "the file is at /home/" + "alice/work/notes.md", readermap.FirewallRefuse},
	{"allowlisted home placeholder", "docs use /home/operator/flotilla", readermap.FirewallOK},
	{"github token", "token ghp_" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ012345 here", readermap.FirewallRefuse},
	{"aws key", "AKIA" + "0123456789ABCDEF in the config", readermap.FirewallRefuse},
	{"warn vocab", "we flattened the position", readermap.FirewallWarn},
	{"warn phrase", "the special daemon restarted", readermap.FirewallWarn},
	{"deny beats warn", "the acme-desk flattened it", readermap.FirewallRefuse},
}

func TestFirewallConformance_GoMatchesBash(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "check-private-boundary.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Skipf("boundary guard script not found (%v) — skipping conformance", err)
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available — skipping conformance")
	}

	ts, err := readermap.NewTermSet(conformanceDeny, conformanceWarn)
	if err != nil {
		t.Fatalf("NewTermSet: %v", err)
	}

	for _, c := range conformanceCorpus {
		t.Run(c.name, func(t *testing.T) {
			got := readermap.Check(c.text, ts).Decision
			if got != c.want {
				t.Fatalf("Go firewall: %q → %v, want %v (the corpus expectation is wrong, or the Go engine regressed)", c.text, got, c.want)
			}
			bash := bashVerdict(t, script, conformanceDeny, conformanceWarn, c.text)
			if bash != c.want {
				t.Fatalf("CONFORMANCE MISMATCH: %q → Go=%v bash=%v (the two engines diverge on the shared term-list surface)", c.text, got, bash)
			}
		})
	}
}

// bashVerdict runs check-private-boundary.sh --file over a one-line fixture with the
// given deny/warn lists supplied via env (so no .flotilla file is needed), and maps the
// script's exit code + output to a FirewallDecision: exit 1 → Refuse; exit 0 with an
// ADVISORY WARN section → Warn; exit 0 otherwise → OK.
func bashVerdict(t *testing.T, script string, deny, warn []string, text string) readermap.FirewallDecision {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(tmp, []byte(text+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, "--file", tmp)
	cmd.Env = append(cleanFirewallEnviron(),
		"FLOTILLA_PRIVATE_DENYLIST="+strings.Join(deny, "|"),
		"FLOTILLA_PRIVATE_WARNLIST="+strings.Join(warn, "|"),
	)
	out, err := cmd.CombinedOutput()
	exit := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running boundary guard: %v\n%s", err, out)
		}
		exit = ee.ExitCode()
	}
	switch {
	case exit == 1:
		return readermap.FirewallRefuse
	case exit == 0 && strings.Contains(string(out), "ADVISORY WARN"):
		return readermap.FirewallWarn
	case exit == 0:
		return readermap.FirewallOK
	default:
		t.Fatalf("boundary guard exited %d (unexpected)\n%s", exit, out)
		return readermap.FirewallOK
	}
}

// cleanFirewallEnviron returns the process environment with every FLOTILLA_PRIVATE_*
// var stripped, so the host's real deployment lists cannot bleed into the conformance
// run (the test supplies its own deny/warn env).
func cleanFirewallEnviron() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "FLOTILLA_PRIVATE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
