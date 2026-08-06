package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/messagebuffer"
	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

type cancelOpts struct {
	id           string
	rosterPath   string
	legacyOutbox bool
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
	legacyOutbox := fs.Bool("legacy-outbox", false, "cancel the legacy outbox generation containing id")
	if err := fs.Parse(args); err != nil {
		return cancelOpts{}, err
	}
	rest := fs.Args()
	if id == "" && len(rest) > 0 {
		id, rest = rest[0], rest[1:]
	}
	if id == "" || len(rest) != 0 {
		return cancelOpts{}, fmt.Errorf("usage: flotilla cancel <message-id> [--roster <path>] [--legacy-outbox]")
	}
	return cancelOpts{id: id, rosterPath: *rosterPath, legacyOutbox: *legacyOutbox}, nil
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
	dir := filepath.Dir(rosterPath)
	if opts.legacyOutbox {
		result, err := outbox.Cancel(dir, opts.id)
		if err != nil {
			return err
		}
		fmt.Printf("flotilla cancel: stood down %d queued send(s) on %s → %s; epoch advanced to %d\n", result.Canceled, result.Sender, result.Recipient, result.Epoch)
		return nil
	}
	cancel, target, bufferErr := messagebuffer.Cancel(dir, opts.id)
	if bufferErr == nil {
		fmt.Printf("flotilla cancel: buffered cancellation id=%s supersedes=%s on %s → %s\n", cancel.ID, target.ID, target.Sender, target.Recipient)
		bestEffortPullNudge(rosterPath, target.Recipient)
		return nil
	}
	if !errors.Is(bufferErr, messagebuffer.ErrNotFound) {
		return bufferErr
	}
	return fmt.Errorf("cancel buffered message %q: %w (legacy generation cancellation requires --legacy-outbox)", opts.id, bufferErr)
}

func bestEffortPullNudge(rosterPath, recipient string) {
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		logNudgeMiss(recipient, err)
		return
	}
	agent, err := cfg.Agent(recipient)
	if err != nil {
		logNudgeMiss(recipient, err)
		return
	}
	drv, ok := surface.Get(agent.Surface)
	if !ok {
		logNudgeMiss(recipient, fmt.Errorf("unknown surface %q", agent.Surface))
		return
	}
	pane, err := deliver.ResolvePane(agent.Title())
	if err != nil {
		logNudgeMiss(recipient, err)
		return
	}
	txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
	if err != nil {
		logNudgeMiss(recipient, err)
		return
	}
	defer txn.Release()
	if err := deliverSendOnce(drv, pane, pullNudgeText); err != nil {
		logNudgeMiss(recipient, err)
	}
}
