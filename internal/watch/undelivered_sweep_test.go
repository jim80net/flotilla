package watch

import (
	"testing"
	"time"
)

func TestUndeliveredAlertSet_MarkOnce(t *testing.T) {
	s := NewUndeliveredAlertSet()
	if !s.Mark("l1/a") {
		t.Fatal("first mark")
	}
	if s.Mark("l1/a") {
		t.Fatal("second mark must be false")
	}
	if !s.Mark("l2/a") {
		t.Fatal("distinct key")
	}
	if NewUndeliveredAlertSet().Mark("x") != true {
		t.Fatal("fresh set")
	}
	var nilSet *UndeliveredAlertSet
	if !nilSet.Mark("any") {
		t.Fatal("nil set Mark returns true (no exactly-once)")
	}
}

func TestUndeliveredAlertSet_MarkL1Watched(t *testing.T) {
	s := NewUndeliveredAlertSet()
	t0 := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	if !s.MarkL1("l1/x", t0) {
		t.Fatal("first L1")
	}
	if s.MarkL1("l1/x", t0.Add(time.Minute)) {
		t.Fatal("second L1 must be false")
	}
	if s.L1Watched("l1/x", t0.Add(10*time.Minute), 15*time.Minute) {
		t.Fatal("not yet watched long enough")
	}
	if !s.L1Watched("l1/x", t0.Add(16*time.Minute), 15*time.Minute) {
		t.Fatal("should be watched")
	}
}
