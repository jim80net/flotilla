package discord

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func attachmentResponse(status int, retryAfter string) *http.Response {
	header := make(http.Header)
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("rejected")),
	}
}

func TestPostWithAttachmentsRetriesExplicit5xxThenTranscodes(t *testing.T) {
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

	originalHits, fallbackHits := 0, 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mediaType, params, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if parseErr != nil || mediaType != "multipart/form-data" {
			t.Fatalf("Content-Type = %q: %v", r.Header.Get("Content-Type"), parseErr)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		filename := ""
		for {
			part, nextErr := mr.NextPart()
			if nextErr == io.EOF {
				break
			}
			if nextErr != nil {
				t.Fatalf("multipart: %v", nextErr)
			}
			if part.FileName() != "" {
				filename = part.FileName()
			}
			_, _ = io.Copy(io.Discard, part)
		}
		if strings.HasSuffix(filename, ".png") {
			originalHits++
			return attachmentResponse(http.StatusInternalServerError, ""), nil
		}
		if strings.Contains(filename, "-downscaled-") && strings.HasSuffix(filename, ".jpg") {
			fallbackHits++
			return attachmentResponse(http.StatusNoContent, ""), nil
		}
		t.Fatalf("unexpected attachment filename %q", filename)
		return nil, nil
	})}

	err = postWithAttachments("https://discord.invalid/hook", "discord.invalid", "xo", "evidence", []string{pngPath}, attachmentPostOptions{
		client:    client,
		attempts:  2,
		backoff:   func(int) time.Duration { return 0 },
		wait:      func(time.Duration) {},
		transcode: transcodeImageAttachments,
	})
	if err != nil {
		t.Fatalf("postWithAttachments retry/transcode: %v", err)
	}
	if originalHits != 2 {
		t.Errorf("original PNG attempts = %d, want 2", originalHits)
	}
	if fallbackHits != 1 {
		t.Errorf("downscaled JPEG attempts = %d, want 1", fallbackHits)
	}
}

func TestPostWithAttachmentsAmbiguousTransportIsNeverReplayed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.txt")
	if err := os.WriteFile(path, []byte("evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	posts, transcodes := 0, 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		posts++ // model a server accepting the body before its response is lost
		return nil, errors.New("response lost")
	})}
	err := postWithAttachments("https://discord.invalid/hook", "discord.invalid", "xo", "evidence", []string{path}, attachmentPostOptions{
		client:   client,
		attempts: 3,
		backoff:  func(int) time.Duration { return time.Millisecond },
		wait:     func(time.Duration) { t.Fatal("ambiguous transport must not back off for replay") },
		transcode: func([]string) ([]string, func(), error) {
			transcodes++
			return nil, nil, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "delivery status unknown") || !strings.Contains(err.Error(), "not replayed") {
		t.Fatalf("ambiguous error = %v", err)
	}
	if posts != 1 || transcodes != 0 {
		t.Fatalf("posts=%d transcodes=%d, want at-most-once post and no fallback", posts, transcodes)
	}
}

func TestPostAttachmentAttemptsRetryPolicyAndRetryAfter(t *testing.T) {
	cases := []struct {
		name       string
		statuses   []int
		retryAfter string
		wantPosts  int
		wantWait   time.Duration
	}{
		{"400 is terminal", []int{http.StatusBadRequest}, "", 1, 0},
		{"429 honors decimal seconds", []int{http.StatusTooManyRequests, http.StatusNoContent}, "1.5", 2, 1500 * time.Millisecond},
		{"429 honors HTTP date", []int{http.StatusTooManyRequests, http.StatusNoContent}, "Thu, 01 Jan 1970 00:00:02 GMT", 2, 2 * time.Second},
		{"429 malformed uses backoff", []int{http.StatusTooManyRequests, http.StatusNoContent}, "later", 2, 77 * time.Millisecond},
		{"429 missing uses backoff", []int{http.StatusTooManyRequests, http.StatusNoContent}, "", 2, 77 * time.Millisecond},
		{"500 retries", []int{http.StatusInternalServerError, http.StatusNoContent}, "", 2, 77 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			var waits []time.Duration
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				status := tc.statuses[min(posts, len(tc.statuses)-1)]
				posts++
				return attachmentResponse(status, tc.retryAfter), nil
			})}
			err := postAttachmentAttempts(attachmentPostOptions{
				client:   client,
				attempts: 3,
				backoff:  func(int) time.Duration { return 77 * time.Millisecond },
				wait:     func(d time.Duration) { waits = append(waits, d) },
				now:      func() time.Time { return time.Unix(0, 0) },
			}, "https://discord.invalid/hook", "discord.invalid", []byte("body"), "text/plain")
			if tc.statuses[len(tc.statuses)-1] == http.StatusNoContent && err != nil {
				t.Fatalf("attempts: %v", err)
			}
			if posts != tc.wantPosts {
				t.Fatalf("posts=%d, want %d", posts, tc.wantPosts)
			}
			if tc.wantWait == 0 {
				if len(waits) != 0 {
					t.Fatalf("waits=%v, want none", waits)
				}
			} else if len(waits) != 1 || waits[0] != tc.wantWait {
				t.Fatalf("waits=%v, want [%s]", waits, tc.wantWait)
			}
		})
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

func TestTranscodeRejectsOversizedDimensionsBeforeDecode(t *testing.T) {
	source := filepath.Join(t.TempDir(), "huge-header.png")
	if err := os.WriteFile(source, pngHeaderOnly(fallbackSourceMaxDimension+1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(f)
	_ = f.Close()
	if err != nil || format != "png" || config.Width != fallbackSourceMaxDimension+1 {
		t.Fatalf("fixture DecodeConfig = %dx%d %q %v", config.Width, config.Height, format, err)
	}
	_, _, err = transcodeOneImage(source, t.TempDir(), 0)
	if err == nil || !strings.Contains(err.Error(), "decode safety limit") {
		t.Fatalf("oversized source error = %v", err)
	}
}

func TestTranscodeCompositesAlphaOntoWhite(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "alpha.png")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 100, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 100; x++ {
			if x < 50 {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 0})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{B: 255, A: 128})
			}
		}
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	converted, ok, err := transcodeOneImage(source, dir, 0)
	if err != nil || !ok {
		t.Fatalf("transcode = (%q, %v, %v)", converted, ok, err)
	}
	out, err := os.Open(converted)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(out)
	_ = out.Close()
	if err != nil {
		t.Fatal(err)
	}
	lr, lg, lb, _ := decoded.At(20, 25).RGBA()
	if lr < 60000 || lg < 60000 || lb < 60000 {
		t.Fatalf("fully transparent pixel became dark: rgb16=(%d,%d,%d)", lr, lg, lb)
	}
	rr, rg, rb, _ := decoded.At(80, 25).RGBA()
	if rr < 25000 || rr > 40000 || rg < 25000 || rg > 40000 || rb < 60000 {
		t.Fatalf("semi-transparent blue was not composited on white: rgb16=(%d,%d,%d)", rr, rg, rb)
	}
}

func pngHeaderOnly(width, height int) []byte {
	var out bytes.Buffer
	out.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
	writeChunk := func(kind string, data []byte) {
		_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))
		out.WriteString(kind)
		out.Write(data)
		crc := crc32.NewIEEE()
		_, _ = crc.Write([]byte(kind))
		_, _ = crc.Write(data)
		_ = binary.Write(&out, binary.BigEndian, crc.Sum32())
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(height))
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	writeChunk("IHDR", ihdr)
	writeChunk("IEND", nil)
	return out.Bytes()
}
