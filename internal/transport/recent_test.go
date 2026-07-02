package transport

import "testing"

type recentHistoryTransport struct {
	fakeTransport
}

func (r *recentHistoryTransport) Recent(Destination, int) ([]Message, error) {
	return nil, nil
}

func TestRecentHistory_PresentTypeAsserts(t *testing.T) {
	var tr Transport = &recentHistoryTransport{fakeTransport{name: "with-recent"}}
	if _, ok := tr.(RecentHistory); !ok {
		t.Error("a transport implementing RecentHistory must type-assert as RecentHistory")
	}
}

func TestRecentHistory_AbsentTypeAssertsFalse(t *testing.T) {
	var tr Transport = &fakeTransport{name: "no-recent"}
	if _, ok := tr.(RecentHistory); ok {
		t.Error("a transport NOT implementing RecentHistory must NOT type-assert as RecentHistory")
	}
}
