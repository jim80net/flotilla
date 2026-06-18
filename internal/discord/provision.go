package discord

// Channel provisioning: create/list/delete Discord channels via the bot token,
// the mechanical complement to the federation routing in internal/roster. These
// are one-shot REST calls (no gateway is opened — see NewProvisioner), structurally
// like `flotilla notify`, not the long-lived `flotilla watch` relay.
//
// All Discord I/O goes through the guildAPI seam so the POLICY (permission
// preflight, idempotency, error taxonomy, binding emission) is unit-tested with a
// fake; only the thin discordgoAPI adapter at the bottom talks to discordgo and is
// the one part exercised live rather than in tests.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/jim80net/flotilla/internal/roster"
)

// Channel type values flotilla provisions. They are tied to discordgo's wire
// constants so they cannot drift from the library; the seam exposes them as plain
// ints so no discordgo type leaks into the policy layer.
const (
	ChannelTypeText     = int(discordgo.ChannelTypeGuildText)     // 0
	ChannelTypeCategory = int(discordgo.ChannelTypeGuildCategory) // 4
)

// Channel is a Discord channel as flotilla sees it — the provisioned OBJECT, not a
// roster.Channel binding. Type holds Discord's numeric channel type.
type Channel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     int    `json:"type"`
	ParentID string `json:"parent_id,omitempty"`
}

// CreateSpec describes a channel to create.
type CreateSpec struct {
	Name     string
	Type     int
	Topic    string
	ParentID string
}

// guildInfo is the slice of guild facts the permission preflight needs.
type guildInfo struct {
	ownerID   string
	rolePerms map[string]int64 // role id -> permission bits
}

// guildAPI is the Discord REST surface provisioning needs. The real implementation
// (discordgoAPI) wraps *discordgo.Session; tests inject a fake. It is the seam that
// keeps discordgo types — and the credential-bearing *discordgo.RESTError — out of
// the policy layer.
type guildAPI interface {
	// selfMember returns the bot's own user id and its role ids in the guild, via two
	// canonical bot-token routes (GET /users/@me then GET /guilds/{id}/members/{id}).
	selfMember(guildID string) (botUserID string, roleIDs []string, err error)
	guild(guildID string) (*guildInfo, error)
	channels(guildID string) ([]Channel, error)
	createChannel(guildID string, in CreateSpec) (Channel, error)
	deleteChannel(channelID string) error
}

// Provisioner creates/lists/deletes Discord channels for the configured guild.
type Provisioner struct{ api guildAPI }

// NewProvisioner builds a provisioner over a real discordgo session authenticated
// with the bot token. It does NOT call Open() — every method used is a plain REST
// call, so no gateway/websocket is established, and the default LogError level means
// the token is never written to a log.
//
// It disables discordgo's ShouldRetryOnRateLimit: that auto-retry is UNBOUNDED on a
// sustained 429 (restapi.go:295 re-issues with the SAME sequence — MaxRestRetries
// only caps the 5xx path), so a global rate limit during a squadron burst would hang
// the command indefinitely with no output. With it off, a 429 surfaces immediately as
// a clear rate-limit error (see mapErr) and the operator re-runs — which is safe
// because Create is idempotent (already-created channels skip).
func NewProvisioner(botToken string) (*Provisioner, error) {
	sess, err := discordgo.New("Bot " + botToken)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	sess.ShouldRetryOnRateLimit = false
	return &Provisioner{api: &discordgoAPI{sess: sess}}, nil
}

// Preflight fails with a clear, actionable error when the bot lacks Manage Channels
// in the guild — checked BEFORE any create, so a misconfigured bot fails fast rather
// than half-way through a multi-channel stand-up. A create-time 403 is the backstop
// (see Create) for the cases a guild-level check cannot see (category overwrites).
func (p *Provisioner) Preflight(guildID string) error {
	botID, roleIDs, err := p.api.selfMember(guildID)
	if err != nil {
		// A 404 here means the bot is not a member of guild_id (or guild_id is wrong) —
		// the most common misconfiguration; say so rather than leaving a bare HTTP 404.
		var ae *apiError
		if errors.As(err, &ae) && ae.status == 404 {
			return fmt.Errorf("bot is not a member of guild %s (invite the bot to that guild, and check the roster guild_id): %w", guildID, err)
		}
		return err
	}
	g, err := p.api.guild(guildID)
	if err != nil {
		return err
	}
	if !hasManageChannels(effectivePermissions(g, guildID, botID, roleIDs)) {
		return errors.New(manageChannelsMsg(guildID))
	}
	return nil
}

