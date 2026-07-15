package codexstore

import (
	"fmt"
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

func TestResolveRolloutForProcessBindsPaneNotNewestCWD(t *testing.T) {
	home := t.TempDir()
	proc := t.TempDir()
	cwd := "/srv/fleet/shared"
	dir := filepath.Join(home, "sessions", "2026", "07", "15")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := `{"type":"session_meta","payload":{"cwd":"` + cwd + `"}}`
	older := filepath.Join(dir, "rollout-2026-07-15T10-00-00-seat-a.jsonl")
	newer := filepath.Join(dir, "rollout-2026-07-15T11-00-00-seat-b.jsonl")
	if err := os.WriteFile(older, []byte(meta+"\n"+`{"type":"event_msg","payload":{"type":"agent_message","message":"seat A result"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte(meta+"\n"+`{"type":"event_msg","payload":{"type":"agent_message","message":"seat B newer result"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeProcNode(t, proc, 100, "101")
	makeProcNode(t, proc, 101, "")
	if err := os.Symlink(older, filepath.Join(proc, "101", "fd", "19")); err != nil {
		t.Fatal(err)
	}

	path, err := resolveRolloutForProcess(home, cwd, 100, proc)
	if err != nil || path != older {
		t.Fatalf("resolveRolloutForProcess = (%q, %v), want seat A rollout %q", path, err, older)
	}
	got, err := lastAgentText(path)
	if err != nil || got != "seat A result" {
		t.Fatalf("pane-bound result = (%q, %v), want seat A result", got, err)
	}
}

func TestResolveRolloutForProcessFailsClosed(t *testing.T) {
	home := t.TempDir()
	proc := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "07", "15")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(dir, "rollout-2026-07-15T10-00-00-seat.jsonl")
	if err := os.WriteFile(rollout, []byte(`{"type":"session_meta","payload":{"cwd":"/different"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	makeProcNode(t, proc, 200, "")
	if err := os.Symlink(rollout, filepath.Join(proc, "200", "fd", "7")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRolloutForProcess(home, "/wanted", 200, proc); err == nil {
		t.Fatal("cwd mismatch must fail closed")
	}
	if _, err := resolveRolloutForProcess(home, "/wanted", 999, proc); err == nil {
		t.Fatal("missing process rollout must fail closed")
	}
}

func TestResolveRolloutForProcessRejectsAmbiguousAndOutsideFiles(t *testing.T) {
	home := t.TempDir()
	proc := t.TempDir()
	cwd := "/srv/fleet/shared"
	dir := filepath.Join(home, "sessions", "2026", "07", "15")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := []byte(`{"type":"session_meta","payload":{"cwd":"` + cwd + `"}}` + "\n")
	one := filepath.Join(dir, "rollout-one.jsonl")
	two := filepath.Join(dir, "rollout-two.jsonl")
	outside := filepath.Join(t.TempDir(), "rollout-outside.jsonl")
	for _, path := range []string{one, two, outside} {
		if err := os.WriteFile(path, meta, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	makeProcNode(t, proc, 300, "")
	for fd, path := range map[string]string{"3": one, "4": two, "5": outside} {
		if err := os.Symlink(path, filepath.Join(proc, "300", "fd", fd)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := resolveRolloutForProcess(home, cwd, 300, proc); err == nil {
		t.Fatal("two in-store rollouts must fail ambiguous; outside rollout must not count")
	}
}

func TestProcessChildrenUnionsAllThreads(t *testing.T) {
	proc := t.TempDir()
	makeProcNode(t, proc, 400, "401")
	task := filepath.Join(proc, "400", "task", "499")
	if err := os.MkdirAll(task, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task, "children"), []byte("402 401"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := processChildren(proc, 400)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !containsPID(got, 401) || !containsPID(got, 402) {
		t.Fatalf("processChildren = %v, want unique 401 and 402", got)
	}
}

func containsPID(pids []int, want int) bool {
	for _, pid := range pids {
		if pid == want {
			return true
		}
	}
	return false
}

func makeProcNode(t *testing.T, proc string, pid int, children string) {
	t.Helper()
	base := filepath.Join(proc, fmt.Sprint(pid))
	if err := os.MkdirAll(filepath.Join(base, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	task := filepath.Join(base, "task", fmt.Sprint(pid))
	if err := os.MkdirAll(task, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task, "children"), []byte(children), 0o644); err != nil {
		t.Fatal(err)
	}
}
