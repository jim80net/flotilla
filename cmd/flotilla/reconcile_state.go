package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jim80net/flotilla/internal/statereconcile"
)

type reconcileStateExit int

func (e reconcileStateExit) Error() string { return "" }
func (e reconcileStateExit) ExitCode() int { return int(e) }

type reconcileStateConfigError struct{ err error }

func (e reconcileStateConfigError) Error() string { return e.err.Error() }
func (reconcileStateConfigError) ExitCode() int   { return 2 }

func cmdReconcileState(args []string) error {
	return runReconcileState(args, os.Stdout, statereconcile.DefaultObserver{})
}

func runReconcileState(args []string, stdout io.Writer, observer statereconcile.Observer) error {
	fs := flag.NewFlagSet("reconcile-state", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "host-private authorized-state manifest (required)")
	asJSON := fs.Bool("json", false, "emit machine-readable report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *manifestPath == "" {
		return fmt.Errorf("usage: flotilla reconcile-state --manifest <path> [--json]")
	}
	manifest, err := statereconcile.Load(*manifestPath)
	if err != nil {
		return reconcileStateConfigError{err: err}
	}
	report := statereconcile.Run(context.Background(), manifest, observer, time.Now())
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		statereconcile.WriteHuman(stdout, report)
	}
	if code := report.ExitCode(); code != 0 {
		return reconcileStateExit(code)
	}
	return nil
}
