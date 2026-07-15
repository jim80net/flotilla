package dash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
)

func historyLine(at int, from, to, gist string) string {
	return strings.TrimSuffix(cos.Line(cos.Entry{
		Time: time.Unix(int64(at), 0).UTC(), From: from, To: to, Channel: "C1", Gist: gist,
	}), "\n")
}

func TestBuildHistoryPageCursorHasNoGapsOrDuplicates(t *testing.T) {
	lines := []string{
		historyLine(1, "operator", "alpha", "alpha-1"),
		historyLine(2, "operator", "beta", "beta-1"),
		historyLine(3, "alpha", "operator", "alpha-2"),
		"future malformed record",
		historyLine(4, "operator", "alpha", "alpha-3"),
		historyLine(5, "operator", "beta", "beta-2"),
		historyLine(6, "alpha", "operator", "alpha-4"),
	}
	raw := strings.Join(lines, "\n") + "\n"
	q := HistoryQuery{Desk: "@ALPHA", Limit: 2}
	var got []string
	for {
		page, err := BuildHistoryPage(raw, "", q)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Ledger {
			got = append(got, entry.Gist)
			if entry.Raw != "" {
				t.Errorf("parsed entry duplicated raw source: %+v", entry)
			}
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" {
			t.Fatal("has_more page omitted next_cursor")
		}
		q.Cursor = page.NextCursor
	}
	want := []string{"alpha-4", "alpha-3", "alpha-2", "alpha-1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("cursor traversal = %v, want %v", got, want)
	}
}

