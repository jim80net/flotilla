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

// briefArgs is the parsed `flotilla brief` invocation. desk is the desk to brief when
// not using --all; from is the orchestrator's identity issuing the request (recorded in
// the delivery confirmation line for audit symmetry with send; the desk still publishes
// under its OWN webhook identity, not from); audience is the reader the brief is modeled for.
type briefArgs struct {
	desk       string
	all        bool
	from       string
	rosterPath string
	audience   string
}

// parseBriefArgs parses `flotilla brief [--all] [<desk>] [--from] [--roster] [--audience]`.
// The desk positional may appear anywhere among the args (like doctrine install/register).
func parseBriefArgs(args []string) (briefArgs, error) {
	desk, flagArgs, err := parseBriefInterleavedArgs(args)
	if err != nil {
		return briefArgs{}, err
	}
	fs := flag.NewFlagSet("brief", flag.ContinueOnError)
	from := fs.String("from", os.Getenv("FLOTILLA_SELF"), "orchestrator identity issuing the brief request")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	audience := fs.String("audience", string(readermap.AudienceOperator), "the reader the brief is modeled for (operator|newcomer|maintainer|desk:<name>)")
	all := fs.Bool("all", false, "brief every non-primary-XO agent in the roster")
	if err := fs.Parse(flagArgs); err != nil {
		return briefArgs{}, err
	}
	if len(fs.Args()) != 0 {
		return briefArgs{}, fmt.Errorf("usage: flotilla brief [--all] [<desk>] [--audience <who>] [--roster <path>]")
	}
	if *all && desk != "" {
		return briefArgs{}, fmt.Errorf("usage: flotilla brief [--all] [<desk>] … (not both --all and <desk>)")
	}
	if !*all && desk == "" {
		return briefArgs{}, fmt.Errorf("usage: flotilla brief [--all] [<desk>] [--audience <who>] [--roster <path>]")
	}
	return briefArgs{
		desk:       desk,
		all:        *all,
		from:       *from,
		rosterPath: *rosterPath,
		audience:   strings.TrimSpace(*audience),
	}, nil
}

// parseBriefInterleavedArgs splits an optional desk name (accepted anywhere among the
// args) from flag tokens so `brief --all --audience operator` and `brief --from cos
// backend` both work — the stdlib flag parser stops at the first non-flag token.
func parseBriefInterleavedArgs(args []string) (desk string, flagArgs []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--roster" && i+1 < len(args):
			flagArgs = append(flagArgs, a, args[i+1])
			i++
		case a == "--audience" && i+1 < len(args):
			flagArgs = append(flagArgs, a, args[i+1])
			i++
		case a == "--from" && i+1 < len(args):
			flagArgs = append(flagArgs, a, args[i+1])
			i++
		case strings.HasPrefix(a, "-"):
			flagArgs = append(flagArgs, a)
		case desk == "":
			desk = a
		default:
			return "", nil, fmt.Errorf("unexpected argument %q", a)
		}
	}
	return desk, flagArgs, nil
}

// buildBriefRequest is the pure brief-request prompt injected into the desk's pane.
// It instructs the desk to AUTHOR a reader-modeled brief and emit it as a fenced
// reader-map envelope (the structured-output authoring of Pillar B), and it carries
// the desk-secret-free invariant forward: the desk answers IN-PANE and never runs
// notify nor touches a secret — the watch daemon records the turn-final in the
// session-mirror ledger for dash visibility. The envelope is what the mirror detects
// and renders; the desk's modeling judgment goes into the anchor/decision.
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

