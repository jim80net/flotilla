package paradeconversation

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAppendConcurrentAndAuthorDistinction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversations.json")
	const count = 32
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			author := "operator"
			if i%2 == 1 {
				author = "alpha-desk"
			}
			_, err := Append(path, 0, "Alpha · Claim", Message{
				ID: fmt.Sprintf("m-%02d", i), TS: "2026-07-18T14:00:00Z", Author: author, Kind: "note", Text: fmt.Sprintf("message %d", i),
			})
			if err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(doc.Slides["0"].Messages); got != count {
		t.Fatalf("messages = %d, want %d", got, count)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("sidecar mode = %v err=%v, want 0600", info, err)
	}
}

func TestLatestRepliesAndUnansweredOperatorMessages(t *testing.T) {
	doc := Document{Schema: Schema, Slides: map[string]Thread{
		"0": {Title: "Answered", Messages: []Message{
			{ID: "op-1", Author: "operator"}, {ID: "agent-1", Author: "alpha-desk"},
		}},
		"1": {Title: "Pending", Messages: []Message{
			{ID: "agent-2", Author: "beta-desk"}, {ID: "op-2", Author: "operator", Text: "Please respond"},
		}},
	}}
	latest := LatestAgentReplies(doc)
	if latest["0"] != "agent-1" || latest["1"] != "agent-2" {
		t.Fatalf("latest replies = %+v", latest)
	}
	pending := UnansweredOperatorMessages(doc)
	if len(pending) != 1 || pending[0].Slide != 1 || pending[0].Message.ID != "op-2" {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestNewMessageValidation(t *testing.T) {
	now := time.Date(2026, 7, 18, 14, 0, 0, 0, time.UTC)
	message, err := NewMessage(" alpha-desk ", "NOTE", " reply ", now)
	if err != nil || message.Author != "alpha-desk" || message.Kind != "note" || message.Text != "reply" || message.ID == "" || message.TS != "2026-07-18T14:00:00Z" {
		t.Fatalf("message = %+v err=%v", message, err)
	}
	for _, tc := range []struct{ author, kind, text string }{{"", "note", "x"}, {"alpha", "bad", "x"}, {"alpha", "note", " "}} {
		if _, err := NewMessage(tc.author, tc.kind, tc.text, now); err == nil {
			t.Fatalf("NewMessage(%q,%q,%q) unexpectedly passed", tc.author, tc.kind, tc.text)
		}
	}
}
