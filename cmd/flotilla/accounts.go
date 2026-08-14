package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/accounts"
)

func cmdAccounts(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla accounts init|list|refresh")
	}
	switch args[0] {
	case "init":
		return cmdAccountsInit(args[1:])
	case "list":
		return cmdAccountsList(args[1:])
	case "refresh":
		return cmdAccountsRefresh(args[1:])
	default:
		return fmt.Errorf("unknown accounts subcommand %q (try: init, list, refresh)", args[0])
	}
}

const accountsAuthProbeTimeout = 10 * time.Second

type accountRefreshReport struct {
	SubscriptionID string `json:"subscription_id"`
	Credential     string `json:"credential_status"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	LoggedIn       *bool  `json:"logged_in,omitempty"`
	Probe          string `json:"probe"`
	Warning        string `json:"warning,omitempty"`
}

type claudeAuthStatus struct {
	LoggedIn *bool `json:"loggedIn"`
}

var runClaudeAuthStatus = func(ctx context.Context, configDir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "claude", "auth", "status", "--json")
	cmd.Env = accountEnv(configDir)
	return cmd.Output()
}

var runClaudeAuthLogin = func(configDir string) error {
	cmd := exec.Command("claude", "auth", "login", "--claudeai")
	cmd.Env = accountEnv(configDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdAccountsRefresh(args []string) error {
	fs := flag.NewFlagSet("accounts refresh", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm starting the provider OAuth login flow")
	probeOnly := fs.Bool("probe-only", false, "run the read-only credential/auth probe and exit")
	asJSON := fs.Bool("json", false, "emit the read-only probe as JSON (requires --probe-only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 || (*yes && *probeOnly) || (*asJSON && !*probeOnly) {
		return fmt.Errorf("usage: flotilla accounts refresh [--probe-only [--json] | --yes] <subscription-id>")
	}
	id, err := accounts.NormalizeID(fs.Args()[0])
	if err != nil {
		return err
	}
	dir, err := accounts.ConfigDir(id)
	if err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("subscription account %q is not initialized; run `flotilla accounts init %s` first", id, id)
	}

	report := probeAccountRefresh(id, dir, time.Now())
	if err := printAccountRefreshReport(report, *asJSON); err != nil {
		return err
	}
	if *probeOnly {
		return nil
	}
	if !*yes {
		return fmt.Errorf("OAuth renewal not started: review the probe, then rerun with --yes to open the provider login flow (no model request is made)")
	}

	fmt.Fprintf(os.Stderr, "Starting Claude subscription OAuth renewal for %q; this may replace credentials only in %s.\n", id, dir)
	if err := runClaudeAuthLogin(dir); err != nil {
		return fmt.Errorf("Claude OAuth renewal failed; provider details were withheld from flotilla output")
	}
	fmt.Printf("OAuth renewal completed for %q; run `flotilla accounts refresh --probe-only %s` to verify.\n", id, id)
	return nil
}

func probeAccountRefresh(id, dir string, now time.Time) accountRefreshReport {
	h, _ := accounts.ProbeHealth(id, now)
	report := accountRefreshReport{
		SubscriptionID: id,
		Credential:     h.Status,
		Probe:          "unavailable",
		Warning:        accountCredentialWarning(h.Status),
	}
	if !h.ExpiresAt.IsZero() {
		report.ExpiresAt = h.ExpiresAt.UTC().Format(time.RFC3339)
	}
	ctx, cancel := context.WithTimeout(context.Background(), accountsAuthProbeTimeout)
	defer cancel()
	raw, _ := runClaudeAuthStatus(ctx, dir)
	var status claudeAuthStatus
	if json.Unmarshal(raw, &status) == nil && status.LoggedIn != nil {
		report.Probe = "ok"
		report.LoggedIn = status.LoggedIn
		if !*status.LoggedIn && report.Warning == "" {
			report.Warning = "provider reports this account is not logged in"
		}
	}
	return report
}

func accountCredentialWarning(status string) string {
	switch status {
	case accounts.StatusExpired:
		return "OAuth credentials are expired"
	case accounts.StatusExpiresSoon:
		return "OAuth credentials expire within 24 hours"
	case accounts.StatusMissingCreds, accounts.StatusNoCredsFile:
		return "OAuth credentials are missing"
	case accounts.StatusUnreadable:
		return "OAuth credential metadata is unreadable"
	default:
		return ""
	}
}

func printAccountRefreshReport(report accountRefreshReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	fmt.Printf("account=%s credential=%s auth_probe=%s", report.SubscriptionID, report.Credential, report.Probe)
	if report.LoggedIn != nil {
		fmt.Printf(" logged_in=%t", *report.LoggedIn)
	}
	if report.ExpiresAt != "" {
		fmt.Printf(" expires=%s", report.ExpiresAt)
	}
	fmt.Println()
	if report.Warning != "" {
		fmt.Printf("CAPACITY_WARN: %s\n", report.Warning)
	}
	return nil
}

func accountEnv(configDir string) []string {
	prefix := accounts.ClaudeConfigEnv + "="
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, prefix) {
			env = append(env, entry)
		}
	}
	return append(env, prefix+configDir)
}

func cmdAccountsInit(args []string) error {
	fs := flag.NewFlagSet("accounts init", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: flotilla accounts init <subscription-id>")
	}
	id := rest[0]
	dir, err := accounts.Init(id)
	if err != nil {
		return err
	}
	fmt.Printf("subscription account ready: %q\n", id)
	fmt.Printf("  config_dir: %s\n", dir)
	fmt.Println()
	fmt.Println("One-time login (run once per subscription):")
	fmt.Printf("  CLAUDE_CONFIG_DIR=%s claude\n", shellQuote(dir))
	fmt.Println("  Then use /login in the session.")
	fmt.Println()
	fmt.Printf("Desks with subscription_id %q in a claude-code harness slot receive this config dir automatically at relaunch.\n", id)
	return nil
}

func cmdAccountsList(args []string) error {
	fs := flag.NewFlagSet("accounts list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: flotilla accounts list [--json]")
	}
	list, err := accounts.List(time.Now())
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(list)
	}
	if len(list) == 0 {
		fmt.Println("accounts: (none)")
		return nil
	}
	for _, h := range list {
		exp := "-"
		if !h.ExpiresAt.IsZero() {
			exp = h.ExpiresAt.UTC().Format(time.RFC3339)
		}
		mtime := "-"
		if !h.CredFileMtime.IsZero() {
			mtime = h.CredFileMtime.UTC().Format(time.RFC3339)
		}
		fmt.Printf("%-24s %-16s expires=%s mtime=%s type=%s\n",
			h.SubscriptionID, h.Status, exp, mtime, strings.TrimSpace(h.SubscriptionType))
		fmt.Printf("  %s\n", h.ConfigDir)
	}
	return nil
}