func TestBuildHistoryPageCursorStableAcrossAppend(t *testing.T) {
	original := strings.Join([]string{
		historyLine(1, "operator", "alpha", "one"),
		historyLine(2, "operator", "alpha", "two"),
		historyLine(3, "operator", "alpha", "three"),
	}, "\n") + "\n"
	first, err := BuildHistoryPage(original, "", HistoryQuery{Desk: "alpha", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore || first.Ledger[0].Gist != "three" {
		t.Fatalf("first page = %+v", first)
	}
	appended := original + historyLine(4, "operator", "alpha", "new-after-cursor") + "\n"
	second, err := BuildHistoryPage(appended, "", HistoryQuery{Desk: "alpha", Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Ledger) != 1 || second.Ledger[0].Gist != "two" {
		t.Fatalf("append shifted cursor or duplicated a row: %+v", second)
	}
}

func TestBuildHistoryPageFiltersDeskAndChannel(t *testing.T) {
	raw := historyLine(1, "operator", "alpha", "c1") + "\n" + strings.TrimSuffix(cos.Line(cos.Entry{
		Time: time.Unix(2, 0).UTC(), From: "operator", To: "alpha", Channel: "C2", Gist: "c2",
	}), "\n") + "\n"
	page, err := BuildHistoryPage(raw, "", HistoryQuery{Desk: "alpha", Channel: "c2", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Ledger) != 1 || page.Ledger[0].Gist != "c2" || page.Desk != "alpha" || page.Channel != "c2" {
		t.Fatalf("desk/channel isolation failed: %+v", page)
	}
}

func TestBuildHistoryPageScopedRawFallbackUsesWholeAgentToken(t *testing.T) {
	raw := strings.Join([]string{
		"future record operator -> @alpha payload",
		"future record operator -> @alpha-adj payload",
		"future record operator -> @beta payload",
	}, "\n") + "\n"
	page, err := BuildHistoryPage(raw, "", HistoryQuery{Desk: "alpha", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Ledger) != 1 || page.Ledger[0].Parsed || page.Ledger[0].Raw != "future record operator -> @alpha payload" {
		t.Fatalf("scoped raw fallback = %+v", page.Ledger)
	}
	channelPage, err := BuildHistoryPage(raw, "", HistoryQuery{Desk: "alpha", Channel: "C1", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(channelPage.Ledger) != 0 {
		t.Fatalf("channel-scoped malformed rows cannot be proven: %+v", channelPage.Ledger)
	}
}

func TestBuildHistoryPageNormalizedEmptyDeskCompletesWithoutMatch(t *testing.T) {
	raw := "future record operator -> @alpha payload\n"
	page, err := BuildHistoryPage(raw, "", HistoryQuery{Desk: "@", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Ledger) != 0 || page.HasMore {
		t.Fatalf("normalized-empty desk must fail closed: %+v", page)
	}
	if historyRawHasParticipant(raw, "") {
		t.Fatal("empty participant must never enter the raw-token search")
	}
}

func TestHandleHistoryBoundedMetaAndValidation(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	srv, dir := newTestServer(t, singleFleetRoster, now)
	srv.cfg.LedgerPath = filepath.Join(dir, "context-ledger.md")
	var lines []string
	for i := 1; i <= 80; i++ {
		lines = append(lines, historyLine(i, "operator", "alpha", fmt.Sprintf("message-%03d %s", i, strings.Repeat("x", 1500))))
	}
	if err := os.WriteFile(filepath.Join(dir, "context-ledger.md"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".flotilla-state.md"), []byte("## Backlog\n- [in-flight] bound history\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := doGet(t, srv, "/api/history?desk=alpha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page HistoryPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Ledger) != defaultHistoryLimit || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("bounded page contract = %+v", page)
	}
	if rec.Body.Len() >= 500*1024 {
		t.Fatalf("initial production-shaped history page = %d bytes, want <500 KiB", rec.Body.Len())
	}
	if !page.Backlog.Found || page.LedgerSignature == "" || page.BacklogSignature == "" {
		t.Fatalf("page metadata/backlog missing: %+v", page)
	}

	metaRec := doGet(t, srv, "/api/history?meta=1")
	var meta HistoryPage
	if err := json.Unmarshal(metaRec.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if len(meta.Ledger) != 0 || meta.LedgerSignature != page.LedgerSignature || !meta.Backlog.Found {
		t.Fatalf("metadata response = %+v", meta)
	}
	if metaRec.Body.Len() >= 32*1024 {
		t.Fatalf("metadata response unexpectedly large: %d bytes", metaRec.Body.Len())
	}

	for _, path := range []string{"/api/history?limit=0", "/api/history?limit=201", "/api/history?cursor=not-a-cursor", "/api/history?cursor=999999999"} {
		if got := doGet(t, srv, path); got.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d, want 400", path, got.Code)
		}
	}
}

func TestHistoryPageHydratesReturnedEntriesOnly(t *testing.T) {
	raw := strings.Join([]string{
		historyLine(1, "operator", "alpha", "old"),
		historyLine(2, "operator", "alpha", "middle"),
		historyLine(3, "operator", "alpha", "new"),
	}, "\n") + "\n"
	page, err := BuildHistoryPage(raw, "", HistoryQuery{Desk: "alpha", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for i := range page.Ledger {
		page.Ledger[i].Nonce = fmt.Sprintf("nonce-%d", i)
	}
	HydrateLedgerBodies(page.Ledger, func(nonce string) (string, bool) {
		calls++
		return "full " + nonce, true
	})
	if calls != 1 || page.Ledger[0].Body != "full nonce-0" {
		t.Fatalf("hydration calls=%d page=%+v", calls, page)
	}
}

func TestHistoryAssetsUseSelectedWindowMetadataAndExplicitPaging(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	srv, _ := newTestServer(t, singleFleetRoster, now)
	js := doGet(t, srv, "/static/dash.js").Body.String()
	html := doGet(t, srv, "/").Body.String()
	for _, marker := range []string{
		`desk=" + encodeURIComponent(desk || "") + "&limit=50"`,
		`getJSON(historyURL("", "", true))`,
		`meta.ledger_signature !== known.ledger_signature`,
		`fetchSelectedHistory(false)`,
		`thread.scrollTop = beforeTop + Math.max(0, thread.scrollHeight - beforeHeight)`,
		`Could not load coordination history`,
		`Showing the last loaded messages`,
		`var lastGood = reset && historyMatchesSelected(prior) && !prior.error ? prior : null`,
		`if (hadFocus && btn.hidden)`,
		`title.focus()`,
		`"#error:" + cheapHash(loadError)`,
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("bounded history UI missing %q", marker)
		}
	}
	if strings.Contains(js, `getJSON("/api/history")`) {
		t.Error("dash startup must not fetch the unscoped history endpoint")
	}
	if !strings.Contains(html, `id="thread-load-earlier"`) {
		t.Error("Conversations must expose explicit earlier-history paging")
	}
}
