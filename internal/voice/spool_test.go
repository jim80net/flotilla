package voice

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// isolateSpool points the spool at a per-test temp dir via the FLOTILLA_STATE_ROOT
// override, so tests never touch the real cwd-relative `state/` and never collide.
func isolateSpool(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(stateRootEnv, root)
	return filepath.Join(root, "voice", "outbound")
}

// spoolCount returns how many .txt entries live in the spool dir (the bound's unit).
func spoolCount(t *testing.T) int {
	t.Helper()
	names, err := ListSpool()
	if err != nil {
		t.Fatalf("ListSpool: %v", err)
	}
	return len(names)
}

// WriteSpeak must succeed and leave a readable file even when NOTHING consumes the spool —
// i.e. the `flotilla voice` process is down. This is the never-blocks-the-turn invariant:
// speak's success is independent of voice's liveness.
func TestWriteSpeakSucceedsWithNoConsumer(t *testing.T) {
	isolateSpool(t)

	path, err := WriteSpeak("hello operator")
	if err != nil {
		t.Fatalf("WriteSpeak with no consumer: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written entry %q: %v", path, err)
	}
	if string(body) != "hello operator" {
		t.Errorf("body = %q, want %q", body, "hello operator")
	}
	if got := spoolCount(t); got != 1 {
		t.Errorf("spool holds %d entries, want 1", got)
	}
}

// SpoolDir is created lazily on first write; merely deriving the path must not create it,
// and the first WriteSpeak must MkdirAll it (so `speak` exits 0 even when state/ is absent).
func TestWriteSpeakCreatesSpoolDirLazily(t *testing.T) {
	dir := isolateSpool(t)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("spool dir exists before any write (stat err = %v); want absent", err)
	}
	if _, err := WriteSpeak("first line"); err != nil {
		t.Fatalf("WriteSpeak: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("spool dir not created lazily on write (err = %v)", err)
	}
}

// The spool is BOUNDED: writing more than SpoolMaxFiles entries leaves exactly the cap,
// and the OLDEST are the ones dropped — the newest survive, and the just-written file is
// always present (drop-oldest, never refuse-new).
func TestSpoolBoundedDropsOldest(t *testing.T) {
	isolateSpool(t)

	const overflow = 10
	total := SpoolMaxFiles + overflow
	written := make([]string, 0, total)
	for i := 0; i < total; i++ {
		path, err := WriteSpeak("line " + strconv.Itoa(i))
		if err != nil {
			t.Fatalf("WriteSpeak #%d: %v", i, err)
		}
		written = append(written, filepath.Base(path))

		// After every write the spool is never over the cap, and the file we JUST wrote is
		// always present — overflow must drop the oldest, never the new write.
		names, err := ListSpool()
		if err != nil {
			t.Fatalf("ListSpool after #%d: %v", i, err)
		}
		if len(names) > SpoolMaxFiles {
			t.Fatalf("after %d writes spool holds %d, exceeds cap %d", i+1, len(names), SpoolMaxFiles)
		}
		if !contains(names, filepath.Base(path)) {
			t.Fatalf("just-written entry %q was dropped by the bound", filepath.Base(path))
		}
	}

	// Exactly the cap remains, and it is precisely the NEWEST SpoolMaxFiles writes — the
	// oldest `overflow` were evicted.
	names, err := ListSpool()
	if err != nil {
		t.Fatalf("ListSpool: %v", err)
	}
	if len(names) != SpoolMaxFiles {
		t.Fatalf("spool holds %d entries, want exactly cap %d", len(names), SpoolMaxFiles)
	}
	wantSurvivors := written[overflow:] // newest SpoolMaxFiles, oldest-first
	sort.Strings(names)
	for i, name := range names {
		if name != wantSurvivors[i] {
			t.Errorf("survivor[%d] = %q, want %q (newest should survive, oldest dropped)", i, name, wantSurvivors[i])
		}
	}
	// And the evicted ones (oldest `overflow`) are gone.
	for _, dropped := range written[:overflow] {
		if contains(names, dropped) {
			t.Errorf("oldest entry %q survived; should have been dropped", dropped)
		}
	}
}

