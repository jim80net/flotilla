package discord

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostWithAttachmentsMultipartShape(t *testing.T) {
	const agent = "xo"
	var (
		gotUser    string
		gotContent string
		gotNames   []string
		gotBodies  [][]byte
		gotCT      string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(gotCT)
		if err != nil || mediaType != "multipart/form-data" {
			t.Errorf("Content-Type = %q, want multipart/form-data", gotCT)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read part: %v", err)
			}
			name := part.FormName()
			data, _ := io.ReadAll(part)
			switch name {
			case "payload_json":
				var p webhookPayload
				if err := json.Unmarshal(data, &p); err != nil {
					t.Fatalf("payload_json: %v", err)
				}
				gotUser, gotContent = p.Username, p.Content
			default:
				if strings.HasPrefix(name, "files[") {
					gotNames = append(gotNames, part.FileName())
					gotBodies = append(gotBodies, data)
				}
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	dir := t.TempDir()
	f1 := filepath.Join(dir, "report.html")
	f2 := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(f1, []byte("<html>proto</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("line two"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := PostWithAttachments(srv.URL, agent, "here", []string{f1, f2}); err != nil {
		t.Fatalf("PostWithAttachments: %v", err)
	}
	if gotUser != agent {
		t.Errorf("username = %q, want %q", gotUser, agent)
	}
	if gotContent != "here" {
		t.Errorf("content = %q, want %q", gotContent, "here")
	}
	if len(gotNames) != 2 || gotNames[0] != "report.html" || gotNames[1] != "notes.txt" {
		t.Errorf("filenames = %v, want [report.html notes.txt]", gotNames)
	}
	if string(gotBodies[0]) != "<html>proto</html>" || string(gotBodies[1]) != "line two" {
		t.Errorf("file bodies wrong: %q / %q", gotBodies[0], gotBodies[1])
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data;") {
		t.Errorf("Content-Type = %q, want multipart", gotCT)
	}
}

func TestOpenAttachmentsRejectsMissing(t *testing.T) {
	_, err := OpenAttachments([]string{filepath.Join(t.TempDir(), "nope.bin")})
	if err == nil {
		t.Fatal("OpenAttachments(missing) = nil, want error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention not found", err.Error())
	}
}

func TestOpenAttachmentsRejectsOversize(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, MaxAttachmentBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := OpenAttachments([]string{big})
	if err == nil {
		t.Fatal("OpenAttachments(oversize) = nil, want error")
	}
	if !strings.Contains(err.Error(), "exceeds Discord limit") {
		t.Errorf("error %q should cite size limit", err.Error())
	}
}

func TestPostWithAttachmentsFailsClosedBeforePost(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := PostWithAttachments(srv.URL, "xo", "hi", []string{filepath.Join(t.TempDir(), "missing.txt")})
	if err == nil {
		t.Fatal("PostWithAttachments(bad path) = nil, want error")
	}
	if hits != 0 {
		t.Errorf("server received %d requests; bad attachment must post NOTHING", hits)
	}
}
