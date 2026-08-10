package watch

import (
	"strconv"
	"strings"
	"sync/atomic"
)

var directDeliverySequence atomic.Uint64

// deliveryIdentity constructs an injective identity from typed fields. Each
// component is length-prefixed, so separators inside field content cannot make
// two distinct tuples alias. All watch delivery identities flow through here.
func deliveryIdentity(kind string, fields ...string) string {
	var b strings.Builder
	b.WriteString("v1")
	writeIdentityField(&b, kind)
	for _, field := range fields {
		writeIdentityField(&b, field)
	}
	return b.String()
}

// NewDirectDeliveryIdentity returns a fresh identity for legacy one-shot send
// call sites that do not carry a durable message key.
func NewDirectDeliveryIdentity() string {
	return deliveryIdentity("direct", strconv.FormatUint(directDeliverySequence.Add(1), 10))
}

func writeIdentityField(b *strings.Builder, value string) {
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(len(value)))
	b.WriteByte(':')
	b.WriteString(value)
}
