package main

import (
	"sync/atomic"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

func TestAdjutantSeamClaimConfirmRecordsAndClears(t *testing.T) {
	dir := t.TempDir()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "xo")
	reason := "backend: finished a turn (working→idle)"
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil || len(f.Items) != 1 {
		t.Fatalf("peek: %+v err=%v", f, err)
	}
	claims := newAdjutantSeamClaims()
	key := adjutantSeamClaimKey("xo")
	claims.register(key, adjutantSeamClaim{
		owner: "xo", bufferPath: bufferPath, deliveredPath: deliveredPath, recordItems: f.Items,
	})
	claims.confirm(key)
	got, _, err := adjutantbuffer.LoadDelivered(deliveredPath)
	if err != nil || !got.Has(f.Items[0].Key, f.Items[0].StateHash) {
		t.Fatalf("ledger after confirm: %+v err=%v", got, err)
	}
	if adjutantbuffer.Len(bufferPath) != 0 {
		t.Fatal("buffer should be cleared after confirm")
	}
}

func TestAdjutantSeamClaimAbortRetainsBuffer(t *testing.T) {
	dir := t.TempDir()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "xo")
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{"backend: edge"}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	claims := newAdjutantSeamClaims()
	key := adjutantSeamClaimKey("xo")
	claims.register(key, adjutantSeamClaim{
		owner: "xo", bufferPath: bufferPath, deliveredPath: deliveredPath, recordItems: f.Items,
	})
	claims.abort(key)
	if adjutantbuffer.Len(bufferPath) != 1 {
		t.Fatal("busy abort must retain buffer for retry")
	}
	got, _, err := adjutantbuffer.LoadDelivered(deliveredPath)
	if err != nil || len(got.Entries) != 0 {
		t.Fatalf("ledger must stay empty on abort, got %+v err=%v", got, err)
	}
}

func TestAdjutantSeamClaimBusyDropViaInjector(t *testing.T) {
	dir := t.TempDir()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "xo")
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{"backend: edge"}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	claims := newAdjutantSeamClaims()
	key := adjutantSeamClaimKey("xo")
	claims.register(key, adjutantSeamClaim{
		owner: "xo", bufferPath: bufferPath, deliveredPath: deliveredPath, recordItems: f.Items,
	})
	var confirmed, aborted int32
	in := watch.NewInjector(func(string, string) error { return surface.ErrBusy }, 4)
	in.SetDetectorClaimHooks(
		func(k string) {
			atomic.AddInt32(&confirmed, 1)
			claims.confirm(k)
		},
		func(k string) {
			atomic.AddInt32(&aborted, 1)
			claims.abort(k)
		},
	)
	in.Start()
	in.Enqueue(watch.Job{Agent: "xo", Kind: watch.KindDetector, ClaimKey: key, Message: "brief"})
	in.Stop()
	if atomic.LoadInt32(&aborted) != 1 || atomic.LoadInt32(&confirmed) != 0 {
		t.Fatalf("abort=%d confirm=%d, want abort=1 confirm=0", aborted, confirmed)
	}
	if adjutantbuffer.Len(bufferPath) != 1 {
		t.Fatal("buffer must survive busy drop")
	}
}
