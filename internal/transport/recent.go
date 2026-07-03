package transport

import "time"

// RecentHistory is an OPTIONAL Transport capability: fetch the most recent N
// messages in a destination, ascending by id. Used by the un-acked operator
// backstop poller (#234). A transport without channel history (loopback web) need
// not implement it; callers type-assert and skip cleanly when absent.
type RecentHistory interface {
	// Recent returns up to limit of dest's most recent messages, ascending.
	Recent(dest Destination, limit int) ([]Message, error)
	// RecentSince returns ascending messages with Timestamp >= since, walking
	// backward in non-overlapping pages (Discord before-pagination).
	RecentSince(dest Destination, since time.Time) ([]Message, error)
}
