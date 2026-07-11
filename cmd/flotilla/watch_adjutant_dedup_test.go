package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

// #469: corrupt delivered ledger quarantines and seam drain proceeds with empty dedup state.
func TestAdjutantSeamBriefCorruptLedgerFailsOpen(t *testing.T) {
	dir := t.TempDir()
	bufferPath := roster.LayerBufferPath(dir, "alpha-xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "alpha-xo")
	// Judgment item so corrupt ledger fail-open still produces a Needs-you inject.
	reason := "backend PR gate needs decision"
	if err := adjutantbuffer.Append(bufferPath, "alpha-xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deliveredPath, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	brief, ok, _, record := adjutantSeamBrief(bufferPath, deliveredPath, "alpha-xo", dir)
	if !ok || len(record) != 1 {
		t.Fatalf("corrupt ledger must fail-open to inject fresh items, ok=%v record=%+v", ok, record)
	}
	if !strings.Contains(brief, reason) {
		t.Fatalf("brief missing reason:\n%s", brief)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var quarantined bool
	for _, e := range entries {
		if strings.Contains(e.Name(), "buffer-delivered.json.corrupt-") {
			quarantined = true
		}
	}
	if !quarantined {
		t.Fatal("corrupt delivered ledger should be quarantined")
	}
}

// #469: per-layer delivered ledger suppresses re-inject for same key+state hash.
func TestAdjutantSeamBriefPerLayerDedupSkipsConsumed(t *testing.T) {
	dir := t.TempDir()
	bufferPath := roster.LayerBufferPath(dir, "alpha-xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "alpha-xo")
	reason := "backend: finished a turn (working→idle)"
	if err := adjutantbuffer.Append(bufferPath, "alpha-xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil || len(f.Items) != 1 {
		t.Fatal(err)
	}
	if err := adjutantbuffer.RecordDelivered(deliveredPath, "alpha-xo", f.Items); err != nil {
		t.Fatal(err)
	}
	brief, ok, clearAfter, _ := adjutantSeamBrief(bufferPath, deliveredPath, "alpha-xo", dir)
	if ok {
		t.Fatalf("consumed per-layer item must not inject, brief=%q", brief)
	}
	if !clearAfter {
		t.Fatal("all-consumed buffer should set clearAfter")
	}
}

// #469: fresh judgment occurrence with new state hash re-injects at per-layer seam.
func TestAdjutantSeamBriefPerLayerDeltaRedelivers(t *testing.T) {
	dir := t.TempDir()
	charter := roster.LayerCharterPath(dir, "alpha-xo")
	if err := os.WriteFile(charter, []byte("# charter"), 0o600); err != nil {
		t.Fatal(err)
	}
	bufferPath := roster.LayerBufferPath(dir, "alpha-xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "alpha-xo")
	reason := "backend PR gate needs decision"
	if err := adjutantbuffer.Append(bufferPath, "alpha-xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := adjutantbuffer.RecordDelivered(deliveredPath, "alpha-xo", f.Items); err != nil {
		t.Fatal(err)
	}
	if err := adjutantbuffer.Clear(bufferPath); err != nil {
		t.Fatal(err)
	}
	// Re-append same reason text — new At produces new state hash (delta-only).
	time.Sleep(2 * time.Millisecond)
	if err := adjutantbuffer.Append(bufferPath, "alpha-xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	brief, ok, _, record := adjutantSeamBrief(bufferPath, deliveredPath, "alpha-xo", dir)
	if !ok || len(record) != 1 {
		t.Fatalf("fresh judgment occurrence must inject, ok=%v record=%+v brief=%q", ok, record, brief)
	}
	if !strings.Contains(brief, "1 buffered item(s)") {
		t.Fatalf("count must match post-dedup list:\n%s", brief)
	}
}

// Wave 0.2: mechanical finish-edge delta is auto-consume, not Needs-you inject.
func TestAdjutantSeamBriefPerLayerMechanicalDeltaNoInject(t *testing.T) {
	dir := t.TempDir()
	bufferPath := roster.LayerBufferPath(dir, "alpha-xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "alpha-xo")
	reason := "backend: finished a turn (working→idle)"
	if err := adjutantbuffer.Append(bufferPath, "alpha-xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := adjutantbuffer.RecordDelivered(deliveredPath, "alpha-xo", f.Items); err != nil {
		t.Fatal(err)
	}
	if err := adjutantbuffer.Clear(bufferPath); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := adjutantbuffer.Append(bufferPath, "alpha-xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	brief, ok, _, record := adjutantSeamBrief(bufferPath, deliveredPath, "alpha-xo", dir)
	if ok {
		t.Fatalf("mechanical must not inject, brief=%q", brief)
	}
	if len(record) != 1 {
		t.Fatalf("mechanical auto-consume record = %d", len(record))
	}
}
