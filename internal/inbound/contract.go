package inbound

import "fmt"

// Dispatch ack contract (#472): recipients settle the nonce in the durable dispatch
// ledger. The machine protocol stays fleet-side and never needs operator-facing prose.

const (
	// EchoInstruction is retained as the shared instruction name for compatibility with
	// reinject construction; it now directs a durable ack rather than a prose echo.
	EchoInstruction      = "After handling this dispatch, record its durable ack: `flotilla dispatch-ack %s`"
	ReinjectEchoReminder = "Before you go idle again, record the durable ack above."
)

// FormatDispatchFooter appends the machine-readable nonce and human ack instruction.
func FormatDispatchFooter(nonce string) string {
	return fmt.Sprintf(
		"\n\n---\nflotilla dispatch ack (#472)\n%s\n[dispatch nonce: `%s`]\n",
		fmt.Sprintf(EchoInstruction, nonce),
		nonce,
	)
}
