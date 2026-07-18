// Package paradeconversation owns the durable per-slide conversation sidecar
// shared by the dash and fleet-authored reply CLI.
package paradeconversation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	Schema       = 1
	MaxTextRunes = 4000
)

var (
	validKinds       = map[string]bool{"kudos": true, "invest": true, "feedback": true, "note": true}
	slideSeparatorRE = regexp.MustCompile(`(?m)^---\s*$`)
)

// Message is one operator or fleet-seat message on a parade slide.
type Message struct {
	ID     string `json:"id"`
	TS     string `json:"ts"`
	Author string `json:"author"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
}

// Thread stores the latest title snapshot alongside messages for one slide.
type Thread struct {
	Title    string    `json:"title"`
	Messages []Message `json:"messages"`
}

// Document is the archive-local conversations.json document.
type Document struct {
	Schema int               `json:"schema"`
	Slides map[string]Thread `json:"slides"`
}

// PendingOperatorMessage is the latest operator message on a slide that has no
// later fleet-authored reply.
type PendingOperatorMessage struct {
	Slide   int
	Title   string
	Message Message
}

func Empty() Document { return Document{Schema: Schema, Slides: map[string]Thread{}} }

// NewMessage validates and constructs a message. The caller supplies author from
// a trusted identity seam (the operator handler or FLOTILLA_SELF), never request JSON.
func NewMessage(author, kind, text string, now time.Time) (Message, error) {
	author = strings.TrimSpace(author)
	kind = strings.ToLower(strings.TrimSpace(kind))
	text = strings.TrimSpace(text)
	if author == "" {
		return Message{}, errors.New("parade conversation author is required")
	}
	if kind == "" {
		kind = "note"
	}
	if !validKinds[kind] {
		return Message{}, errors.New("message kind must be kudos, invest, feedback, or note")
	}
	if text == "" {
		return Message{}, errors.New("message text is required")
	}
	if utf8.RuneCountInString(text) > MaxTextRunes {
		return Message{}, fmt.Errorf("message text exceeds %d characters", MaxTextRunes)
	}
	id, err := newMessageID()
	if err != nil {
		return Message{}, err
	}
	return Message{ID: id, TS: now.UTC().Format(time.RFC3339), Author: author, Kind: kind, Text: text}, nil
}

func ValidKind(kind string) bool { return validKinds[strings.ToLower(strings.TrimSpace(kind))] }

// Load reads one conversation sidecar. An absent file is an honest empty document.
func Load(path string) (Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Empty(), nil
		}
		return Document{}, fmt.Errorf("read parade conversations: %w", err)
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Document{}, fmt.Errorf("decode parade conversations: %w", err)
	}
	if doc.Schema != Schema {
		return Document{}, fmt.Errorf("parade conversations: unsupported schema %d", doc.Schema)
	}
	if doc.Slides == nil {
		doc.Slides = map[string]Thread{}
	}
	return doc, nil
}

// Append serializes read-modify-write across dash and CLI processes with a
// kernel advisory lock, then atomically replaces the sidecar.
func Append(path string, index int, title string, message Message) (Thread, error) {
	if index < 0 {
		return Thread{}, errors.New("parade conversation slide must be non-negative")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Thread{}, fmt.Errorf("open parade conversation lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Thread{}, fmt.Errorf("lock parade conversations: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck -- close also releases

	doc, err := Load(path)
	if err != nil {
		return Thread{}, err
	}
	key := strconv.Itoa(index)
	thread := doc.Slides[key]
	thread.Title = title
	thread.Messages = append(thread.Messages, message)
	doc.Slides[key] = thread
	if err := writeAtomic(path, doc); err != nil {
		return Thread{}, err
	}
	return thread, nil
}

func writeAtomic(path string, doc Document) error {
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode parade conversations: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".conversations-*.tmp")
	if err != nil {
		return fmt.Errorf("create parade conversations temp file: %w", err)
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
		return fmt.Errorf("chmod parade conversations temp file: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write parade conversations temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync parade conversations temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close parade conversations temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace parade conversations: %w", err)
	}
	ok = true
	return nil
}

// SlideTitles returns authored slide titles from slides.md (or the legacy report.md).
func SlideTitles(dir, fallbackTitle string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "slides.md"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if os.IsNotExist(err) {
		raw, err = os.ReadFile(filepath.Join(dir, "report.md"))
	}
	if err != nil {
		if os.IsNotExist(err) {
			return []string{fallbackTitle}, nil
		}
		return nil, err
	}
	chunks := slideSeparatorRE.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), -1)
	titles := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		line := strings.SplitN(chunk, "\n", 2)[0]
		titles = append(titles, strings.TrimSpace(strings.TrimLeft(line, "#")))
	}
	if len(titles) == 0 {
		return []string{fallbackTitle}, nil
	}
	return titles, nil
}

// LatestAgentReplies maps each slide to its newest non-operator message ID.
func LatestAgentReplies(doc Document) map[string]string {
	out := map[string]string{}
	for slide, thread := range doc.Slides {
		for i := len(thread.Messages) - 1; i >= 0; i-- {
			message := thread.Messages[i]
			if !strings.EqualFold(strings.TrimSpace(message.Author), "operator") {
				out[slide] = message.ID
				break
			}
		}
	}
	return out
}

// UnansweredOperatorMessages returns one pending item per slide when the newest
// operator message is later than the newest fleet-authored reply.
func UnansweredOperatorMessages(doc Document) []PendingOperatorMessage {
	var out []PendingOperatorMessage
	for rawSlide, thread := range doc.Slides {
		var pending *Message
		for i := range thread.Messages {
			message := thread.Messages[i]
			if strings.EqualFold(strings.TrimSpace(message.Author), "operator") {
				copy := message
				pending = &copy
			} else {
				pending = nil
			}
		}
		if pending == nil {
			continue
		}
		slide, err := strconv.Atoi(rawSlide)
		if err != nil || slide < 0 {
			continue
		}
		out = append(out, PendingOperatorMessage{Slide: slide, Title: thread.Title, Message: *pending})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slide < out[j].Slide })
	return out
}

func newMessageID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate parade message id: %w", err)
	}
	return "pm-" + hex.EncodeToString(raw[:]), nil
}
