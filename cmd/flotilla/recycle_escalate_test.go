package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/roster"
)

func TestClassifyRecycleAbort(t *testing.T) {
	cases := []struct {
		err  string
		want recycleAbortClass
	}{
		{"phase 0: backend did not settle to idle at a cleared composer within 1m — ABORT, desk untouched", abortBusyDesk},
		{"phase 2 re-verify: backend is no longer idle at a cleared composer — ABORT, desk untouched", abortBusyDesk},
		{"phase 2: the graceful close of \"backend\" did not confirm the process exited within 30s — the desk MAY STILL BE LIVE", abortPhase2Close},
		{"phase 1: handoff not durably confirmed for \"backend\" within 5m", abortHandoff},
		{"phase 1: target session for \"backend\" appears uncooperative (pane shows \"out of usage credits\") — use `flotilla resume backend --force`", abortHandoff},
		{"refusing to recycle \"xo\": %5 is THIS command's own pane", abortSelf},
		{"something else", abortOther},
	}
	for _, tc := range cases {
		got := classifyRecycleAbort(errors.New(tc.err))
		if got != tc.want {
			t.Errorf("classify(%q) = %q, want %q", tc.err, got, tc.want)
		}
	}
	if classifyRecycleAbort(nil) != "" {
		t.Fatal("nil err must classify empty")
	}
}

func TestIsRetryableBusy(t *testing.T) {
	if !isRetryableBusy(errors.New("phase 0: x did not settle to idle")) {
		t.Fatal("phase 0 busy must be retryable")
	}
	if isRetryableBusy(errors.New("phase 2: close did not confirm")) {
		t.Fatal("phase 2 close must not be busy-retryable")
	}
}

func TestRecycleAbortNotice(t *testing.T) {
	err := errors.New("phase 2: the graceful close of \"backend\" did not confirm")
	got := recycleAbortNotice("backend", "phase 2", abortPhase2Close, err, "/repo/.claude/handoffs/x.md")
	for _, want := range []string{
		"ABORT", "backend", "phase-2-close", "phase 2",
		"resume backend --force", "Handoff path", "#436",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("notice missing %q\nfull: %s", want, got)
		}
	}
}

func TestRecycleAbortNoticeBusyDistinguishesActiveFromStuck(t *testing.T) {
	err := errors.New("phase 0: backend did not settle to idle at a cleared composer")
	got := recycleAbortNotice("backend", "phase 0", abortBusyDesk, err, "")
	for _, want := range []string{
		"genuinely running a turn",
		"composer appears idle but status remains Working/Composing",
		"do NOT retry recycle",
		"flotilla resume backend --force",
		"verified durable handoff",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("busy abort notice missing %q\nfull: %s", want, got)
		}
	}
}

func TestRecycleAbortRouteCoordinatorUsesAdjutant(t *testing.T) {
	cfg := &roster.Config{
		XOAgent:  "cos",
		CosAgent: "cos",
		Agents: []roster.Agent{
			{Name: "cos"},
			{Name: "cos-adj", AdjutantFor: "cos"},
		},
	}
	sender, owner, ok := recycleAbortRoute(cfg, "cos")
	if !ok || sender != "cos-adj" || owner != "cos" {
		t.Fatalf("route = (%q, %q, %t), want cos-adj -> cos", sender, owner, ok)
	}
}

func TestDeliverRecycleAbortBusyQueuesDurablyAndDedupes(t *testing.T) {
	dir := t.TempDir()
	ops := recycleAbortEscalationOps{
		submit:  func(string, string) error { return fmt.Errorf("coordinator busy") },
		enqueue: outbox.Enqueue,
	}
	for i := 0; i < 2; i++ {
		queued, err := deliverRecycleAbort(ops, dir, "cos-adj", "cos", "recycle abort body")
		if err != nil || !queued {
			t.Fatalf("attempt %d = queued %t, err %v", i, queued, err)
		}
	}
	path, err := outbox.Path(dir, "cos-adj")
	if err != nil {
		t.Fatal(err)
	}
	got := outbox.NewStore(path).Load()
	if len(got) != 1 {
		t.Fatalf("outbox entries = %d, want one deduplicated abort", len(got))
	}
	if got[0].Sender != "cos-adj" || got[0].Recipient != "cos" || got[0].Message != "recycle abort body" {
		t.Fatalf("outbox entry = %+v", got[0])
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("outbox path = %q, want roster dir %q", path, dir)
	}
}

func TestDeliverRecycleAbortDirectSuccessDoesNotQueue(t *testing.T) {
	queuedCalls := 0
	ops := recycleAbortEscalationOps{
		submit: func(owner, notice string) error {
			if owner != "cos" || notice != "abort" {
				t.Fatalf("submit(%q, %q)", owner, notice)
			}
			return nil
		},
		enqueue: func(string, string, string, string) (string, bool, error) {
			queuedCalls++
			return "", false, nil
		},
	}
	queued, err := deliverRecycleAbort(ops, t.TempDir(), "cos-adj", "cos", "abort")
	if err != nil || queued || queuedCalls != 0 {
		t.Fatalf("queued=%t calls=%d err=%v", queued, queuedCalls, err)
	}
}
