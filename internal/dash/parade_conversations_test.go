package dash

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dash/control"
	"github.com/jim80net/flotilla/internal/outbox"
)

const paradeConversationRoster = `{
	"channel_id":"C1", "xo_agent":"xo", "cos_agent":"cos", "heartbeat_interval":"20m",
	"agents":[{"name":"xo"},{"name":"cos"},{"name":"alpha","surface":"aider"}]
}`

func TestParadeConversationStoreAtomicConcurrentAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conversations.json")
	const count = 24
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := appendParadeConversation(path, 2, "Alpha · Generic claim", ParadeConversationMessage{
				ID: fmt.Sprintf("m-%02d", i), TS: "2026-07-15T05:40:00Z", Author: "operator", Kind: "note", Text: fmt.Sprintf("note %d", i),
			})
			if err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	doc, err := loadParadeConversations(path)
	if err != nil {
		t.Fatal(err)
	}
	thread := doc.Slides["2"]
	if doc.Schema != 1 || thread.Title != "Alpha · Generic claim" || len(thread.Messages) != count {
		t.Fatalf("stored document = %+v, want schema/title/%d messages", doc, count)
	}
	if temps, err := filepath.Glob(filepath.Join(dir, ".conversations-*.tmp")); err != nil || len(temps) != 0 {
		t.Fatalf("atomic write left temp files %v (err=%v)", temps, err)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Errorf("conversations mode = %o, want 600", mode)
	}
}

