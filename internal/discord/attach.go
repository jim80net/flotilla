package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// MaxAttachmentBytes is Discord's default per-file upload cap for webhook
// attachments (~25 MiB). Fail closed before posting when any file exceeds it.
const (
	MaxAttachmentBytes        = 25 * 1024 * 1024
	defaultAttachmentTimeout  = 30 * time.Second
	attachmentAttempts        = 3
	fallbackImageMaxDimension = 1600
	fallbackImageMaxBytes     = 2 * 1024 * 1024
)

type attachmentPostOptions struct {
	client    *http.Client
	attempts  int
	backoff   func(attempt int)
	transcode func(paths []string) (fallback []string, cleanup func(), err error)
}

type attachmentHTTPError struct {
	statusCode int
	status     string
	snippet    string
}

func (e *attachmentHTTPError) Error() string {
	return fmt.Sprintf("webhook returned %s: %s", e.status, e.snippet)
}

// attachmentFile is an opened, validated attachment ready for multipart upload.
type attachmentFile struct {
	filename string
	size     int64
	f        *os.File
}

func (a attachmentFile) close() {
	if a.f != nil {
		_ = a.f.Close()
	}
}

// OpenAttachments validates paths and opens each regular file for upload.
// Every path is checked before any file is opened; the first validation error
// aborts without posting a bodyless message.
func OpenAttachments(paths []string) ([]attachmentFile, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]attachmentFile, 0, len(paths))
	for _, raw := range paths {
		path := filepath.Clean(raw)
		info, err := os.Stat(path)
		if err != nil {
			closeAttachments(out)
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("attachment %q: file not found", raw)
			}
			return nil, fmt.Errorf("attachment %q: %w", raw, err)
		}
		if info.IsDir() {
			closeAttachments(out)
			return nil, fmt.Errorf("attachment %q: is a directory, not a file", raw)
		}
		if !info.Mode().IsRegular() {
			closeAttachments(out)
			return nil, fmt.Errorf("attachment %q: not a regular file", raw)
		}
		if info.Size() > MaxAttachmentBytes {
			closeAttachments(out)
			return nil, fmt.Errorf("attachment %q: size %d bytes exceeds Discord limit %d bytes", raw, info.Size(), MaxAttachmentBytes)
		}
		f, err := os.Open(path)
		if err != nil {
			closeAttachments(out)
			return nil, fmt.Errorf("attachment %q: %w", raw, err)
		}
		out = append(out, attachmentFile{
			filename: filepath.Base(path),
			size:     info.Size(),
			f:        f,
		})
	}
	return out, nil
}

func closeAttachments(files []attachmentFile) {
	for _, af := range files {
		af.close()
	}
}

// PostWithAttachments sends content and file attachments to a Discord webhook via
// multipart/form-data: a payload_json part (JSON built programmatically) plus
// files[0..n] parts. Discord returns 204 No Content on success.
func PostWithAttachments(webhookURL, username, content string, attachPaths []string) error {
	parsed, err := url.Parse(webhookURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("invalid webhook URL")
	}
	timeout, err := notifyAttachmentTimeout()
	if err != nil {
		return err
	}
	client := *httpClient
	client.Timeout = timeout
	return postWithAttachments(webhookURL, parsed.Host, username, content, attachPaths, attachmentPostOptions{
		client:    &client,
		attempts:  attachmentAttempts,
		backoff:   attachmentBackoff,
		transcode: transcodeImageAttachments,
	})
}

// notifyAttachmentTimeout returns the attachment-specific request budget. The default is
// deliberately longer than ordinary webhook prose; operators can tune slow links without a
// rebuild through FLOTILLA_NOTIFY_ATTACH_TIMEOUT (a positive Go duration such as "45s").
func notifyAttachmentTimeout() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv("FLOTILLA_NOTIFY_ATTACH_TIMEOUT"))
	if raw == "" {
		return defaultAttachmentTimeout, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid FLOTILLA_NOTIFY_ATTACH_TIMEOUT %q: use a positive duration such as 45s", raw)
	}
	return d, nil
}

func attachmentBackoff(attempt int) {
	// 250ms, 500ms between the three bounded attempts.
	time.Sleep(time.Duration(attempt) * 250 * time.Millisecond)
}

func postWithAttachments(webhookURL, host, username, content string, attachPaths []string, opts attachmentPostOptions) error {
	if opts.client == nil {
		return errors.New("attachment upload: HTTP client is nil")
	}
	if opts.attempts < 1 {
		opts.attempts = 1
	}
	body, contentType, err := buildAttachmentBody(username, content, attachPaths)
	if err != nil {
		return err
	}
	originalErr, timedOut := postAttachmentAttempts(opts, webhookURL, host, body, contentType)
	if originalErr == nil {
		return nil
	}
	if !timedOut || opts.transcode == nil {
		return fmt.Errorf("attachment upload failed after %d attempt(s): %w", opts.attempts, originalErr)
	}

	fallback, cleanup, transcodeErr := opts.transcode(attachPaths)
	if cleanup != nil {
		defer cleanup()
	}
	if transcodeErr != nil {
		return fmt.Errorf("attachment upload timed out after %d attempt(s); automatic image fallback failed: %w (original error: %v)", opts.attempts, transcodeErr, originalErr)
	}
	fallbackBody, fallbackType, buildErr := buildAttachmentBody(username, content, fallback)
	if buildErr != nil {
		return fmt.Errorf("attachment upload timed out after %d attempt(s); build automatic image fallback: %w", opts.attempts, buildErr)
	}
	fallbackErr, _ := postAttachmentAttempts(opts, webhookURL, host, fallbackBody, fallbackType)
	if fallbackErr != nil {
		return fmt.Errorf("attachment upload timed out after %d attempt(s); downscaled JPEG fallback also failed after %d attempt(s): %w", opts.attempts, opts.attempts, fallbackErr)
	}
	return nil
}

