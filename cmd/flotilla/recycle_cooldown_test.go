package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/chapterend"
)

func seedRecycleCooldown(t *testing.T, dir, token string) {
	t.Helper()
	if err := recordSuccessfulRecycleCooldown(dir, token, time.Date(2026, 8, 28, 5, 20, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func TestChapterEndRecycleCooldownSuppressesSuccessorFirstFinishForAllSignals1037(t *testing.T) {
	for _, signal := range []chapterend.Signal{
		chapterend.SignalPRMergedSettled,
		chapterend.SignalLaneDone,
		chapterend.SignalCoordinatorMark,
	} {
		t.Run(string(signal), func(t *testing.T) {
			dir := t.TempDir()
			seedRecycleCooldown(t, dir, "commanded-token")
			admit, reason, err := admitChapterEndAfterRecycle(
				dir,
				chapterend.Result{ChapterEnd: true, Signal: signal},
				"## Backlog\n- [done] prior chapter\n",
			)
			if err != nil || admit || reason != "successor-first-finish" {
				t.Fatalf("signal=%s admit=%t reason=%q err=%v", signal, admit, reason, err)
			}
		})
	}
}

func TestCLIRecycleThenFinishEdgeDoesNotDispatchSecondSelfRecycle1037(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plan := testPlan()
	plan.agent = "product-xo"
	plan.token = "commanded-cli-token"
	writeLastRecycle(plan.agent, plan, "recycled", nil, worktreeCloseNote{})

	statusDir := filepath.Join(home, ".flotilla", plan.agent)
	admit, reason, err := admitChapterEndAfterRecycle(
		statusDir,
		chapterend.Result{ChapterEnd: true, Signal: chapterend.SignalPRMergedSettled},
		"## Backlog\n- [done] merged work\n",
	)
	if err != nil || admit {
		t.Fatalf("commanded CLI recycle successor finish admit=%t reason=%q err=%v", admit, reason, err)
	}
	if reason != "successor-first-finish" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestWatcherTenureRecycleThenSuccessorCoordinatorMarkDoesNotFireAgain1037(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	plan := testPlan()
	plan.agent = "dormant-product-xo"
	plan.token = "watcher-tenure-token"
	writeLastRecycle(plan.agent, plan, "self-recycled after coordinator tenure", nil, worktreeCloseNote{})

	admit, reason, err := admitChapterEndAfterRecycle(
		filepath.Join(home, ".flotilla", plan.agent),
		chapterend.Result{ChapterEnd: true, Signal: chapterend.SignalCoordinatorMark},
		"## Backlog\n- [done] gather\n",
	)
	if err != nil || admit || reason != "successor-first-finish" {
		t.Fatalf("watcher tenure successor mark admit=%t reason=%q err=%v", admit, reason, err)
	}
}

func TestRecycleStatusAuditPreservesFirstSuccessfulTokenBeyondRollingWindow1037(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	first := testPlan()
	first.agent = "product-xo"
	first.token = "first-commanded-token"
	writeLastRecycle(first.agent, first, "first success", nil, worktreeCloseNote{})

	for i := 0; i < 9; i++ {
		later := first
		later.token = fmt.Sprintf("later-%d", i)
		writeLastRecycle(later.agent, later, "later success", nil, worktreeCloseNote{})
	}

	raw, err := os.ReadFile(filepath.Join(home, ".flotilla", first.agent, "last-recycle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Token        string                      `json:"token"`
		History      []recycleStatusHistoryEntry `json:"history"`
		FirstSuccess recycleStatusHistoryEntry   `json:"first_success"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Token != "later-8" {
		t.Fatalf("current token=%q", got.Token)
	}
	if len(got.History) != maxRecycleStatusHistory {
		t.Fatalf("rolling history length=%d, want %d", len(got.History), maxRecycleStatusHistory)
	}
	for _, entry := range got.History {
		if entry.Token == "first-commanded-token" {
			t.Fatalf("first token unexpectedly remained in rolling history: %+v", got.History)
		}
	}
	if got.FirstSuccess.Token != "first-commanded-token" || !got.FirstSuccess.OK {
		t.Fatalf("first_success=%+v, want immutable first successful token", got.FirstSuccess)
	}
}

func TestChapterEndRecycleCooldownRearmsOnlyForNewWorkOrLaterExplicitSignal1037(t *testing.T) {
	t.Run("new unblocked backlog", func(t *testing.T) {
		dir := t.TempDir()
		seedRecycleCooldown(t, dir, "token")
		admit, reason, err := admitChapterEndAfterRecycle(
			dir,
			chapterend.Result{SuppressReason: "unblocked-items-remain"},
			"## Backlog\n- [next] new chapter work\n",
		)
		if err != nil || !admit || reason != "new-unblocked-backlog" {
			t.Fatalf("admit=%t reason=%q err=%v", admit, reason, err)
		}
		if _, err := os.Stat(filepath.Join(dir, chapterEndRecycleCooldownFile)); !os.IsNotExist(err) {
			t.Fatalf("cooldown must clear after new work: %v", err)
		}
	})

	t.Run("later explicit coordinator mark", func(t *testing.T) {
		dir := t.TempDir()
		seedRecycleCooldown(t, dir, "token")
		if admit, _, err := admitChapterEndAfterRecycle(dir, chapterend.Result{}, "## Backlog\n- [done] old\n"); err != nil || admit {
			t.Fatalf("first ordinary finish admit=%t err=%v", admit, err)
		}
		admit, reason, err := admitChapterEndAfterRecycle(
			dir,
			chapterend.Result{ChapterEnd: true, Signal: chapterend.SignalCoordinatorMark},
			"## Backlog\n- [done] old\n",
		)
		if err != nil || !admit || reason != "explicit-new-chapter-signal" {
			t.Fatalf("admit=%t reason=%q err=%v", admit, reason, err)
		}
	})

	t.Run("later inferred lane signal stays suppressed", func(t *testing.T) {
		dir := t.TempDir()
		seedRecycleCooldown(t, dir, "token")
		if admit, _, err := admitChapterEndAfterRecycle(dir, chapterend.Result{}, "## Backlog\n- [done] old\n"); err != nil || admit {
			t.Fatalf("first ordinary finish admit=%t err=%v", admit, err)
		}
		admit, reason, err := admitChapterEndAfterRecycle(
			dir,
			chapterend.Result{ChapterEnd: true, Signal: chapterend.SignalLaneDone},
			"## Backlog\n- [done] old\n",
		)
		if err != nil || admit || reason != "recycle-cooldown" {
			t.Fatalf("admit=%t reason=%q err=%v", admit, reason, err)
		}
	})
}

func TestNewBacklogRearmsPriorAutoSignalInMemory1037(t *testing.T) {
	dir := t.TempDir()
	seedRecycleCooldown(t, dir, "auto-lane-token")
	tracker := chapterend.NewTracker()
	laneDone := chapterend.Result{ChapterEnd: true, Signal: chapterend.SignalLaneDone}
	if !tracker.Record("product-xo", laneDone) {
		t.Fatal("precondition: prior auto lane signal must be latched")
	}
	admit, reason, err := admitChapterEndAfterRecycle(
		dir,
		chapterend.Result{SuppressReason: "unblocked-items-remain"},
		"## Backlog\n- [next] new chapter work\n",
	)
	if err != nil || !admit || reason != "new-unblocked-backlog" {
		t.Fatalf("admit=%t reason=%q err=%v", admit, reason, err)
	}
	rearmChapterEndTrackerForCooldownReason(tracker, "product-xo", reason)
	if !tracker.Record("product-xo", laneDone) {
		t.Fatal("new unblocked backlog must rearm the same later lane signal")
	}
}
