package dispatch

import "fmt"

// FormatQueuedAck is the desk-visible machine-readable line printed when a send
// lands in the durable busy outbox (#475 / #614). Desks and detectors can grep
// QUEUED without parsing free-form stderr prose.
func FormatQueuedAck(id, sender, recipient string, deduped bool) string {
	status := "busy_outbox"
	if deduped {
		status = "already_queued"
	}
	return fmt.Sprintf("QUEUED id=%s sender=%s recipient=%s status=%s", id, sender, recipient, status)
}
