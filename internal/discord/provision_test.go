package discord

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// fakeAPI is an in-memory guildAPI for testing the provisioner policy.
type fakeAPI struct {
	botID      string
	botRoles   []string
	info       *guildInfo
	existing   []Channel
	created    []CreateSpec
	createRet  Channel
	createErr  error
	deleteErr  error
	listErr    error
	guildErr   error
	selfErr    error
	deletedIDs []string
	hooks      []Webhook
}

func (f *fakeAPI) webhooks(string) ([]Webhook, error) { return f.hooks, nil }
func (f *fakeAPI) createWebhook(channelID, name string) (Webhook, error) {
	h := Webhook{ID: "hookid", Name: name, ChannelID: channelID, URL: "https://discord.com/api/webhooks/hookid/token"}
	f.hooks = append(f.hooks, h)
	return h, nil
}

func (f *fakeAPI) editChannel(id string, in CreateSpec) (Channel, error) {
	for i := range f.existing {
		if f.existing[i].ID == id {
			f.existing[i].ParentID = in.ParentID
			return f.existing[i], nil
		}
	}
	return Channel{}, &apiError{status: 404}
}

func (f *fakeAPI) selfMember(string) (string, []string, error) {
	if f.selfErr != nil {
		return "", nil, f.selfErr
	}
	return f.botID, f.botRoles, nil
}
func (f *fakeAPI) guild(string) (*guildInfo, error) {
	if f.guildErr != nil {
		return nil, f.guildErr
	}
	return f.info, nil
}
func (f *fakeAPI) channels(string) ([]Channel, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.existing, nil
}
func (f *fakeAPI) createChannel(_ string, in CreateSpec) (Channel, error) {
	if f.createErr != nil {
		return Channel{}, f.createErr
	}
	f.created = append(f.created, in)
	if f.createRet.ID != "" {
		return f.createRet, nil
	}
	// default: echo back a created channel with a synthesized id and retain it,
	// like Discord, so multi-step/idempotency tests see prior creates.
	id := "newid"
	if len(f.created) > 1 {
		id = fmt.Sprintf("newid%d", len(f.created))
	}
	ch := Channel{ID: id, Name: in.Name, Type: in.Type, ParentID: in.ParentID}
	f.existing = append(f.existing, ch)
	return ch, nil
}
func (f *fakeAPI) deleteChannel(id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}

const guildID = "100"

func TestProvisionOrgStackDualPlacementAndIdempotency(t *testing.T) {
	f := &fakeAPI{botID: "bot", info: &guildInfo{rolePerms: map[string]int64{guildID: discordgo.PermissionManageChannels}}}
	p := &Provisioner{api: f}
	stack, err := p.ProvisionOrgStack(guildID, "Canary Fleet", "canary-xo", "canary", "canary-xo")
	if err != nil {
		t.Fatal(err)
	}
	if stack.C2.ParentID != stack.COS.ID {
		t.Fatalf("C2 parent = %q, want COS %q", stack.C2.ParentID, stack.COS.ID)
	}
	if stack.Product.ParentID != stack.Category.ID {
		t.Fatalf("product parent = %q, want flotilla category %q", stack.Product.ParentID, stack.Category.ID)
	}
	if !stack.Created["webhook"] || stack.XO.URL == "" {
		t.Fatalf("webhook not created: %+v", stack)
	}
	created := len(f.created)
	second, err := p.ProvisionOrgStack(guildID, "Canary Fleet", "canary-xo", "canary", "canary-xo")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.created) != created || second.Created["category"] || second.Created["c2"] || second.Created["product"] || second.Created["webhook"] {
		t.Fatalf("second provision was not idempotent: created=%d→%d flags=%v", created, len(f.created), second.Created)
	}
}