Do NOT run "flotilla notify" and do NOT touch any secret or webhook — just answer in-pane; your brief is published to the dash automatically by the fleet ledger.`,
		reader, readermap.FenceTag, readermap.FenceTag, audience)
}

// deskIsDark reports whether a desk's channel webhook does not resolve (no URL or an
// error) — a "dark" desk whose brief cannot be published. Pure, so the pre-check
// decision is testable without a real secrets file.
func deskIsDark(webhookURL string, webhookErr error) bool {
	return webhookErr != nil || strings.TrimSpace(webhookURL) == ""
}

// cmdBrief elicits a reader-modeled brief from one desk or every non-primary-XO agent
// and lets the shipped ledger publish it to the dash (Pillar A). It injects the request
// through the same confirmed-delivery path as send; neither orchestrator nor desk needs
// Discord secrets for a brief.
func cmdBrief(args []string) error {
	a, err := parseBriefArgs(args)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(a.rosterPath)
	if err != nil {
		return err
	}
	if a.all {
		return cmdBriefAll(cfg, a)
	}
	return deliverBriefOne(cfg, a, a.desk)
}

// primaryXOAgent returns the hub XO name using the same rule as flotilla watch and the
// turn-final ledger: explicit xo_agent, else the first roster agent (legacy single-fleet).
func primaryXOAgent(cfg *roster.Config) string {
	xo := cfg.XOAgent
	if xo == "" && len(cfg.Agents) > 0 {
		xo = cfg.Agents[0].Name
	}
	return xo
}

// briefTargets returns every roster agent except the primary XO — the same set the
// per-desk ledger covers (logMirrorCoverage / detector pendingMirrors).
func briefTargets(cfg *roster.Config) []string {
	xo := primaryXOAgent(cfg)
	out := make([]string, 0, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		if agent.Name == xo {
			continue
		}
		out = append(out, agent.Name)
	}
	return out
}

func cmdBriefAll(cfg *roster.Config, a briefArgs) error {
	var failures int
	for _, desk := range briefTargets(cfg) {
		if err := deliverBriefOne(cfg, a, desk); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %s: %v\n", desk, err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("flotilla brief --all: %d desk(s) failed (roster %s)", failures, a.rosterPath)
	}
	return nil
}

// deliverBriefOne injects a ledger-bound brief request into one desk.
func deliverBriefOne(cfg *roster.Config, a briefArgs, desk string) error {
	agent, err := cfg.Agent(desk)
	if err != nil {
		return err
	}

	// Resolve the desk's surface driver (how it submits a turn). Unknown surface is a
	// clear error, never a silent mis-drive.
	drv, ok := surface.Get(agent.Surface)
	if !ok {
		return fmt.Errorf("desk %q: unknown surface %q", desk, agent.Surface)
	}

	message := buildBriefRequest(a.audience)

	// Inject the brief-request as a confirmed delivery — the SAME idle-gate → submit →
	// confirm path `send` uses (so the desk actually starts the brief turn), holding
	// the per-pane transaction lock so it cannot interleave with a watch rotate or a
	// dash control action on the same pane.
	// TODO(#213): this confirmed-delivery core duplicates cmdSend's; extract a shared
	// confirmedDeliver(cfg, agent, message) helper (kept inline here to avoid
	// refactoring the safety-critical send path during P0).
	pane, err := deliver.ResolvePane(agent.Title())
	if err != nil {
		return err
	}
	txn, err := deliver.AcquirePaneTxn(pane, deliver.PaneTxnTimeout)
	if err != nil {
		return fmt.Errorf("%s pane is busy (another delivery/rotate in progress) — brief NOT delivered; retry: %w", desk, err)
	}
	defer txn.Release()
	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirm.SendCtrlC = deliver.SendCtrlC
	}
	if err := confirm.SubmitWithSelfHeal(drv, pane, message); err != nil {
		switch {
		case errors.Is(err, surface.ErrBusy):
			return fmt.Errorf("%s is busy (mid-turn) — brief NOT delivered; retry when it is idle", desk)
		case errors.Is(err, surface.ErrCrashed):
			return fmt.Errorf("%s is at a shell (crashed) — brief NOT delivered", desk)
		case errors.Is(err, surface.ErrPanelBlocked):
			return fmt.Errorf("%s is input-blocked behind the agents panel — brief NOT delivered; it needs a human keystroke or click into the composer at its pane, then retry", desk)
		default: // ErrTransient / ErrUnconfirmed / a paste-lock error
			return fmt.Errorf("brief request to %s could not be confirmed: %w", desk, err)
		}
	}
	if a.from != "" {
		fmt.Printf("brief request from %s delivered to %s (pane %s) — turn confirmed; its turn-final publishes to the dash ledger\n", a.from, desk, pane)
	} else {
		fmt.Printf("brief request delivered to %s (pane %s) — turn confirmed; its turn-final publishes to the dash ledger\n", desk, pane)
	}
	return nil
}
