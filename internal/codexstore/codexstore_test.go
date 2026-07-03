package codexstore

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRollout(t *testing.T, codexHome, cwd, body string) {
	t.Helper()
	dir := filepath.Join(codexHome, "sessions", "2026", "07", "02")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-07-02T12-00-00-testuuid.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLatestResultAgentMessage(t *testing.T) {
	home := t.TempDir()
	cwd := "/srv/fleet/backend"
	meta := `{"timestamp":"2026-07-02T12:00:00Z","type":"session_meta","payload":{"id":"t1","cwd":"` + cwd + `"}}`
	agent1 := `{"timestamp":"2026-07-02T12:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"first"}}`
	agent2 := `{"timestamp":"2026-07-02T12:00:02Z","type":"event_msg","payload":{"type":"agent_message","message":"latest full result"}}`
	writeRollout(t, home, cwd, meta+"\n"+agent1+"\n"+agent2+"\n")

	got, err := LatestResult(home, cwd)
	if err != nil || got != "latest full result" {
		t.Errorf("LatestResult = (%q, %v), want latest agent message", got, err)
	}
}

func TestLatestResultResponseItem(t *testing.T) {
	home := t.TempDir()
	cwd := "/srv/fleet/data"
	meta := `{"timestamp":"2026-07-02T12:00:00Z","type":"session_meta","payload":{"cwd":"` + cwd + `"}}`
	item := `{"timestamp":"2026-07-02T12:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"from response_item"}]}}`
	writeRollout(t, home, cwd, meta+"\n"+item+"\n")

	got, err := LatestResult(home, cwd)
	if err != nil || got != "from response_item" {
		t.Errorf("LatestResult = (%q, %v)", got, err)
	}
}

func TestReplyAfterResponseItem(t *testing.T) {
	home := t.TempDir()
	cwd := "/srv/fleet/xo"
	meta := `{"timestamp":"2026-07-02T12:00:00Z","type":"session_meta","payload":{"cwd":"` + cwd + `"}}`
	user := `{"type":"event_msg","payload":{"type":"user_message","message":"operator ping"}}`
	item := `{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"XO reply via response_item"}]}}`
	writeRollout(t, home, cwd, meta+"\n"+user+"\n"+item+"\n")

	got, found, err := ReplyAfter(home, cwd, "operator ping")
	if err != nil || !found || got != "XO reply via response_item" {
		t.Errorf("ReplyAfter = (%q, %v, %v), want response_item assistant reply", got, found, err)
	}
}

func TestReplyAfter(t *testing.T) {
	home := t.TempDir()
	cwd := "/srv/fleet/xo"
	meta := `{"timestamp":"2026-07-02T12:00:00Z","type":"session_meta","payload":{"cwd":"` + cwd + `"}}`
	user := `{"type":"event_msg","payload":{"type":"user_message","message":"operator ping"}}`
	agent := `{"type":"event_msg","payload":{"type":"agent_message","message":"XO reply text"}}`
	writeRollout(t, home, cwd, meta+"\n"+user+"\n"+agent+"\n")

	got, found, err := ReplyAfter(home, cwd, "operator ping")
	if err != nil || !found || got != "XO reply text" {
		t.Errorf("ReplyAfter = (%q, %v, %v)", got, found, err)
	}
}

func TestLatestResultNewestRolloutWins(t *testing.T) {
	home := t.TempDir()
	cwd := "/srv/fleet/backend"
	dir := filepath.Join(home, "sessions", "2026", "07", "02")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"type":"session_meta","payload":{"cwd":"` + cwd + `"}}`
	oldPath := filepath.Join(dir, "rollout-2026-07-02T10-00-00-olduuid.jsonl")
	newPath := filepath.Join(dir, "rollout-2026-07-02T12-00-00-newuuid.jsonl")
	oldAgent := `{"type":"event_msg","payload":{"type":"agent_message","message":"old rollout"}}`
	newAgent := `{"type":"event_msg","payload":{"type":"agent_message","message":"newest rollout wins"}}`
	if err := os.WriteFile(oldPath, []byte(meta+"\n"+oldAgent+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte(meta+"\n"+newAgent+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LatestResult(home, cwd)
	if err != nil || got != "newest rollout wins" {
		t.Errorf("LatestResult = (%q, %v), want newest rollout by filename", got, err)
	}
}

func TestLatestResultNoSession(t *testing.T) {
	home := t.TempDir()
	if _, err := LatestResult(home, "/missing"); err == nil {
		t.Fatal("want error when no rollout matches cwd")
	}
}
