package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
)

// fakeReader is a fake MessageReader: Latest + MessagesAfterPaged return canned
// values per channel. MessagesAfterPaged is one-shot per channel (returns the batch
// once, then empty) so a re-sweep does not re-deliver in tests.
type fakeReader struct {
	mu         sync.Mutex
	latest     map[string]transport.Message
	latestOK   map[string]bool
	paged      map[string][]transport.Message
	capped     map[string]bool
	err        error
	pagedCalls int
	latestErr  bool
}

func (f *fakeReader) Latest(ch string) (transport.Message, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil && f.latestErr {
		return transport.Message{}, false, f.err
	}
	return f.latest[ch], f.latestOK[ch], nil
}

func (f *fakeReader) MessagesAfterPaged(ch, after string, pl, pc int) ([]transport.Message, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pagedCalls++
	if f.err != nil {
		return nil, false, f.err
	}
	out := f.paged[ch]
	f.paged[ch] = nil // one-shot
	return out, f.capped[ch], nil
}

func opMsg(id uint64, ts time.Time) transport.Message {
	return transport.Message{ID: itoa(id), SnowID: id, AuthorID: "op", Content: "do thing " + itoa(id), Timestamp: ts}
}

// webhookMsg is the adversarial self-mirror case: a message carrying a non-empty
// WebhookID AND authored by the operator. The webhook drop must fire ahead of the
// operator-author Accept, so even an operator-authored self-mirror post is never
// re-relayed through REST history.
func webhookMsg(id uint64, ts time.Time) transport.Message {
	return transport.Message{ID: itoa(id), SnowID: id, WebhookID: "wh1", AuthorID: "op", Content: "mirror " + itoa(id), Timestamp: ts}
}

// catchupHarness wires a Catchup over a real Relay + Injector whose confirmed-delivery
// mirror records jobs, plus recording notify/alert sinks.
type catchupHarness struct {
	cu       *Catchup
	col      *collector
	inj      *Injector
	notifies *[]string
	alerts   *[]string
	mu       *sync.Mutex
}

func newCatchupHarness(t *testing.T, cfg *roster.Config, reader MessageReader) *catchupHarness {
	t.Helper()
	col := &collector{}
	inj := NewInjector(func(agent, msg string) error { return nil }, 16)
	inj.SetMirror(col.enqueue)
	inj.Start()
	t.Cleanup(inj.Stop)
	rel := NewRelay(cfg, inj, nil, nil)
	var mu sync.Mutex
	var notifies, alerts []string
	cu := NewCatchup(cfg, rel, reader, filepath.Join(t.TempDir(), "cursor.json"),
		func(s string) { mu.Lock(); notifies = append(notifies, s); mu.Unlock() },
		func(s string) { mu.Lock(); alerts = append(alerts, s); mu.Unlock() })
	return &catchupHarness{cu: cu, col: col, inj: inj, notifies: &notifies, alerts: &alerts, mu: &mu}
}

func (h *catchupHarness) alertCount() int { h.mu.Lock(); defer h.mu.Unlock(); return len(*h.alerts) }
func (h *catchupHarness) notifyCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(*h.notifies)
}

// waitForDelivered polls until the mirror has recorded want jobs (the injector
// worker delivers asynchronously), failing after a bounded timeout.
func waitForDelivered(t *testing.T, col *collector, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if col.count() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := col.count(); got != want {
		t.Fatalf("delivered %d jobs, want %d", got, want)
	}
}

func oneChannelCfg() *roster.Config {
	return &roster.Config{
		OperatorUserID: "op",
		ChannelID:      "C1",
		XOAgent:        "xo",
		Agents:         []roster.Agent{{Name: "xo"}},
	}
}

// NewCatchup must wire the dedup gate into the relay (so the live path dedups
// against the poller) — the central wiring invariant of the backstop.
func TestNewCatchup_WiresGateIntoRelay(t *testing.T) {
	inj := NewInjector(func(string, string) error { return nil }, 4)
	rel := NewRelay(oneChannelCfg(), inj, nil, nil)
	if rel.gate != nil {
		t.Fatal("relay gate should be nil before NewCatchup")
	}
	cu := NewCatchup(oneChannelCfg(), rel, &fakeReader{}, filepath.Join(t.TempDir(), "c.json"), nil, nil)
	if rel.gate == nil {
		t.Fatal("NewCatchup did not wire the gate into the relay")
	}
	if rel.gate != cu.gate {
		t.Fatal("relay and poller must share the SAME gate instance")
	}
}

