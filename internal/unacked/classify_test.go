package unacked

import "testing"

func TestLooksLikeRequest_SkipsAtMentionedTrivialAck(t *testing.T) {
	for _, msg := range []string{"@xo thanks", "@bot done", "@frontend ✅", "@cos thank you!"} {
		t.Run(msg, func(t *testing.T) {
			if looksLikeRequest(msg) {
				t.Fatalf("%q should not classify as request", msg)
			}
		})
	}
}

func TestLooksLikeRequest_AtMentionedDirective(t *testing.T) {
	if !looksLikeRequest("@xo can you ship the fix?") {
		t.Fatal("@-addressed directive should classify as request")
	}
}

func TestIsWorkingOnIt_SpecificPhrases(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"working on it", true},
		{"cos is working on your message", true},
		{"still working on the rollout", true},
		{"I'll route the reply when it lands", true},
		{"getting on it now", true},
		{"Shipped — depends on it passing CI", false},
		{"Please focus on it next sprint", false},
		{"The fix is on it way", false},
	}
	for _, tc := range cases {
		t.Run(tc.content, func(t *testing.T) {
			if got := isWorkingOnIt(tc.content); got != tc.want {
				t.Fatalf("isWorkingOnIt(%q)=%v want %v", tc.content, got, tc.want)
			}
		})
	}
}
