package main

import (
	"flag"
	"fmt"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/workspace"
)

// smartPushSnippetTemplate is the canonical "reporting to the fleet" convention seeded
// into a non-Claude desk's identity file to make it a push-capable peer (smart-desks,
// #63). It instructs the desk to report to the XO via `flotilla send` — PURE TMUX
// INJECTION into the XO's pane, which needs NO secrets — and NEVER via `flotilla notify`
// (which needs the fleet secrets: the Discord bot token + every webhook). The desk
// reports a POINTER (status + where its output lives), so the XO's context stays cheap
// (it pulls detail from the pane/files — the pull-participant model still underlies push).
// The two `%s` are filled with the desk's and the XO's roster names.
const smartPushSnippetTemplate = `## Reporting to the fleet (flotilla)

When you finish a delegated task, get blocked needing a decision, or hit an error you
cannot resolve, report to the XO by running this shell command:

    flotilla send --from %s %s "<one line: status + where your output is>"

- Report a POINTER, not your whole transcript — a one-line status and where the result
  lives (a file path, a branch, a PR). The XO reads your pane/files for the detail.
- Report once per completion, not once per step.
- Do NOT run "flotilla notify" and do NOT touch any secrets or webhook — you report to
  the XO (pane injection, no secrets); the XO owns the operator-facing Discord channel
  and relays onward if it needs the operator.
`

// cmdPushSnippet prints the smart-push convention snippet for a desk, filled with the
// desk's and the XO's names from the roster, for the operator/launch to append to the
// desk's native identity file (which it also names). It loads ONLY the roster — it
// NEVER loads or emits any secret. That is the smart-desks security invariant: a desk is
// provisioned for push with the binary + the secret-free roster + its own --from
// identity, NEVER the secrets file (push is secret-free `send` to the XO, not
// secret-bearing `notify` to Discord). The desk's launch environment MUST NOT include
// $FLOTILLA_SECRETS — that is a provisioning contract this helper documents but the
// binary cannot enforce.
func cmdPushSnippet(args []string) error {
	fs := flag.NewFlagSet("push-snippet", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: flotilla push-snippet <desk-agent> [--roster <path>]")
	}
	deskName := rest[0]

	cfg, err := roster.Load(*rosterPath)
	if err != nil {
		return err
	}
	out, err := buildPushSnippet(cfg, deskName)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

// buildPushSnippet is the pure core: it resolves the desk + XO + the desk's identity
// file from the roster and returns the full snippet text (the header + the convention).
// It takes ONLY the roster — there is no secrets parameter and it never reaches for one
// (the security invariant). Split out so the output is testable without stdout.
func buildPushSnippet(cfg *roster.Config, deskName string) (string, error) {
	// The desk must be a real roster agent (else a provisioning typo yields a
	// bogus-sender report the XO can't route).
	desk, err := cfg.Agent(deskName)
	if err != nil {
		return "", err
	}
	xo := cfg.XOAgent
	if xo == "" {
		xo = cfg.Agents[0].Name
	}
	if xo == deskName {
		return "", fmt.Errorf("agent %q is the XO — the smart-push snippet provisions a DESK to report TO the XO, not the XO itself", deskName)
	}
	// The desk's native instruction file (AGENTS.md / CONVENTIONS.md / CLAUDE.md).
	idFile, err := workspace.IdentityFileName(desk.Surface)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("# Append the following to %s's identity file (%s) to provision it for push.\n", deskName, idFile) +
		"# Push is secret-free (flotilla send -> the XO's pane); do NOT provision $FLOTILLA_SECRETS to this desk.\n\n"
	return header + fmt.Sprintf(smartPushSnippetTemplate, deskName, xo), nil
}
