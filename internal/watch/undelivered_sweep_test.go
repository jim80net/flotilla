package watch

import "testing"

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
		// nil set path covered by Mark(nil) in production hooks — New never nil.
		t.Fatal("fresh set")
	}
	var nilSet *UndeliveredAlertSet
	if !nilSet.Mark("any") {
		t.Fatal("nil set Mark returns true (no exactly-once)")
	}
}
