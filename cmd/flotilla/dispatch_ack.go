package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
)

// cmdDispatchAck settles one confirmed inbound dispatch for the current seat in
// the durable consumed registry. Registry-first ordering makes a crash safe: the
// watch sweep can clear a still-present inbound row after the durable write.
func cmdDispatchAck(args []string) error {
	fs := flag.NewFlagSet("dispatch-ack", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 {
		return fmt.Errorf("usage: flotilla dispatch-ack [--roster <path>] <nonce>")
	}
	nonce := strings.TrimSpace(fs.Args()[0])
	if inbound.ParseDispatchNonce(nonce) != nonce {
		return fmt.Errorf("dispatch-ack: invalid nonce %q", nonce)
	}
	from := strings.TrimSpace(os.Getenv("FLOTILLA_SELF"))
	if from == "" {
		return fmt.Errorf("dispatch-ack: recipient identity required (set $FLOTILLA_SELF)")
	}
	rp, err := resolveRosterPath(*rosterPath)
	if err != nil {
		return err
	}
	rosterDir := filepath.Dir(rp)
	reg := dispatch.NewRegistry(rosterDir)
	if settled, ok := reg.LookupNonce(nonce); ok {
		if settled.Recipient != "" && settled.Recipient != from {
			return fmt.Errorf("dispatch-ack: nonce belongs to recipient %q, not %q", settled.Recipient, from)
		}
		fmt.Printf("dispatch ack already durable nonce=%s recipient=%s\n", nonce, from)
		return nil
	}
	path, err := inbound.Path(rosterDir, from)
	if err != nil {
		return err
	}
	st := inbound.NewStore(path)
	var match *inbound.Entry
	for _, entry := range st.Load() {
		if entry.Nonce == nonce {
			e := entry
			match = &e
			break
		}
	}
	if match == nil {
		return fmt.Errorf("dispatch-ack: nonce %q is not pending for recipient %q", nonce, from)
	}
	if match.Recipient != from {
		return fmt.Errorf("dispatch-ack: inbound recipient %q does not match seat %q", match.Recipient, from)
	}
	if _, err := reg.Consume(dispatch.ConsumeFromInbound(
		match.Nonce, match.Message, dispatch.ReasonDurableAck, match.Sender, match.Recipient,
	)); err != nil {
		return fmt.Errorf("dispatch-ack: write durable ack: %w", err)
	}
	st.Remove(match.ID)
	fmt.Printf("dispatch ack durable nonce=%s recipient=%s\n", nonce, from)
	return nil
}
