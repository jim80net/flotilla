package dash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/cos"
)

func writeProductionShapedHistory(t *testing.T, path string, count int) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < count; i++ {
		to := "alpha"
		nonce := ""
		body := fmt.Sprintf("message-%04d %s", i, strings.Repeat("measured payload ", 200))
		if i%2 == 1 {
			to = "gamma"
		} else {
			nonce = fmt.Sprintf("%032x", i+1)
			if err := cos.WriteBody(path, nonce, body); err != nil {
				t.Fatal(err)
			}
		}
		b.WriteString(cos.Line(cos.Entry{
			Time:    time.Date(2026, 7, 15, 0, 0, i, 0, time.UTC),
			Channel: "C1",
			From:    "operator",
			To:      to,
			Gist:    body,
			Nonce:   nonce,
		}))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func decodeHistoryResponse(t *testing.T, body []byte) HistoryDoc {
	t.Helper()
	var doc HistoryDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestHistoryPageBoundedDeskCursorAndPayload749(t *testing.T) {
	srv, dir := newTestServer(t, singleFleetRoster, time.Now())
	ledgerPath := filepath.Join(dir, "history.md")
	writeProductionShapedHistory(t, ledgerPath, 7000)
	srv.cfg.LedgerPath = ledgerPath
	legacyRec := doGet(t, srv, "/api/history")
	if legacyRec.Body.Len() < 10*1024*1024 {
		t.Fatalf("production-shaped legacy payload = %d bytes, want >=10MB baseline", legacyRec.Body.Len())
	}

	firstRec := doGet(t, srv, "/api/history?desk=alpha&limit=80")
	if firstRec.Code != 200 {
		t.Fatalf("first page status = %d: %s", firstRec.Code, firstRec.Body.String())
	}
	if firstRec.Body.Len() >= 500*1024 {
		t.Fatalf("initial history payload = %d bytes, want <500KB", firstRec.Body.Len())
	}
	t.Logf("measured history transfer: legacy=%d bytes bounded=%d bytes reduction=%.1fx",
		legacyRec.Body.Len(), firstRec.Body.Len(), float64(legacyRec.Body.Len())/float64(firstRec.Body.Len()))
	first := decodeHistoryResponse(t, firstRec.Body.Bytes())
	if len(first.Ledger) != 80 || first.Limit != 80 || !first.HasMore || first.NextCursor == "" || first.Total != 3500 {
		t.Fatalf("first page contract = ledger:%d limit:%d has_more:%v cursor:%q total:%d", len(first.Ledger), first.Limit, first.HasMore, first.NextCursor, first.Total)
	}
	if len(first.Ledger[0].Body) < 3000 || !strings.HasPrefix(first.Ledger[0].Body, "message-6998 ") {
		t.Fatalf("returned row was not completely hydrated: body bytes=%d prefix=%q", len(first.Ledger[0].Body), first.Ledger[0].Body[:min(32, len(first.Ledger[0].Body))])
	}
	for _, entry := range first.Ledger {
		if historyParticipant(entry.To) != "alpha" && historyParticipant(entry.From) != "alpha" {
			t.Fatalf("cross-desk entry leaked into alpha page: %+v", entry)
		}
	}

	// A concurrent append must not move the older-page boundary: the absolute
	// chronological cursor continues immediately after the prior oldest row.
	f, err := os.OpenFile(ledgerPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(cos.Line(cos.Entry{Time: time.Now(), Channel: "C1", From: "operator", To: "alpha", Gist: "new append"}))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append=%v close=%v", err, closeErr)
	}
	secondRec := doGet(t, srv, "/api/history?desk=alpha&limit=80&cursor="+first.NextCursor)
	second := decodeHistoryResponse(t, secondRec.Body.Bytes())
	if len(second.Ledger) != 80 {
		t.Fatalf("second page rows = %d", len(second.Ledger))
	}
	if got := second.Ledger[0].Gist; !strings.HasPrefix(got, "message-6838 ") {
		t.Fatalf("cursor gap after append: second page starts %q, want message-6838", got)
	}
	seen := make(map[string]bool, len(first.Ledger))
	for _, entry := range first.Ledger {
		seen[entry.Gist] = true
	}
	for _, entry := range second.Ledger {
		if seen[entry.Gist] {
			t.Fatalf("cursor duplicated entry %q after append", entry.Gist)
		}
	}
}

func TestHistoryMetadataIsTinyAndCarriesNoLedger749(t *testing.T) {
	srv, dir := newTestServer(t, singleFleetRoster, time.Now())
	ledgerPath := filepath.Join(dir, "history.md")
	writeProductionShapedHistory(t, ledgerPath, 7000)
	srv.cfg.LedgerPath = ledgerPath

	rec := doGet(t, srv, "/api/history?meta=1")
	doc := decodeHistoryResponse(t, rec.Body.Bytes())
	if len(doc.Ledger) != 0 || doc.Signature == "" {
		t.Fatalf("metadata response = %+v", doc)
	}
	if rec.Body.Len() >= 16*1024 {
		t.Fatalf("metadata payload = %d bytes, want <16KB", rec.Body.Len())
	}
	if got := rec.Header().Get("Server-Timing"); !strings.Contains(got, "history-meta") {
		t.Fatalf("Server-Timing = %q", got)
	}
}

func TestHistoryBoundedQueryRejectsInvalidContract749(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	for _, path := range []string{
		"/api/history?limit=80",
		"/api/history?desk=alpha&limit=0",
		"/api/history?desk=alpha&limit=201",
		"/api/history?desk=alpha&cursor=bad",
	} {
		if rec := doGet(t, srv, path); rec.Code != 400 {
			t.Errorf("GET %s status = %d, want 400", path, rec.Code)
		}
	}
}
