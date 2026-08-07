package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/messagebuffer"
	"github.com/jim80net/flotilla/internal/outbox"
)

func TestParseCancelArgsAcceptsFlagsOnEitherSide(t *testing.T) {
	for _, args := range [][]string{
		{"queued-id", "--roster", "/tmp/fleet.json"},
		{"--roster", "/tmp/fleet.json", "queued-id"},
		{"--legacy-outbox", "--roster", "/tmp/fleet.json", "queued-id"},
	} {
		opts, err := parseCancelArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		if opts.id != "queued-id" || opts.rosterPath != "/tmp/fleet.json" {
			t.Fatalf("parse %v = %+v", args, opts)
		}
		if opts.legacyOutbox != (args[0] == "--legacy-outbox") {
			t.Fatalf("parse %v legacy-outbox = %v", args, opts.legacyOutbox)
		}
	}
}

func TestParseCancelArgsRejectsMissingAndExtraPositionals(t *testing.T) {
	for _, args := range [][]string{{}, {"one", "two"}} {
		if _, err := parseCancelArgs(args); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("parse %v error = %v, want usage", args, err)
		}
	}
}

func TestCmdCancelAdvancesOutboxPair(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	t.Setenv("FLOTILLA_SELF", "alpha-desk")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	id, _, err := outbox.Enqueue(dir, "alpha-desk", "alpha-xo", "queued task")
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdCancel([]string{id, "--roster", rosterPath, "--legacy-outbox"}); err != nil {
		t.Fatal(err)
	}
	if got := outbox.ListAll(dir); len(got) != 0 {
		t.Fatalf("pending after cancel = %+v", got)
	}
}

func TestCmdCancelBufferMissCannotDestroyLegacyGeneration(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	t.Setenv("FLOTILLA_SELF", "alpha-desk")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	id, _, err := outbox.Enqueue(dir, "alpha-desk", "alpha-xo", "operator-authorized deploy")
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdCancel([]string{id, "--roster", rosterPath}); err == nil ||
		!strings.Contains(err.Error(), "requires --legacy-outbox") {
		t.Fatalf("buffer miss error = %v, want explicit legacy opt-in", err)
	}
	if got := outbox.ListAll(dir); len(got) != 1 || got[0].ID != id {
		t.Fatalf("buffer miss mutated legacy generation: %+v", got)
	}
}

func TestCmdCancelFailsClosedWhenRosterDoesNotResolve(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	t.Setenv("FLOTILLA_SELF", "alpha-desk")
	dir := t.TempDir()
	id, _, err := outbox.Enqueue(dir, "alpha-desk", "alpha-xo", "queued task")
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.json")
	if err := cmdCancel([]string{id, "--roster", missing}); err == nil || !strings.Contains(err.Error(), "cancel: stat roster") {
		t.Fatalf("missing roster error = %v", err)
	}
	if got := outbox.ListAll(dir); len(got) != 1 || got[0].ID != id {
		t.Fatalf("missing roster mutated outbox: %+v", got)
	}
}

func TestCmdCancelRejectsDifferentBufferedSenderWithoutHistoryChange(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	t.Setenv("FLOTILLA_SELF", "seat-b")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	target, _, err := messagebuffer.Enqueue(dir, "seat-a", "recipient", "authorized work", messagebuffer.EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := cmdCancel([]string{target.ID, "--roster", rosterPath}); err == nil || !strings.Contains(err.Error(), "does not own") {
		t.Fatalf("cross-sender buffer cancel error = %v", err)
	}
	entries := messagebuffer.ListAll(dir)
	if len(entries) != 1 || entries[0].ID != target.ID || entries[0].SupersededBy != "" {
		t.Fatalf("cross-sender refusal changed buffer history: %+v", entries)
	}
}

func TestCmdCancelRejectsDifferentLegacySenderWithoutGenerationChange(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	t.Setenv("FLOTILLA_SELF", "seat-b")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	first, _, err := outbox.Enqueue(dir, "seat-a", "recipient", "first authorized task")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := outbox.Enqueue(dir, "seat-a", "recipient", "second authorized task")
	if err != nil {
		t.Fatal(err)
	}

	if err := cmdCancel([]string{first, "--roster", rosterPath, "--legacy-outbox"}); err == nil || !strings.Contains(err.Error(), "does not own") {
		t.Fatalf("cross-sender legacy cancel error = %v", err)
	}
	entries := outbox.ListAll(dir)
	if len(entries) != 2 || entries[0].ID != first || entries[1].ID != second || entries[0].Epoch != 1 || entries[1].Epoch != 1 {
		t.Fatalf("cross-sender refusal changed legacy generation: %+v", entries)
	}
}

func TestCmdCancelAllowsOriginalBufferedSender(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	t.Setenv("FLOTILLA_SELF", "seat-a")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	target, _, err := messagebuffer.Enqueue(dir, "seat-a", "recipient", "authorized work", messagebuffer.EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := cmdCancel([]string{target.ID, "--roster", rosterPath}); err != nil {
		t.Fatal(err)
	}
	entries := messagebuffer.ListAll(dir)
	if len(entries) != 2 || entries[0].ID != target.ID || entries[0].SupersededBy == "" || entries[1].Sender != "seat-a" {
		t.Fatalf("original-sender cancellation history = %+v", entries)
	}
}

func TestCmdCancelRequiresCallerIdentity(t *testing.T) {
	t.Setenv("FLOTILLA_ROSTER", "")
	t.Setenv("FLOTILLA_SELF", "")
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	target, _, err := messagebuffer.Enqueue(dir, "seat-a", "recipient", "authorized work", messagebuffer.EnqueueOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := cmdCancel([]string{target.ID, "--roster", rosterPath}); err == nil || !strings.Contains(err.Error(), "FLOTILLA_SELF") {
		t.Fatalf("missing caller identity error = %v", err)
	}
	entries := messagebuffer.ListAll(dir)
	if len(entries) != 1 || entries[0].ID != target.ID || entries[0].SupersededBy != "" {
		t.Fatalf("missing-identity refusal changed buffer history: %+v", entries)
	}
}
