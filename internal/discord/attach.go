package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
)

// MaxAttachmentBytes is Discord's default per-file upload cap for webhook
// attachments (~25 MiB). Fail closed before posting when any file exceeds it.
const MaxAttachmentBytes = 25 * 1024 * 1024

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

	files, err := OpenAttachments(attachPaths)
	if err != nil {
		return err
	}
	defer closeAttachments(files)

	payload, err := json.Marshal(webhookPayload{
		Username:        username,
		Content:         clampContent(content),
		AllowedMentions: allowedMentions{Parse: []string{}},
	})
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	jsonPart, err := mw.CreateFormField("payload_json")
	if err != nil {
		return fmt.Errorf("build multipart payload_json field: %w", err)
	}
	if _, err := jsonPart.Write(payload); err != nil {
		return fmt.Errorf("write payload_json: %w", err)
	}

	for i, af := range files {
		part, err := mw.CreateFormFile(fmt.Sprintf("files[%d]", i), af.filename)
		if err != nil {
			return fmt.Errorf("build multipart file part %d: %w", i, err)
		}
		if _, err := io.Copy(part, af.f); err != nil {
			return fmt.Errorf("read attachment %q: %w", af.filename, err)
		}
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("finalize multipart body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, &body)
	if err != nil {
		return fmt.Errorf("build webhook request for host %s: %w", parsed.Host, urlFreeCause(err))
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", UserAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post to webhook host %s: %w", parsed.Host, urlFreeCause(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("webhook returned %s: %s", resp.Status, snippet)
	}
	return nil
}
