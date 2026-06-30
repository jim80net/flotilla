package readermap

import (
	"fmt"
	"regexp"
	"strings"
)

// The private-egress firewall (Pillar D). It is the runtime half of flotilla's
// public/private partition: before any outbound artifact is published, Check runs it
// through the partition firewall and REFUSES (never rewrites) a known private leak.
//
// Two tiers, mirroring scripts/check-private-boundary.sh so the runtime guard and the
// static CI guard agree (a conformance test pins the equivalence):
//
//   - FAIL-CLOSED denylist + built-in generic patterns + the canonical tmux-target
//     pattern → REFUSE. A hit is never published.
//   - ADVISORY warnlist (deployment domain vocabulary) → WARN. A hit is published
//     anyway; it only earns a human glance.
//
// This file is PURE (regex matching only — no file/env I/O, preserving the package
// contract in envelope.go). The I/O loader that reads the gitignored term sources
// lives in cmd/flotilla (LoadFirewall) and hands the compiled TermSet here. The
// SUPPRESS-vs-bounce-vs-escalate rendering of a Refuse is the caller's job (the
// egress decides), so FirewallResult is egress-agnostic.

// FirewallDecision is the three-way verdict of the partition firewall.
type FirewallDecision int

const (
	// FirewallOK: no private leak and no domain-vocabulary hit — publish as-is.
	FirewallOK FirewallDecision = iota
	// FirewallWarn: a warnlist (domain-vocabulary) hit and NO fail-closed hit —
	// advisory only; the artifact is still published / CI still exits 0.
	FirewallWarn
	// FirewallRefuse: a denylist, built-in generic, or canonical-pattern hit — the
	// artifact is withheld (suppressed/bounced/escalated by the caller), never rewritten.
	FirewallRefuse
)

func (d FirewallDecision) String() string {
	switch d {
	case FirewallOK:
		return "OK"
	case FirewallWarn:
		return "WARN"
	case FirewallRefuse:
		return "REFUSE"
	default:
		return fmt.Sprintf("FirewallDecision(%d)", int(d))
	}
}

// FirewallResult is the egress-agnostic verdict. On a REFUSE it carries the offending
// Token (a substring of the input — never a rewrite) and a generic Abstraction the
// desk can apply in-context. On a WARN it carries the matched WarnTerms for the
// advisory note. It has NO body field BY CONSTRUCTION: the firewall refuses, it never
// returns a mutated artifact (a runtime strip would corrupt the modeled delta).
type FirewallResult struct {
	Decision    FirewallDecision
	Token       string   // the offending substring (REFUSE only)
	Abstraction string   // a generic suggestion, distinct from Token (REFUSE only)
	WarnTerms   []string // the matched warnlist terms (WARN only)
}

// genericRule is one built-in, deployment-AGNOSTIC pattern (a leak for ANY
// deployment: a username-revealing home path, a webhook URL, a secret shape). These
// mirror scripts/check-private-boundary.sh's GENERIC_PATTERNS. RE2 has no
// negative-lookahead, so the bash guard's `/home/(?!operator|user|...)` allowlist is
// re-expressed here as MATCH-THEN-FILTER: match the path, then drop the hit when the
// captured segment (group 1) is an allowlisted placeholder.
type genericRule struct {
	re          *regexp.Regexp
	allow       map[string]bool // non-nil ⇒ a hit whose group(1) is in this set is NOT a leak
	abstraction string
}

// TermSet is the compiled firewall bundle: the deployment-supplied deny/warn
// alternations (injected — the loader reads them from the gitignored sources) plus
// the built-in generic + canonical rules. It is compiled ONCE (NewTermSet) and reused
// across checks — Check never recompiles, since it runs on the hot auto-mirror path.
type TermSet struct {
	deny      *regexp.Regexp // deployment denylist alternation; nil if none configured
	warn      *regexp.Regexp // deployment warnlist alternation; nil if none configured
	generic   []genericRule  // built-in deployment-agnostic patterns
	canonical []genericRule  // the canonical tmux-target / #-c2 channel patterns (P2 owns)
}

