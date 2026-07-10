package adjutantbuffer

import "testing"

func TestFormatExtractOperatorReason(t *testing.T) {
	const id = "snow123"
	const body = "ship the gate\nverbatim"
	reason := FormatOperatorReason(id, body)
	gotID, gotBody, ok := ExtractOperatorBody(reason)
	if !ok || gotID != id || gotBody != body {
		t.Fatalf("round-trip failed: id=%q body=%q ok=%v", gotID, gotBody, ok)
	}
}

func TestPartitionItemsSeparatesOperator(t *testing.T) {
	items := []Item{
		{Reason: FormatOperatorReason("m1", "hi")},
		{Reason: "backend: finished"},
	}
	op, other := PartitionItems(items)
	if len(op) != 1 || len(other) != 1 {
		t.Fatalf("partition = %d op %d other", len(op), len(other))
	}
	if !IsOperatorReason(op[0].Reason) {
		t.Fatal("expected operator item")
	}
}

func TestHasOperatorMessage(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/buf.json"
	if HasOperatorMessage(path, "m1") {
		t.Fatal("absent buffer should not report message")
	}
	if err := Append(path, "cos", []string{FormatOperatorReason("m1", "hello")}); err != nil {
		t.Fatal(err)
	}
	if !HasOperatorMessage(path, "m1") {
		t.Fatal("expected buffered operator message")
	}
	if HasOperatorMessage(path, "m2") {
		t.Fatal("wrong id should not match")
	}
}
