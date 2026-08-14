package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
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

type accountRefreshReport struct {
	SubscriptionID string `json:"subscription_id"`
	Credential     string `json:"credential_status"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	LoggedIn       *bool  `json:"logged_in,omitempty"`
	Probe          string `json:"probe"`
	Warning        string `json:"warning,omitempty"`
}

var runClaudeAuthLogin = func(configDir string) error {
	cmd := exec.Command("claude", "auth", "login", "--claudeai")
	cmd.Env = accountEnv(configDir)
	cmd.Stdin = os.Stdin
	stdout := newOAuthOutputBroker(os.Stderr)
	stderr := newOAuthOutputBroker(os.Stderr)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	stdout.Flush()
	stderr.Flush()
	return err
}

func cmdAccountsRefresh(args []string) error {
	fs := flag.NewFlagSet("accounts refresh", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm starting the provider OAuth login flow")
	probeOnly := fs.Bool("probe-only", false, "run the read-only credential-file probe and exit")
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

	report := probeAccountRefresh(id, time.Now())
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
		return fmt.Errorf("Claude OAuth renewal failed; raw provider details were withheld from flotilla output")
	}
	fmt.Printf("OAuth renewal completed for %q; run `flotilla accounts refresh --probe-only %s` to verify.\n", id, id)
	return nil
}

func probeAccountRefresh(id string, now time.Time) accountRefreshReport {
	h, _ := accounts.ProbeHealth(id, now)
	report := accountRefreshReport{
		SubscriptionID: id,
		Credential:     h.Status,
		Probe:          "ok",
		Warning:        accountCredentialWarning(h.Status),
	}
	if !h.ExpiresAt.IsZero() {
		report.ExpiresAt = h.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if h.Status == accounts.StatusUnreadable {
		report.Probe = "unavailable"
	}
	return report
}

// oauthOutputBroker is the credential boundary around the provider CLI. It
// emits only the browser URL needed to finish OAuth and canonical progress
// messages; provider text is never passed through verbatim.
type oauthOutputBroker struct {
	mu      sync.Mutex
	dst     io.Writer
	pending bytes.Buffer
}

func newOAuthOutputBroker(dst io.Writer) *oauthOutputBroker {
	return &oauthOutputBroker{dst: dst}
}

func (b *oauthOutputBroker) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _ = b.pending.Write(p)
	for {
		line, err := b.pending.ReadString('\n')
		if err != nil {
			_, _ = b.pending.WriteString(line)
			// Interactive CLIs commonly render an input prompt without a
			// trailing newline. Emit its canonical form immediately so stdin
			// remains usable instead of waiting until the child exits.
			if prompt := safeOAuthPrompt(b.pending.String()); prompt != "" {
				fmt.Fprintln(b.dst, prompt)
				b.pending.Reset()
			}
			break
		}
		b.emitSafeLine(line)
	}
	return len(p), nil
}

func (b *oauthOutputBroker) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending.Len() != 0 {
		b.emitSafeLine(b.pending.String())
		b.pending.Reset()
	}
}

func (b *oauthOutputBroker) emitSafeLine(line string) {
	if authURL := allowedOAuthURL(line); authURL != "" {
		fmt.Fprintf(b.dst, "OAuth URL: %s\n", authURL)
		return
	}
	if prompt := safeOAuthPrompt(line); prompt != "" {
		fmt.Fprintln(b.dst, prompt)
	}
}

func safeOAuthPrompt(line string) string {
	lower := strings.ToLower(strings.TrimSpace(stripANSI(line)))
	switch {
	case strings.HasPrefix(lower, "press enter to open"):
		return "Press Enter to open the OAuth page in your browser."
	case strings.HasPrefix(lower, "opening") && strings.Contains(lower, "browser"):
		return "Opening the OAuth page in your browser."
	case strings.HasPrefix(lower, "waiting for") && strings.Contains(lower, "auth"):
		return "Waiting for OAuth authentication in your browser."
	case strings.HasPrefix(lower, "login successful") || strings.HasPrefix(lower, "authentication successful"):
		return "OAuth authentication succeeded."
	}
	return ""
}

func allowedOAuthURL(line string) string {
	for _, field := range strings.Fields(stripANSI(line)) {
		candidate := strings.Trim(field, "<>[](){}\"',.;")
		if !strings.HasPrefix(candidate, "https://") {
			continue
		}
		u, err := url.Parse(candidate)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || !allowedOAuthHost(u.Hostname()) {
			continue
		}
		return u.String()
	}
	return ""
}

func stripANSI(s string) string {
	var clean strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) {
				final := s[i]
				i++
				if final >= 0x40 && final <= 0x7e {
					break
				}
			}
			continue
		}
		clean.WriteByte(s[i])
		i++
	}
	return clean.String()
}

func allowedOAuthHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "claude.ai" || strings.HasSuffix(host, ".claude.ai") ||
		host == "anthropic.com" || strings.HasSuffix(host, ".anthropic.com")
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
