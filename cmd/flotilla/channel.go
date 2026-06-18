package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
)

// cmdChannel provisions Discord channels mechanically via the bot token — the
// channel-creation complement to the F#105 routing bindings. It is a one-shot REST
// surface (no gateway): create, list, delete.
func cmdChannel(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla channel <create|list|delete> ...")
	}
	switch args[0] {
	case "create":
		return cmdChannelCreate(args[1:])
	case "list":
		return cmdChannelList(args[1:])
	case "delete":
		return cmdChannelDelete(args[1:])
	default:
		return fmt.Errorf("unknown channel subcommand %q (want create|list|delete)", args[0])
	}
}

// channelCreateOpts is the parsed `channel create` invocation.
type channelCreateOpts struct {
	name, ctype, topic, category, xo, role string
	members                                []string
	rosterPath, secretsPath                string
}

// stringSliceFlag collects a repeatable string flag (e.g. --member a --member b).
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// parseChannelCreateArgs parses `channel create`, accepting the name positional
// either before or after the flags (the register-style migration shape). Pure, so
// the parsing is unit-tested.
func parseChannelCreateArgs(args []string) (channelCreateOpts, error) {
	var o channelCreateOpts
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		o.name, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("channel create", flag.ContinueOnError)
	ctype := fs.String("type", "text", "channel type: text|category")
	topic := fs.String("topic", "", "channel topic")
	category := fs.String("category", "", "parent category (name or snowflake id)")
	xo := fs.String("xo", "", "XO agent for the emitted F#105 binding")
	role := fs.String("role", "", "binding role label (e.g. project, fleet-command)")
	var members stringSliceFlag
	fs.Var(&members, "member", "member agent for the binding (repeatable)")
	rp := fs.String("roster", rosterDefault(), "roster config path")
	sp := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path")
	if err := fs.Parse(args); err != nil {
		return channelCreateOpts{}, err
	}
	rest := fs.Args()
	if o.name == "" && len(rest) >= 1 { // name supplied after the flags
		o.name, rest = rest[0], rest[1:]
	}
	if o.name == "" || len(rest) != 0 {
		return channelCreateOpts{}, fmt.Errorf("usage: flotilla channel create <name> [--type text|category] [--topic <t>] [--category <name|id>] [--xo <agent> [--member <agent>]... [--role <label>]]")
	}
	if *ctype != "text" && *ctype != "category" {
		return channelCreateOpts{}, fmt.Errorf("--type must be text or category, got %q", *ctype)
	}
	// --member/--role only make sense alongside --xo (they shape the emitted binding).
	if *xo == "" && (len(members) > 0 || *role != "") {
		return channelCreateOpts{}, fmt.Errorf("--member/--role require --xo (the binding's hub)")
	}
	o.ctype, o.topic, o.category, o.xo, o.role = *ctype, *topic, *category, *xo, *role
	o.members, o.rosterPath, o.secretsPath = members, *rp, *sp
	return o, nil
}

func cmdChannelCreate(args []string) error {
	o, err := parseChannelCreateArgs(args)
	if err != nil {
		return err
	}
	// Precedence: roster/agents → bot token → guild_id → preflight → create, so the
	// first error a user sees is deterministic.
	cfg, err := roster.Load(o.rosterPath)
	if err != nil {
		return err
	}
	// Validate --xo/--member against the roster BEFORE any network call, so an emitted
	// binding can never name a non-existent agent (fail-closed, like the relay binding).
	if o.xo != "" {
		if _, err := cfg.Agent(o.xo); err != nil {
			return fmt.Errorf("--xo %q: %w", o.xo, err)
		}
		for _, m := range o.members {
			if _, err := cfg.Agent(m); err != nil {
				return fmt.Errorf("--member %q: %w", m, err)
			}
		}
	}
	token, err := botToken(o.secretsPath)
	if err != nil {
		return err
	}
	if cfg.GuildID == "" {
		return fmt.Errorf("roster has no guild_id — set it so the bot knows which guild to provision in")
	}
	prov, err := discord.NewProvisioner(token)
	if err != nil {
		return err
	}
	ctype := discord.ChannelTypeText
	if o.ctype == "category" {
		ctype = discord.ChannelTypeCategory
	}
	parentID := ""
	if o.category != "" {
		if ctype == discord.ChannelTypeCategory {
			return fmt.Errorf("a category cannot be nested under another category (--category places a text channel under a parent)")
		}
		parentID, err = prov.ResolveParentCategory(cfg.GuildID, o.category)
		if err != nil {
			return err
		}
	}
	if err := prov.Preflight(cfg.GuildID); err != nil {
		return err
	}
	ch, created, err := prov.Create(cfg.GuildID, discord.CreateSpec{
		Name:     o.name,
		Type:     ctype,
		Topic:    o.topic,
		ParentID: parentID,
	})
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("created #%s (%s)\n", ch.Name, ch.ID)
	} else {
		fmt.Printf("exists #%s (%s) — skipped (topic/parent NOT updated)\n", ch.Name, ch.ID)
	}
	if o.xo != "" {
		snippet, err := discord.BindingSnippet(ch.ID, o.xo, o.members, o.role)
		if err != nil {
			return err
		}
		fmt.Println("\nadd this to flotilla.json channels[]:")
		fmt.Println(snippet)
	}
	return nil
}

