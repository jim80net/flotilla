package gmailbroker

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/authdomain"
	"github.com/jim80net/flotilla/internal/roster"
)

type audit struct {
	grants []authdomain.AuditEvent
	gmail  []AuditEvent
}

func (a *audit) Record(e authdomain.AuditEvent) error { a.grants = append(a.grants, e); return nil }
func (a *audit) RecordGmail(e AuditEvent) error       { a.gmail = append(a.gmail, e); return nil }

type info struct {
	mode fs.FileMode
	uid  uint32
}

func (info) Name() string        { return "oauth.json" }
func (info) Size() int64         { return 1 }
func (i info) Mode() fs.FileMode { return i.mode }
func (info) ModTime() time.Time  { return time.Time{} }
func (info) IsDir() bool         { return false }
func (i info) Sys() any          { return &syscall.Stat_t{Uid: i.uid} }

type fakeFS struct {
	data        []byte
	mode        fs.FileMode
	uid, owner  int
	lstat, read int
	err         error
}

func (f *fakeFS) Lstat(string) (fs.FileInfo, error) {
	f.lstat++
	if f.err != nil {
		return nil, f.err
	}
	owner := f.owner
	if owner == 0 {
		owner = f.uid
	}
	return info{f.mode, uint32(owner)}, nil
}
func (f *fakeFS) ReadFile(string) ([]byte, error) { f.read++; return f.data, nil }
func (f *fakeFS) EUID() int                       { return f.uid }

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func grants(t *testing.T) *authdomain.Set {
	t.Helper()
	d := t.TempDir()
	rp := filepath.Join(d, "r.json")
	raw := `{"xo_agent":"xo","agents":[{"name":"xo","coordinator":true},{"name":"pa","coordinator":false},{"name":"other","coordinator":false}],"channel_id":"c"}`
	if err := os.WriteFile(rp, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, e := roster.Load(rp)
	if e != nil {
		t.Fatal(e)
	}
	g := `schema: 1
id: pa-gmail-readonly
principal: {kind: desk, name: pa}
capability: gmail.api
oauth_scopes: [https://www.googleapis.com/auth/gmail.readonly]
actions: [gmail.messages.list, gmail.messages.get, gmail.threads.list, gmail.threads.get, gmail.labels.list, gmail.labels.get]
resources: {accounts: [operator-primary], labels: []}
secret_ref: pa-gmail-oauth
approval: {send: deny, modify: deny}
audit: {mode: metadata-only, retain: P30D}
`
	s, e := authdomain.Load(cfg, []byte(g))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func credential() []byte {
	return []byte(`{"type":"authorized_user","client_id":"client","client_secret":"secret","refresh_token":"refresh","token_uri":"https://oauth2.googleapis.com/token","scopes":["https://www.googleapis.com/auth/gmail.readonly"]}`)
}
func connector(t *testing.T, principal string, f *fakeFS, a *audit, calls *[]string) *Connector {
	t.Helper()
	h := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		*calls = append(*calls, r.Method+" "+r.URL.String())
		switch r.URL.Path {
		case "/token":
			return response(200, `{"access_token":"access-secret","expires_in":3600,"scope":"https://www.googleapis.com/auth/gmail.readonly"}`), nil
		case "/gmail/v1/users/me/profile":
			return response(200, `{"emailAddress":"approved@example.invalid"}`), nil
		default:
			return response(200, `{"ok":true}`), nil
		}
	})}
	c, e := New(Config{Grants: grants(t), GrantAudit: a, Audit: a, Principal: principal, ApprovedAccount: "approved@example.invalid", AccountResource: "operator-primary", HTTP: h, Now: func() time.Time { return time.Unix(1, 0) }, LookupEnv: func(k string) (string, bool) {
		if k != CredentialEnv {
			t.Fatalf("env=%s", k)
		}
		return "/private/oauth.json", true
	}, fs: f})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestReadOnlySmokeAndAllowlist(t *testing.T) {
	f := &fakeFS{data: credential(), mode: 0600, uid: 123}
	a := &audit{}
	var calls []string
	c := connector(t, "pa", f, a, &calls)
	if _, e := c.ListLabels(context.Background(), ""); e != nil {
		t.Fatal(e)
	}
	if _, e := c.GetMessage(context.Background(), "m1", ""); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 5 {
		t.Fatalf("calls=%v", calls)
	}
	if calls[0] != "POST https://oauth2.googleapis.com/token" || !strings.Contains(strings.Join(calls, "\n"), "/profile") || !strings.Contains(strings.Join(calls, "\n"), "/labels") {
		t.Fatalf("smoke calls=%v", calls)
	}
	if f.lstat != 2 || f.read != 2 {
		t.Fatalf("file checks=%d/%d", f.lstat, f.read)
	}
	if _, e := c.Execute(context.Background(), "gmail.messages.send"); e == nil {
		t.Fatal("send allowed")
	}
	if _, e := c.GetMessage(context.Background(), "../token", ""); e == nil {
		t.Fatal("arbitrary path allowed")
	}
	dump := strings.Join([]string{a.gmail[0].Principal, a.gmail[0].GrantID, a.gmail[0].Action, a.gmail[0].Resource, a.gmail[0].Result, a.gmail[0].Reason}, " ")
	for _, secret := range []string{"access-secret", "approved@example.invalid", "refresh", "client_secret"} {
		if strings.Contains(dump, secret) {
			t.Fatalf("audit leaked %q: %s", secret, dump)
		}
	}
}

