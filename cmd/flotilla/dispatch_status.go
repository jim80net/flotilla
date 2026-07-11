package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
)

func cmdDispatchStatus(args []string) error {
	fs := flag.NewFlagSet("dispatch-status", flag.ContinueOnError)
	rosterPath := fs.String("roster", "", "roster config path (default: discover via $FLOTILLA_ROSTER / cwd / state/)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: flotilla dispatch-status [--roster <path>] <nonce>")
	}
	nonce := rest[0]
	rp, err := resolveRosterPath(*rosterPath)
	if err != nil {
		return err
	}
	rosterDir := filepath.Dir(rp)
	st := dispatch.LookupNonce(rosterDir, nonce, time.Now().UTC())
	fmt.Println(dispatch.FormatStatus(st))
	if st.Disposition == dispatch.DispositionUnknown {
		return fmt.Errorf("dispatch-status: nonce %q not found under %s", nonce, rosterDir)
	}
	return nil
}