func TestMoveReparentsAndSkipsWhenAlreadyPlaced(t *testing.T) {
	f := &fakeAPI{existing: []Channel{{ID: "10", Name: "orphan", Type: ChannelTypeText}, {ID: "20", Name: "Fleet", Type: ChannelTypeCategory}}}
	p := &Provisioner{api: f}
	ch, moved, err := p.Move(guildID, "10", "20")
	if err != nil || !moved || ch.ParentID != "20" {
		t.Fatalf("first move = (%+v,%v,%v)", ch, moved, err)
	}
	_, moved, err = p.Move(guildID, "10", "20")
	if err != nil || moved {
		t.Fatalf("second move = moved %v, err %v", moved, err)
	}
}

func TestEffectivePermissions(t *testing.T) {
	everyoneNone := &guildInfo{ownerID: "owner", rolePerms: map[string]int64{guildID: 0}}
	tests := []struct {
		name   string
		info   *guildInfo
		botID  string
		roles  []string
		manage bool
	}{
		{
			name:   "owner short-circuits to all",
			info:   &guildInfo{ownerID: "bot", rolePerms: map[string]int64{guildID: 0}},
			botID:  "bot",
			manage: true,
		},
		{
			name:   "administrator role short-circuits even without explicit manage",
			info:   &guildInfo{ownerID: "owner", rolePerms: map[string]int64{guildID: 0, "admin": discordgo.PermissionAdministrator}},
			botID:  "bot",
			roles:  []string{"admin"},
			manage: true,
		},
		{
			name:   "@everyone grants manage channels",
			info:   &guildInfo{ownerID: "owner", rolePerms: map[string]int64{guildID: discordgo.PermissionManageChannels}},
			botID:  "bot",
			manage: true,
		},
		{
			name:   "a named role grants manage channels",
			info:   &guildInfo{ownerID: "owner", rolePerms: map[string]int64{guildID: 0, "mods": discordgo.PermissionManageChannels}},
			botID:  "bot",
			roles:  []string{"mods"},
			manage: true,
		},
		{
			name:   "no grant -> no manage",
			info:   everyoneNone,
			botID:  "bot",
			manage: false,
		},
		{
			name:   "unknown role id contributes nothing",
			info:   everyoneNone,
			botID:  "bot",
			roles:  []string{"ghost"},
			manage: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hasManageChannels(effectivePermissions(tc.info, guildID, tc.botID, tc.roles))
			if got != tc.manage {
				t.Fatalf("hasManageChannels = %v, want %v", got, tc.manage)
			}
		})
	}
}

func TestChannelNameKey(t *testing.T) {
	tests := []struct {
		name, in string
		ctype    int
		want     string
	}{
		{"text lowercases", "Fleet-Command", ChannelTypeText, "fleet-command"},
		{"text spaces to hyphens", "Fleet Command", ChannelTypeText, "fleet-command"},
		{"text trims edge hyphens", " Fleet ", ChannelTypeText, "fleet"},
		{"text does NOT fold underscore (no over-match)", "team_a", ChannelTypeText, "team_a"},
		{"category case-insensitive, space-preserving", "Family Office", ChannelTypeCategory, "family office"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := channelNameKey(tc.in, tc.ctype); got != tc.want {
				t.Fatalf("key(%q,%d) = %q, want %q", tc.in, tc.ctype, got, tc.want)
			}
		})
	}
	// the over-match guard: team_a and team-a must NOT share a key
	if channelNameKey("team_a", ChannelTypeText) == channelNameKey("team-a", ChannelTypeText) {
		t.Fatal("team_a and team-a must not collide (over-normalization would falsely skip)")
	}
}

func TestFindExisting(t *testing.T) {
	chans := []Channel{
		{ID: "1", Name: "fleet-command", Type: ChannelTypeText, ParentID: ""},
		{ID: "2", Name: "notes", Type: ChannelTypeText, ParentID: "catA"},
		{ID: "3", Name: "Family Office", Type: ChannelTypeCategory, ParentID: ""},
	}
	cases := []struct {
		name      string
		reqName   string
		ctype     int
		parent    string
		wantID    string
		wantFound bool
	}{
		{"normalized name match", "Fleet Command", ChannelTypeText, "", "1", true},
		{"type mismatch (category vs text)", "fleet-command", ChannelTypeCategory, "", "", false},
		{"parent mismatch", "notes", ChannelTypeText, "", "", false},
		{"same name different parent", "notes", ChannelTypeText, "catA", "2", true},
		{"category case-insensitive", "family office", ChannelTypeCategory, "", "3", true},
		{"absent", "nope", ChannelTypeText, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := findExisting(chans, tc.reqName, tc.ctype, tc.parent)
			if ok != tc.wantFound || got.ID != tc.wantID {
				t.Fatalf("findExisting = (%q,%v), want (%q,%v)", got.ID, ok, tc.wantID, tc.wantFound)
			}
		})
	}
}

