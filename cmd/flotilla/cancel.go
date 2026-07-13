package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/outbox"
)

type cancelOpts struct {
	id         string
	rosterPath string
}

// parseCancelArgs accepts the outbox id on either side of --roster, matching the
// positional/flag ordering supported by other flotilla read-and-recovery verbs.
func parseCancelArgs(args []string) (cancelOpts, error) {
	fs := flag.NewFlagSet("cancel", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	if err := fs.Parse(args); err != nil {
		return cancelOpts{}, err
	}
	if fs.NArg() == 0 {
		return cancelOpts{}, fmt.Errorf("usage: flotilla cancel <outbox-id> [--roster <path>]")
	}
	id := fs.Arg(0)
	if err := fs.Parse(fs.Args()[1:]); err != nil {
		return cancelOpts{}, err
	}
	if fs.NArg() != 0 {
		return cancelOpts{}, fmt.Errorf("unexpected extra argument(s) %v — usage: flotilla cancel <outbox-id> [--roster <path>]", fs.Args())
	}
	return cancelOpts{id: id, rosterPath: *rosterPath}, nil
}

func cmdCancel(args []string) error {
	opts, err := parseCancelArgs(args)
	if err != nil {
		return err
	}
	rosterPath, err := resolveRosterPath(opts.rosterPath)
	if err != nil {
		return err
	}
	result, err := outbox.Cancel(filepath.Dir(rosterPath), opts.id)
	if err != nil {
		return err
	}
	fmt.Printf("flotilla cancel: stood down %d queued send(s) on %s → %s; epoch advanced to %d\n", result.Canceled, result.Sender, result.Recipient, result.Epoch)
	return nil
}
