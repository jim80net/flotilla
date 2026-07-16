// Package gmailbroker implements the read-only Gmail Authorization Domain.
package gmailbroker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/authdomain"
)

const (
	CredentialEnv = "FLOTILLA_PA_GMAIL_OAUTH_FILE"
	GrantID       = "pa-gmail-readonly"
	Scope         = "https://www.googleapis.com/auth/gmail.readonly"
	apiBase       = "https://gmail.googleapis.com/gmail/v1/users/me"
	tokenURL      = "https://oauth2.googleapis.com/token"
)

type fileSystem interface {
	Lstat(string) (fs.FileInfo, error)
	ReadFile(string) ([]byte, error)
	EUID() int
}
type osFS struct{}

func (osFS) Lstat(p string) (fs.FileInfo, error) { return os.Lstat(p) }
func (osFS) ReadFile(p string) ([]byte, error)   { return os.ReadFile(p) }
func (osFS) EUID() int                           { return os.Geteuid() }

type AuditEvent struct {
	At                                                   time.Time
	Principal, GrantID, Action, Resource, Result, Reason string
}
type AuditSink interface{ RecordGmail(AuditEvent) error }

type Config struct {
	Grants                                      *authdomain.Set
	GrantAudit                                  authdomain.AuditSink
	Audit                                       AuditSink
	Principal, ApprovedAccount, AccountResource string
	HTTP                                        *http.Client
	Now                                         func() time.Time
	LookupEnv                                   func(string) (string, bool)
	fs                                          fileSystem
}
type Connector struct {
	cfg    Config
	mu     sync.Mutex
	access string
	expiry time.Time
	smoke  bool
}

