package readermap

import (
	"strings"
	"testing"
)

func validEnvelope() Envelope {
	return Envelope{
		Audience: AudienceOperator,
		Anchor:   "the data backfill you are tracking",
		Delta:    "the head backfill finished; the gap is closed",
		Decision: "none",
	}
}

// --- Envelope.Validate (Pillar B schema / Pillar C tier-1 presence) ---------

func TestValidate_WellFormed(t *testing.T) {
	if err := validEnvelope().Validate(); err != nil {
		t.Fatalf("well-formed envelope should validate, got %v", err)
	}
}

func TestValidate_MissingDecisionIsInvalid(t *testing.T) {
	e := validEnvelope()
	e.Decision = ""
	if err := e.Validate(); err == nil {
		t.Fatal("an empty decision (neither an action nor \"none\") must be invalid")
	}
}

func TestValidate_EmptyAnchorIsInvalid(t *testing.T) {
	e := validEnvelope()
	e.Anchor = "   "
	if err := e.Validate(); err == nil {
		t.Fatal("an empty/whitespace anchor must be invalid")
	}
}

func TestValidate_EmptyDeltaIsInvalid(t *testing.T) {
	e := validEnvelope()
	e.Delta = ""
	if err := e.Validate(); err == nil {
		t.Fatal("an empty delta must be invalid")
	}
}

func TestValidate_EmptyAudienceIsInvalid(t *testing.T) {
	e := validEnvelope()
	e.Audience = ""
	if err := e.Validate(); err == nil {
		t.Fatal("an empty audience must be invalid")
	}
}

func TestValidate_UnrecognizedAudienceIsAccepted(t *testing.T) {
	// audience is open-stringly-typed: an extension value (and a desk:<name>) is
	// accepted, not rejected.
	for _, a := range []Audience{"desk:flotilla", "auditor", "investor"} {
		e := validEnvelope()
		e.Audience = a
		if err := e.Validate(); err != nil {
			t.Fatalf("open-stringly-typed audience %q should be accepted, got %v", a, err)
		}
	}
}

func TestValidate_KnownAudiencesValidate(t *testing.T) {
	for _, a := range []Audience{AudienceOperator, AudienceNewcomer, AudienceMaintainer, "desk:backend"} {
		e := validEnvelope()
		e.Audience = a
		if err := e.Validate(); err != nil {
			t.Fatalf("known audience %q should validate, got %v", a, err)
		}
	}
}

func TestValidate_DecisionActionValidates(t *testing.T) {
	e := validEnvelope()
	e.Decision = "approve the corpus ingest in #20"
	if err := e.Validate(); err != nil {
		t.Fatalf("a real decision string should validate, got %v", err)
	}
}

// --- Detect (the three-way wire-format predicate) ---------------------------

func TestDetect_PresentParseableBlock(t *testing.T) {
	turn := "Here is my brief for you.\n\n" +
		"```reader-map\n" +
		`{"audience":"operator","anchor":"the X you track","delta":"X shipped","decision":"none"}` + "\n" +
		"```\n\nthanks"
	env, outcome := Detect(turn)
	if outcome != OutcomePresent {
		t.Fatalf("expected OutcomePresent, got %v", outcome)
	}
	if env == nil || env.Anchor != "the X you track" {
		t.Fatalf("expected parsed envelope, got %+v", env)
	}
}

func TestDetect_MalformedBlockIsNotMissing(t *testing.T) {
	turn := "```reader-map\n{not valid json at all]\n```"
	env, outcome := Detect(turn)
	if outcome != OutcomeMalformed {
		t.Fatalf("expected OutcomeMalformed, got %v", outcome)
	}
	if env != nil {
		t.Fatal("a malformed block must return a nil envelope")
	}
}

func TestDetect_NoBlockIsAbsent(t *testing.T) {
	turn := "Just an ordinary turn-final with no envelope at all."
	env, outcome := Detect(turn)
	if outcome != OutcomeAbsent {
		t.Fatalf("expected OutcomeAbsent, got %v", outcome)
	}
	if env != nil {
		t.Fatal("an absent block must return a nil envelope")
	}
}

func TestDetect_SecondBlockIsMalformed(t *testing.T) {
	turn := "```reader-map\n" +
		`{"audience":"operator","anchor":"a","delta":"b","decision":"none"}` + "\n```\n" +
		"and another\n```reader-map\n" +
		`{"audience":"operator","anchor":"c","delta":"d","decision":"none"}` + "\n```"
	_, outcome := Detect(turn)
	if outcome != OutcomeMalformed {
		t.Fatalf("two reader-map blocks must be OutcomeMalformed, got %v", outcome)
	}
}

func TestDetect_PresentButEmptyFieldsStillParses(t *testing.T) {
	// Valid JSON with empty fields PARSES (OutcomePresent); the empty fields are a
	// TIER-1 failure, not a detect-malformed — the two axes are distinct.
	turn := "```reader-map\n{}\n```"
	env, outcome := Detect(turn)
	if outcome != OutcomePresent {
		t.Fatalf("empty-but-valid JSON should be OutcomePresent, got %v", outcome)
	}
	if got := Tier1Lint(*env); got.Pass {
		t.Fatal("an empty envelope must FAIL tier-1 (presence), even though it is OutcomePresent")
	}
}