func TestCrossSeatRefusedBeforeEnvironmentFileOrHTTP(t *testing.T) {
	f := &fakeFS{data: credential(), mode: 0600, uid: 1}
	a := &audit{}
	env := 0
	httpCalls := 0
	_, e := New(Config{Grants: grants(t), GrantAudit: a, Audit: a, Principal: "other", ApprovedAccount: "x", AccountResource: "operator-primary", LookupEnv: func(string) (string, bool) { env++; return "", false }, fs: f, HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { httpCalls++; return nil, errors.New("called") })}})
	if e == nil {
		t.Fatal("cross-seat connector accepted")
	}
	if env+f.lstat+f.read+httpCalls != 0 {
		t.Fatalf("side effects env=%d fs=%d/%d http=%d", env, f.lstat, f.read, httpCalls)
	}
}

func TestCredentialSecurityAndStrictJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode fs.FileMode
		data []byte
	}{
		{"symlink", fs.ModeSymlink | 0777, credential()}, {"mode", 0644, credential()}, {"unknown json", 0600, []byte(`{"type":"authorized_user","client_id":"c","client_secret":"s","refresh_token":"r","token_uri":"https://oauth2.googleapis.com/token","scopes":["https://www.googleapis.com/auth/gmail.readonly"],"extra":true}`)}, {"broad scope", 0600, []byte(`{"type":"authorized_user","client_id":"c","client_secret":"s","refresh_token":"r","token_uri":"https://oauth2.googleapis.com/token","scopes":["https://mail.google.com/"]}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeFS{data: tc.data, mode: tc.mode, uid: 1}
			a := &audit{}
			var calls []string
			c := connector(t, "pa", f, a, &calls)
			if _, e := c.ListLabels(context.Background(), ""); e == nil {
				t.Fatal("unsafe credential accepted")
			}
			if len(calls) != 0 {
				t.Fatalf("HTTP called: %v", calls)
			}
		})
	}
}

func TestResourceDenialBeforeEnvironmentFileOrHTTP(t *testing.T) {
	f := &fakeFS{data: credential(), mode: 0600, uid: 1}
	a := &audit{}
	env, httpCalls := 0, 0
	c, e := New(Config{Grants: grants(t), GrantAudit: a, Audit: a, Principal: "pa", ApprovedAccount: "approved@example.invalid", AccountResource: "ungranted-account", LookupEnv: func(string) (string, bool) { env++; return "/x", true }, fs: f, HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { httpCalls++; return nil, errors.New("called") })}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.ListLabels(context.Background(), ""); e == nil {
		t.Fatal("ungranted resource allowed")
	}
	if env+f.lstat+f.read+httpCalls != 0 {
		t.Fatalf("side effects env=%d fs=%d/%d http=%d", env, f.lstat, f.read, httpCalls)
	}
}

func TestWrongOwnerRefused(t *testing.T) {
	f := &fakeFS{data: credential(), mode: 0600, uid: 1, owner: 2}
	a := &audit{}
	var calls []string
	c := connector(t, "pa", f, a, &calls)
	if _, e := c.ListLabels(context.Background(), ""); e == nil {
		t.Fatal("wrong owner accepted")
	}
	if len(calls) != 0 {
		t.Fatalf("HTTP called: %v", calls)
	}
}

func TestEnforceLabelSelectors(t *testing.T) {
	if _, e := enforceLabel("gmail.messages.get", "allowed", []byte(`{"labelIds":["other"]}`)); e == nil {
		t.Fatal("wrong message label allowed")
	}
	got, e := enforceLabel("gmail.labels.list", "allowed", []byte(`{"labels":[{"id":"other","name":"private"},{"id":"allowed","name":"safe"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(got), "private") || !strings.Contains(string(got), "safe") {
		t.Fatalf("filtered labels=%s", got)
	}
	got, e = enforceLabel("gmail.threads.get", "allowed", []byte(`{"messages":[{"id":"one","labelIds":["allowed"]},{"id":"two","labelIds":["other"]}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(got), `"two"`) || !strings.Contains(string(got), `"one"`) {
		t.Fatalf("filtered thread=%s", got)
	}
}

func TestRefreshErrorsAreSanitized(t *testing.T) {
	f := &fakeFS{data: credential(), mode: 0600, uid: 1}
	a := &audit{}
	c, e := New(Config{Grants: grants(t), GrantAudit: a, Audit: a, Principal: "pa", ApprovedAccount: "approved@example.invalid", AccountResource: "operator-primary", HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return response(400, "refresh-token-secret"), nil })}, LookupEnv: func(string) (string, bool) { return "/x", true }, fs: f})
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.ListLabels(context.Background(), "")
	if e == nil || strings.Contains(e.Error(), "refresh-token-secret") {
		t.Fatalf("error=%v", e)
	}
}