func TestPreflight(t *testing.T) {
	t.Run("passes with manage channels", func(t *testing.T) {
		p := &Provisioner{api: &fakeAPI{
			botID: "bot",
			info:  &guildInfo{ownerID: "owner", rolePerms: map[string]int64{guildID: discordgo.PermissionManageChannels}},
		}}
		if err := p.Preflight(guildID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("fails clearly without manage channels", func(t *testing.T) {
		p := &Provisioner{api: &fakeAPI{
			botID: "bot",
			info:  &guildInfo{ownerID: "owner", rolePerms: map[string]int64{guildID: 0}},
		}}
		err := p.Preflight(guildID)
		if err == nil || !strings.Contains(err.Error(), "Manage Channels") {
			t.Fatalf("want a Manage Channels error, got %v", err)
		}
	})
}

func TestCreateIdempotent(t *testing.T) {
	t.Run("creates when absent", func(t *testing.T) {
		f := &fakeAPI{}
		p := &Provisioner{api: f}
		ch, created, err := p.Create(guildID, CreateSpec{Name: "fleet-command", Type: ChannelTypeText})
		if err != nil || !created || ch.ID != "newid" {
			t.Fatalf("Create = (%+v,%v,%v)", ch, created, err)
		}
		if len(f.created) != 1 {
			t.Fatalf("expected 1 create call, got %d", len(f.created))
		}
	})
	t.Run("skips when present (no create call)", func(t *testing.T) {
		f := &fakeAPI{existing: []Channel{{ID: "9", Name: "fleet-command", Type: ChannelTypeText}}}
		p := &Provisioner{api: f}
		ch, created, err := p.Create(guildID, CreateSpec{Name: "Fleet Command", Type: ChannelTypeText})
		if err != nil || created || ch.ID != "9" {
			t.Fatalf("Create = (%+v,%v,%v), want skip of id 9", ch, created, err)
		}
		if len(f.created) != 0 {
			t.Fatalf("expected no create call on skip, got %d", len(f.created))
		}
	})
	t.Run("empty id in success is an error", func(t *testing.T) {
		// A 2xx response with no channel id must be rejected, not reported as created.
		p := &Provisioner{api: &emptyIDAPI{}}
		_, _, err := p.Create(guildID, CreateSpec{Name: "x", Type: ChannelTypeText})
		if err == nil || !strings.Contains(err.Error(), "no id") {
			t.Fatalf("want no-id error, got %v", err)
		}
	})
}

// emptyIDAPI always returns a created channel with an empty id.
type emptyIDAPI struct{ fakeAPI }

func (e *emptyIDAPI) createChannel(string, CreateSpec) (Channel, error) {
	return Channel{ID: ""}, nil
}
func (e *emptyIDAPI) editChannel(string, CreateSpec) (Channel, error) { return Channel{}, nil }
func (e *emptyIDAPI) webhooks(string) ([]Webhook, error)              { return nil, nil }
func (e *emptyIDAPI) createWebhook(string, string) (Webhook, error)   { return Webhook{}, nil }

func TestCreate403Backstop(t *testing.T) {
	f := &fakeAPI{createErr: &apiError{status: 403, msg: "Missing Permissions"}}
	p := &Provisioner{api: f}
	_, _, err := p.Create(guildID, CreateSpec{Name: "x", Type: ChannelTypeText})
	if err == nil || !strings.Contains(err.Error(), "Manage Channels") || !strings.Contains(err.Error(), "overwrites") {
		t.Fatalf("want manage-channels-at-create error, got %v", err)
	}
}

func TestCreateRateLimitSurfaces(t *testing.T) {
	f := &fakeAPI{createErr: &rateLimitError{retryAfter: 2 * time.Second}}
	p := &Provisioner{api: f}
	_, _, err := p.Create(guildID, CreateSpec{Name: "x", Type: ChannelTypeText})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("want rate-limit error, got %v", err)
	}
}

func TestDelete404(t *testing.T) {
	f := &fakeAPI{deleteErr: &apiError{status: 404, msg: "Unknown Channel"}}
	p := &Provisioner{api: f}
	err := p.Delete("123")
	if err == nil || !strings.Contains(err.Error(), "no channel with id 123") {
		t.Fatalf("want clear 404 error, got %v", err)
	}
}

func TestDeleteHappyPath(t *testing.T) {
	f := &fakeAPI{}
	p := &Provisioner{api: f}
	if err := p.Delete("123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.deletedIDs) != 1 || f.deletedIDs[0] != "123" {
		t.Fatalf("expected delete of 123, got %v", f.deletedIDs)
	}
}

func TestCreate400Passthrough(t *testing.T) {
	// A 400 (e.g. the 50-per-category / 500-per-guild limit) surfaces Discord's own
	// message verbatim — not swallowed, not mistaken for a permission error.
	f := &fakeAPI{createErr: &apiError{status: 400, msg: "Maximum number of channels reached (500)"}}
	p := &Provisioner{api: f}
	_, _, err := p.Create(guildID, CreateSpec{Name: "x", Type: ChannelTypeText})
	if err == nil || !strings.Contains(err.Error(), "Maximum number of channels") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("want the 400 message surfaced, got %v", err)
	}
}

func TestList(t *testing.T) {
	want := []Channel{{ID: "1", Name: "a", Type: ChannelTypeText}}
	p := &Provisioner{api: &fakeAPI{existing: want}}
	got, err := p.List(guildID)
	if err != nil || len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("List = (%v,%v)", got, err)
	}
}

