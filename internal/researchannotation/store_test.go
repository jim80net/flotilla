package researchannotation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func validInput() CreateInput {
	return CreateInput{
		DocumentID: "notes/field.md", DocumentDigest: testDigest,
		Author: "operator", Comment: "private note", Now: time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC),
	}
}

func TestResolveUsesRuneOffsetsThenUniqueContext(t *testing.T) {
	text := "Intro café target sentence.\n"
	anchor := Anchor{Quote: "café target", Prefix: "Intro ", Suffix: " sentence", Start: 6, End: 17}
	if got := Resolve(text, anchor); got.State != AnchorAttached || got.Start != 6 || got.End != 17 {
		t.Fatalf("fast resolution = %+v", got)
	}

	changed := "New. " + text
	if got := Resolve(changed, anchor); got.State != AnchorAttached || got.Start != 11 || got.End != 22 {
		t.Fatalf("reanchored resolution = %+v", got)
	}
	if got := Reanchor("target and target", Anchor{Quote: "target", Start: 0, End: 6}); got.State != AnchorNeedsReview {
		t.Fatalf("ambiguous resolution = %+v", got)
	}
	if got := Reanchor("left target / right target", Anchor{Quote: "target", Prefix: "right ", Start: 0, End: 6}); got.State != AnchorAttached || got.Start != 20 {
		t.Fatalf("context resolution = %+v", got)
	}
}

func TestCreateIsAtomicPrivateAndCASProtected(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	in := validInput()
	in.Comment = `<script>alert("private")</script>`
	doc, created, err := Create(root, in)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Generation != 1 || len(doc.Annotations) != 1 || created.ID == "" || created.Comments[0].ID == "" {
		t.Fatalf("create result = %+v / %+v", doc, created)
	}
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{{root, 0o700}, {StorePath(root), 0o600}, {filepath.Join(root, lockFileName), 0o600}} {
		info, statErr := os.Lstat(item.path)
		if statErr != nil || info.Mode().Perm() != item.mode || (item.path != root && !info.Mode().IsRegular()) {
			t.Fatalf("unsafe mode/type for %s: info=%v err=%v", item.path, info, statErr)
		}
	}
	raw, err := os.ReadFile(StorePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "<script>") || !strings.Contains(string(raw), `\u003cscript\u003e`) {
		t.Fatalf("stored JSON did not HTML-escape untrusted text: %s", raw)
	}
	temps, _ := filepath.Glob(filepath.Join(root, ".annotations-*.tmp"))
	if len(temps) != 0 {
		t.Fatalf("temporary files remain: %v", temps)
	}

	if current, _, err := Create(root, in); !errors.Is(err, ErrConflict) || current.Generation != 1 {
		t.Fatalf("stale CAS = generation %d, err %v", current.Generation, err)
	}
	in.ExpectedGeneration = 1
	if current, _, err := Create(root, in); err != nil || current.Generation != 2 {
		t.Fatalf("successor CAS = generation %d, err %v", current.Generation, err)
	}
}

func TestConcurrentCreateHasOneWinner(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := Create(root, validInput())
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	doc, err := LoadDocument(root, "notes/field.md")
	if err != nil || wins != 1 || conflicts != 1 || doc.Generation != 1 || len(doc.Annotations) != 1 {
		t.Fatalf("wins=%d conflicts=%d doc=%+v err=%v", wins, conflicts, doc, err)
	}
}

func TestStorageRejectsUnsafeAndOversizedFiles(t *testing.T) {
	t.Run("symlink root", func(t *testing.T) {
		target := t.TempDir()
		root := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(target, root); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Create(root, validInput()); !errors.Is(err, ErrUnsafeStorage) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("public root", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Create(root, validInput()); !errors.Is(err, ErrUnsafeStorage) {
			t.Fatalf("error = %v", err)
		}
	})
	for _, kind := range []string{"symlink", "directory", "public"} {
		t.Run(kind+" store", func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				if err := os.Symlink(filepath.Join(root, "missing"), StorePath(root)); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(StorePath(root), 0o700); err != nil {
					t.Fatal(err)
				}
			case "public":
				if err := os.WriteFile(StorePath(root), []byte("{}"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load(root); !errors.Is(err, ErrUnsafeStorage) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	t.Run("oversized", func(t *testing.T) {
		root := t.TempDir()
		_ = os.Chmod(root, 0o700)
		f, err := os.OpenFile(StorePath(root), os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(MaxStoreBytes + 1); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		if _, err := Load(root); !errors.Is(err, ErrStoreTooLarge) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("symlink lock", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, lockFileName)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Create(root, validInput()); !errors.Is(err, ErrUnsafeStorage) {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestValidationRejectsTraversalLimitsAndMalformedStore(t *testing.T) {
	for _, id := range []string{"../secret.md", "notes/../../secret.md", `/abs.md`, `notes\\secret.md`, ".hidden.md", "notes/.hidden.md", "notes.txt"} {
		if ValidDocumentID(id) {
			t.Errorf("accepted unsafe id %q", id)
		}
	}
	for name, mutate := range map[string]func(*CreateInput){
		"bad digest":    func(in *CreateInput) { in.DocumentDigest = "nope" },
		"large comment": func(in *CreateInput) { in.Comment = strings.Repeat("x", MaxCommentRunes+1) },
		"large quote": func(in *CreateInput) {
			in.Anchor = &Anchor{Quote: strings.Repeat("x", MaxQuoteRunes+1), End: MaxQuoteRunes + 1}
		},
		"bad offsets": func(in *CreateInput) { in.Anchor = &Anchor{Quote: "x", Start: 2, End: 2} },
	} {
		t.Run(name, func(t *testing.T) {
			in := validInput()
			mutate(&in)
			if _, _, err := Create(filepath.Join(t.TempDir(), "private"), in); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	for _, raw := range []string{`{}`, `{"schema":2,"documents":{}}`, `{"schema":1,"documents":{}} trailing`} {
		root := t.TempDir()
		_ = os.Chmod(root, 0o700)
		if err := os.WriteFile(StorePath(root), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root); !errors.Is(err, ErrMalformedStore) {
			t.Errorf("raw %q: error = %v", raw, err)
		}
	}
	t.Run("malformed generation", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "private")
		if _, _, err := Create(root, validInput()); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(StorePath(root))
		if err != nil {
			t.Fatal(err)
		}
		raw = []byte(strings.Replace(string(raw), `"generation": 1`, `"generation": -1`, 1))
		if err := os.WriteFile(StorePath(root), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(root); !errors.Is(err, ErrMalformedStore) {
			t.Fatalf("error = %v", err)
		}
	})
}
