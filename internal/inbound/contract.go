package inbound

import "fmt"

// Dispatch ack contract (#472): recipients MUST echo the dispatch nonce verbatim in an
// operator-facing turn-final so DroppedDispatchOnFinish can clear the inbound ledger.
// Without this echo, handled work looks like a drop and triggers duplicate reinject.

const (
	// EchoInstruction is the doctrine-layer ack requirement surfaced on every dispatch.
	EchoInstruction = "Turn-final ack (#472): include this dispatch nonce verbatim in your " +
		"operator-facing turn-final (footer is fine): `%s`"
	// ReinjectEchoReminder repeats the contract on the one-shot resume wake.
	ReinjectEchoReminder = "Before you go idle again, echo the nonce above verbatim in your turn-final."
)

// FormatDispatchFooter appends the machine-readable nonce and human ack instruction.
func FormatDispatchFooter(nonce string) string {
	return fmt.Sprintf(
		"\n\n---\nflotilla dispatch ack (#472)\n%s\n[dispatch nonce: `%s`]\n",
		fmt.Sprintf(EchoInstruction, nonce),
		nonce,
	)
}
