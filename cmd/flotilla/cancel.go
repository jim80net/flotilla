package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/outbox"
)

type cancelOpts struct {
	id         string
	rosterPath string
	standDown  bool
}

// parseCancelArgs accepts the outbox id on either side of --roster, matching the
// positional/flag ordering supported by other flotilla read-and-recovery verbs.
func parseCancelArgs(args []string) (cancelOpts, error) {
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("cancel", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	standDown := fs.Bool("stand-down", false, "confirm this is an intentional work stand-down, never queue cleanup")
	if err := fs.Parse(args); err != nil {
		return cancelOpts{}, err
	}
	rest := fs.Args()
	if id == "" && len(rest) > 0 {
		id, rest = rest[0], rest[1:]
	}
	if id == "" || len(rest) != 0 || !*standDown {
		return cancelOpts{}, fmt.Errorf("usage: flotilla cancel <outbox-id> --stand-down [--roster <path>] (cancel is never queue cleanup)")
	}
	return cancelOpts{id: id, rosterPath: *rosterPath, standDown: *standDown}, nil
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
	info, err := os.Stat(rosterPath)
	if err != nil {
		return fmt.Errorf("cancel: stat roster %q: %w", rosterPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("cancel roster %q is a directory", rosterPath)
	}
	result, err := outbox.Cancel(filepath.Dir(rosterPath), opts.id)
	if err != nil {
		return err
	}
	fmt.Printf("flotilla cancel: stood down %d queued send(s) on %s → %s; epoch advanced to %d\n", result.Canceled, result.Sender, result.Recipient, result.Epoch)
	return nil
}
