package main

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestNextSendRetryWait(t *testing.T) {
	if got := nextSendRetryWait(sendRetryInitial); got != 10*time.Second {
		t.Fatalf("got %v", got)
	}
	if got := nextSendRetryWait(40 * time.Second); got != sendRetryMax {
		t.Fatalf("cap got %v", got)
	}
}

func TestErrRetryableBusyUnwrap(t *testing.T) {
	err := fmt.Errorf("%w", errRetryableBusy{agent: "cos"})
	if !errors.Is(err, surface.ErrBusy) {
		t.Fatal("should unwrap to ErrBusy")
	}
}