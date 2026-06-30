package readermap

import (
	"strings"
	"testing"
)

// termSet builds a TermSet from injected deny/warn slices — the PURE path (no file
// or env I/O; the loader in cmd/flotilla supplies the gitignored sources at runtime).
func termSet(t *testing.T, deny, warn []string) *TermSet {
	t.Helper()
	ts, err := NewTermSet(deny, warn)
	if err != nil {
		t.Fatalf("NewTermSet(%v, %v): %v", deny, warn, err)
	}
	return ts
}

// Generic-leak fixtures are ASSEMBLED from fragments at runtime, so the literal leak
// pattern is never contiguous in this committed test file — otherwise the static
// boundary guard (scripts/check-private-boundary.sh, which greps the tracked tree)
// would flag the test's OWN fixture as a real leak. The runtime firewall sees the
// assembled whole and refuses it, which is exactly what these tests assert. The desk
// names below (worker-desk, example-desk) are obviously-generic placeholders — never a
// real deployment codename (that would itself be a partition leak).
var (
	fxHomePath = "/home/" + "alice/work/notes.md"
	fxWebhook  = "https://discord.com/api/webhooks/123456789/" + "AbCdEfGhIjKlMnOpQrStUvWx"
	fxGhp      = "ghp_" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ012345"
	fxSkAnt    = "sk-ant-" + "abcdefghijklmnopqrstuvwxyz0123"
	fxAkia     = "AKIA" + "0123456789ABCDEF"
)

// --- denylist: a fail-closed REFUSE -----------------------------------------

func TestFirewall_DenylistTermRefuses(t *testing.T) {
	ts := termSet(t, []string{"acme-desk", "AcmeCorp"}, nil)
	r := Check("the acme-desk finished its run", ts)
	if r.Decision != FirewallRefuse {
		t.Fatalf("a denylisted term must REFUSE, got %v", r.Decision)
	}
	if r.Token == "" {
		t.Fatal("a refusal must return the offending token")
	}
	if r.Abstraction == "" {
		t.Fatal("a refusal must return a generic abstraction suggestion")
	}
	if !strings.Contains("the acme-desk finished its run", r.Token) {
		t.Fatalf("the token %q must be a substring of the input (not a rewrite)", r.Token)
	}
}

// --- the canonical <prefix>:<n>.<m> / #<deployment>-c2 pattern (P2 owns it) --

func TestFirewall_TmuxTargetNonAllowlistedPrefixRefuses(t *testing.T) {
	// A tmux <desk>:<window>.<pane> target leaks the desk name (placeholders here).
	for _, leak := range []string{
		"see worker-desk:2.3 for the pane",
		"the target is example-desk:0.1",
	} {
		r := Check(leak, termSet(t, nil, nil))
		if r.Decision != FirewallRefuse {
			t.Fatalf("%q (non-allowlisted tmux target) must REFUSE, got %v", leak, r.Decision)
		}
		if r.Token == "" || r.Abstraction == "" {
			t.Fatalf("%q: refusal must carry token+abstraction, got token=%q abs=%q", leak, r.Token, r.Abstraction)
		}
	}
}

func TestFirewall_DeploymentChannelHashtagRefuses(t *testing.T) {
	r := Check("posted to #worker-desk-c2 earlier", termSet(t, nil, nil))
	if r.Decision != FirewallRefuse {
		t.Fatalf("a #<deployment>-c2 channel ref must REFUSE, got %v", r.Decision)
	}
}

func TestFirewall_AllowlistedGenericPrefixIsOK(t *testing.T) {
	// flotilla:<n>.<m> and session:<n>.<m> are the tool's own generic references —
	// the allowlist MUST be enumerated, since session:window.pane is a legitimate
	// generic tmux shape used across the tree (resume.go, internal/deliver/tmux.go).
	for _, ok := range []string{
		"flotilla:3.1 is the section",
		"the pane is session:1.2",
		"the generic shape is session:window.pane", // non-numeric → not even a match
	} {
		r := Check(ok, termSet(t, nil, nil))
		if r.Decision != FirewallOK {
			t.Fatalf("%q (allowlisted/generic) must be OK, got %v (token=%q)", ok, r.Decision, r.Token)
		}
	}
}

// --- built-in deployment-AGNOSTIC generic patterns (mirror the bash guard) ---