func postAttachmentAttempts(opts attachmentPostOptions, webhookURL, host string, body []byte, contentType string) (last error, timedOut bool) {
	for attempt := 1; attempt <= opts.attempts; attempt++ {
		err := postAttachmentBody(opts.client, webhookURL, host, body, contentType)
		if err == nil {
			return nil, timedOut
		}
		last = err
		if isTimeout(err) {
			timedOut = true
		}
		if !retryableAttachmentError(err) || attempt == opts.attempts {
			break
		}
		if opts.backoff != nil {
			opts.backoff(attempt)
		}
	}
	return last, timedOut
}

func retryableAttachmentError(err error) bool {
	if isTimeout(err) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var httpErr *attachmentHTTPError
	return errors.As(err, &httpErr) && (httpErr.statusCode == http.StatusTooManyRequests || httpErr.statusCode >= 500)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func buildAttachmentBody(username, content string, attachPaths []string) ([]byte, string, error) {

	files, err := OpenAttachments(attachPaths)
	if err != nil {
		return nil, "", err
	}
	defer closeAttachments(files)

	payload, err := json.Marshal(webhookPayload{
		Username:        username,
		Content:         clampContent(content),
		AllowedMentions: allowedMentions{Parse: []string{}},
	})
	if err != nil {
		return nil, "", fmt.Errorf("encode webhook payload: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	jsonPart, err := mw.CreateFormField("payload_json")
	if err != nil {
		return nil, "", fmt.Errorf("build multipart payload_json field: %w", err)
	}
	if _, err := jsonPart.Write(payload); err != nil {
		return nil, "", fmt.Errorf("write payload_json: %w", err)
	}

	for i, af := range files {
		part, err := mw.CreateFormFile(fmt.Sprintf("files[%d]", i), af.filename)
		if err != nil {
			return nil, "", fmt.Errorf("build multipart file part %d: %w", i, err)
		}
		if _, err := io.Copy(part, af.f); err != nil {
			return nil, "", fmt.Errorf("read attachment %q: %w", af.filename, err)
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("finalize multipart body: %w", err)
	}
	return body.Bytes(), mw.FormDataContentType(), nil
}

func postAttachmentBody(client *http.Client, webhookURL, host string, body []byte, contentType string) error {
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request for host %s: %w", host, urlFreeCause(err))
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post to webhook host %s: %w", host, urlFreeCause(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return &attachmentHTTPError{statusCode: resp.StatusCode, status: resp.Status, snippet: string(snippet)}
	}
	return nil
}

func transcodeImageAttachments(paths []string) ([]string, func(), error) {
	dir, err := os.MkdirTemp("", "flotilla-notify-images-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create image fallback directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	out := make([]string, 0, len(paths))
	converted := 0
	for i, path := range paths {
		convertedPath, ok, convertErr := transcodeOneImage(path, dir, i)
		if convertErr != nil {
			cleanup()
			return nil, nil, convertErr
		}
		if ok {
			out = append(out, convertedPath)
			converted++
		} else {
			out = append(out, path)
		}
	}
	if converted == 0 {
		cleanup()
		return nil, nil, errors.New("no PNG or JPEG attachment was available to downscale/transcode")
	}
	return out, cleanup, nil
}

func transcodeOneImage(path, dir string, index int) (string, bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", false, fmt.Errorf("open image fallback source %q: %w", path, err)
	}
	img, format, decodeErr := image.Decode(f)
	_ = f.Close()
	if decodeErr != nil {
		return "", false, nil // non-image attachments are preserved byte-for-byte
	}
	if format != "png" && format != "jpeg" {
		return "", false, nil
	}

	var encoded []byte
	for attempt, maxDimension := range []int{fallbackImageMaxDimension, 1200, 900, 675, 500} {
		bounded := resizeNearest(img, maxDimension)
		var buf bytes.Buffer
		quality := 82 - attempt*7
		if err := jpeg.Encode(&buf, bounded, &jpeg.Options{Quality: quality}); err != nil {
			return "", false, fmt.Errorf("encode image fallback for %q: %w", path, err)
		}
		encoded = buf.Bytes()
		if len(encoded) <= fallbackImageMaxBytes {
			break
		}
	}
	if len(encoded) > fallbackImageMaxBytes {
		return "", false, fmt.Errorf("image fallback for %q remains %d bytes after downscaling (limit %d)", path, len(encoded), fallbackImageMaxBytes)
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + "-downscaled-" + strconv.Itoa(index) + ".jpg"
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, encoded, 0o600); err != nil {
		return "", false, fmt.Errorf("write image fallback for %q: %w", path, err)
	}
	return dst, true, nil
}

func resizeNearest(src image.Image, maxDimension int) image.Image {
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxDimension && h <= maxDimension {
		return src
	}
	scale := float64(maxDimension) / float64(max(w, h))
	dw, dh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := bounds.Min.Y + y*h/dh
		for x := 0; x < dw; x++ {
			sx := bounds.Min.X + x*w/dw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