// The consume API lists entries OLDEST-FIRST (chronological), reads a body, and deletes
// EXACTLY ONE — the watch→consume→delete loop the `flotilla voice` process runs.
func TestConsumeOrderingAndDeleteOne(t *testing.T) {
	isolateSpool(t)

	bodies := []string{"first", "second", "third"}
	for _, b := range bodies {
		if _, err := WriteSpeak(b); err != nil {
			t.Fatalf("WriteSpeak(%q): %v", b, err)
		}
	}

	names, err := ListSpool()
	if err != nil {
		t.Fatalf("ListSpool: %v", err)
	}
	if len(names) != len(bodies) {
		t.Fatalf("listed %d entries, want %d", len(names), len(bodies))
	}
	// Oldest-first: reading entries in list order must reproduce the write order.
	for i, name := range names {
		body, err := ReadSpool(name)
		if err != nil {
			t.Fatalf("ReadSpool(%q): %v", name, err)
		}
		if body != bodies[i] {
			t.Errorf("entry[%d] body = %q, want %q (not oldest-first)", i, body, bodies[i])
		}
	}

	// DeleteSpool removes exactly one (the oldest), leaving the rest intact and still ordered.
	if err := DeleteSpool(names[0]); err != nil {
		t.Fatalf("DeleteSpool(%q): %v", names[0], err)
	}
	after, err := ListSpool()
	if err != nil {
		t.Fatalf("ListSpool after delete: %v", err)
	}
	if len(after) != len(bodies)-1 {
		t.Fatalf("after deleting one, %d entries remain, want %d", len(after), len(bodies)-1)
	}
	if contains(after, names[0]) {
		t.Errorf("deleted entry %q still present", names[0])
	}
	// DeleteSpool is idempotent: deleting an already-gone entry is not an error.
	if err := DeleteSpool(names[0]); err != nil {
		t.Errorf("second DeleteSpool of an absent entry returned %v, want nil (idempotent)", err)
	}
}

// Two writes "at once" must produce two DISTINCT files — never one overwriting the other
// (a same-nanosecond collision would silently lose a spoken line). We hammer concurrent
// writes and assert no entry is lost.
func TestWriteSpeakCollisionSafety(t *testing.T) {
	isolateSpool(t)

	// Stay at the cap so the bound never evicts mid-test (eviction would confound the
	// "no entry lost" count). SpoolMaxFiles concurrent writes still heavily exercises the
	// same-nanosecond collision window the random suffix exists to defend.
	n := SpoolMaxFiles
	var wg sync.WaitGroup
	paths := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			paths[i], errs[i] = WriteSpeak("concurrent " + strconv.Itoa(i))
		}(i)
	}
	wg.Wait()

	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent WriteSpeak #%d: %v", i, errs[i])
		}
		if _, dup := seen[paths[i]]; dup {
			t.Fatalf("two writes produced the same path %q (collision/overwrite)", paths[i])
		}
		seen[paths[i]] = struct{}{}
	}
	if got := spoolCount(t); got != n {
		t.Errorf("after %d concurrent writes, spool holds %d entries — some were overwritten", n, got)
	}
}

// The collision-safety guarantee is the random suffix: even when the nanosecond stamp is
// IDENTICAL, two spoolName calls must produce distinct filenames (else a same-nanosecond
// write would overwrite and silently lose a spoken line). The concurrent WriteSpeak test
// above can't force a true same-nanosecond hit; this one pins the exact instant.
func TestSpoolNameDistinctAtSameInstant(t *testing.T) {
	instant := time.Unix(0, 1_700_000_000_000_000_000) // a single, fixed nanosecond
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		name, err := spoolName(instant)
		if err != nil {
			t.Fatalf("spoolName: %v", err)
		}
		if _, dup := seen[name]; dup {
			t.Fatalf("spoolName collided at a fixed instant: %q (random suffix failed to disambiguate)", name)
		}
		seen[name] = struct{}{}
	}
}

// spoolName must zero-pad the nanosecond prefix to a fixed width AND carry the entry
// extension, so a lexical sort of filenames is a chronological sort (the property
// ListSpool/trimSpool rely on for oldest-first ordering and deterministic eviction).
func TestSpoolNameSortsChronologically(t *testing.T) {
	// Distinct increasing nanosecond stamps must yield lexically-increasing names.
	names := make([]string, 0, 64)
	for i := int64(0); i < 64; i++ {
		// time.Unix(0, ns) gives a deterministic instant; spacing them apart guarantees
		// distinct, monotonically-increasing nanosecond prefixes.
		name, err := spoolName(time.Unix(0, i*1_000))
		if err != nil {
			t.Fatalf("spoolName: %v", err)
		}
		if !strings.HasSuffix(name, spoolEntryExt) {
			t.Errorf("name %q lacks the %q extension", name, spoolEntryExt)
		}
		prefix := strings.SplitN(name, "-", 2)[0]
		if len(prefix) != spoolNanoWidth {
			t.Errorf("nanos prefix %q has width %d, want fixed %d (lexical sort breaks otherwise)", prefix, len(prefix), spoolNanoWidth)
		}
		names = append(names, name)
	}
	if !sort.StringsAreSorted(names) {
		t.Error("names from increasing timestamps are not lexically sorted — chronological-sort invariant violated")
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