// Create provisions a channel idempotently: it lists the guild, and if a channel of
// the same type with a matching (normalized) name already exists under the same
// parent it SKIPS (created=false) reporting that channel; otherwise it creates one.
// The created channel's id is read from Discord's returned object (the authoritative
// confirmation), never inferred from text. Idempotency is for sequential single-actor
// use — concurrent creates can still race (Discord enforces no name uniqueness).
func (p *Provisioner) Create(guildID string, spec CreateSpec) (Channel, bool, error) {
	existing, err := p.api.channels(guildID)
	if err != nil {
		return Channel{}, false, err
	}
	if c, ok := findExisting(existing, spec.Name, spec.Type, spec.ParentID); ok {
		return c, false, nil
	}
	ch, err := p.api.createChannel(guildID, spec)
	if err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.status == 403 {
			return Channel{}, false, fmt.Errorf("%s (denied at create — check the category's/channel's permission overwrites)", manageChannelsMsg(guildID))
		}
		return Channel{}, false, err
	}
	if ch.ID == "" {
		return Channel{}, false, errors.New("discord: created channel returned no id")
	}
	return ch, true, nil
}

// List returns the guild's channels.
func (p *Provisioner) List(guildID string) ([]Channel, error) { return p.api.channels(guildID) }

// Delete removes a channel by id. A 404 (well-formed id, no such channel) is reported
// as a clear error rather than a silent success.
func (p *Provisioner) Delete(channelID string) error {
	err := p.api.deleteChannel(channelID)
	var ae *apiError
	if errors.As(err, &ae) && ae.status == 404 {
		return fmt.Errorf("no channel with id %s (already deleted, or wrong id)", channelID)
	}
	return err
}

// ResolveParentCategory turns a --category reference (a snowflake id, or a category
// name) into a category channel id. A name is matched among category-type channels
// via the same normalization key as idempotency; ambiguity (two categories share the
// name) and not-found are both clear, fail-closed errors.
func (p *Provisioner) ResolveParentCategory(guildID, ref string) (string, error) {
	chans, err := p.api.channels(guildID)
	if err != nil {
		return "", err
	}
	if IsSnowflake(ref) {
		for _, c := range chans {
			if c.ID == ref && c.Type == ChannelTypeCategory {
				return c.ID, nil
			}
		}
		return "", fmt.Errorf("no category with id %s in the guild", ref)
	}
	want := channelNameKey(ref, ChannelTypeCategory)
	var matches []Channel
	for _, c := range chans {
		if c.Type == ChannelTypeCategory && channelNameKey(c.Name, ChannelTypeCategory) == want {
			matches = append(matches, c)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no category named %q (create it first, or pass its snowflake id)", ref)
	case 1:
		return matches[0].ID, nil
	default:
		return "", fmt.Errorf("category name %q is ambiguous (%d categories match) — pass the snowflake id instead", ref, len(matches))
	}
}

// BindingSnippet renders the F#105 roster.Channel binding for a freshly-created
// channel as paste-ready indented JSON (empty members/role are omitted, per the
// roster omitempty shape). The created channel's real id is filled in so wiring
// routing is copy-one-block.
func BindingSnippet(channelID, xo string, members []string, role string) (string, error) {
	b := roster.Channel{ChannelID: channelID, XOAgent: xo, Members: members, Role: role}
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render binding: %w", err)
	}
	return string(out), nil
}