// genericPrefixAllowlist is the enumerated set of prefixes that legitimately use the
// canonical `<prefix>:<n>.<m>` / `#<prefix>-c2` shape WITHOUT naming a deployment:
// flotilla's own section references and the generic tmux `session:` shape. Everything
// else in that shape (a `<desk>:<window>.<pane>` tmux target, a `#<deployment>-c2`
// channel) names a deployment and is REFUSED.
//
// TRADEOFF (documented, surfaced to the operator): the canonical pattern also matches
// version-string / docker-tag shapes like `python:3.11`, so a desk turn-final that
// mentions one is REFUSED as a false positive. The allowlist is the only syntactic
// lever (there is no way to distinguish a `<desk>:<window>.<pane>` tmux target from
// `python:3.11` except the prefix). A loadable, deployment-extensible prefix allowlist is the follow-up if
// real-world false positives bite; the refuse is recoverable (a bounce/suppress+alert,
// not a lost artifact).
var genericPrefixAllowlist = map[string]bool{
	"flotilla": true,
	"session":  true,
}

// NewTermSet compiles a TermSet from the deployment-supplied deny + warn term lists.
// Each entry is a regex fragment (literal terms are valid regexes); the fragments are
// joined into one alternation and compiled ONCE. An invalid fragment is a hard error
// (NEVER a silent skip — a dropped denylist term is a silent partition hole). It is
// PURE: regex compilation only, no file/env I/O.
func NewTermSet(deny, warn []string) (*TermSet, error) {
	denyRe, err := compileAlternation(deny)
	if err != nil {
		return nil, fmt.Errorf("readermap: compiling denylist: %w", err)
	}
	warnRe, err := compileAlternation(warn)
	if err != nil {
		return nil, fmt.Errorf("readermap: compiling warnlist: %w", err)
	}
	return &TermSet{
		deny:      denyRe,
		warn:      warnRe,
		generic:   builtinGenericRules(),
		canonical: builtinCanonicalRules(),
	}, nil
}

// Check runs text through the firewall in precedence order: (1) the fail-closed tier
// (denylist → built-in generic patterns → canonical pattern) REFUSES on the first hit;
// (2) the advisory warnlist WARNS only if nothing refused. Denylist precedence means a
// term on both lists REFUSES. Check NEVER mutates text.
func Check(text string, ts *TermSet) FirewallResult {
	if ts == nil {
		return FirewallResult{Decision: FirewallOK}
	}
	// (1) fail-closed: deployment denylist first (the most meaningful token to report).
	if ts.deny != nil {
		if m := ts.deny.FindString(text); m != "" {
			return FirewallResult{Decision: FirewallRefuse, Token: m, Abstraction: denylistAbstraction}
		}
	}
	// (1) fail-closed: built-in generic patterns + the canonical pattern (match-then-filter).
	for _, rules := range [][]genericRule{ts.generic, ts.canonical} {
		for _, g := range rules {
			if tok, hit := g.firstLeak(text); hit {
				return FirewallResult{Decision: FirewallRefuse, Token: tok, Abstraction: g.abstraction}
			}
		}
	}
	// (2) advisory: warnlist (only reached when nothing refused).
	if ts.warn != nil {
		if terms := uniqueMatches(ts.warn, text); len(terms) > 0 {
			return FirewallResult{Decision: FirewallWarn, WarnTerms: terms}
		}
	}
	return FirewallResult{Decision: FirewallOK}
}

// firstLeak returns the first match of g whose captured segment is NOT allowlisted
// (or the whole match, when g has no allowlist). hit=false means every match was an
// allowlisted placeholder (or there was no match) — not a leak.
func (g genericRule) firstLeak(text string) (token string, hit bool) {
	if g.allow == nil {
		if m := g.re.FindString(text); m != "" {
			return m, true
		}
		return "", false
	}
	for _, sm := range g.re.FindAllStringSubmatch(text, -1) {
		seg := ""
		if len(sm) > 1 {
			seg = strings.ToLower(sm[1])
		}
		if !g.allow[seg] {
			return sm[0], true
		}
	}
	return "", false
}

// --- compilation helpers -----------------------------------------------------

