package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/readermap"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

// briefArgs is the parsed `flotilla brief` invocation. desk is the desk to brief;
// from is the orchestrator's identity (only used for messaging symmetry with send);
// audience is the reader the brief is modeled for (default operator).
type briefArgs struct {
	desk        string
	from        string
	rosterPath  string
	secretsPath string
	audience    string
}

// parseBriefArgs parses `flotilla brief <desk> [--from] [--roster] [--secrets] [--audience]`.
// It is split out so the parsing is unit-testable without tmux or Discord.
func parseBriefArgs(args []string) (briefArgs, error) {
	fs := flag.NewFlagSet("brief", flag.ContinueOnError)
	from := fs.String("from", os.Getenv("FLOTILLA_SELF"), "orchestrator identity issuing the brief request")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path (for the dark-desk pre-check)")
	audience := fs.String("audience", string(readermap.AudienceOperator), "the reader the brief is modeled for (operator|newcomer|maintainer|desk:<name>)")
	if err := fs.Parse(args); err != nil {
		return briefArgs{}, err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return briefArgs{}, fmt.Errorf("usage: flotilla brief <desk> [--audience <who>] [--roster <path>] [--secrets <path>]")
	}
	// Go's flag parser stops at the first positional, so a flag after the desk is
	// silently swallowed — catch it (the same guard send/notify use).
	for _, a := range rest[1:] {
		if strings.HasPrefix(a, "-") {
			return briefArgs{}, fmt.Errorf("unexpected %q after the desk name: put flags before the desk", a)
		}
	}
	return briefArgs{
		desk:        rest[0],
		from:        *from,
		rosterPath:  *rosterPath,
		secretsPath: *secretsPath,
		audience:    strings.TrimSpace(*audience),
	}, nil
}

// buildBriefRequest is the pure brief-request prompt injected into the desk's pane.
// It instructs the desk to AUTHOR a reader-modeled brief and emit it as a fenced
// reader-map envelope (the structured-output authoring of Pillar B), and it carries
// the desk-secret-free invariant forward: the desk answers IN-PANE and never runs
// notify nor touches a secret — the watch daemon's mirror publishes the turn-final
// to the desk's channel automatically. The envelope is what the mirror detects,
// renders, and posts; the desk's modeling judgment goes into the anchor/decision.
func buildBriefRequest(audience string) string {
	if audience == "" {
		audience = string(readermap.AudienceOperator)
	}
	// The reader phrase humanizes a "desk:<name>" or a role audience for the prose.
	reader := audience
	if strings.HasPrefix(audience, "desk:") {
		reader = "desk " + strings.TrimPrefix(audience, "desk:")
	}
	return fmt.Sprintf(`flotilla brief request — produce an executive brief for the %s and emit it AS YOUR TURN-FINAL.

Write it reader-modeled: open from the reader's existing mental map (their terms, not your internal state), and LEAD WITH THE ONE DECISION they must take (or "none").

Emit the brief as a fenced code block tagged %s containing this JSON (fill every field; keep it terse but whole):

`+"```"+`%s
{
  "audience": "%s",
  "anchor": "<the reader's map entry this brief updates, in their terms>",
  "delta": "<what changed>",
  "decision": "<the one action the reader must take, or \"none\">"
}
`+"```"+`

Do NOT run "flotilla notify" and do NOT touch any secret or webhook — just answer in-pane; your brief is published to your channel automatically by the fleet mirror.`,
		reader, readermap.FenceTag, readermap.FenceTag, audience)
}

// deskIsDark reports whether a desk's channel webhook does not resolve (no URL or an
// error) — a "dark" desk whose brief cannot be published. Pure, so the pre-check
// decision is testable without a real secrets file.
func deskIsDark(webhookURL string, webhookErr error) bool {
	return webhookErr != nil || strings.TrimSpace(webhookURL) == ""
}

// cmdBrief elicits a reader-modeled brief from a desk and lets the shipped mirror
// publish it (Pillar A). It is orchestrator-run: when --secrets is available it runs
// the dark-desk pre-check (reporting a desk whose channel webhook does not resolve),
// then injects the brief-request into the desk's pane via the SAME confirmed-delivery
// path as `send` (idle-gate → submit → confirm a turn started). It never calls notify
// and the DESK never touches a secret to publish — the watch daemon's deskMirror
// publishes the desk's turn-final to its channel.
func cmdBrief(args []string) error {
	a, err := parseBriefArgs(args)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(a.rosterPath)
	if err != nil {
		return err
	}
	agent, err := cfg.Agent(a.desk)
	if err != nil {
		return err
	}

	// Dark-desk pre-check (orchestrator capability; does NOT make the desk hold a
	// secret). When secrets are available, verify the desk's channel webhook resolves
	// — a brief to a dark desk would be authored in-pane and then silently never
	// reach the channel (the unconfigured-webhook re-skin of #207).
	if a.secretsPath != "" {
		secrets, serr := roster.LoadSecrets(a.secretsPath)
		if serr != nil {
			return serr
		}
		url, werr := secrets.Webhook(a.desk)
		if deskIsDark(url, werr) {
			return fmt.Errorf("desk %q is DARK: its channel webhook does not resolve — its brief cannot be published (configure the webhook in secrets, then retry)", a.desk)
		}
	} else {
		fmt.Fprintln(os.Stderr, "flotilla brief: note — no --secrets, skipping the dark-desk webhook pre-check (the brief is still injected)")
	}

	// Resolve the desk's surface driver (how it submits a turn). Unknown surface is a
	// clear error, never a silent mis-drive.
	drv, ok := surface.Get(agent.Surface)
	if !ok {
		return fmt.Errorf("desk %q: unknown surface %q", a.desk, agent.Surface)
	}

	message := buildBriefRequest(a.audience)

	// Inject the brief-request as a confirmed delivery — the SAME idle-gate → submit →
	// confirm path `send` uses (so the desk actually starts the brief turn), holding
	// the per-pane transaction lock so it cannot interleave with a watch rotate or a
	// dash control action on the same pane.
	// TODO(reader-modeling): this confirmed-delivery core duplicates cmdSend's; extract
	// a shared confirmedDeliver(cfg, agent, message) helper in a follow-up (kept inline
	// here to avoid refactoring the safety-critical send path during P0).
	pane, err := deliver.ResolvePane(agent.Title())
	if err != nil {
		return err
	}
	txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
	if err != nil {
		return fmt.Errorf("%s pane is busy (another delivery/rotate in progress) — brief NOT delivered; retry: %w", a.desk, err)
	}
	defer txn.Release()
	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirm.SendCtrlC = deliver.SendCtrlC
	}
	if err := confirm.SubmitWithSelfHeal(drv, pane, message); err != nil {
		switch {
		case errors.Is(err, surface.ErrBusy):
			return fmt.Errorf("%s is busy (mid-turn) — brief NOT delivered; retry when it is idle", a.desk)
		case errors.Is(err, surface.ErrCrashed):
			return fmt.Errorf("%s is at a shell (crashed) — brief NOT delivered", a.desk)
		default: // ErrTransient / ErrUnconfirmed / ErrPanelBlocked / a paste-lock error
			return fmt.Errorf("brief request to %s could not be confirmed: %w", a.desk, err)
		}
	}
	fmt.Printf("brief request delivered to %s (pane %s) — turn confirmed; its turn-final publishes to its channel via the mirror\n", a.desk, pane)
	return nil
}
