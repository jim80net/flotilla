package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/readermap"
)

// baseDeps builds replyDeps with instant sleep + recorders; the caller overrides the fields it drives.
func baseDeps() (*replyDeps, *[]string, *[]string) {
	var posts, alerts []string
	d := replyDeps{
		dest:     func(string) (string, bool) { return "https://wh/origin", true },
		post:     func(url, user, content string) error { posts = append(posts, user+"|"+content); return nil },
		escalate: func(originChannel, msg string) { alerts = append(alerts, msg) },
		sleep:    func(time.Duration) {},
		logf:     func(string, ...any) {},
		softTTL:  2 * time.Second,
		hardTTL:  5 * time.Second,
		interval: 1 * time.Second,
	}
	return &d, &posts, &alerts
}

type replyResult struct {
	text  string
	found bool
	err   error
}

// replySeq returns a reply func yielding the given results on successive calls; the LAST repeats.
// Guarded by a mutex so concurrent watchers (during a supersede overlap) are race-clean under -race.
func replySeq(seq ...replyResult) func(agent, operatorMsg string) (string, bool, error) {
	var mu sync.Mutex
	i := 0
	return func(string, string) (string, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		s := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return s.text, s.found, s.err
	}
}

func TestReplyWatch_RoutesWhenReplyFound(t *testing.T) {
	d, posts, alerts := baseDeps()
	// not-yet, not-yet, then the verbatim reply lands.
	d.reply = replySeq(replyResult{"", false, nil}, replyResult{"", false, nil}, replyResult{"here is what I need from you", true, nil})
	runReplyWatch(context.Background(), *d, "xo", "chan123", "what do you need")
	if len(*alerts) != 0 {
		t.Fatalf("unexpected alerts: %v", *alerts)
	}
	if len(*posts) != 1 || !strings.Contains((*posts)[0], "here is what I need") || !strings.HasPrefix((*posts)[0], "xo|") {
		t.Fatalf("posts = %v, want one attributed verbatim reply", *posts)
	}
}

// A reply that lands AFTER the soft TTL still routes (and the soft escalation fired once meanwhile).
func TestReplyWatch_SoftTTLThenLateReplyStillRoutes(t *testing.T) {
	d, posts, alerts := baseDeps()
	// soft TTL = 3 polls; reply lands on the 4th.
	d.reply = replySeq(replyResult{"", false, nil}, replyResult{"", false, nil}, replyResult{"", false, nil}, replyResult{"late but real reply", true, nil})
	runReplyWatch(context.Background(), *d, "xo", "chan123", "q")
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "is working on your message") {
		t.Fatalf("alerts = %v, want exactly one soft 'still working' escalation", *alerts)
	}
	if len(*posts) != 1 || !strings.Contains((*posts)[0], "late but real reply") {
		t.Fatalf("posts = %v, want the late reply still routed", *posts)
	}
}

func TestReplyWatch_HardTTL_Escalates(t *testing.T) {
	d, posts, alerts := baseDeps()
	d.reply = replySeq(replyResult{"", false, nil}) // never lands
	runReplyWatch(context.Background(), *d, "xo", "chan123", "q")
	if len(*posts) != 0 {
		t.Fatalf("must not post when no reply arrives: %v", *posts)
	}
	// one soft 'still working' + one hard 'has not answered'.
	if len(*alerts) != 2 || !strings.Contains((*alerts)[1], "has not answered") {
		t.Fatalf("alerts = %v, want soft then a hard not-answered escalation", *alerts)
	}
}

func TestReplyWatch_WebhookUnresolved_Escalates(t *testing.T) {
	d, posts, alerts := baseDeps()
	d.reply = replySeq(replyResult{"reply", true, nil})
	d.dest = func(string) (string, bool) { return "", false }
	runReplyWatch(context.Background(), *d, "xo", "chan123", "q")
	if len(*posts) != 0 {
		t.Fatalf("must not post with no destination: %v", *posts)
	}
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "can't route") {
		t.Fatalf("alerts = %v, want a no-webhook escalation", *alerts)
	}
}

func TestReplyWatch_PostError_EscalatesPartial(t *testing.T) {
	d, _, alerts := baseDeps()
	d.reply = replySeq(replyResult{"reply", true, nil})
	d.post = func(string, string, string) error { return context.Canceled }
	runReplyWatch(context.Background(), *d, "xo", "chan123", "q")
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "read its pane for the rest") {
		t.Fatalf("alerts = %v, want a partial-delivery escalation", *alerts)
	}
}

func TestReplyWatch_SupersededBeforeRoute_NoStalePost(t *testing.T) {
	d, posts, alerts := baseDeps()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already superseded
	d.reply = replySeq(replyResult{"reply", true, nil})
	runReplyWatch(ctx, *d, "xo", "chan123", "q")
	if len(*posts) != 0 || len(*alerts) != 0 {
		t.Fatalf("a superseded watcher must emit nothing: posts=%v alerts=%v", *posts, *alerts)
	}
}

func TestReplyWatch_MultiChunk_OrderedPosts(t *testing.T) {
	d, posts, alerts := baseDeps()
	big := strings.Repeat("x", mirrorChunkLimit+50) // forces 2 chunks
	d.reply = replySeq(replyResult{big, true, nil})
	runReplyWatch(context.Background(), *d, "xo", "chan123", "q")
	if len(*alerts) != 0 {
		t.Fatalf("unexpected alerts: %v", *alerts)
	}
	if len(*posts) != 2 || !strings.Contains((*posts)[0], "reply 1/2") || !strings.Contains((*posts)[1], "reply 2/2") {
		t.Fatalf("posts = %d chunks, want 2 ordered (1/2, 2/2)", len(*posts))
	}
}

