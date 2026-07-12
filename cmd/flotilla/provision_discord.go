package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
)

type discordPlan struct {
	FlotillaKey string `json:"flotilla_key"`
	Category    string `json:"category"`
	COSCategory string `json:"cos_category"`
	C2Channel   string `json:"c2_channel"`
	ProductHub  string `json:"product_hub"`
	XOAgent     string `json:"xo_agent"`
	WebhookKey  string `json:"webhook_secret_key"`
	Placement   string `json:"placement"`
}

func cmdProvisionDiscord(args []string) error {
	key := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		key, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("provision-discord", flag.ContinueOnError)
	name := fs.String("name", "", "flotilla category display name (default: title-cased key)")
	xo := fs.String("xo", "", "XO roster agent (default: <key>-xo)")
	c2 := fs.String("c2", "", "C2 channel name (default: <key>-xo)")
	product := fs.String("product", "", "product hub channel name (default: <key>)")
	rp := fs.String("roster", rosterDefault(), "roster config path")
	sp := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path")
	dryRun := fs.Bool("dry-run", false, "print the complete plan without Discord or file mutations")
	applyRoster := fs.Bool("apply-roster", false, "atomically append the two channel bindings to the roster")
	var members stringSliceFlag
	fs.Var(&members, "member", "XO subordinate included in the C2 binding (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if key == "" && len(rest) > 0 {
		key, rest = rest[0], rest[1:]
	}
	if strings.TrimSpace(key) == "" || len(rest) != 0 {
		return fmt.Errorf("usage: flotilla provision-discord <flotilla-key> [--dry-run] [--apply-roster]")
	}
	key = strings.TrimSpace(key)
	if *name == "" {
		*name = displayName(key)
	}
	if *xo == "" {
		*xo = key + "-xo"
	}
	if *c2 == "" {
		*c2 = key + "-xo"
	}
	if *product == "" {
		*product = key
	}
	plan := discordPlan{key, *name, "COS", *c2, *product, *xo, roster.WebhookKey(*xo), "C2 under COS; product hub under flotilla category (intentional dual placement)"}
	if *dryRun {
		out, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	cfg, err := roster.Load(*rp)
	if err != nil {
		return err
	}
	if _, err := cfg.Agent(*xo); err != nil {
		return fmt.Errorf("--xo %q: %w", *xo, err)
	}
	for _, m := range members {
		if _, err := cfg.Agent(m); err != nil {
			return fmt.Errorf("--member %q: %w", m, err)
		}
	}
	if cfg.GuildID == "" {
		return fmt.Errorf("roster has no guild_id")
	}
	if *applyRoster && cfg.ChannelID != "" {
		return fmt.Errorf("--apply-roster cannot add channels[] while legacy channel_id is set; migrate the roster binding form first")
	}
	token, err := botToken(*sp)
	if err != nil {
		return err
	}
	prov, err := discord.NewProvisioner(token)
	if err != nil {
		return err
	}
	stack, err := prov.ProvisionOrgStack(cfg.GuildID, *name, *c2, *product, *xo)
	if err != nil {
		return err
	}
	bindings := []roster.Channel{
		{ChannelID: stack.C2.ID, XOAgent: *xo, Members: members, Role: "fleet-command"},
		{ChannelID: stack.Product.ID, XOAgent: *xo, Role: "project"},
	}
	if *applyRoster {
		if err := patchRosterChannels(*rp, bindings); err != nil {
			return err
		}
		fmt.Printf("applied 2 channel bindings to %s\n", *rp)
	} else {
		out, _ := json.MarshalIndent(bindings, "", "  ")
		fmt.Printf("add these entries to flotilla.json channels[] (or rerun with --apply-roster):\n%s\n", out)
	}
	if stack.Created["webhook"] {
		fmt.Printf("append this secret to %s (value shown once):\n%s=%s\n", *sp, plan.WebhookKey, stack.XO.URL)
	} else {
		fmt.Printf("webhook %q already exists; ensure %s is present in %s (Discord cannot reveal its token)\n", *xo, plan.WebhookKey, *sp)
	}
	return nil
}

func displayName(key string) string {
	parts := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(key))
	for i := range parts {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, " ")
}

func patchRosterChannels(path string, add []roster.Channel) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse roster for patch: %w", err)
	}
	var channels []roster.Channel
	if v := doc["channels"]; len(v) > 0 {
		if err := json.Unmarshal(v, &channels); err != nil {
			return fmt.Errorf("parse roster channels: %w", err)
		}
	}
	seen := make(map[string]bool)
	for _, c := range channels {
		seen[c.ChannelID] = true
	}
	for _, c := range add {
		if !seen[c.ChannelID] {
			channels = append(channels, c)
			seen[c.ChannelID] = true
		}
	}
	v, _ := json.Marshal(channels)
	doc["channels"] = v
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".discord.tmp")
	if err := os.WriteFile(tmp, append(out, '\n'), info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := roster.Load(tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("patched roster failed validation: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
