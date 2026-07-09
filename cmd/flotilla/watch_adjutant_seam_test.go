package main

import (
	"os"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

func TestAdjutantSeamBriefSkipsAllConsumedAndClearsBuffer(t *testing.T) {
	dir := t.TempDir()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "xo")
	reason := "backend: finished a turn (working→idle)"
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	f, _, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := adjutantbuffer.RecordDelivered(deliveredPath, "xo", f.Items); err != nil {
		t.Fatal(err)
	}
	brief, ok, clearAfter, record := adjutantSeamBrief(bufferPath, deliveredPath, "xo", dir)
	if ok {
		t.Fatalf("all-consumed seam must not inject, brief=%q record=%+v", brief, record)
	}
	if !clearAfter {
		t.Fatal("buffer with only consumed items should still clear")
	}
	if len(record) != 0 {
		t.Fatalf("all-consumed must not produce record items, got %+v", record)
	}
}

func TestAdjutantSeamBriefInjectsFreshDeltaOnly(t *testing.T) {
	dir := t.TempDir()
	charter := roster.LayerCharterPath(dir, "xo")
	if err := os.WriteFile(charter, []byte("# charter"), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if err := adjutantbuffer.RecordDelivered(deliveredPath, "xo", f.Items); err != nil {
		t.Fatal(err)
	}
	if err := adjutantbuffer.Clear(bufferPath); err != nil {
		t.Fatal(err)
	}
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{reason}); err != nil {
		t.Fatal(err)
	}
	brief, ok, clearAfter, record := adjutantSeamBrief(bufferPath, deliveredPath, "xo", dir)
	if !ok || len(record) != 1 {
		t.Fatalf("fresh delta must inject, ok=%v record=%+v brief=%q", ok, record, brief)
	}
	if !clearAfter {
		t.Fatal("fresh buffer items must set clearAfter for post-confirm clear")
	}
	if brief == "" {
		t.Fatal("brief must be non-empty for inject path")
	}
}
