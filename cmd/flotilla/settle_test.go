package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type settleHarness struct {
	t       *testing.T
	root    string
	calls   [][]string
	failAt  string
	touched []string
	audits  []settleAudit
}

func newSettleHarness(t *testing.T, backlogBody string) (*settleHarness, settlePlan, settleOps) {
	t.Helper()
	root := t.TempDir()
	backlogPath := filepath.Join(root, "state", "flotilla-backend-backlog.md")
	if err := os.MkdirAll(filepath.Dir(backlogPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backlogPath, []byte(backlogBody), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &settleHarness{t: t, root: root}
	plan := settlePlan{
		Actor: "backend", Reason: "walk complete", RosterPath: filepath.Join(root, "flotilla.json"),
		Remote: "origin", Ref: defaultSettleRef,
	}
	ops := settleOps{
		readFile: os.ReadFile,
		git: func(args ...string) (string, error) {
			h.calls = append(h.calls, append([]string(nil), args...))
			joined := strings.Join(args, " ")
			if h.failAt != "" && strings.HasPrefix(joined, h.failAt) {
				return "", errors.New("injected failure")
			}
			switch joined {
			case "diff --cached --name-only":
				return "state/flotilla-backend-backlog.md\n", nil
			case "rev-parse HEAD":
				return "0123456789abcdef0123456789abcdef01234567\n", nil
			default:
				return "", nil
			}
		},
		now: func() time.Time { return time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC) },
		audit: func(_ string, event settleAudit) error {
			h.audits = append(h.audits, event)
			return nil
		},
		touch: func(path string) error {
			h.touched = append(h.touched, path)
			return nil
		},
	}
	return h, plan, ops
}

func TestRunSettleCapturesSHAProvesAncestorThenTouchesMarkers(t *testing.T) {
	h, plan, ops := newSettleHarness(t, "## Backlog\n- [done] shipped\n- [blocked] operator question\n")
	if err := runSettle(plan, ops); err != nil {
		t.Fatal(err)
	}
	wantCalls := [][]string{
		{"add", "--", filepath.Join(h.root, "state", "flotilla-backend-backlog.md")},
		{"diff", "--cached", "--name-only"},
		{"commit", "-m", "settle(backend): walk complete"},
		{"rev-parse", "HEAD"},
		{"push", "origin", "0123456789abcdef0123456789abcdef01234567:" + defaultSettleRef},
		{"fetch", "--quiet", "origin", defaultSettleRef},
		{"merge-base", "--is-ancestor", "0123456789abcdef0123456789abcdef01234567", "FETCH_HEAD"},
	}
	if !reflect.DeepEqual(h.calls, wantCalls) {
		t.Fatalf("git calls = %#v, want %#v", h.calls, wantCalls)
	}
	if len(h.audits) != 1 || h.audits[0].SHA != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("audits = %+v", h.audits)
	}
	if len(h.touched) != 2 || !strings.HasSuffix(h.touched[0], "-alive") || !strings.HasSuffix(h.touched[1], "-settled") {
		t.Fatalf("touched = %v", h.touched)
	}
}

func TestRunSettleRefusesUndrainedBacklogBeforeGit(t *testing.T) {
	h, plan, ops := newSettleHarness(t, "## Backlog\n- [in-flight] still working\n")
	err := runSettle(plan, ops)
	if err == nil || !strings.Contains(err.Error(), "not drained") {
		t.Fatalf("error = %v, want not drained", err)
	}
	if len(h.calls) != 0 || len(h.touched) != 0 || len(h.audits) != 0 {
		t.Fatalf("side effects before gate: calls=%v touched=%v audits=%v", h.calls, h.touched, h.audits)
	}
}

func TestRunSettleAncestryFailureLeavesMarkersUntouched(t *testing.T) {
	h, plan, ops := newSettleHarness(t, "## Backlog\n")
	h.failAt = "merge-base --is-ancestor"
	err := runSettle(plan, ops)
	if err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("error = %v, want ancestry failure", err)
	}
	if len(h.touched) != 0 || len(h.audits) != 0 {
		t.Fatalf("proof failure wrote settled state: touched=%v audits=%v", h.touched, h.audits)
	}
}

func TestAppendSettleAuditRecordsActorReasonAndSHA(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	event := settleAudit{
		At: time.Date(2026, 8, 21, 5, 0, 0, 0, time.UTC), Actor: "frontend",
		Reason: "scorecard complete", SHA: "abcdef", Remote: "origin", Ref: defaultSettleRef,
	}
	if err := appendSettleAudit("frontend", event); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".flotilla", "frontend", "settle-log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var got settleAudit
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Actor != event.Actor || got.Reason != event.Reason || got.SHA != event.SHA {
		t.Fatalf("audit = %+v, want actor/reason/sha from %+v", got, event)
	}
}
