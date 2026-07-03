package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/accounts"
)

func cmdAccounts(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla accounts init|list")
	}
	switch args[0] {
	case "init":
		return cmdAccountsInit(args[1:])
	case "list":
		return cmdAccountsList(args[1:])
	default:
		return fmt.Errorf("unknown accounts subcommand %q (try: init, list)", args[0])
	}
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