func TestCatchup_FirstBootTailInit_RelaysNothing(t *testing.T) {
	r := &fakeReader{
		latest:   map[string]transport.Message{"C1": opMsg(500, time.Now())},
		latestOK: map[string]bool{"C1": true},
		paged:    map[string][]transport.Message{},
		capped:   map[string]bool{},
	}
	h := newCatchupHarness(t, oneChannelCfg(), r)
	h.cu.sweep()
	if r.pagedCalls != 0 {
		t.Fatalf("first boot should NOT page (tail-init only); pagedCalls=%d", r.pagedCalls)
	}
	if c, ok := h.cu.gate.cursorOf("C1"); !ok || c != 500 {
		t.Fatalf("cursor = %d ok=%v, want tail-init 500", c, ok)
	}
	time.Sleep(20 * time.Millisecond)
	if h.col.count() != 0 {
		t.Fatalf("first boot relayed %d, want 0 (no history replay)", h.col.count())
	}
}

func TestCatchup_RecoversFreshFewBatch(t *testing.T) {
	now := time.Now()
	r := &fakeReader{
		latest:   map[string]transport.Message{},
		latestOK: map[string]bool{},
		paged: map[string][]transport.Message{"C1": {
			opMsg(10, now.Add(-2*time.Second)), opMsg(20, now.Add(-1*time.Second)),
		}},
		capped: map[string]bool{},
	}
	h := newCatchupHarness(t, oneChannelCfg(), r)
	h.cu.gate.initCursor("C1", 5) // not first-boot: cursor already at 5
	h.cu.sweep()

	waitForDelivered(t, h.col, 2)
	if h.notifyCount() != 1 {
		t.Fatalf("notify count = %d, want 1 catch-up notice", h.notifyCount())
	}
	if h.alertCount() != 0 {
		t.Fatalf("alert count = %d, want 0 (fresh+few auto-relays)", h.alertCount())
	}
	if c, _ := h.cu.gate.cursorOf("C1"); c != 20 {
		t.Fatalf("cursor = %d, want 20 (advanced past the batch)", c)
	}
}

func TestCatchup_BulkBacklogAlertsNotRelays(t *testing.T) {
	now := time.Now()
	var batch []transport.Message
	for i := uint64(1); i <= 10; i++ { // 10 > bulkCap 5
		batch = append(batch, opMsg(i, now))
	}
	r := &fakeReader{paged: map[string][]transport.Message{"C1": batch}, capped: map[string]bool{}, latest: map[string]transport.Message{}, latestOK: map[string]bool{}}
	h := newCatchupHarness(t, oneChannelCfg(), r)
	h.cu.gate.initCursor("C1", 0)
	h.cu.sweep()

	if h.alertCount() != 1 {
		t.Fatalf("alert count = %d, want 1 (bulk backlog)", h.alertCount())
	}
	time.Sleep(20 * time.Millisecond)
	if h.col.count() != 0 {
		t.Fatalf("bulk backlog relayed %d, want 0 (alert, don't flood)", h.col.count())
	}
	// cursor still advances (the backlog was surfaced; don't re-alert every sweep)
	if c, _ := h.cu.gate.cursorOf("C1"); c != 10 {
		t.Fatalf("cursor = %d, want 10 (advances even on alert)", c)
	}
}

func TestCatchup_AncientBacklogAlerts(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour) // older than staleCeiling 24h
	r := &fakeReader{paged: map[string][]transport.Message{"C1": {opMsg(10, old), opMsg(20, old)}}, capped: map[string]bool{}, latest: map[string]transport.Message{}, latestOK: map[string]bool{}}
	h := newCatchupHarness(t, oneChannelCfg(), r)
	h.cu.gate.initCursor("C1", 5)
	h.cu.sweep()
	if h.alertCount() != 1 {
		t.Fatalf("alert count = %d, want 1 (ancient backlog)", h.alertCount())
	}
	time.Sleep(20 * time.Millisecond)
	if h.col.count() != 0 {
		t.Fatalf("ancient backlog relayed %d, want 0", h.col.count())
	}
}

func TestCatchup_CappedForcesAlert(t *testing.T) {
	now := time.Now()
	// only 2 recovered (under bulkCap) but capped=true → still alert (more remain above)
	r := &fakeReader{paged: map[string][]transport.Message{"C1": {opMsg(10, now), opMsg(20, now)}}, capped: map[string]bool{"C1": true}, latest: map[string]transport.Message{}, latestOK: map[string]bool{}}
	h := newCatchupHarness(t, oneChannelCfg(), r)
	h.cu.gate.initCursor("C1", 5)
	h.cu.sweep()
	if h.alertCount() != 1 {
		t.Fatalf("capped sweep alert count = %d, want 1", h.alertCount())
	}
}

