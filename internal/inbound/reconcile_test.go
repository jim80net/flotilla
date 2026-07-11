package inbound

import (
	"testing"
	"time"
)

func TestClearAcknowledged_RemovesNonceMatch(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := AppendDispatchNonce("body long enough for distinctive snippet tests")
	if err != nil {
		t.Fatal(err)
	}
	if err := Record(dir, Entry{
		ID: "a1", Sender: "xo", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	path, _ := Path(dir, "desk")
	st := NewStore(path)
	cleared := st.ClearAcknowledged("finished with " + nonce)
	if len(cleared) != 1 || cleared[0].Nonce != nonce {
		t.Fatalf("cleared = %+v", cleared)
	}
	if len(st.Load()) != 0 {
		t.Fatal("pending should be empty")
	}
}

func TestClearConsumed_RemovesMatchingNonce(t *testing.T) {
	dir := t.TempDir()
	msg, nonce, err := AppendDispatchNonce("another body long enough for distinctive snippet")
	if err != nil {
		t.Fatal(err)
	}
	if err := Record(dir, Entry{
		ID: "a2", Sender: "xo", Recipient: "desk", Message: msg, Nonce: nonce,
		DeliveredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	path, _ := Path(dir, "desk")
	st := NewStore(path)
	cleared := st.ClearConsumed(func(n, _ string) bool { return n == nonce })
	if len(cleared) != 1 {
		t.Fatalf("cleared = %d", len(cleared))
	}
	if len(st.Load()) != 0 {
		t.Fatal("pending should be empty")
	}
}
