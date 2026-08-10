package deliveryidentity

import "testing"

func TestEncodeSeparatorContentCannotAlias(t *testing.T) {
	left := Encode("unacked", "a:b", "c")
	right := Encode("unacked", "a", "b:c")
	if left == right {
		t.Fatalf("exact colliding pair aliased: %q", left)
	}
	values := []string{"", ":", "a", "a:b", "b:c", "::", "x|1:y"}
	seen := make(map[string][2]string)
	for _, first := range values {
		for _, second := range values {
			id := Encode("property", first, second)
			if prior, ok := seen[id]; ok && prior != [2]string{first, second} {
				t.Fatalf("distinct tuples %q and %q aliased as %q", prior, [2]string{first, second}, id)
			}
			seen[id] = [2]string{first, second}
		}
	}
}