func New(cfg Config) (*Connector, error) {
	if cfg.Grants == nil || cfg.GrantAudit == nil || cfg.Audit == nil || cfg.Principal != "pa" || cfg.ApprovedAccount == "" || cfg.AccountResource == "" {
		return nil, errors.New("gmail broker: exact PA principal, grant set, audits, and approved account are required")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.LookupEnv == nil {
		cfg.LookupEnv = os.LookupEnv
	}
	if cfg.fs == nil {
		cfg.fs = osFS{}
	}
	return &Connector{cfg: cfg}, nil
}

func (c *Connector) ListLabels(ctx context.Context, label string) (json.RawMessage, error) {
	return c.get(ctx, "gmail.labels.list", "/labels", "labels", label)
}
func (c *Connector) GetLabel(ctx context.Context, id, label string) (json.RawMessage, error) {
	return c.getID(ctx, "gmail.labels.get", "/labels/", id, "label", label)
}
func (c *Connector) ListMessages(ctx context.Context, label string) (json.RawMessage, error) {
	p := "/messages"
	if label != "" {
		p += "?labelIds=" + url.QueryEscape(label)
	}
	return c.get(ctx, "gmail.messages.list", p, "messages", label)
}
func (c *Connector) GetMessage(ctx context.Context, id, label string) (json.RawMessage, error) {
	return c.getID(ctx, "gmail.messages.get", "/messages/", id, "message", label)
}
func (c *Connector) ListThreads(ctx context.Context, label string) (json.RawMessage, error) {
	p := "/threads"
	if label != "" {
		p += "?labelIds=" + url.QueryEscape(label)
	}
	return c.get(ctx, "gmail.threads.list", p, "threads", label)
}
func (c *Connector) GetThread(ctx context.Context, id, label string) (json.RawMessage, error) {
	return c.getID(ctx, "gmail.threads.get", "/threads/", id, "thread", label)
}

// Execute rejects every operation outside the ratified read allowlist.
func (c *Connector) Execute(context.Context, string) (json.RawMessage, error) {
	return nil, errors.New("gmail broker: operation not allowed")
}
func (c *Connector) getID(ctx context.Context, action, prefix, id, resource, label string) (json.RawMessage, error) {
	if !safeID(id) {
		return nil, errors.New("gmail broker: invalid resource id")
	}
	return c.get(ctx, action, prefix+url.PathEscape(id), resource, label)
}
func safeID(s string) bool {
	return s != "" && !strings.ContainsAny(s, "/\\?#") && s != "." && s != ".."
}

func (c *Connector) get(ctx context.Context, action, path, resource, label string) (out json.RawMessage, err error) {
	now := c.cfg.Now()
	req := authdomain.Request{Desk: c.cfg.Principal, Capability: "gmail.api", Action: action, Scope: Scope, Account: c.cfg.AccountResource, Label: label}
	auth, aerr := c.cfg.Grants.Authorize(req, now, c.cfg.GrantAudit)
	d := auth.Decision()
	if aerr != nil || !d.Allowed || d.GrantID != GrantID {
		return nil, errors.New("gmail broker: grant not found")
	}
	if err := c.audit(now, d, action, resource, "authorized", ""); err != nil {
		return nil, errors.New("gmail broker: audit unavailable")
	}
	b := &binding{c: c}
	if err = auth.BindSecret(b); err != nil {
		return nil, errors.New("gmail broker: credential binding unavailable")
	}
	tok, err := c.token(ctx, b.user, now)
	if err != nil {
		c.audit(now, d, action, resource, "denied", "credential")
		return nil, err
	}
	h, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	h.Header.Set("Authorization", "Bearer "+tok)
	resp, e := c.cfg.HTTP.Do(h)
	if e != nil {
		c.audit(now, d, action, resource, "failed", "provider")
		return nil, errors.New("gmail broker: provider request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, resp.Body)
		c.audit(now, d, action, resource, "failed", "provider")
		return nil, errors.New("gmail broker: provider request refused")
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if e != nil || !json.Valid(body) {
		return nil, errors.New("gmail broker: invalid provider response")
	}
	c.audit(now, d, action, resource, "allowed", "")
	return body, nil
}
func (c *Connector) audit(at time.Time, d authdomain.Decision, action, resource, result, reason string) error {
	return c.cfg.Audit.RecordGmail(AuditEvent{At: at, Principal: c.cfg.Principal, GrantID: d.GrantID, Action: action, Resource: resource, Result: result, Reason: reason})
}

type authorizedUser struct {
	Type           string   `json:"type"`
	ClientID       string   `json:"client_id"`
	ClientSecret   string   `json:"client_secret"`
	RefreshToken   string   `json:"refresh_token"`
	TokenURI       string   `json:"token_uri"`
	Token          string   `json:"token,omitempty"`
	Scopes         []string `json:"scopes"`
	UniverseDomain string   `json:"universe_domain,omitempty"`
	Account        string   `json:"account,omitempty"`
	Expiry         string   `json:"expiry,omitempty"`
}
type binding struct {
	c    *Connector
	user authorizedUser
}

func (b *binding) LookupSecretRef(ref string) error {
	if ref != "pa-gmail-oauth" {
		return errors.New("gmail broker: unexpected credential binding")
	}
	p, ok := b.c.cfg.LookupEnv(CredentialEnv)
	if !ok || p == "" {
		return errors.New("gmail broker: credential binding unavailable")
	}
	i, e := b.c.cfg.fs.Lstat(p)
	if e != nil {
		return errors.New("gmail broker: credential file unavailable")
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !i.Mode().IsRegular() || i.Mode().Perm() != 0600 || !ok || int(st.Uid) != b.c.cfg.fs.EUID() {
		return errors.New("gmail broker: credential file security check failed")
	}
	raw, e := b.c.cfg.fs.ReadFile(p)
	if e != nil {
		return errors.New("gmail broker: credential file unreadable")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&b.user) != nil || dec.Decode(&struct{}{}) != io.EOF || b.user.Type != "authorized_user" || b.user.ClientID == "" || b.user.ClientSecret == "" || b.user.RefreshToken == "" || b.user.TokenURI != tokenURL || len(b.user.Scopes) != 1 || b.user.Scopes[0] != Scope {
		return errors.New("gmail broker: invalid authorized-user credential")
	}
	return nil
}
func (c *Connector) token(ctx context.Context, u authorizedUser, now time.Time) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.access != "" && now.Add(time.Minute).Before(c.expiry) {
		return c.access, nil
	}
	v := url.Values{"client_id": {u.ClientID}, "client_secret": {u.ClientSecret}, "refresh_token": {u.RefreshToken}, "grant_type": {"refresh_token"}}
	h, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(v.Encode()))
	h.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := c.cfg.HTTP.Do(h)
	if e != nil {
		return "", errors.New("gmail broker: token refresh failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		io.Copy(io.Discard, resp.Body)
		return "", errors.New("gmail broker: token refresh refused")
	}
	var tr struct {
		Access  string      `json:"access_token"`
		Expires json.Number `json:"expires_in"`
		Scope   string      `json:"scope"`
	}
	d := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	d.UseNumber()
	if d.Decode(&tr) != nil || tr.Access == "" || strings.TrimSpace(tr.Scope) != Scope {
		return "", errors.New("gmail broker: invalid token response")
	}
	secs, e := strconv.ParseInt(string(tr.Expires), 10, 64)
	if e != nil || secs <= 0 {
		return "", errors.New("gmail broker: invalid token lifetime")
	}
	c.access = tr.Access
	c.expiry = now.Add(time.Duration(secs) * time.Second)
	if !c.smoke {
		if e = c.profileAndLabels(ctx); e != nil {
			c.access = ""
			return "", e
		}
		c.smoke = true
	}
	return c.access, nil
}
func (c *Connector) profileAndLabels(ctx context.Context) error {
	for _, p := range []string{"/profile", "/labels"} {
		h, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+p, nil)
		h.Header.Set("Authorization", "Bearer "+c.access)
		r, e := c.cfg.HTTP.Do(h)
		if e != nil {
			return errors.New("gmail broker: account smoke check failed")
		}
		if r.StatusCode/100 != 2 {
			r.Body.Close()
			return errors.New("gmail broker: account smoke check refused")
		}
		if p == "/profile" {
			var v struct {
				Email string `json:"emailAddress"`
			}
			e = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&v)
			r.Body.Close()
			if e != nil || v.Email != c.cfg.ApprovedAccount {
				return errors.New("gmail broker: approved account mismatch")
			}
		} else {
			io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
			r.Body.Close()
		}
	}
	return nil
}