func TestChannelTypeName(t *testing.T) {
	cases := map[int]string{
		ChannelTypeText:     "text",
		ChannelTypeCategory: "category",
		2:                   "voice",
		99:                  "type99",
	}
	for in, want := range cases {
		if got := ChannelTypeName(in); got != want {
			t.Fatalf("ChannelTypeName(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveParentCategory(t *testing.T) {
	chans := []Channel{
		{ID: "201", Name: "Family Office", Type: ChannelTypeCategory},
		{ID: "202", Name: "Family Office", Type: ChannelTypeCategory}, // duplicate name
		{ID: "203", Name: "Flotilla", Type: ChannelTypeCategory},
		{ID: "301", Name: "general", Type: ChannelTypeText},
	}
	t.Run("by id", func(t *testing.T) {
		p := &Provisioner{api: &fakeAPI{existing: chans}}
		got, err := p.ResolveParentCategory(guildID, "203")
		if err != nil || got != "203" {
			t.Fatalf("got (%q,%v)", got, err)
		}
	})
	t.Run("by unique name", func(t *testing.T) {
		p := &Provisioner{api: &fakeAPI{existing: chans}}
		got, err := p.ResolveParentCategory(guildID, "Flotilla")
		if err != nil || got != "203" {
			t.Fatalf("got (%q,%v)", got, err)
		}
	})
	t.Run("ambiguous name is an error", func(t *testing.T) {
		p := &Provisioner{api: &fakeAPI{existing: chans}}
		_, err := p.ResolveParentCategory(guildID, "Family Office")
		if err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("want ambiguous error, got %v", err)
		}
	})
	t.Run("not found is an error", func(t *testing.T) {
		p := &Provisioner{api: &fakeAPI{existing: chans}}
		_, err := p.ResolveParentCategory(guildID, "Nope")
		if err == nil || !strings.Contains(err.Error(), "no category") {
			t.Fatalf("want not-found error, got %v", err)
		}
	})
	t.Run("id that is not a category is an error", func(t *testing.T) {
		p := &Provisioner{api: &fakeAPI{existing: chans}}
		_, err := p.ResolveParentCategory(guildID, "301")
		if err == nil {
			t.Fatalf("want error for a non-category id")
		}
	})
}

func TestBindingSnippet(t *testing.T) {
	t.Run("full binding", func(t *testing.T) {
		out, err := BindingSnippet("123", "alpha-xo", []string{"d1", "d2"}, "project")
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("not valid JSON: %v\n%s", err, out)
		}
		if got["channel_id"] != "123" || got["xo_agent"] != "alpha-xo" || got["role"] != "project" {
			t.Fatalf("wrong fields: %v", got)
		}
		if ms, ok := got["members"].([]any); !ok || len(ms) != 2 {
			t.Fatalf("members wrong: %v", got["members"])
		}
	})
	t.Run("empty members and role are omitted", func(t *testing.T) {
		out, err := BindingSnippet("123", "alpha-xo", nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "members") || strings.Contains(out, "role") {
			t.Fatalf("empty members/role should be omitted:\n%s", out)
		}
	})
}

func TestIsSnowflake(t *testing.T) {
	for _, s := range []string{"123", "000", "123456789012345678"} {
		if !IsSnowflake(s) {
			t.Fatalf("%q should be a snowflake", s)
		}
	}
	for _, s := range []string{"", "abc", "12a", "fleet-command", "12 3"} {
		if IsSnowflake(s) {
			t.Fatalf("%q should NOT be a snowflake", s)
		}
	}
}

// TestMapErrSecretDiscipline is the security-critical test: a discordgo RESTError
// (whose embedded Request carries the bot token) must be reduced to a credential-free
// flotilla error — neither the token/Authorization string NOR the *RESTError itself
// (the %+v struct-dump leak vector) may survive into the returned error chain.
func TestMapErrSecretDiscipline(t *testing.T) {
	const token = "Bot super-secret-token-value"
	req, _ := http.NewRequest("POST", "https://discord.com/api/v9/guilds/100/channels", nil)
	req.Header.Set("Authorization", token)
	restErr := &discordgo.RESTError{
		Request:      req,
		Response:     &http.Response{StatusCode: 403, Status: "403 Forbidden"},
		ResponseBody: []byte(`{"message":"Missing Permissions","code":50013}`),
		Message:      &discordgo.APIErrorMessage{Code: 50013, Message: "Missing Permissions"},
	}
	got := mapErr(restErr)

	// (a) status + message preserved, as an *apiError
	var ae *apiError
	if !errors.As(got, &ae) || ae.status != 403 {
		t.Fatalf("want *apiError status 403, got %T %v", got, got)
	}
	// (b) NO *discordgo.RESTError anywhere in the returned chain
	var leaked *discordgo.RESTError
	if errors.As(got, &leaked) {
		t.Fatal("a *discordgo.RESTError leaked into the returned chain (its embedded request carries the token)")
	}
	// (c) the rendered string carries neither the token nor an Authorization header
	if s := got.Error(); strings.Contains(s, "secret") || strings.Contains(s, "Authorization") || strings.Contains(s, token) {
		t.Fatalf("error string leaks a credential: %q", s)
	}
}

func TestMapErrRateLimit(t *testing.T) {
	rl := &discordgo.RateLimitError{RateLimit: &discordgo.RateLimit{TooManyRequests: &discordgo.TooManyRequests{RetryAfter: 3 * time.Second}}}
	got := mapErr(rl)
	var rle *rateLimitError
	if !errors.As(got, &rle) {
		t.Fatalf("want *rateLimitError, got %T %v", got, got)
	}
}

func TestMapErrPassthrough(t *testing.T) {
	got := mapErr(errors.New("Exceeded Max retries HTTP 502 Bad Gateway"))
	if got == nil || !strings.Contains(got.Error(), "discord:") || !strings.Contains(got.Error(), "502") {
		t.Fatalf("want passthrough with discord: prefix, got %v", got)
	}
	if mapErr(nil) != nil {
		t.Fatal("mapErr(nil) must be nil")
	}
}
