package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
)

// cmdMirrorSelf is the pane-independent coordinator finish-edge (#432 / #572 item 1):
// harness Stop hooks (and any external driver) feed a turn-final body on stdin/file;
// flotilla writes session-mirror + optional Discord under the same deskMirror path as
// Working→Idle detector finishes — WITHOUT requiring a detector pane state transition.
//
// Remote-control / pane-less coordinators never emit Working→Idle on Assess; this CLI is
// the trigger-independent path so cos/xo turn-finals still land in dash conversations.
//
//	flotilla mirror-self --from <agent> --file <path|->
//
// Best-effort: missing webhook → loud WARN + session-mirror only (when roster adjacent
// dir is known). Always exit 0 from hooks' perspective when possible (callers use
// check=False); returns error only for hard misuse (empty agent/body, bad flags).
func cmdMirrorSelf(args []string) error {
	fs := flag.NewFlagSet("mirror-self", flag.ContinueOnError)
	from := fs.String("from", os.Getenv("FLOTILLA_SELF"), "agent identity (coordinator or desk)")
	file := fs.String("file", "", "turn-final body file ('-' = stdin)")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file (optional; Discord when present)")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path (session-mirror + CosLedger)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" {
		return fmt.Errorf("mirror-self: --from is required (or set $FLOTILLA_SELF)")
	}
	rest := fs.Args()
	for _, a := range rest {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("mirror-self: unexpected flag %q after positionals — put flags first or use --file", a)
		}
	}
	if *file == "" && len(rest) == 0 {
		return fmt.Errorf("mirror-self: provide --file <path|-> or inline body text")
	}
	if *file == "-" {
		if fi, statErr := os.Stdin.Stat(); statErr == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return fmt.Errorf("mirror-self: --file - requires piped stdin, but stdin is a terminal")
		}
	}
	body, err := resolveMessage(*file, rest, os.Stdin)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("mirror-self: turn-final body is empty")
	}

	var cfg *roster.Config
	if *rosterPath != "" {
		if c, lerr := roster.Load(*rosterPath); lerr == nil {
			cfg = c
		} else {
			log.Printf("flotilla mirror-self: load roster %q: %v — session-mirror may be inert", *rosterPath, lerr)
		}
	}
	rosterDir := ""
	if *rosterPath != "" {
		rosterDir = filepath.Dir(*rosterPath)
	}

	var secrets *roster.Secrets
	if *secretsPath != "" {
		if s, serr := roster.LoadSecrets(*secretsPath); serr == nil {
			secrets = s
		} else {
			log.Printf("flotilla mirror-self: load secrets %q: %v — Discord inert", *secretsPath, serr)
		}
	}

	var tr transport.Transport
	if secrets != nil {
		if t, terr := outboundTransport(secrets); terr == nil {
			tr = t
		} else {
			log.Printf("flotilla mirror-self: outbound transport: %v — Discord inert", terr)
		}
	}

	// Fixed body: no pane Assess / ResultReader — the Stop hook already extracted the turn-final.
	turnBody := body
	m := deskMirror{
		rosterDir: rosterDir,
		claimDiscord: func(a, turnFinal string) bool {
			return claimParadePending(rosterDir, a, turnFinal, time.Now())
		},
		webhook: func(a string) (string, bool) {
			if secrets == nil {
				return "", false
			}
			url, err := secrets.Webhook(a)
			if err != nil || url == "" {
				return "", false
			}
			return url, true
		},
		turnFinal: func(string) (string, bool, error) {
			return turnBody, true, nil
		},
		post: func(url, username, content string) error {
			if tr == nil {
				return fmt.Errorf("no outbound transport")
			}
			return tr.Post(transport.NewWebhookDestination(url), username, content)
		},
		onDiscordSuccess: func(agent, modeled string) {
			if cfg == nil || *rosterPath == "" {
				return
			}
			// Cos who-knows-what: same path coordinatorMirrorOnFinish uses after Discord.
			if cfg.IsCoordinator(agent) || (cfg.CosAgent != "" && agent == cfg.CosAgent) {
				mirrorNotifyToLedger(*rosterPath, agent, modeled)
			}
		},
		logf: log.Printf,
	}
	m.run(*from)
	return nil
}
