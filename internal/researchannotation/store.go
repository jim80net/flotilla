// Package researchannotation owns the host-private research annotation sidecar.
// It never reads or writes source Markdown; callers supply stable document IDs,
// content digests, and anchors derived from the current research document.
package researchannotation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	Schema            = 1
	MaxStoreBytes     = 16 << 20
	MaxQuoteRunes     = 2000
	MaxContextRunes   = 256
	MaxCommentRunes   = 4000
	MaxAnnotationsDoc = 5000
	MaxCommentsThread = 500
	storeFileName     = "annotations.json"
	lockFileName      = ".annotations.lock"
)

var (
	ErrConflict       = errors.New("research annotation generation conflict")
	ErrInvalid        = errors.New("invalid research annotation")
	ErrUnsafeStorage  = errors.New("unsafe research annotation storage")
	ErrStoreTooLarge  = errors.New("research annotation store is too large")
	ErrMalformedStore = errors.New("malformed research annotation store")
)

// Anchor is a durable text-quote selector. Start and End are Unicode rune
// offsets into the document content and are only a fast path; Quote plus the
// bounded Prefix/Suffix context remain the durable identity.
type Anchor struct {
	Quote  string `json:"quote"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
}

type Comment struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

type Annotation struct {
	ID             string    `json:"id"`
	DocumentID     string    `json:"document_id"`
	DocumentDigest string    `json:"document_digest"`
	Anchor         *Anchor   `json:"anchor,omitempty"`
	Author         string    `json:"author"`
	CreatedAt      string    `json:"created_at"`
	UpdatedAt      string    `json:"updated_at"`
	Comments       []Comment `json:"comments"`
	Resolved       bool      `json:"resolved"`
	ResolvedAt     string    `json:"resolved_at,omitempty"`
	Routing        Routing   `json:"routing"`
}

const (
	RouteSaved     = "saved"
	RouteQueued    = "queued"
	RouteDelivered = "delivered"
)

// Routing records what is proven about the annotation's product-coordination
// handoff. "Saved" means only the private annotation sidecar is durable;
// "queued" means an outbox entry is durable; "delivered" means pane submission
// was confirmed. Assignment is deliberately absent: routing is not ownership.
type Routing struct {
	State      string `json:"state"`
	Key        string `json:"key"`
	Generation uint64 `json:"generation"`
	Target     string `json:"target,omitempty"`
	QueuedID   string `json:"queued_id,omitempty"`
	UpdatedAt  string `json:"updated_at"`
}

type Document struct {
	DocumentID  string       `json:"document_id"`
	Generation  uint64       `json:"generation"`
	Annotations []Annotation `json:"annotations"`
}

type Store struct {
	Schema    int                 `json:"schema"`
	Documents map[string]Document `json:"documents"`
}

type CreateInput struct {
	DocumentID         string
	DocumentDigest     string
	ExpectedGeneration uint64
	Anchor             *Anchor
	Author             string
	Comment            string
	Now                time.Time
}

// ReplyInput records an explicit owner response on an existing annotation.
// ExpectedGeneration protects annotation creation and replies with the same
// document-local compare-and-swap used by Create.
type ReplyInput struct {
	DocumentID         string
	AnnotationID       string
	ExpectedGeneration uint64
	Author             string
	Comment            string
	Resolve            bool
	Now                time.Time
}

func Empty() Store { return Store{Schema: Schema, Documents: map[string]Document{}} }

func EmptyDocument(id string) Document {
	return Document{DocumentID: id, Annotations: []Annotation{}}
}

func ValidDocumentID(id string) bool {
	if id == "" || strings.ContainsRune(id, '\x00') || strings.Contains(id, `\`) ||
		strings.HasPrefix(id, "/") || path.Clean(id) != id || !strings.EqualFold(path.Ext(id), ".md") {
		return false
	}
	for _, part := range strings.Split(id, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func ValidDigest(digest string) bool {
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	return err == nil
}

func StorePath(root string) string { return filepath.Join(root, storeFileName) }

// Load returns an honest empty store when the private directory or sidecar does
// not exist. Existing storage must be private, non-symlink, regular, bounded,
// schema-valid JSON.
func Load(root string) (Store, error) {
	if err := validateRoot(root, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Empty(), nil
		}
		return Store{}, err
	}
	raw, err := readRegularNoFollow(StorePath(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Empty(), nil
		}
		return Store{}, err
	}
	return decodeStore(raw)
}

func LoadDocument(root, id string) (Document, error) {
	if !ValidDocumentID(id) {
		return Document{}, fmt.Errorf("%w: invalid document id", ErrInvalid)
	}
	store, err := Load(root)
	if err != nil {
		return Document{}, err
	}
	doc, ok := store.Documents[id]
	if !ok {
		return EmptyDocument(id), nil
	}
	return doc, nil
}

// Create performs a document-local generation compare-and-swap under a kernel
// lock, then atomically replaces the regular sidecar. A losing concurrent write
// receives ErrConflict and can reload/retry; it is never silently overwritten.
func Create(root string, in CreateInput) (Document, Annotation, error) {
	if err := validateCreate(in); err != nil {
		return Document{}, Annotation{}, err
	}
	if err := validateRoot(root, true); err != nil {
		return Document{}, Annotation{}, err
	}
	lock, err := openRegularNoFollow(filepath.Join(root, lockFileName), syscall.O_CREAT|syscall.O_RDWR, 0o600)
	if err != nil {
		return Document{}, Annotation{}, fmt.Errorf("%w: open lock: %v", ErrUnsafeStorage, err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Document{}, Annotation{}, fmt.Errorf("lock research annotations: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck -- close also releases

	store, err := Load(root)
	if err != nil {
		return Document{}, Annotation{}, err
	}
	doc, ok := store.Documents[in.DocumentID]
	if !ok {
		doc = EmptyDocument(in.DocumentID)
	}
	if doc.Generation != in.ExpectedGeneration {
		return doc, Annotation{}, ErrConflict
	}
	if doc.Generation == ^uint64(0) {
		return Document{}, Annotation{}, fmt.Errorf("%w: document generation exhausted", ErrMalformedStore)
	}
	if len(doc.Annotations) >= MaxAnnotationsDoc {
		return Document{}, Annotation{}, fmt.Errorf("%w: document annotation limit reached", ErrInvalid)
	}
	now := in.Now.UTC().Format(time.RFC3339Nano)
	annotationID, err := newID("ra_")
	if err != nil {
		return Document{}, Annotation{}, err
	}
	commentID, err := newID("rc_")
	if err != nil {
		return Document{}, Annotation{}, err
	}
	annotation := Annotation{
		ID: annotationID, DocumentID: in.DocumentID, DocumentDigest: in.DocumentDigest,
		Anchor: cloneAnchor(in.Anchor), Author: in.Author, CreatedAt: now, UpdatedAt: now,
		Comments: []Comment{{ID: commentID, Author: in.Author, Text: in.Comment, CreatedAt: now}},
	}
	doc.Annotations = append(doc.Annotations, annotation)
	doc.Generation++
	annotation.Routing = Routing{
		State: RouteSaved, Key: RouteKey(annotation.ID, doc.Generation),
		Generation: doc.Generation, UpdatedAt: now,
	}
	doc.Annotations[len(doc.Annotations)-1] = annotation
	store.Documents[in.DocumentID] = doc
	if err := writeAtomic(root, store); err != nil {
		return Document{}, Annotation{}, err
	}
	return doc, annotation, nil
}

// RouteKey is the stable idempotency identity for the first annotation
// revision. It is included in the routed envelope, so retrying a pending handoff
// collapses onto the existing durable outbox entry.
func RouteKey(annotationID string, generation uint64) string {
	return fmt.Sprintf("%s:%d", annotationID, generation)
}

// UpdateRouting advances one annotation from saved to queued or delivered
// without changing the document generation used by annotation-create CAS. A
// terminal route is idempotent: retries return the already-proven state.
func UpdateRouting(root, documentID, annotationID string, generation uint64, next Routing) (Document, Annotation, error) {
	if !ValidDocumentID(documentID) || annotationID == "" || generation == 0 {
		return Document{}, Annotation{}, fmt.Errorf("%w: invalid route identity", ErrInvalid)
	}
	if err := validateRouting(next, annotationID, generation); err != nil {
		return Document{}, Annotation{}, err
	}
	if err := validateRoot(root, false); err != nil {
		return Document{}, Annotation{}, err
	}
	lock, err := openRegularNoFollow(filepath.Join(root, lockFileName), syscall.O_RDWR, 0o600)
	if err != nil {
		return Document{}, Annotation{}, fmt.Errorf("%w: open lock: %v", ErrUnsafeStorage, err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Document{}, Annotation{}, fmt.Errorf("lock research annotations: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck -- close also releases

	store, err := Load(root)
	if err != nil {
		return Document{}, Annotation{}, err
	}
	doc, ok := store.Documents[documentID]
	if !ok {
		return Document{}, Annotation{}, fmt.Errorf("%w: annotation document not found", ErrInvalid)
	}
	for i := range doc.Annotations {
		annotation := &doc.Annotations[i]
		if annotation.ID != annotationID {
			continue
		}
		if annotation.Routing.Key != RouteKey(annotationID, generation) || annotation.Routing.Generation != generation {
			return Document{}, Annotation{}, fmt.Errorf("%w: route generation changed", ErrConflict)
		}
		if annotation.Routing.State == RouteQueued || annotation.Routing.State == RouteDelivered {
			return doc, *annotation, nil
		}
		annotation.Routing = next
		store.Documents[documentID] = doc
		if err := writeAtomic(root, store); err != nil {
			return Document{}, Annotation{}, err
		}
		return doc, *annotation, nil
	}
	return Document{}, Annotation{}, fmt.Errorf("%w: annotation not found", ErrInvalid)
}

// Reply appends an owner response under the same lock and atomic replacement as
// annotation creation. Repeating an identical author+comment is idempotent.
// Resolving is monotonic: a reply may close a thread, but this command never
// silently reopens one.
func Reply(root string, in ReplyInput) (Document, Annotation, Comment, bool, error) {
	if err := validateReply(in); err != nil {
		return Document{}, Annotation{}, Comment{}, false, err
	}
	if err := validateRoot(root, false); err != nil {
		return Document{}, Annotation{}, Comment{}, false, err
	}
	lock, err := openRegularNoFollow(filepath.Join(root, lockFileName), syscall.O_RDWR, 0o600)
	if err != nil {
		return Document{}, Annotation{}, Comment{}, false, fmt.Errorf("%w: open lock: %v", ErrUnsafeStorage, err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Document{}, Annotation{}, Comment{}, false, fmt.Errorf("lock research annotations: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck -- close also releases

	store, err := Load(root)
	if err != nil {
		return Document{}, Annotation{}, Comment{}, false, err
	}
	doc, ok := store.Documents[in.DocumentID]
	if !ok {
		return Document{}, Annotation{}, Comment{}, false, fmt.Errorf("%w: annotation document not found", ErrInvalid)
	}
	if doc.Generation != in.ExpectedGeneration {
		return doc, Annotation{}, Comment{}, false, ErrConflict
	}
	if doc.Generation == ^uint64(0) {
		return Document{}, Annotation{}, Comment{}, false, fmt.Errorf("%w: document generation exhausted", ErrMalformedStore)
	}
	for i := range doc.Annotations {
		annotation := &doc.Annotations[i]
		if annotation.ID != in.AnnotationID {
			continue
		}
		for _, existing := range annotation.Comments {
			if existing.Author == in.Author && existing.Text == in.Comment {
				if !in.Resolve || annotation.Resolved {
					return doc, *annotation, existing, false, nil
				}
				now := in.Now.UTC().Format(time.RFC3339Nano)
				annotation.Resolved = true
				annotation.ResolvedAt = now
				annotation.UpdatedAt = now
				doc.Generation++
				store.Documents[in.DocumentID] = doc
				if err := writeAtomic(root, store); err != nil {
					return Document{}, Annotation{}, Comment{}, false, err
				}
				return doc, *annotation, existing, false, nil
			}
		}
		if len(annotation.Comments) >= MaxCommentsThread {
			return Document{}, Annotation{}, Comment{}, false, fmt.Errorf("%w: annotation comment limit reached", ErrInvalid)
		}
		commentID, err := newID("rc_")
		if err != nil {
			return Document{}, Annotation{}, Comment{}, false, err
		}
		now := in.Now.UTC().Format(time.RFC3339Nano)
		comment := Comment{ID: commentID, Author: in.Author, Text: in.Comment, CreatedAt: now}
		annotation.Comments = append(annotation.Comments, comment)
		annotation.UpdatedAt = now
		if in.Resolve && !annotation.Resolved {
			annotation.Resolved = true
			annotation.ResolvedAt = now
		}
		doc.Generation++
		store.Documents[in.DocumentID] = doc
		if err := writeAtomic(root, store); err != nil {
			return Document{}, Annotation{}, Comment{}, false, err
		}
		return doc, *annotation, comment, true, nil
	}
	return Document{}, Annotation{}, Comment{}, false, fmt.Errorf("%w: annotation not found", ErrInvalid)
}

func validateCreate(in CreateInput) error {
	if !ValidDocumentID(in.DocumentID) {
		return fmt.Errorf("%w: invalid document id", ErrInvalid)
	}
	if !ValidDigest(in.DocumentDigest) {
		return fmt.Errorf("%w: invalid document digest", ErrInvalid)
	}
	if strings.TrimSpace(in.Author) == "" {
		return fmt.Errorf("%w: author is required", ErrInvalid)
	}
	if strings.TrimSpace(in.Comment) == "" {
		return fmt.Errorf("%w: comment is required", ErrInvalid)
	}
	if utf8.RuneCountInString(in.Comment) > MaxCommentRunes {
		return fmt.Errorf("%w: comment exceeds %d characters", ErrInvalid, MaxCommentRunes)
	}
	if in.Now.IsZero() {
		return fmt.Errorf("%w: timestamp is required", ErrInvalid)
	}
	if in.Anchor != nil {
		if err := ValidateAnchor(*in.Anchor); err != nil {
			return err
		}
	}
	return nil
}

func validateReply(in ReplyInput) error {
	if !ValidDocumentID(in.DocumentID) || strings.TrimSpace(in.AnnotationID) == "" {
		return fmt.Errorf("%w: invalid reply identity", ErrInvalid)
	}
	if strings.TrimSpace(in.Author) == "" || strings.EqualFold(strings.TrimSpace(in.Author), "operator") {
		return fmt.Errorf("%w: an owner author is required", ErrInvalid)
	}
	if strings.TrimSpace(in.Comment) == "" {
		return fmt.Errorf("%w: comment is required", ErrInvalid)
	}
	if utf8.RuneCountInString(in.Comment) > MaxCommentRunes {
		return fmt.Errorf("%w: comment exceeds %d characters", ErrInvalid, MaxCommentRunes)
	}
	if in.Now.IsZero() {
		return fmt.Errorf("%w: timestamp is required", ErrInvalid)
	}
	return nil
}

func ValidateAnchor(anchor Anchor) error {
	if anchor.Quote == "" || utf8.RuneCountInString(anchor.Quote) > MaxQuoteRunes {
		return fmt.Errorf("%w: quote must be 1-%d characters", ErrInvalid, MaxQuoteRunes)
	}
	if utf8.RuneCountInString(anchor.Prefix) > MaxContextRunes || utf8.RuneCountInString(anchor.Suffix) > MaxContextRunes {
		return fmt.Errorf("%w: anchor context exceeds %d characters", ErrInvalid, MaxContextRunes)
	}
	if anchor.Start < 0 || anchor.End < anchor.Start || anchor.End-anchor.Start != utf8.RuneCountInString(anchor.Quote) {
		return fmt.Errorf("%w: malformed anchor offsets", ErrInvalid)
	}
	return nil
}

func decodeStore(raw []byte) (Store, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var store Store
	if err := dec.Decode(&store); err != nil {
		return Store{}, fmt.Errorf("%w: decode: %v", ErrMalformedStore, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Store{}, fmt.Errorf("%w: trailing content", ErrMalformedStore)
	}
	// Schema v1 stores written before routed annotations had no routing member.
	// Treat those records honestly as saved-only; the next successful route write
	// persists the explicit state without changing the public schema.
	for id, doc := range store.Documents {
		for i := range doc.Annotations {
			if doc.Annotations[i].Routing.State == "" {
				doc.Annotations[i].Routing = Routing{
					State: RouteSaved, Key: RouteKey(doc.Annotations[i].ID, doc.Generation),
					Generation: doc.Generation, UpdatedAt: doc.Annotations[i].UpdatedAt,
				}
			}
		}
		store.Documents[id] = doc
	}
	if err := validateStore(store); err != nil {
		return Store{}, err
	}
	return store, nil
}

func validateStore(store Store) error {
	if store.Schema != Schema || store.Documents == nil {
		return fmt.Errorf("%w: unsupported schema or missing documents", ErrMalformedStore)
	}
	seenAnnotations := map[string]bool{}
	seenComments := map[string]bool{}
	for id, doc := range store.Documents {
		if !ValidDocumentID(id) || doc.DocumentID != id || doc.Annotations == nil || len(doc.Annotations) > MaxAnnotationsDoc {
			return fmt.Errorf("%w: invalid document record", ErrMalformedStore)
		}
		for _, annotation := range doc.Annotations {
			if annotation.ID == "" || seenAnnotations[annotation.ID] || annotation.DocumentID != id || !ValidDigest(annotation.DocumentDigest) ||
				annotation.Author == "" || annotation.CreatedAt == "" || annotation.UpdatedAt == "" || len(annotation.Comments) == 0 {
				return fmt.Errorf("%w: invalid annotation record", ErrMalformedStore)
			}
			if _, err := time.Parse(time.RFC3339Nano, annotation.CreatedAt); err != nil {
				return fmt.Errorf("%w: invalid annotation timestamp", ErrMalformedStore)
			}
			if _, err := time.Parse(time.RFC3339Nano, annotation.UpdatedAt); err != nil {
				return fmt.Errorf("%w: invalid annotation timestamp", ErrMalformedStore)
			}
			if annotation.Resolved != (annotation.ResolvedAt != "") {
				return fmt.Errorf("%w: inconsistent resolution state", ErrMalformedStore)
			}
			if annotation.ResolvedAt != "" {
				if _, err := time.Parse(time.RFC3339Nano, annotation.ResolvedAt); err != nil {
					return fmt.Errorf("%w: invalid resolution timestamp", ErrMalformedStore)
				}
			}
			if err := validateRouting(annotation.Routing, annotation.ID, annotation.Routing.Generation); err != nil {
				return fmt.Errorf("%w: invalid annotation routing", ErrMalformedStore)
			}
			seenAnnotations[annotation.ID] = true
			if annotation.Anchor != nil {
				if err := ValidateAnchor(*annotation.Anchor); err != nil {
					return fmt.Errorf("%w: invalid stored anchor", ErrMalformedStore)
				}
			}
			for _, comment := range annotation.Comments {
				if comment.ID == "" || seenComments[comment.ID] || comment.Author == "" || strings.TrimSpace(comment.Text) == "" || comment.CreatedAt == "" ||
					utf8.RuneCountInString(comment.Text) > MaxCommentRunes {
					return fmt.Errorf("%w: invalid comment record", ErrMalformedStore)
				}
				if _, err := time.Parse(time.RFC3339Nano, comment.CreatedAt); err != nil {
					return fmt.Errorf("%w: invalid comment timestamp", ErrMalformedStore)
				}
				seenComments[comment.ID] = true
			}
			if len(annotation.Comments) > MaxCommentsThread {
				return fmt.Errorf("%w: annotation comment limit exceeded", ErrMalformedStore)
			}
		}
	}
	return nil
}

func validateRouting(route Routing, annotationID string, generation uint64) error {
	if generation == 0 || route.Generation != generation || route.Key != RouteKey(annotationID, generation) {
		return fmt.Errorf("%w: invalid route identity", ErrInvalid)
	}
	if route.State != RouteSaved && route.State != RouteQueued && route.State != RouteDelivered {
		return fmt.Errorf("%w: invalid route state", ErrInvalid)
	}
	if route.UpdatedAt == "" {
		return fmt.Errorf("%w: route timestamp is required", ErrInvalid)
	}
	if _, err := time.Parse(time.RFC3339Nano, route.UpdatedAt); err != nil {
		return fmt.Errorf("%w: invalid route timestamp", ErrInvalid)
	}
	if route.State == RouteSaved && (route.Target != "" || route.QueuedID != "") {
		return fmt.Errorf("%w: saved-only route has delivery metadata", ErrInvalid)
	}
	if route.State == RouteQueued && (route.Target == "" || route.QueuedID == "") {
		return fmt.Errorf("%w: queued route is incomplete", ErrInvalid)
	}
	if route.State == RouteDelivered && (route.Target == "" || route.QueuedID != "") {
		return fmt.Errorf("%w: delivered route is incomplete", ErrInvalid)
	}
	return nil
}

func validateRoot(root string, create bool) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("%w: empty storage root", ErrUnsafeStorage)
	}
	info, err := os.Lstat(root)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) || !create {
			return err
		}
		if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create research annotation root: %w", err)
		}
		info, err = os.Lstat(root)
		if err != nil {
			return err
		}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: root must be a private non-symlink directory", ErrUnsafeStorage)
	}
	return nil
}

func readRegularNoFollow(file string) ([]byte, error) {
	f, err := openRegularNoFollow(file, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxStoreBytes {
		return nil, ErrStoreTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxStoreBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxStoreBytes {
		return nil, ErrStoreTooLarge
	}
	return raw, nil
}

func openRegularNoFollow(file string, flags int, perm os.FileMode) (*os.File, error) {
	fd, err := syscall.Open(file, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, uint32(perm.Perm()))
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("%w: symlink is not allowed", ErrUnsafeStorage)
		}
		return nil, err
	}
	f := os.NewFile(uintptr(fd), file)
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%w: file must be private, regular, and non-symlink", ErrUnsafeStorage)
	}
	return f, nil
}

func writeAtomic(root string, store Store) error {
	if err := validateStore(store); err != nil {
		return err
	}
	if info, err := os.Lstat(StorePath(root)); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("%w: destination must be private, regular, and non-symlink", ErrUnsafeStorage)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > MaxStoreBytes {
		return ErrStoreTooLarge
	}
	tmp, err := os.CreateTemp(root, ".annotations-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, StorePath(root)); err != nil {
		return err
	}
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return err
	}
	ok = true
	return nil
}

func cloneAnchor(anchor *Anchor) *Anchor {
	if anchor == nil {
		return nil
	}
	copy := *anchor
	return &copy
}

func newID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw[:]), nil
}