func cmdChannelList(args []string) error {
	fs := flag.NewFlagSet("channel list", flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	sp := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path")
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := roster.Load(*rp)
	if err != nil {
		return err
	}
	token, err := botToken(*sp)
	if err != nil {
		return err
	}
	if cfg.GuildID == "" {
		return fmt.Errorf("roster has no guild_id — set it so the bot knows which guild to read")
	}
	prov, err := discord.NewProvisioner(token)
	if err != nil {
		return err
	}
	chans, err := prov.List(cfg.GuildID)
	if err != nil {
		return err
	}
	if *asJSON {
		out, err := json.MarshalIndent(chans, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	for _, c := range chans {
		line := fmt.Sprintf("%-20s  %-8s  %s", c.ID, discord.ChannelTypeName(c.Type), c.Name)
		if c.ParentID != "" {
			line += fmt.Sprintf("  [parent %s]", c.ParentID)
		}
		fmt.Println(line)
	}
	return nil
}

// parseChannelDeleteArgs parses `channel delete <id> --yes`. delete takes the
// snowflake id ONLY (validated before any REST call, so a name typo cannot target a
// real channel) and REQUIRES --yes (the one destructive verb is never a one-keystroke
// fire). Pure, so it is unit-tested.
func parseChannelDeleteArgs(args []string) (id string, secretsPath string, err error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("channel delete", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm the (destructive) deletion")
	sp := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file path")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	rest := fs.Args()
	if id == "" && len(rest) >= 1 {
		id, rest = rest[0], rest[1:]
	}
	if id == "" || len(rest) != 0 {
		return "", "", fmt.Errorf("usage: flotilla channel delete <channel-id> --yes")
	}
	if !discord.IsSnowflake(id) {
		return "", "", fmt.Errorf("%q is not a channel id (a snowflake is all digits) — delete takes the id from `flotilla channel list`, never a name", id)
	}
	if !*yes {
		return "", "", fmt.Errorf("refusing to delete %s without --yes (delete is the one destructive verb)", id)
	}
	return id, *sp, nil
}

func cmdChannelDelete(args []string) error {
	id, secretsPath, err := parseChannelDeleteArgs(args)
	if err != nil {
		return err
	}
	token, err := botToken(secretsPath)
	if err != nil {
		return err
	}
	prov, err := discord.NewProvisioner(token)
	if err != nil {
		return err
	}
	if err := prov.Delete(id); err != nil {
		return err
	}
	fmt.Printf("deleted %s\n", id)
	return nil
}

// botToken loads the Discord bot token from the secrets file, with a clear error
// when the secrets path or the FLOTILLA_BOT_TOKEN key is missing. The token is
// returned for immediate use and never logged.
func botToken(secretsPath string) (string, error) {
	if secretsPath == "" {
		return "", fmt.Errorf("channel provisioning needs a bot token (set --secrets or $FLOTILLA_SECRETS, with FLOTILLA_BOT_TOKEN inside)")
	}
	secrets, err := roster.LoadSecrets(secretsPath)
	if err != nil {
		return "", err
	}
	t := secrets.BotToken()
	if t == "" {
		return "", fmt.Errorf("channel provisioning needs a bot token (set FLOTILLA_BOT_TOKEN in the secrets file)")
	}
	return t, nil
}
