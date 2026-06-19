package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
	"github.com/jim80net/flotilla/internal/roster"
)

// fixedTime keeps ledger entries deterministic.
var fixedTime = time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC)

// newTestController builds a LibraryController over an in-memory roster + a temp
// secrets file, with the discord/cos seams captured for assertions.
func newTestController(t *testing.T, rosterBody, secretsBody string) (*LibraryController, *capture) {
	t.Helper()
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatalf("roster.Load: %v", err)
	}
	secretsPath := ""
	if secretsBody != "" {
		secretsPath = filepath.Join(dir, "secrets.env")
		if err := os.WriteFile(secretsPath, []byte(secretsBody), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	xo := rc.XOAgent
	if xo == "" {
		xo = rc.Agents[0].Name
	}
	c := NewLibrary(rc, xo, secretsPath)
	cap := &capture{}
	c.post = cap.post
	c.appendCos = cap.append
	c.now = func() time.Time { return fixedTime }
	return c, cap
}

type capture struct {
	postWebhook, postUser, postContent string
	postErr                            error
	postCalls                          int
	ledger                             []cos.Entry
	ledgerErr                          error
}

func (c *capture) post(webhook, username, content string) error {
	c.postCalls++
	c.postWebhook, c.postUser, c.postContent = webhook, username, content
	return c.postErr
}

func (c *capture) append(_ string, e cos.Entry) error {
	c.ledger = append(c.ledger, e)
	return c.ledgerErr
}

const rosterCos = `{
	"channel_id": "C1",
	"xo_agent": "xo",
	"cos_agent": "xo",
	"heartbeat_interval": "20m",
	"agents": [{"name": "xo"}, {"name": "alpha"}]
}`

const secretsXO = "FLOTILLA_WEBHOOK_XO=https://discord.example/webhook/xo\n"

func TestNotify_HappyPath_PostsAndMirrors(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	if err := c.Notify(context.Background(), "fleet, stand by for a deploy"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if cap.postCalls != 1 || cap.postWebhook != "https://discord.example/webhook/xo" {
		t.Errorf("post = %d calls, webhook %q", cap.postCalls, cap.postWebhook)
	}
	if cap.postUser != dashProvenance {
		t.Errorf("post username = %q, want %q", cap.postUser, dashProvenance)
	}
	if cap.postContent != "fleet, stand by for a deploy" {
		t.Errorf("post content = %q", cap.postContent)
	}
	// CoS mirror with dash provenance.
	if len(cap.ledger) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(cap.ledger))
	}
	e := cap.ledger[0]
	if e.From != dashProvenance || e.To != "xo" || e.Gist != "fleet, stand by for a deploy" {
		t.Errorf("ledger entry = %+v", e)
	}
}

func TestNotify_EmptyMessage(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	if err := c.Notify(context.Background(), "   "); !errors.Is(err, ErrEmptyMessage) {
		t.Errorf("err = %v, want ErrEmptyMessage", err)
	}
	if cap.postCalls != 0 {
		t.Error("empty message must not post")
	}
}

func TestNotify_OverLength(t *testing.T) {
	c, _ := newTestController(t, rosterCos, secretsXO)
	long := make([]byte, 2001)
	for i := range long {
		long[i] = 'x'
	}
	if err := c.Notify(context.Background(), string(long)); !errors.Is(err, ErrOverLength) {
		t.Errorf("err = %v, want ErrOverLength", err)
	}
}

func TestNotify_NoSecretsConfigured(t *testing.T) {
	c, _ := newTestController(t, rosterCos, "") // no secrets file
	if err := c.Notify(context.Background(), "hi"); !errors.Is(err, ErrWebhookMissing) {
		t.Errorf("err = %v, want ErrWebhookMissing", err)
	}
}

func TestNotify_WebhookMissingForXO(t *testing.T) {
	// Secrets file exists but has no webhook for the XO.
	c, _ := newTestController(t, rosterCos, "FLOTILLA_WEBHOOK_OTHER=https://x\n")
	if err := c.Notify(context.Background(), "hi"); !errors.Is(err, ErrWebhookMissing) {
		t.Errorf("err = %v, want ErrWebhookMissing", err)
	}
}

func TestNotify_PostFailureSurfaced(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	cap.postErr = errors.New("discord 500")
	if err := c.Notify(context.Background(), "hi"); err == nil {
		t.Fatal("a post failure must be surfaced")
	}
	// A failed post must NOT mirror to the ledger (the post didn't happen).
	if len(cap.ledger) != 0 {
		t.Error("no ledger entry on post failure")
	}
}

func TestNotify_LedgerFailureIsBestEffort(t *testing.T) {
	c, cap := newTestController(t, rosterCos, secretsXO)
	cap.ledgerErr = errors.New("ledger write failed")
	// The post succeeded; a ledger failure must NOT fail the notify.
	if err := c.Notify(context.Background(), "hi"); err != nil {
		t.Errorf("ledger failure must not fail notify, got %v", err)
	}
}

func TestNotify_NoCosLedgerSkipsMirror(t *testing.T) {
	// Roster without cos_agent ⇒ CosLedger inert ⇒ no mirror, notify still posts.
	rosterNoCos := `{"channel_id":"C1","xo_agent":"xo","heartbeat_interval":"20m","agents":[{"name":"xo"}]}`
	c, cap := newTestController(t, rosterNoCos, secretsXO)
	if err := c.Notify(context.Background(), "hi"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if cap.postCalls != 1 {
		t.Error("notify should still post with an inert CoS")
	}
	if len(cap.ledger) != 0 {
		t.Error("inert CoS ⇒ no ledger mirror")
	}
}

// --- pane-driving verbs fail closed until the cross-process lock lands ---

func TestRoute_FailsClosedPendingLock(t *testing.T) {
	c, _ := newTestController(t, rosterCos, secretsXO)
	if _, err := c.Route(context.Background(), "alpha", "do X"); !errors.Is(err, ErrControlUnavailable) {
		t.Errorf("Route err = %v, want ErrControlUnavailable", err)
	}
}

func TestResume_FailsClosedPendingLock(t *testing.T) {
	c, _ := newTestController(t, rosterCos, secretsXO)
	if _, err := c.Resume(context.Background(), "alpha"); !errors.Is(err, ErrControlUnavailable) {
		t.Errorf("Resume err = %v, want ErrControlUnavailable", err)
	}
}