func TestFirewall_GenericLeaksRefuse(t *testing.T) {
	cases := map[string]string{
		"non-allowlisted home path": "the file is at " + fxHomePath,
		"discord webhook":           "hook " + fxWebhook,
		"github token":              "token " + fxGhp,
		"openai/anthropic secret":   "key " + fxSkAnt,
		"aws access key":            fxAkia + " here",
	}
	for name, leak := range cases {
		r := Check(leak, termSet(t, nil, nil))
		if r.Decision != FirewallRefuse {
			t.Fatalf("%s: %q must REFUSE, got %v", name, leak, r.Decision)
		}
		if r.Token == "" {
			t.Fatalf("%s: refusal must return the offending token", name)
		}
	}
}

func TestFirewall_AllowlistedHomePlaceholderIsOK(t *testing.T) {
	// /home/operator, /home/user, /home/runner are the documented generic
	// placeholders (mirrors the bash guard's negative-lookahead allowlist).
	for _, ok := range []string{
		"the path is /home/operator/flotilla",
		"runs as /home/runner/work in CI",
		"docs use /home/user/path",
	} {
		r := Check(ok, termSet(t, nil, nil))
		if r.Decision != FirewallOK {
			t.Fatalf("%q (allowlisted home placeholder) must be OK, got %v (token=%q)", ok, r.Decision, r.Token)
		}
	}
}

// --- the advisory WARN tier --------------------------------------------------

func TestFirewall_WarnlistTermWarnsNeverBlocks(t *testing.T) {
	ts := termSet(t, nil, []string{"flatten(ed|s|ing)?", "the special daemon"})
	r := Check("we flattened the book before the close", ts)
	if r.Decision != FirewallWarn {
		t.Fatalf("a warnlist-only hit must WARN, got %v", r.Decision)
	}
	if len(r.WarnTerms) == 0 {
		t.Fatal("a WARN must report the matched warnlist term(s)")
	}
}

func TestFirewall_DenylistPrecedesWarnlist(t *testing.T) {
	// A term on BOTH lists → REFUSE (denylist precedence); the WARN tier never
	// relaxes the fail-closed denylist.
	ts := termSet(t, []string{"acme-desk"}, []string{"acme-desk"})
	r := Check("the acme-desk reported", ts)
	if r.Decision != FirewallRefuse {
		t.Fatalf("a term on both lists must REFUSE (denylist precedence), got %v", r.Decision)
	}
}

func TestFirewall_WarnAndDenyTogether_DenyWins(t *testing.T) {
	// Distinct deny + warn hits in the same text → REFUSE (the fail-closed tier
	// always wins; the post is withheld regardless of the advisory class).
	ts := termSet(t, []string{"acme-desk"}, []string{"flatten(ed|s|ing)?"})
	r := Check("acme-desk flattened the position", ts)
	if r.Decision != FirewallRefuse {
		t.Fatalf("a deny hit alongside a warn hit must REFUSE, got %v", r.Decision)
	}
}

// --- empty/absent lists: only the generic patterns apply --------------------

func TestFirewall_NoListsOnlyGenericPatternsApply(t *testing.T) {
	ts := termSet(t, nil, nil)
	// Generic prose with no deployment specific and no secret shape → OK.
	if r := Check("the backfill finished and the gap is closed", ts); r.Decision != FirewallOK {
		t.Fatalf("clean generic prose with empty lists must be OK, got %v (token=%q)", r.Decision, r.Token)
	}
	// A built-in generic leak still REFUSES even with empty deploy lists.
	if r := Check("at "+fxHomePath, ts); r.Decision != FirewallRefuse {
		t.Fatalf("a built-in generic leak must REFUSE even with empty lists, got %v", r.Decision)
	}
}

func TestFirewall_NeverRewrites(t *testing.T) {
	// The FirewallResult has no rewritten-body field BY CONSTRUCTION — a refusal
	// can only carry the offending token + a suggestion, never a mutated artifact.
	// This test pins the contract: the offending token is a substring of the input
	// (a pointer INTO the text), never a generalized replacement of it.
	in := "the example-desk:0.1 pane"
	r := Check(in, termSet(t, nil, nil))
	if r.Decision != FirewallRefuse {
		t.Fatalf("expected REFUSE, got %v", r.Decision)
	}
	if !strings.Contains(in, r.Token) {
		t.Fatalf("token %q must be a substring of the input (no rewrite)", r.Token)
	}
	if r.Token == r.Abstraction {
		t.Fatal("the abstraction must be a SUGGESTION distinct from the offending token, not a silent swap")
	}
}

func TestNewTermSet_InvalidRegexErrors(t *testing.T) {
	if _, err := NewTermSet([]string{"("}, nil); err == nil {
		t.Fatal("an invalid denylist regex must surface as an error, not a silent skip")
	}
	if _, err := NewTermSet(nil, []string{"("}); err == nil {
		t.Fatal("an invalid warnlist regex must surface as an error")
	}
}