func TestLoadParadeConversationsMissingAndInvalid(t *testing.T) {
	doc, err := loadParadeConversations(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || doc.Schema != 1 || doc.Slides == nil || len(doc.Slides) != 0 {
		t.Fatalf("missing document = %+v, err=%v", doc, err)
	}
	path := filepath.Join(t.TempDir(), "conversations.json")
	if err := os.WriteFile(path, []byte(`{"schema":2,"slides":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadParadeConversations(path); err == nil {
		t.Fatal("unsupported schema must fail loudly")
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func paradeConversationServer(t *testing.T, f *fakeController) (*Server, string, string) {
	t.Helper()
	now := time.Date(2026, 7, 15, 5, 40, 0, 0, time.UTC)
	srv, dir := newTestServer(t, paradeConversationRoster, now)
	srv.control = f
	date := "2026-07-15"
	paradeDir := filepath.Join(dir, "parades", date)
	if err := os.MkdirAll(paradeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	deck := "# Alpha · First claim\nBody\n---\n# Beta · Second claim\nMore"
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte(deck), 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, dir, date
}

func TestParadeConversationHandlersPersistAndRouteToCos(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "cos", Outcome: control.OutcomeDelivered}}
	srv, dir, date := paradeConversationServer(t, f)

	empty := doGet(t, srv, "/api/parades/"+date+"/conversations")
	var emptyDoc ParadeConversations
	if err := json.Unmarshal(empty.Body.Bytes(), &emptyDoc); empty.Code != http.StatusOK || err != nil || emptyDoc.Schema != 1 {
		t.Fatalf("empty GET = %d %s", empty.Code, empty.Body.String())
	}

	body := `{"kind":"invest","text":"Please fund <script>alert(1)</script> & validate.\nSecond line."}`
	rec := doWrite(t, srv, "POST", "/api/parades/"+date+"/slides/1/messages", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
	}
	if f.lastRouteTarget != "cos" {
		t.Fatalf("route target = %q, want cos", f.lastRouteTarget)
	}
	wantRoute := "[parade 2026-07-15 · slide 2/2 · Beta · Second claim]\nkind=invest\ntext: Please fund <script>alert(1)</script> & validate. Second line."
	if f.lastRouteMsg != wantRoute {
		t.Errorf("route message = %q, want %q", f.lastRouteMsg, wantRoute)
	}

	storedPath := filepath.Join(dir, "parades", date, "conversations.json")
	doc, err := loadParadeConversations(storedPath)
	if err != nil {
		t.Fatal(err)
	}
	thread := doc.Slides["1"]
	if thread.Title != "Beta · Second claim" || len(thread.Messages) != 1 {
		t.Fatalf("thread = %+v", thread)
	}
	if got := thread.Messages[0]; got.Kind != "invest" || got.Author != "operator" || got.ID == "" || got.TS != "2026-07-15T05:40:00Z" || !strings.Contains(got.Text, "<script>") || !strings.Contains(got.Text, "\nSecond line.") {
		t.Fatalf("stored message = %+v", got)
	}

	get := doGet(t, srv, "/api/parades/"+date+"/conversations")
	var roundTrip ParadeConversations
	if err := json.Unmarshal(get.Body.Bytes(), &roundTrip); err != nil || len(roundTrip.Slides["1"].Messages) != 1 {
		t.Fatalf("GET round trip = %+v, err=%v", roundTrip, err)
	}
}

func TestParadeConversationPostQueuesWhenCosBusy(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "cos", Outcome: control.OutcomeBusy}}
	srv, dir, date := paradeConversationServer(t, f)
	rec := doWrite(t, srv, "POST", "/api/parades/"+date+"/slides/0/messages", `{"text":"Strong work","kind":"kudos"}`)
	var response paradeMessageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); rec.Code != http.StatusAccepted || err != nil || response.Delivery != "queued" {
		t.Fatalf("busy POST = %d %s", rec.Code, rec.Body.String())
	}
	pending := outbox.ListAll(dir)
	if len(pending) != 1 || pending[0].Sender != "operator" || pending[0].Recipient != "cos" || !strings.Contains(pending[0].Message, "kind=kudos") {
		t.Fatalf("pending delivery = %+v", pending)
	}
}

func TestParadeConversationPostRequiresWriteGate(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "cos", Outcome: control.OutcomeDelivered}}
	srv, _, date := paradeConversationServer(t, f)
	path := "/api/parades/" + date + "/slides/0/messages"
	req := httptest.NewRequest("POST", "http://127.0.0.1:8787"+path, strings.NewReader(`{"text":"hello"}`))
	req.Host = "127.0.0.1:8787"
	req.Header.Set("Origin", "http://127.0.0.1:8787")
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || f.calls != 0 {
		t.Fatalf("ungated POST = %d calls=%d", rec.Code, f.calls)
	}
}

func TestParadeConversationValidation(t *testing.T) {
	f := &fakeController{routeRes: control.RouteResult{Target: "cos", Outcome: control.OutcomeDelivered}}
	srv, _, date := paradeConversationServer(t, f)
	tests := []struct {
		path string
		body string
		code int
	}{
		{"/api/parades/not-a-date/slides/0/messages", `{"text":"x"}`, http.StatusBadRequest},
		{"/api/parades/" + date + "/slides/-1/messages", `{"text":"x"}`, http.StatusBadRequest},
		{"/api/parades/" + date + "/slides/9/messages", `{"text":"x"}`, http.StatusNotFound},
		{"/api/parades/" + date + "/slides/0/messages", `{"text":"   "}`, http.StatusBadRequest},
		{"/api/parades/" + date + "/slides/0/messages", `{"text":"x","kind":"celebrate"}`, http.StatusBadRequest},
		{"/api/parades/" + date + "/slides/0/messages", `{"text":"` + strings.Repeat("界", paradeMessageTextCap+1) + `"}`, http.StatusBadRequest},
	}
	for _, tc := range tests {
		rec := doWrite(t, srv, "POST", tc.path, tc.body)
		if rec.Code != tc.code {
			t.Errorf("POST %s = %d %s, want %d", tc.path, rec.Code, rec.Body.String(), tc.code)
		}
	}
}

func TestParadeConversationDeckMarkers741(t *testing.T) {
	srv, _, _ := paradeConversationServer(t, &fakeController{})
	js := doGet(t, srv, "/static/parade.js").Body.String()
	for _, marker := range []string{
		`messageTextHtml`, `esc(text).replace`, `pd-convo-count`, `maxlength="4000"`,
		`"kudos", "invest", "feedback"`, `"X-Flotilla-Dash": "1"`,
		`e.target.closest("input, textarea, select, button, summary, [contenteditable=true]")`,
		`e.target.closest(".pd-conversation")`,
	} {
		if !strings.Contains(js, marker) {
			t.Errorf("parade.js missing #741 executable marker %q", marker)
		}
	}
	css := doGet(t, srv, "/static/dash.css").Body.String()
	for _, marker := range []string{".pd-conversation", ".pd-convo-thread", ".pd-convo-form", ".pd-kind input:checked + span"} {
		if !strings.Contains(css, marker) {
			t.Errorf("dash.css missing #741 conversation style %q", marker)
		}
	}
}
