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
		t.Fatal("buffer should be empty after confirm when no concurrent appends")
	}
}

func TestAdjutantSeamClaimConfirmScopedRemovePreservesLaterAppends(t *testing.T) {
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
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{"frontend: edge"}); err != nil {
		t.Fatal(err)
	}
	claims := newAdjutantSeamClaims()
	key := adjutantSeamClaimKey("xo")
	claims.register(key, adjutantSeamClaim{
		owner: "xo", bufferPath: bufferPath, deliveredPath: deliveredPath, recordItems: f.Items,
	})
	claims.confirm(key)
	if adjutantbuffer.Len(bufferPath) != 1 {
		t.Fatalf("scoped confirm must retain post-peek append, len=%d", adjutantbuffer.Len(bufferPath))
	}
}

func TestAdjutantSeamClaimKeyUniquePerDrain(t *testing.T) {
	k1 := adjutantSeamClaimKey("xo")
	k2 := adjutantSeamClaimKey("xo")
	if k1 == k2 {
		t.Fatalf("claim keys must be unique per drain, both %q", k1)
	}
	if !isAdjutantSeamClaimKey(k1) || !isAdjutantSeamClaimKey(k2) {
		t.Fatal("claim keys must keep adjutant-seam prefix for demux")
	}
}

func TestOverlappingSeamClaimsConfirmIndependently(t *testing.T) {
	dir := t.TempDir()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "xo")
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{"first: edge"}); err != nil {
		t.Fatal(err)
	}
	f1, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	key1 := adjutantSeamClaimKey("xo")
	claims := newAdjutantSeamClaims()
	claims.register(key1, adjutantSeamClaim{
		owner: "xo", bufferPath: bufferPath, deliveredPath: deliveredPath, recordItems: f1.Items,
	})
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{"second: edge"}); err != nil {
		t.Fatal(err)
	}
	f2, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil || len(f2.Items) != 2 {
		t.Fatalf("peek second drain: %+v err=%v", f2, err)
	}
	key2 := adjutantSeamClaimKey("xo")
	claims.register(key2, adjutantSeamClaim{
		owner: "xo", bufferPath: bufferPath, deliveredPath: deliveredPath, recordItems: []adjutantbuffer.Item{f2.Items[1]},
	})
	claims.confirm(key1)
	if adjutantbuffer.Len(bufferPath) != 1 {
		t.Fatalf("after first confirm want second item retained, len=%d", adjutantbuffer.Len(bufferPath))
	}
	claims.confirm(key2)
	if adjutantbuffer.Len(bufferPath) != 0 {
		t.Fatal("after both confirms buffer should be empty")
	}
	got, _, err := adjutantbuffer.LoadDelivered(deliveredPath)
	if err != nil || len(got.Entries) != 2 {
		t.Fatalf("ledger should record both deliveries independently: %+v err=%v", got, err)
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