func TestCatchup_NonOperatorMessagesAdvanceCursorButAreNotRelayed(t *testing.T) {
	now := time.Now()
	noise := transport.Message{ID: "30", SnowID: 30, AuthorID: "someone-else", Content: "chatter", Timestamp: now}
	r := &fakeReader{paged: map[string][]transport.Message{"C1": {opMsg(10, now), noise}}, capped: map[string]bool{}, latest: map[string]transport.Message{}, latestOK: map[string]bool{}}
	h := newCatchupHarness(t, oneChannelCfg(), r)
	h.cu.gate.initCursor("C1", 5)
	h.cu.sweep()
	waitForDelivered(t, h.col, 1) // only the operator message
	// cursor advances past the non-operator tail (30), else it re-fetches forever.
	if c, _ := h.cu.gate.cursorOf("C1"); c != 30 {
		t.Fatalf("cursor = %d, want 30 (advances over non-operator tail)", c)
	}
}

// TestCatchup_DropsSelfMirrorWebhookPost pins the SECOND arm of the self-mirror guard
// (the catch-up arm, accepted()'s inline webhook drop in catchup.go). The REST catch-up
// reads channel HISTORY, which includes the transport's OWN webhook audit-mirror posts;
// if the guard regressed here, those self-posts would re-enter the relay and reopen the
// feedback loop the live-path adapter guard closes. The fixture is the adversarial case:
// the webhook post is ALSO authored by the operator, so the drop cannot rely on the
// Accept author check — it must key on the webhook id alone. The post must NOT be
// relayed, while the cursor still advances OVER it (else it re-fetches forever).
func TestCatchup_DropsSelfMirrorWebhookPost(t *testing.T) {
	now := time.Now()
	r := &fakeReader{
		paged: map[string][]transport.Message{"C1": {
			webhookMsg(10, now), // the transport's own audit mirror (webhook set, author==operator)
			opMsg(20, now),      // a genuine operator message — must still be delivered
		}},
		capped:   map[string]bool{},
		latest:   map[string]transport.Message{},
		latestOK: map[string]bool{},
	}
	h := newCatchupHarness(t, oneChannelCfg(), r)
	h.cu.gate.initCursor("C1", 5)
	h.cu.sweep()

	// Exactly the genuine operator message is delivered; the webhook self-mirror is dropped.
	waitForDelivered(t, h.col, 1)
	time.Sleep(20 * time.Millisecond)
	if got := h.col.count(); got != 1 {
		t.Fatalf("delivered %d jobs, want 1 (the self-mirror webhook post must be dropped)", got)
	}
	// The cursor advances PAST the webhook post (20), else the dropped post is re-fetched
	// every sweep forever.
	if c, _ := h.cu.gate.cursorOf("C1"); c != 20 {
		t.Fatalf("cursor = %d, want 20 (advances over the dropped self-mirror post)", c)
	}
}

func TestCatchup_Kick_NonBlockingAndCoalesces(t *testing.T) {
	h := newCatchupHarness(t, oneChannelCfg(), &fakeReader{paged: map[string][]transport.Message{}, capped: map[string]bool{}, latest: map[string]transport.Message{}, latestOK: map[string]bool{}})
	// Kick must never block even when called repeatedly with no consumer.
	for i := 0; i < 5; i++ {
		h.cu.Kick()
	}
	// buffer is size 1 → exactly one pending kick, the rest coalesced.
	if got := len(h.cu.kick); got != 1 {
		t.Fatalf("pending kicks = %d, want 1 (coalesced)", got)
	}
}

func TestCatchup_LivenessEscalatesOnceThenReArms(t *testing.T) {
	r := &fakeReader{paged: map[string][]transport.Message{}, capped: map[string]bool{}, latest: map[string]transport.Message{"C1": opMsg(1, time.Now())}, latestOK: map[string]bool{"C1": true}}
	h := newCatchupHarness(t, oneChannelCfg(), r)
	h.cu.failThresh = 3

	// Make every sweep fail (REST error on the only channel = total outage).
	r.mu.Lock()
	r.err = errFake
	r.latestErr = true
	r.mu.Unlock()

	for i := 0; i < 5; i++ {
		h.cu.sweep()
	}
	if h.alertCount() != 1 {
		t.Fatalf("liveness alert count = %d after sustained failure, want exactly 1 (escalate once)", h.alertCount())
	}

	// Recover: a successful sweep re-arms.
	r.mu.Lock()
	r.err = nil
	r.latestErr = false
	r.mu.Unlock()
	h.cu.sweep() // success → re-arm

	r.mu.Lock()
	r.err = errFake
	r.latestErr = true
	r.mu.Unlock()
	for i := 0; i < 3; i++ {
		h.cu.sweep()
	}
	if h.alertCount() != 2 {
		t.Fatalf("liveness alert count = %d, want 2 (re-armed and escalated again)", h.alertCount())
	}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "fake REST outage" }
