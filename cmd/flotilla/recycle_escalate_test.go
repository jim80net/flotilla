package main

import (
	"errors"
	"strings"
	"testing"
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