func TestDetect_UnterminatedFenceIsAbsent(t *testing.T) {
	turn := "```reader-map\n{\"audience\":\"operator\"}\n(no closing fence)"
	_, outcome := Detect(turn)
	if outcome != OutcomeAbsent {
		t.Fatalf("an unterminated fence is not a well-formed block (absent), got %v", outcome)
	}
}

func TestDetect_ValidFirstMalformedSecondIsMalformed(t *testing.T) {
	// Two reader-map blocks (first valid, second invalid JSON) are still TWO blocks —
	// the count rule makes it Malformed before any per-block parse.
	turn := "```reader-map\n" + `{"audience":"operator","anchor":"a","delta":"b","decision":"none"}` + "\n```\n" +
		"```reader-map\n{broken]\n```"
	if _, outcome := Detect(turn); outcome != OutcomeMalformed {
		t.Fatalf("valid-first + malformed-second must be Malformed (two blocks), got %v", outcome)
	}
}

func TestDetect_ValidFirstUnterminatedSecondIsPresent(t *testing.T) {
	// A valid CLOSED first block followed by a second fence that never closes: the
	// scanner collects block 1, then breaks on the unterminated second fence — so the
	// first envelope is returned (Present). Pinning this chosen semantics.
	turn := "```reader-map\n" + `{"audience":"operator","anchor":"a","delta":"b","decision":"none"}` + "\n```\n" +
		"trailing ```reader-map\n{never closes"
	env, outcome := Detect(turn)
	if outcome != OutcomePresent || env == nil || env.Anchor != "a" {
		t.Fatalf("valid-first + unterminated-second must return the first envelope (Present), got %v / %+v", outcome, env)
	}
}

func TestDetect_CRLFTurnFinalParses(t *testing.T) {
	turn := "intro\r\n```reader-map\r\n" +
		`{"audience":"operator","anchor":"a","delta":"b","decision":"none"}` + "\r\n```\r\n"
	env, outcome := Detect(turn)
	if outcome != OutcomePresent || env == nil {
		t.Fatalf("a CRLF turn-final must detect+parse (Present), got %v", outcome)
	}
	if env.Delta != "b" {
		t.Errorf("CRLF body must parse cleanly without a stray CR; got delta %q", env.Delta)
	}
}

func TestValidate_UnfilledPlaceholderRejected(t *testing.T) {
	for _, field := range []string{"anchor", "delta", "decision"} {
		e := validEnvelope()
		switch field {
		case "anchor":
			e.Anchor = "<the reader's map entry this brief updates>"
		case "delta":
			e.Delta = "<what changed>"
		case "decision":
			e.Decision = "<the one action they must take, or \"none\">"
		}
		if err := e.Validate(); err == nil {
			t.Fatalf("an unfilled <...> placeholder in %s must be rejected (template echo)", field)
		}
	}
}

// --- Tier1Lint (PRESENCE only; slop passes) ---------------------------------

func TestTier1_SlopButPresentPasses(t *testing.T) {
	// The crux: a slop envelope with present, non-empty fields PASSES tier-1.
	// tier-1 cannot judge content; the tier-2 judge catches the slop.
	slop := Envelope{Audience: AudienceOperator, Anchor: "my work", Delta: "made progress", Decision: "none"}
	if got := Tier1Lint(slop); !got.Pass {
		t.Fatalf("a slop-but-present envelope must PASS tier-1 (structure != modeling), got %q", got.Reason)
	}
}

func TestTier1_MissingFieldFails(t *testing.T) {
	e := validEnvelope()
	e.Anchor = ""
	got := Tier1Lint(e)
	if got.Pass {
		t.Fatal("a missing required field must FAIL tier-1")
	}
	if got.Reason == "" {
		t.Fatal("a tier-1 failure must carry a reason")
	}
}

// --- Render (open-from-anchor / lead-with-decision by construction) ---------

func TestRender_OpensWithAnchor(t *testing.T) {
	e := validEnvelope()
	body := Render(e)
	if !strings.HasPrefix(body, e.Anchor) {
		t.Fatalf("rendered body must OPEN with the anchor; got prefix %q", body[:min(len(body), 40)])
	}
}

// (min is a Go builtin as of 1.21; no local helper needed.)

func TestRender_DecisionPrecedesDelta(t *testing.T) {
	e := validEnvelope()
	e.Decision = "approve #20"
	e.Delta = "the corpus is staged"
	body := Render(e)
	di := strings.Index(body, "approve #20")
	xi := strings.Index(body, "the corpus is staged")
	if di < 0 || xi < 0 {
		t.Fatalf("rendered body must contain both the decision and the delta; got %q", body)
	}
	if di > xi {
		t.Fatalf("the decision must LEAD (precede the delta); decision@%d delta@%d", di, xi)
	}
}

func TestRender_NoneIsExplicit(t *testing.T) {
	e := validEnvelope() // Decision == "none"
	body := Render(e)
	if !strings.Contains(strings.ToLower(body), "no action") {
		t.Fatalf("a none decision must render an explicit no-action line; got %q", body)
	}
}
