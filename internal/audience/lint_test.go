package audience

import (
	"os"
	"strings"
	"testing"
)

func TestCommittedAudienceFixtures(t *testing.T) {
	tests := []struct {
		path string
		lint func(string, []string) []Finding
		pass bool
	}{
		{"testdata/parade-pass.md", LintParade, true},
		{"testdata/parade-fail.md", LintParade, false},
		{"testdata/operator-pr-pass.md", LintOperatorPR, true},
		{"testdata/operator-pr-fail.md", LintOperatorPR, false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			got := tc.lint(string(raw), DefaultJargon())
			if tc.pass && len(got) != 0 {
				t.Fatalf("expected pass, got %+v", got)
			}
			if !tc.pass && len(got) == 0 {
				t.Fatal("expected fail fixture to produce findings")
			}
		})
	}
}

func TestLintParadePassesGroundedGlossedSpineAndIgnoresDetails(t *testing.T) {
	src := `# Messages no longer disappear

Before, a busy desk could lose an operator request.
The outbox — a durable delivery queue — now retains it until delivery completes.
After, the request arrives once and its status stays visible.

<details>
<summary>Engineering identifiers</summary>

PR #785 changed nonce handling in a worktree.
</details>`
	if got := LintParade(src, DefaultJargon()); len(got) != 0 {
		t.Fatalf("LintParade findings = %+v", got)
	}
}

func TestLintParadeFailsBareIdentifiersUnglossedJargonAndScoreOnly(t *testing.T) {
	src := `# PR #785

Walk19 moved 9/14 → 12/14.
The outbox improved.

---

# Faster worktree

Score improved to 13/14.`
	got := LintParade(src, DefaultJargon())
	for _, code := range []string{"identifier-title", "identifier-on-spine", "score-only", "unglossed-jargon", "ungrounded-claim"} {
		if !hasCode(got, code) {
			t.Errorf("missing %s in %+v", code, got)
		}
	}
}

func TestLintParadeAllowsIdentifierFooterAndImageDivider(t *testing.T) {
	src := `# Delivery is now reliable

Requests arrive once.

Identifiers: PR #123

---

# Part two

![](assets/divider.png)`
	if got := LintParade(src, DefaultJargon()); len(got) != 0 {
		t.Fatalf("findings = %+v", got)
	}
}

func TestLintOperatorPRPassesContract(t *testing.T) {
	src := `## Operator summary

### Before
Operator requests could disappear while a desk was busy.

### Change
The outbox — a durable delivery queue — now retains each request.

### After
Requests arrive once and their status remains visible.

### Identifiers
- Issue: #775
- Commit: abcdef1

## Engineering details

Internal worktree and nonce discussion may stay here.`
	if got := LintOperatorPR(src, DefaultJargon()); len(got) != 0 {
		t.Fatalf("LintOperatorPR findings = %+v", got)
	}
}

func TestLintOperatorPRFailsMissingOutcomeAndIdentifiersInProse(t *testing.T) {
	src := `## Operator summary

### Change
PR #791 updates the worktree.

### Before
The nonce was unclear.

### Identifiers
- #791`
	got := LintOperatorPR(src, DefaultJargon())
	for _, code := range []string{"missing-after", "summary-order", "identifier-before-footer", "unglossed-jargon"} {
		if !hasCode(got, code) {
			t.Errorf("missing %s in %+v", code, got)
		}
	}
}

func TestLintOperatorPRRequiresDedicatedSection(t *testing.T) {
	got := LintOperatorPR("## Engineering details\n\nTests pass.", nil)
	if len(got) != 1 || got[0].Code != "missing-operator-summary" {
		t.Fatalf("findings = %+v", got)
	}
}

func TestLintOperatorPRTemplateCommentsDoNotSatisfyRequiredSections(t *testing.T) {
	src := `## Operator summary

### Before
<!-- explain the previous behavior -->
### Change
<!-- explain the change -->
### After
<!-- explain the result -->
### Identifiers
<!-- add issue and commit -->`
	got := LintOperatorPR(src, nil)
	for _, code := range []string{"missing-before", "missing-change", "missing-after", "missing-identifiers"} {
		if !hasCode(got, code) {
			t.Errorf("missing %s in %+v", code, got)
		}
	}
}

func hasCode(findings []Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code || strings.HasPrefix(f.Code, code) {
			return true
		}
	}
	return false
}
