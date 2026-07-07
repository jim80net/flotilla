package inbound

import (
	"strings"
	"testing"
)

func TestAppendDispatchNonce_Idempotent(t *testing.T) {
	msg := "do the thing"
	aug, n1, err := AppendDispatchNonce(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aug, n1) || ParseDispatchNonce(aug) != n1 || !strings.Contains(aug, "#472") {
		t.Fatalf("augmented = %q nonce = %q", aug, n1)
	}
	_, n2, err := AppendDispatchNonce(aug)
	if err != nil || n2 != n1 {
		t.Fatalf("second append changed nonce: %q vs %q", n2, n1)
	}
}

func TestStripDispatchFooter_RemovesAckBlock(t *testing.T) {
	base := "deploy complete"
	stamped, nonce, err := AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := StripDispatchFooter(stamped); got != base {
		t.Fatalf("stripped = %q, want %q (nonce was %q)", got, base, nonce)
	}
	if got := StripDispatchFooter(base); got != base {
		t.Fatalf("unstamped passthrough = %q", got)
	}
}
