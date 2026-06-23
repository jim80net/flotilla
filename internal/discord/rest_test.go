package discord

import (
	"errors"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// recordingFetch is a fake channelMessagesFunc that records the args it was called
// with and returns a canned batch — so the projection + ordering are tested
// without a live Discord session.
type recordingFetch struct {
	gotChannel string
	gotLimit   int
	gotBefore  string
	gotAfter   string
	gotAround  string
	ret        []*discordgo.Message
	err        error
}

func (f *recordingFetch) fn(ch string, limit int, before, after, around string) ([]*discordgo.Message, error) {
	f.gotChannel, f.gotLimit, f.gotBefore, f.gotAfter, f.gotAround = ch, limit, before, after, around
	return f.ret, f.err
}

func msg(id, author, content string) *discordgo.Message {
	return &discordgo.Message{ID: id, Author: &discordgo.User{ID: author}, Content: content, Timestamp: time.Unix(0, 0)}
}

func TestMessagesAfter_MapsArgsAndReturnsAscending(t *testing.T) {
	// PROBED shape: Discord returns the oldest block above the cursor, newest-first
	// within the batch (descending). MessagesAfter must reverse to ascending.
	f := &recordingFetch{ret: []*discordgo.Message{
		msg("30", "op", "third"),
		msg("20", "op", "second"),
		msg("10", "op", "first"),
	}}
	r := &REST{fetch: f.fn}

	got, err := r.MessagesAfter("CH", "5", 100)
	if err != nil {
		t.Fatalf("MessagesAfter err: %v", err)
	}
	// arg mapping: ChannelMessages(ch, limit, beforeID="", afterID, aroundID="")
	if f.gotChannel != "CH" || f.gotLimit != 100 || f.gotBefore != "" || f.gotAfter != "5" || f.gotAround != "" {
		t.Fatalf("fetch args = (%q,%d,%q,%q,%q), want (CH,100,'','5','')",
			f.gotChannel, f.gotLimit, f.gotBefore, f.gotAfter, f.gotAround)
	}
	// ascending by snowflake id
	if len(got) != 3 || got[0].ID != "10" || got[1].ID != "20" || got[2].ID != "30" {
		t.Fatalf("order = %v, want ascending [10 20 30]", ids(got))
	}
	if got[0].SnowID != 10 || got[2].SnowID != 30 {
		t.Fatalf("SnowID not parsed: %+v", got)
	}
	if got[0].AuthorID != "op" || got[0].Content != "first" {
		t.Fatalf("projection lost fields: %+v", got[0])
	}
}

func TestMessagesAfter_PropagatesError(t *testing.T) {
	want := errors.New("rate limited")
	r := &REST{fetch: (&recordingFetch{err: want}).fn}
	if _, err := r.MessagesAfter("CH", "5", 10); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestLatest(t *testing.T) {
	f := &recordingFetch{ret: []*discordgo.Message{msg("99", "op", "newest")}}
	r := &REST{fetch: f.fn}
	got, ok, err := r.Latest("CH")
	if err != nil || !ok {
		t.Fatalf("Latest err=%v ok=%v", err, ok)
	}
	if f.gotLimit != 1 || f.gotAfter != "" || f.gotBefore != "" || f.gotAround != "" {
		t.Fatalf("Latest must fetch limit=1 with no anchors; got (%d,%q,%q,%q)",
			f.gotLimit, f.gotBefore, f.gotAfter, f.gotAround)
	}
	if got.ID != "99" || got.SnowID != 99 {
		t.Fatalf("Latest = %+v, want id 99", got)
	}
}

func TestLatest_EmptyChannel(t *testing.T) {
	r := &REST{fetch: (&recordingFetch{ret: nil}).fn}
	_, ok, err := r.Latest("CH")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Fatalf("ok=true on empty channel, want false")
	}
}

func TestRecent_Ascending(t *testing.T) {
	// inbox path: Discord returns newest-first; Recent presents ascending.
	f := &recordingFetch{ret: []*discordgo.Message{
		msg("300", "op", "c"), msg("200", "x", "b"), msg("100", "op", "a"),
	}}
	r := &REST{fetch: f.fn}
	got, err := r.Recent("CH", 20)
	if err != nil {
		t.Fatalf("Recent err: %v", err)
	}
	if f.gotLimit != 20 || f.gotAfter != "" {
		t.Fatalf("Recent args (%d,%q), want (20,'')", f.gotLimit, f.gotAfter)
	}
	if len(got) != 3 || got[0].ID != "100" || got[2].ID != "300" {
		t.Fatalf("Recent order = %v, want ascending", ids(got))
	}
}

func TestProject_SkipsUnparseableAndNilAndWebhook(t *testing.T) {
	wh := msg("50", "", "via webhook")
	wh.Author = nil
	wh.WebhookID = "wh1"
	raw := []*discordgo.Message{
		nil,
		msg("not-a-snowflake", "op", "bad id"),
		msg("40", "op", "good"),
		wh,
	}
	got := project(raw)
	if len(got) != 2 {
		t.Fatalf("project kept %d, want 2 (nil + bad-id dropped): %v", len(got), ids(got))
	}
	if got[0].ID != "40" || got[1].ID != "50" {
		t.Fatalf("project order = %v, want [40 50]", ids(got))
	}
	if got[1].WebhookID != "wh1" || got[1].AuthorID != "" {
		t.Fatalf("webhook projection wrong: %+v", got[1])
	}
}

func TestParseSnowflake(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"1517261693468807260", 1517261693468807260, true},
		{"0", 0, true},
		{"", 0, false},
		{"abc", 0, false},
		{"-5", 0, false},
		{"12.3", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseSnowflake(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("ParseSnowflake(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// pagedFetch serves messages from a backing store, honoring after + limit, and
// returning them NEWEST-FIRST (Discord's order) so the walk's reverse/sort is
// exercised. Each id is "<n>"; opAuthor toggles operator vs non-operator authorship
// so the raw-page-count continuation predicate can be tested against filtering.
type pagedFetch struct {
	all   []*discordgo.Message // ascending by id
	calls int
}

func (p *pagedFetch) fn(ch string, limit int, before, after, around string) ([]*discordgo.Message, error) {
	p.calls++
	var afterN uint64
	if after != "" {
		afterN, _ = ParseSnowflake(after)
	}
	// collect ascending ids > afterN
	var asc []*discordgo.Message
	for _, m := range p.all {
		n, _ := ParseSnowflake(m.ID)
		if n > afterN {
			asc = append(asc, m)
		}
	}
	if len(asc) > limit {
		asc = asc[:limit] // the OLDEST `limit` above the cursor (probed behavior)
	}
	// return newest-first within the batch (descending)
	out := make([]*discordgo.Message, len(asc))
	for i := range asc {
		out[i] = asc[len(asc)-1-i]
	}
	return out, nil
}

func mkAsc(n int, opEvery int) []*discordgo.Message {
	out := make([]*discordgo.Message, n)
	for i := 0; i < n; i++ {
		author := "op"
		if opEvery > 0 && i%opEvery != 0 {
			author = "noise"
		}
		out[i] = msg(itoaTest(uint64(i+1)), author, "m")
	}
	return out
}

func itoaTest(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestMessagesAfterPaged_SinglePageDrained(t *testing.T) {
	p := &pagedFetch{all: mkAsc(3, 0)} // ids 1,2,3
	r := &REST{fetch: p.fn}
	got, capped, err := r.MessagesAfterPaged("CH", "0", 100, 5)
	if err != nil || capped {
		t.Fatalf("err=%v capped=%v, want nil/false", err, capped)
	}
	if len(got) != 3 || got[0].ID != "1" || got[2].ID != "3" {
		t.Fatalf("got %v, want ascending [1 2 3]", ids(got))
	}
	if p.calls != 1 {
		t.Fatalf("calls=%d, want 1 (short page drains immediately)", p.calls)
	}
}

func TestMessagesAfterPaged_WalksUpwardContiguously(t *testing.T) {
	p := &pagedFetch{all: mkAsc(250, 0)} // ids 1..250
	r := &REST{fetch: p.fn}
	got, capped, err := r.MessagesAfterPaged("CH", "0", 100, 5)
	if err != nil || capped {
		t.Fatalf("err=%v capped=%v, want nil/false (drains in 3 pages)", err, capped)
	}
	if len(got) != 250 || got[0].ID != "1" || got[249].ID != "250" {
		t.Fatalf("walk lost messages: len=%d first=%s last=%s", len(got), got[0].ID, got[249].ID)
	}
	if p.calls != 3 { // 100,100,50
		t.Fatalf("calls=%d, want 3", p.calls)
	}
}

func TestMessagesAfterPaged_PageCapFailsClosed(t *testing.T) {
	p := &pagedFetch{all: mkAsc(1000, 0)} // far more than cap*limit
	r := &REST{fetch: p.fn}
	got, capped, err := r.MessagesAfterPaged("CH", "0", 100, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !capped {
		t.Fatal("capped=false, want true (1000 > 3*100)")
	}
	// fail-closed: returns exactly the contiguous run it fetched (300), oldest-first;
	// the remainder (301..1000) stays ABOVE this batch's max for the next sweep.
	if len(got) != 300 || got[0].ID != "1" || got[299].ID != "300" {
		t.Fatalf("capped batch = len %d [%s..%s], want contiguous [1..300]", len(got), got[0].ID, got[len(got)-1].ID)
	}
}

// TestMessagesAfterPaged_FullPageOfNoiseDoesNotStopEarly is the systems-review
// round-2 guard: a FULL page containing mostly non-operator messages must NOT stop
// the walk (the continuation predicate keys on the raw page length, not on any
// filtered count). Here every page is full of mixed op/noise; the walk must reach
// the last message regardless of authorship.
func TestMessagesAfterPaged_FullPageOfNoiseDoesNotStopEarly(t *testing.T) {
	p := &pagedFetch{all: mkAsc(250, 5)} // 250 msgs, only every 5th is "op"
	r := &REST{fetch: p.fn}
	got, capped, err := r.MessagesAfterPaged("CH", "0", 100, 5)
	if err != nil || capped {
		t.Fatalf("err=%v capped=%v", err, capped)
	}
	// MessagesAfterPaged does NOT filter by author (Accept runs later) — it returns
	// the full contiguous run; the point is the WALK reached id 250.
	if len(got) != 250 || got[249].ID != "250" {
		t.Fatalf("walk stopped early on a noise-heavy page: len=%d last=%s", len(got), got[len(got)-1].ID)
	}
	if p.calls != 3 {
		t.Fatalf("calls=%d, want 3 (raw-count continuation, not filtered)", p.calls)
	}
}

func ids(ms []Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}
