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

func TestParseOwnDispatchNonce_FooterVsQuoted707(t *testing.T) {
	stamped, nonce, err := AppendDispatchNonce("do the thing")
	if err != nil {
		t.Fatal(err)
	}
	if got := ParseOwnDispatchNonce(stamped); got != nonce {
		t.Fatalf("footer nonce = %q, want %q", got, nonce)
	}
	// A nonce quoted in prose is NOT the message's own stamp.
	if got := ParseOwnDispatchNonce("dispatched flotilla-dispatch-aaaa1111 to the desk"); got != "" {
		t.Fatalf("quoted-nonce message own-nonce = %q, want empty", got)
	}
	// A quoted nonce ABOVE a real footer must not shadow the footer's stamp.
	quoted := "re flotilla-dispatch-aaaa1111: proceed" + FormatDispatchFooter("flotilla-dispatch-bbbb2222")
	if got := ParseOwnDispatchNonce(quoted); got != "flotilla-dispatch-bbbb2222" {
		t.Fatalf("footer-after-quote own-nonce = %q, want the footer's", got)
	}
	if got := ParseOwnDispatchNonce(""); got != "" {
		t.Fatalf("empty message own-nonce = %q", got)
	}
}
