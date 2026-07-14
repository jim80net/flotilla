package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParadeMarkerExpiresFailQuiet683(t *testing.T) {
	dir := t.TempDir()
	const token = "0123456789abcdef"
	created := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if err := markParadePending(dir, "backend", token, created); err != nil {
		t.Fatal(err)
	}
	turn := appendParadeEgressFooter("late parade", token)
	if claimParadePending(dir, "backend", turn, created.Add(paradeMarkerTTL+time.Second)) {
		t.Fatal("expired marker must not authorize Discord")
	}
	path, _ := paradePendingPath(dir, "backend", token)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired marker was not cleared: %v", err)
	}
}

func TestClearParadePendingOnlyClearsMatchingRequest683(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	const first = "0123456789abcdef"
	const second = "fedcba9876543210"
	if err := markParadePending(dir, "backend", first, now); err != nil {
		t.Fatal(err)
	}
	if err := markParadePending(dir, "backend", second, now); err != nil {
		t.Fatal(err)
	}

	clearParadePending(dir, "backend", first)

	firstPath, _ := paradePendingPath(dir, "backend", first)
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("first request marker was not cleared: %v", err)
	}
	secondPath, _ := paradePendingPath(dir, "backend", second)
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("clearing first request removed second request marker: %v", err)
	}
}

func TestParadeFooterStripsOnlyMachineToken683(t *testing.T) {
	const token = "fedcba9876543210"
	withFooter := appendParadeEgressFooter("operator parade", token)
	if !strings.Contains(withFooter, token) {
		t.Fatal("dispatch body must carry correlation token")
	}
	if got := stripParadeEgressFooter(withFooter); got != "operator parade" {
		t.Fatalf("stripped body = %q", got)
	}
}
