package main

import (
	"context"
	"strings"
	"testing"
	"time"
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
		ttl:      5 * time.Second,
		interval: 1 * time.Second,
	}
	return &d, &posts, &alerts
}

// markSeq returns a mark func yielding the given (count, ok, text) on successive calls; the LAST entry
// repeats for any further calls.
func markSeq(seq ...struct {
	count int
	ok    bool
	text  string
}) func(string) (string, int, bool, error) {
	i := 0
	return func(string) (string, int, bool, error) {
		s := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return s.text, s.count, s.ok, nil
	}
}

type mark = struct {
	count int
	ok    bool
	text  string
}

func TestReplyWatch_RoutesOnCountIncrease(t *testing.T) {
	d, posts, alerts := baseDeps()
	// snapshot N=2; then still 2 (working); then 3 + substantive reply.
	d.mark = markSeq(mark{2, true, "prior"}, mark{2, false, ""}, mark{3, true, "here is what I need from you"})
	runReplyWatch(context.Background(), *d, "empath-lead", "chan123")
	if len(*alerts) != 0 {
		t.Fatalf("unexpected alerts: %v", *alerts)
	}
	if len(*posts) != 1 || !strings.Contains((*posts)[0], "here is what I need") || !strings.HasPrefix((*posts)[0], "empath-lead|") {
		t.Fatalf("posts = %v, want one attributed verbatim reply", *posts)
	}
}

func TestReplyWatch_NoReplyWithinTTL_Escalates(t *testing.T) {
	d, posts, alerts := baseDeps()
	d.mark = markSeq(mark{2, false, ""}) // count never rises
	runReplyWatch(context.Background(), *d, "empath-lead", "chan123")
	if len(*posts) != 0 {
		t.Fatalf("must not post when no reply arrives: %v", *posts)
	}
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "not answered") {
		t.Fatalf("alerts = %v, want a single not-answered escalation", *alerts)
	}
}

func TestReplyWatch_SnapshotReadError_Escalates(t *testing.T) {
	d, _, alerts := baseDeps()
	d.mark = func(string) (string, int, bool, error) { return "", 0, false, context.DeadlineExceeded }
	runReplyWatch(context.Background(), *d, "empath-lead", "chan123")
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "session unreadable") {
		t.Fatalf("alerts = %v, want a snapshot-unreadable escalation", *alerts)
	}
}

func TestReplyWatch_NewTurnButNoSubstantiveText_Escalates(t *testing.T) {
	d, posts, alerts := baseDeps()
	d.mark = markSeq(mark{2, true, "prior"}, mark{3, false, ""}) // count rose but no routable text
	runReplyWatch(context.Background(), *d, "empath-lead", "chan123")
	if len(*posts) != 0 {
		t.Fatalf("must not post empty: %v", *posts)
	}
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "no substantive text") {
		t.Fatalf("alerts = %v, want a no-substantive-text escalation", *alerts)
	}
}

func TestReplyWatch_WebhookUnresolved_Escalates(t *testing.T) {
	d, posts, alerts := baseDeps()
	d.mark = markSeq(mark{2, true, "prior"}, mark{3, true, "reply"})
	d.dest = func(string) (string, bool) { return "", false } // no webhook for the origin channel
	runReplyWatch(context.Background(), *d, "empath-lead", "chan123")
	if len(*posts) != 0 {
		t.Fatalf("must not post with no destination: %v", *posts)
	}
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "can't route") {
		t.Fatalf("alerts = %v, want a no-webhook escalation", *alerts)
	}
}

func TestReplyWatch_PostError_Escalates(t *testing.T) {
	d, _, alerts := baseDeps()
	d.mark = markSeq(mark{2, true, "prior"}, mark{3, true, "reply"})
	d.post = func(string, string, string) error { return context.Canceled }
	runReplyWatch(context.Background(), *d, "empath-lead", "chan123")
	if len(*alerts) != 1 || !strings.Contains((*alerts)[0], "couldn't post") {
		t.Fatalf("alerts = %v, want a post-failure escalation", *alerts)
	}
}

func TestReplyWatch_SupersededBeforeRoute_NoStalePost(t *testing.T) {
	d, posts, alerts := baseDeps()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already superseded
	d.mark = markSeq(mark{2, true, "prior"}, mark{3, true, "reply"})
	runReplyWatch(ctx, *d, "empath-lead", "chan123")
	if len(*posts) != 0 || len(*alerts) != 0 {
		t.Fatalf("a superseded watcher must emit nothing: posts=%v alerts=%v", *posts, *alerts)
	}
}

func TestReplyWatch_MultiChunk_OrderedPosts(t *testing.T) {
	d, posts, alerts := baseDeps()
	big := strings.Repeat("x", mirrorChunkLimit+50) // forces 2 chunks
	d.mark = markSeq(mark{1, true, "prior"}, mark{2, true, big})
	runReplyWatch(context.Background(), *d, "empath-lead", "chan123")
	if len(*alerts) != 0 {
		t.Fatalf("unexpected alerts: %v", *alerts)
	}
	if len(*posts) != 2 || !strings.Contains((*posts)[0], "reply 1/2") || !strings.Contains((*posts)[1], "reply 2/2") {
		t.Fatalf("posts = %d chunks, want 2 ordered (1/2, 2/2)", len(*posts))
	}
}

// arm supersedes the prior watcher for the same XO (re-anchors to the new origin channel).
func TestReplyRouter_ArmSupersedes(t *testing.T) {
	var routed []string
	d := replyDeps{
		mark:     markSeq(mark{0, false, ""}, mark{1, true, "reply"}),
		dest:     func(ch string) (string, bool) { return "wh-" + ch, true },
		post:     func(url, user, content string) error { routed = append(routed, url); return nil },
		escalate: func(string, string) {},
		sleep:    func(time.Duration) {},
		logf:     func(string, ...any) {},
		ttl:      3 * time.Second,
		interval: 1 * time.Second,
	}
	r := newReplyRouter(d)
	done := make(chan struct{}, 2)
	r.dispatch = func(f func()) { go func() { f(); done <- struct{}{} }() }
	r.arm("empath-lead", "chanA")
	r.arm("empath-lead", "chanB") // supersede → chanA's watcher cancels
	<-done
	<-done
	// The surviving route (if any) must target chanB, never chanA.
	for _, u := range routed {
		if u == "wh-chanA" {
			t.Fatalf("a superseded watcher routed to the old channel chanA: %v", routed)
		}
	}
}