// arm supersedes the prior watcher for the same XO (re-anchors to the new origin channel); a
// superseded watcher must not route to the old channel. Deterministic: the reply is keyed on the
// operator message — msg1 (chanA) NEVER lands, so chanA's watcher can only exit via the supersede
// cancel (it never routes); msg2 (chanB) lands, so the surviving route targets chanB.
func TestReplyRouter_ArmSupersedes(t *testing.T) {
	var mu sync.Mutex
	var routed []string
	d := replyDeps{
		reply: func(agent, operatorMsg string) (string, bool, error) {
			if operatorMsg == "msg2" {
				return "the chanB reply", true, nil
			}
			return "", false, nil // msg1 (chanA) never lands — chanA's watcher only exits via cancel
		},
		dest: func(ch string) (string, bool) { return "wh-" + ch, true },
		post: func(url, user, content string) error {
			mu.Lock()
			routed = append(routed, url)
			mu.Unlock()
			return nil
		},
		escalate: func(string, string) {},
		sleep:    func(time.Duration) {},
		logf:     func(string, ...any) {},
		softTTL:  2 * time.Second,
		hardTTL:  3 * time.Second,
		interval: 1 * time.Second,
	}
	r := newReplyRouter(context.Background(), d)
	done := make(chan struct{}, 2)
	r.dispatch = func(f func()) { go func() { f(); done <- struct{}{} }() }
	r.arm("xo", "chanA", "msg1")
	r.arm("xo", "chanB", "msg2") // supersede → chanA's watcher cancels
	<-done
	<-done
	mu.Lock()
	defer mu.Unlock()
	for _, u := range routed {
		if u == "wh-chanA" {
			t.Fatalf("a superseded watcher routed to the old channel chanA: %v", routed)
		}
	}
}

// The firewall gates the DAEMON reply egress: a leaking reply is SUPPRESSED (never
// routed to the operator's channel) and ESCALATED (read-the-pane), not bounced.
func TestRoute_FirewallRefuseSuppressesAndEscalates(t *testing.T) {
	var posts, escalations []string
	fw, err := readermap.NewTermSet([]string{"acme-desk"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := replyDeps{
		dest:     func(string) (string, bool) { return "wh", true },
		post:     func(url, _, content string) error { posts = append(posts, content); return nil },
		escalate: func(_, msg string) { escalations = append(escalations, msg) },
		logf:     func(string, ...any) {},
		firewall: fw,
	}
	// A deployment denylist term in the XO's reply (a placeholder, not a real codename).
	d.route(context.Background(), "xo", "chanA", "the acme-desk reported in")
	if len(posts) != 0 {
		t.Fatalf("a firewall REFUSE must SUPPRESS the route (no post); got posts=%v", posts)
	}
	if len(escalations) != 1 || !strings.Contains(escalations[0], "WITHHELD") {
		t.Fatalf("a refused reply must ESCALATE (read-the-pane); got escalations=%v", escalations)
	}
}

// A warnlist (advisory) hit routes the reply anyway, with an advisory escalation.
func TestRoute_FirewallWarnRoutesWithAdvisory(t *testing.T) {
	var posts, escalations []string
	fw, err := readermap.NewTermSet(nil, []string{"flatten(ed|s|ing)?"})
	if err != nil {
		t.Fatal(err)
	}
	d := replyDeps{
		dest:     func(string) (string, bool) { return "wh", true },
		post:     func(_, _, content string) error { posts = append(posts, content); return nil },
		escalate: func(_, msg string) { escalations = append(escalations, msg) },
		logf:     func(string, ...any) {},
		firewall: fw,
	}
	d.route(context.Background(), "xo", "chanA", "we flattened the book before close")
	if len(posts) != 1 {
		t.Fatalf("a warn-tier reply must still ROUTE; got posts=%v", posts)
	}
	if len(escalations) != 1 || !strings.Contains(escalations[0], "advisory") {
		t.Fatalf("a warn-tier reply must raise ONE advisory escalation; got escalations=%v", escalations)
	}
}

// Stop cancels in-flight watchers (daemon shutdown) — no post/alert after Stop.
func TestReplyRouter_Stop(t *testing.T) {
	var mu sync.Mutex
	var emitted int
	blocked := make(chan struct{})
	d := replyDeps{
		reply:    func(string, string) (string, bool, error) { return "", false, nil },
		dest:     func(string) (string, bool) { return "wh", true },
		post:     func(string, string, string) error { mu.Lock(); emitted++; mu.Unlock(); return nil },
		escalate: func(string, string) { mu.Lock(); emitted++; mu.Unlock() },
		sleep:    func(time.Duration) { <-blocked }, // block the watcher mid-poll until Stop cancels ctx
		logf:     func(string, ...any) {},
		softTTL:  1 * time.Second,
		hardTTL:  2 * time.Second,
		interval: 1 * time.Second,
	}
	r := newReplyRouter(context.Background(), d)
	done := make(chan struct{}, 1)
	r.dispatch = func(f func()) { go func() { f(); done <- struct{}{} }() }
	r.arm("xo", "chanA", "msg")
	r.Stop()       // cancels the watcher's ctx
	close(blocked) // unblock the sleep; the watcher should see ctx cancelled and exit without emitting
	<-done
	mu.Lock()
	defer mu.Unlock()
	if emitted != 0 {
		t.Fatalf("a Stop()'d watcher emitted %d posts/alerts, want 0", emitted)
	}
}
