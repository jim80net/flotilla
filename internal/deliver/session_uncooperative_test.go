package deliver

import (
	"strings"
	"testing"
)

func TestSessionUncooperative_UsageCredits558(t *testing.T) {
	// Live-shaped credit-exhausted Claude/Fable footer (genericized).
	captured := "" +
		"  prior turn output…\n" +
		"  You've reached your model limit for this session.\n" +
		"  You're out of usage credits. Run /usage-credits to top up,\n" +
		"  or switch models with /model.\n" +
		"❯ \n"
	hit, phrase := SessionUncooperative(captured)
	if !hit {
		t.Fatal("usage-credit banner must diagnose uncooperative")
	}
	if !strings.Contains(strings.ToLower(phrase), "usage credit") &&
		!strings.Contains(strings.ToLower(phrase), "reached your") {
		t.Fatalf("phrase = %q, want credit/limit wording", phrase)
	}
}

func TestSessionUncooperative_ClaudeServerSide(t *testing.T) {
	captured := strings.Repeat("old\n", 20) + ClaudeServerSidePhrase + "\n❯ \n"
	hit, phrase := SessionUncooperative(captured)
	if !hit || phrase != ClaudeServerSidePhrase {
		t.Fatalf("got (%v, %q), want Claude server-side phrase", hit, phrase)
	}
}

func TestSessionUncooperative_GrokRateLimitFooter(t *testing.T) {
	captured := "  ⠦ rate limit exceeded; sleeping.\n  │ ❯  │\n"
	hit, phrase := SessionUncooperative(captured)
	if !hit {
		t.Fatal("grok rate-limit STATUS footer must diagnose uncooperative")
	}
	if !strings.Contains(strings.ToLower(phrase), "rate limit") {
		t.Fatalf("phrase = %q", phrase)
	}
}

func TestSessionUncooperative_ProseNotHit(t *testing.T) {
	// Ordinary conversation mentioning limits must not abort recycle as uncooperative.
	captured := "We discussed how the API rate limit exceeded our quota in the design doc.\n" +
		"Next step: write the handoff.\n❯ \n"
	if hit, phrase := SessionUncooperative(captured); hit {
		t.Fatalf("prose must not diagnose uncooperative, phrase=%q", phrase)
	}
}

func TestSessionUncooperative_ScrollbackNotHit(t *testing.T) {
	// Banner scrolled far above the live tail must not false-positive.
	var b strings.Builder
	b.WriteString("You're out of usage credits. Run /usage-credits.\n")
	for i := 0; i < 40; i++ {
		b.WriteString("later conversation line\n")
	}
	b.WriteString("❯ \n")
	if hit, _ := SessionUncooperative(b.String()); hit {
		t.Fatal("scrollback credit banner must not hit (tail-bounded)")
	}
}

func TestSessionUncooperative_Empty(t *testing.T) {
	if hit, _ := SessionUncooperative(""); hit {
		t.Fatal("empty capture must not hit")
	}
}
