package unacked

import "testing"

func TestLooksLikeRequest_SkipsAtMentionedTrivialAck(t *testing.T) {
	for _, msg := range []string{"@xo thanks", "@bot done", "@frontend ✅", "@cos thank you!"} {
		if looksLikeRequest(msg) {
			t.Fatalf("%q should not classify as request", msg)
		}
	}
}

func TestLooksLikeRequest_AtMentionedDirective(t *testing.T) {
	if !looksLikeRequest("@xo can you ship the fix?") {
		t.Fatal("@-addressed directive should classify as request")
	}
}