const denylistAbstraction = "a generic flotilla abstraction (a desk role like 'the XO'/'a desk'; an org → 'a private deployment'; a broker/vendor → 'a broker'/'a data vendor') — see docs/private-public-boundary.md"

// compileAlternation joins non-empty terms with '|' and compiles the result. An empty
// (or all-blank) list compiles to nil — "no list configured", so only the other tiers
// apply (mirroring the bash guard's generic-always / deployment-only-if-configured model).
func compileAlternation(terms []string) (*regexp.Regexp, error) {
	var kept []string
	for _, t := range terms {
		if strings.TrimSpace(t) != "" {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	return regexp.Compile(strings.Join(kept, "|"))
}

// uniqueMatches returns the distinct matches of re in text, order-preserving — the set
// of warnlist terms that fired, for the advisory note.
func uniqueMatches(re *regexp.Regexp, text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllString(text, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// builtinGenericRules returns the deployment-AGNOSTIC patterns, RE2-translated from
// scripts/check-private-boundary.sh's GENERIC_PATTERNS. The home/Users paths use the
// match-then-filter allowlist (RE2 has no negative lookahead); the secret shapes have
// no allowlist (any match is a leak). Compiled once per TermSet.
func builtinGenericRules() []genericRule {
	homeAllow := map[string]bool{"operator": true, "user": true, "runner": true, "youruser": true, "you": true}
	usersAllow := map[string]bool{"operator": true, "user": true, "you": true, "youruser": true}
	mk := func(pat, abs string) genericRule { return genericRule{re: regexp.MustCompile(pat), abstraction: abs} }
	return []genericRule{
		{re: regexp.MustCompile(`/home/([a-z_][a-z0-9_-]*)`), allow: homeAllow, abstraction: "/home/operator (the documented generic placeholder)"},
		{re: regexp.MustCompile(`/Users/([A-Za-z][A-Za-z0-9_-]*)`), allow: usersAllow, abstraction: "/Users/operator (the documented generic placeholder)"},
		mk(`https?://(discord(app)?|slack)\.com/api/webhooks/[0-9]+/[A-Za-z0-9_-]{16,}`, "omit the webhook URL (it is a live credential)"),
		mk(`ghp_[A-Za-z0-9]{20,}`, "omit the token (a live GitHub credential)"),
		mk(`github_pat_[A-Za-z0-9_]{20,}`, "omit the token (a live GitHub credential)"),
		mk(`xox[baprs]-[A-Za-z0-9-]{10,}`, "omit the token (a live Slack credential)"),
		mk(`xai-[A-Za-z0-9]{20,}`, "omit the key (a live API credential)"),
		mk(`sk-(ant-)?[A-Za-z0-9_-]{20,}`, "omit the key (a live API credential)"),
		mk(`AKIA[0-9A-Z]{16}`, "omit the key (a live AWS credential)"),
		mk(`-----BEGIN [A-Z ]*PRIVATE KEY-----`, "omit the private key"),
	}
}

// builtinCanonicalRules returns the canonical leak patterns P2 OWNS: a tmux
// `<prefix>:<n>.<m>` target and a `#<prefix>-c2` channel reference. Both use the
// enumerated generic-prefix allowlist (match-then-filter) so flotilla's own
// `flotilla:3.1` / `session:1.2` references pass while a `<desk>:<window>.<pane>` or
// `#<deployment>-c2` is REFUSED. (#202's static guard mirrors these patterns when it
// ships; a conformance test pins the equivalence on the shared term-list surface.)
func builtinCanonicalRules() []genericRule {
	const abs = "a generic prefix (e.g. flotilla:<n>.<m>) or drop the deployment codename — see docs/private-public-boundary.md"
	return []genericRule{
		{re: regexp.MustCompile(`\b([A-Za-z][A-Za-z0-9_-]*):[0-9]+\.[0-9]+\b`), allow: genericPrefixAllowlist, abstraction: abs},
		{re: regexp.MustCompile(`#([A-Za-z][A-Za-z0-9_-]*)-c2\b`), allow: genericPrefixAllowlist, abstraction: abs},
	}
}
