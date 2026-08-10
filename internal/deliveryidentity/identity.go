// Package deliveryidentity owns construction of collision-free logical
// delivery identities for every producer that feeds surface confirmation.
package deliveryidentity

import (
	"strconv"
	"strings"
	"sync/atomic"
)

var sequence atomic.Uint64

// Encode constructs an injective identity from typed fields. Each component is
// length-prefixed, so arbitrary separators in content cannot alias tuples.
func Encode(kind string, fields ...string) string {
	var b strings.Builder
	b.WriteString("v1")
	writeField(&b, kind)
	for _, field := range fields {
		writeField(&b, field)
	}
	return b.String()
}

// New constructs a process-unique identity for a one-shot delivery without a
// durable external key.
func New(kind string) string {
	return Encode(kind, strconv.FormatUint(sequence.Add(1), 10))
}

func writeField(b *strings.Builder, value string) {
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(len(value)))
	b.WriteByte(':')
	b.WriteString(value)
}