// ChannelTypeName is a short human label for a Discord channel type (for `list`).
func ChannelTypeName(t int) string {
	switch t {
	case ChannelTypeText:
		return "text"
	case ChannelTypeCategory:
		return "category"
	case int(discordgo.ChannelTypeGuildVoice):
		return "voice"
	default:
		return fmt.Sprintf("type%d", t)
	}
}

// IsSnowflake reports whether s is a Discord snowflake (a non-empty all-digit id).
// Used to gate delete (id-only, never a name) and to decide --category id-vs-name.
func IsSnowflake(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// --- pure policy helpers (no I/O — table-tested directly) ---

// effectivePermissions computes the bot's guild-level (base) permission bits. It
// MIRRORS discordgo's own unexported memberPermissions (restapi.go:514) — re-check
// it against discordgo on any library upgrade — and uses discordgo's EXPORTED
// permission constants so the bit math cannot drift. The @everyone grant is the role
// whose id equals the guild id (the documented Discord invariant); owner and
// Administrator both short-circuit to all permissions.
func effectivePermissions(g *guildInfo, guildID, botID string, botRoles []string) int64 {
	if botID != "" && botID == g.ownerID {
		return discordgo.PermissionAll
	}
	perms := g.rolePerms[guildID] // @everyone (role id == guild id)
	for _, rid := range botRoles {
		perms |= g.rolePerms[rid] // unknown role ids contribute 0
	}
	if perms&discordgo.PermissionAdministrator != 0 {
		return discordgo.PermissionAll
	}
	return perms
}

func hasManageChannels(perms int64) bool {
	return perms&discordgo.PermissionManageChannels != 0
}

func manageChannelsMsg(guildID string) string {
	return fmt.Sprintf("bot lacks Manage Channels in guild %s — grant it in Server Settings → Roles → the bot's role → Manage Channels", guildID)
}

// channelNameKey is the idempotency comparison key. It applies ONLY Discord's
// documented normalization (text channels are lowercased and spaces become hyphens;
// categories keep spacing but are case-insensitive) — deliberately no speculative
// transforms (e.g. underscore folding), because over-normalizing is the dangerous
// direction (it would falsely skip a distinct channel), while under-normalizing at
// worst yields a visible duplicate the operator catches via `list`.
func channelNameKey(name string, ctype int) string {
	if ctype == ChannelTypeText {
		k := strings.ToLower(name)
		k = strings.ReplaceAll(k, " ", "-")
		return strings.Trim(k, "-")
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// findExisting returns a channel matching name+type+parent (using the normalization
// key) — the idempotency lookup.
func findExisting(chans []Channel, name string, ctype int, parentID string) (Channel, bool) {
	want := channelNameKey(name, ctype)
	for _, c := range chans {
		if c.Type == ctype && c.ParentID == parentID && channelNameKey(c.Name, ctype) == want {
			return c, true
		}
	}
	return Channel{}, false
}

// --- error taxonomy (flotilla-owned, credential-free) ---

// apiError is a flotilla-owned REST error: a status code + Discord's API message.
// The adapter constructs it from a *discordgo.RESTError and DISCARDS the RESTError,
// because that struct retains the original *http.Request whose header carries the
// bot token — so it must never travel past the seam (a %+v of it would leak the
// token). Mirrors urlFreeCause in discord.go, which drops *url.Error for the same
// reason.
type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string {
	if e.msg == "" {
		return fmt.Sprintf("discord: HTTP %d", e.status)
	}
	return fmt.Sprintf("discord: HTTP %d: %s", e.status, e.msg)
}

// rateLimitError is a flotilla-owned 429. discordgo returns *RateLimitError (NOT a
// *RESTError) and by default transparently retries; this surfaces only when a limit
// reaches the caller anyway, so it is never an opaque/unhandled error.
type rateLimitError struct{ retryAfter time.Duration }

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("discord: rate limited, retry after %s", e.retryAfter)
}

// mapErr converts a discordgo error into a flotilla-owned, credential-free error. It
// is the security-critical seam translation (pure — no I/O — so it is unit-tested):
//   - *discordgo.RESTError  → *apiError (status + Discord's message); the RESTError,
//     with its embedded credential-bearing request, is dropped entirely.
//   - *discordgo.RateLimitError → *rateLimitError.
//   - anything else (502-after-max-retries, dial/network errors) carries no request
//     and is passed through verbatim under a "discord:" prefix.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var re *discordgo.RESTError
	if errors.As(err, &re) {
		status := 0
		if re.Response != nil {
			status = re.Response.StatusCode
		}
		msg := ""
		if re.Message != nil {
			msg = strings.TrimSpace(re.Message.Message)
		}
		if msg == "" {
			// Discord's error body is JSON describing the failure — it carries no
			// credentials (those live in the request header, which we drop).
			msg = strings.TrimSpace(string(re.ResponseBody))
		}
		return &apiError{status: status, msg: msg}
	}
	var rl *discordgo.RateLimitError
	if errors.As(err, &rl) {
		return &rateLimitError{retryAfter: rl.RetryAfter}
	}
	return fmt.Errorf("discord: %w", err)
}

// --- the live adapter (the only unit not unit-tested; exercised live) ---

type discordgoAPI struct{ sess *discordgo.Session }

func (a *discordgoAPI) selfMember(guildID string) (string, []string, error) {
	// Two canonical bot-token routes: GET /users/@me for the bot's id, then
	// GET /guilds/{id}/members/{id} with the REAL snowflake. We deliberately do NOT
	// use GuildMember(guildID, "@me") — /guilds/{id}/members/@me is not a route we can
	// confirm against canonical sources, whereas /users/@me is unambiguous.
	me, err := a.sess.User("@me")
	if err != nil {
		return "", nil, mapErr(err)
	}
	if me == nil || me.ID == "" {
		return "", nil, errors.New("discord: could not resolve the bot's own user id")
	}
	m, err := a.sess.GuildMember(guildID, me.ID)
	if err != nil {
		return "", nil, mapErr(err)
	}
	if m == nil {
		return "", nil, errors.New("discord: guild member lookup returned no member")
	}
	return me.ID, m.Roles, nil
}

func (a *discordgoAPI) guild(guildID string) (*guildInfo, error) {
	g, err := a.sess.Guild(guildID)
	if err != nil {
		return nil, mapErr(err)
	}
	if g == nil {
		// discordgo's Guild() does not guard a null body (it unmarshals and returns);
		// a (nil,nil) would panic on g.Roles below.
		return nil, errors.New("discord: guild lookup returned no guild")
	}
	rp := make(map[string]int64, len(g.Roles))
	for _, r := range g.Roles {
		rp[r.ID] = r.Permissions
	}
	return &guildInfo{ownerID: g.OwnerID, rolePerms: rp}, nil
}

func (a *discordgoAPI) channels(guildID string) ([]Channel, error) {
	cs, err := a.sess.GuildChannels(guildID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Channel, 0, len(cs))
	for _, c := range cs {
		out = append(out, Channel{ID: c.ID, Name: c.Name, Type: int(c.Type), ParentID: c.ParentID})
	}
	return out, nil
}

func (a *discordgoAPI) createChannel(guildID string, in CreateSpec) (Channel, error) {
	c, err := a.sess.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:     in.Name,
		Type:     discordgo.ChannelType(in.Type),
		Topic:    in.Topic,
		ParentID: in.ParentID,
	})
	if err != nil {
		return Channel{}, mapErr(err)
	}
	if c == nil {
		// discordgo's GuildChannelCreateComplex unmarshals and returns without a nil
		// guard; a null 2xx body would otherwise panic at c.ID. (Create also rejects an
		// empty id, covering the non-nil-but-empty case.)
		return Channel{}, errors.New("discord: channel create returned no channel")
	}
	return Channel{ID: c.ID, Name: c.Name, Type: int(c.Type), ParentID: c.ParentID}, nil
}

func (a *discordgoAPI) deleteChannel(channelID string) error {
	if _, err := a.sess.ChannelDelete(channelID); err != nil {
		return mapErr(err)
	}
	return nil
}
