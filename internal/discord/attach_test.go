package discord

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPostWithAttachmentsMultipartShape(t *testing.T) {
	const agent = "xo"
	var (
		gotUser     string
		gotContent  string
		gotNames    []string
		gotBodies   [][]byte
		gotCT       string
		requestHits int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHits++
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
	if len(gotBodies) != 2 {
		t.Fatalf("file parts = %d, want 2", len(gotBodies))
	}
	if string(gotBodies[0]) != "<html>proto</html>" || string(gotBodies[1]) != "line two" {
		t.Errorf("file bodies wrong: %q / %q", gotBodies[0], gotBodies[1])
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data;") {
		t.Errorf("Content-Type = %q, want multipart", gotCT)
	}
	if requestHits != 1 {
		t.Errorf("small-file fast path requests = %d, want exactly 1", requestHits)
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

func TestOpenAttachmentsRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := OpenAttachments([]string{dir})
	if err == nil {
		t.Fatal("OpenAttachments(directory) = nil, want error")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("error %q should mention directory", err.Error())
	}
}

func TestOpenAttachmentsRejectsNonRegular(t *testing.T) {
	_, err := OpenAttachments([]string{"/dev/zero"})
	if err == nil {
		t.Fatal("OpenAttachments(/dev/zero) = nil, want error")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("error %q should reject non-regular files", err.Error())
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

func TestPostWithAttachmentsRetriesTimeoutThenTranscodes(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "evidence.png")
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 96, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 96; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 2), G: uint8(y * 3), B: 80, A: 255})
		}
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	var originalHits, fallbackHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if parseErr != nil || mediaType != "multipart/form-data" {
			t.Errorf("Content-Type = %q: %v", r.Header.Get("Content-Type"), parseErr)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		filename := ""
		for {
			part, nextErr := mr.NextPart()
			if nextErr == io.EOF {
				break
			}
			if nextErr != nil {
				t.Errorf("multipart: %v", nextErr)
				return
			}
			if part.FileName() != "" {
				filename = part.FileName()
			}
			_, _ = io.Copy(io.Discard, part)
		}
		if strings.HasSuffix(filename, ".png") {
			originalHits.Add(1)
			time.Sleep(40 * time.Millisecond) // longer than the injected request budget
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.Contains(filename, "-downscaled-") && strings.HasSuffix(filename, ".jpg") {
			fallbackHits.Add(1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("unexpected attachment filename %q", filename)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	err = postWithAttachments(srv.URL, "127.0.0.1", "xo", "evidence", []string{pngPath}, attachmentPostOptions{
		client:    &http.Client{Timeout: 10 * time.Millisecond},
		attempts:  2,
		backoff:   func(int) {},
		transcode: transcodeImageAttachments,
	})
	if err != nil {
		t.Fatalf("postWithAttachments retry/transcode: %v", err)
	}
	if got := originalHits.Load(); got != 2 {
		t.Errorf("original PNG attempts = %d, want 2", got)
	}
	if got := fallbackHits.Load(); got != 1 {
		t.Errorf("downscaled JPEG attempts = %d, want 1", got)
	}
}

func TestNotifyAttachmentTimeoutConfig(t *testing.T) {
	t.Setenv("FLOTILLA_NOTIFY_ATTACH_TIMEOUT", "45s")
	if got, err := notifyAttachmentTimeout(); err != nil || got != 45*time.Second {
		t.Fatalf("notifyAttachmentTimeout = (%s, %v), want 45s", got, err)
	}
	t.Setenv("FLOTILLA_NOTIFY_ATTACH_TIMEOUT", "not-a-duration")
	if _, err := notifyAttachmentTimeout(); err == nil {
		t.Fatal("invalid configured timeout must fail loudly")
	}
}

func TestTranscodeImageAttachmentsBoundsDimensionsAndSize(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "wide.png")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1800, 900))
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	paths, cleanup, err := transcodeImageAttachments([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	if cleanup == nil {
		t.Fatal("transcode must return temporary-file cleanup")
	}
	defer cleanup()
	if len(paths) != 1 || !strings.HasSuffix(paths[0], ".jpg") {
		t.Fatalf("fallback paths = %v, want one JPEG", paths)
	}
	info, err := os.Stat(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > fallbackImageMaxBytes {
		t.Fatalf("fallback size = %d, limit %d", info.Size(), fallbackImageMaxBytes)
	}
	out, err := os.Open(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(out)
	_ = out.Close()
	if err != nil || format != "jpeg" {
		t.Fatalf("fallback decode = (%s, %v)", format, err)
	}
	if config.Width > fallbackImageMaxDimension || config.Height > fallbackImageMaxDimension {
		t.Fatalf("fallback dimensions = %dx%d, max %d", config.Width, config.Height, fallbackImageMaxDimension)
	}
}
