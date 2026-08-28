package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
)

type recordingWatchPoster struct {
	dest     transport.Destination
	username string
	content  string
}

func (p *recordingWatchPoster) Post(dest transport.Destination, username, content string) error {
	p.dest, p.username, p.content = dest, username, content
	return nil
}

func TestResolveAlertWebhookDedicatedThenLegacyFallback(t *testing.T) {
	tests := []struct {
		name    string
		secrets string
		want    string
	}{
		{name: "dedicated alerts wins", secrets: "FLOTILLA_WEBHOOK_ALERTS=alerts-hook\nFLOTILLA_WEBHOOK_XO=xo-hook\n", want: "alerts-hook"},
		{name: "unset alerts preserves XO", secrets: "FLOTILLA_WEBHOOK_XO=xo-hook\n", want: "xo-hook"},
		{name: "empty alerts preserves XO", secrets: "FLOTILLA_WEBHOOK_ALERTS=\nFLOTILLA_WEBHOOK_XO=xo-hook\n", want: "xo-hook"},
		{name: "neither keeps stderr degradation", secrets: "FLOTILLA_BOT_TOKEN=sentinel\n", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secrets")
			if err := os.WriteFile(path, []byte(tt.secrets), 0o600); err != nil {
				t.Fatal(err)
			}
			secrets, err := roster.LoadSecrets(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := resolveAlertWebhook(secrets, "xo"); got != tt.want {
				t.Fatalf("resolveAlertWebhook = %q, want %q", got, tt.want)
			}
		})
	}
	if got := resolveAlertWebhook(nil, "xo"); got != "" {
		t.Fatalf("nil secrets resolved webhook %q", got)
	}
}

func TestAlertsWebhookKeyIsGeneric(t *testing.T) {
	if got := roster.WebhookKey("alerts"); got != "FLOTILLA_WEBHOOK_ALERTS" {
		t.Fatalf("alerts key = %q", got)
	}
}

func TestWatchPostersPartitionWarningsFromOperatorTraffic(t *testing.T) {
	operatorDest := transport.NewWebhookDestination("xo-hook")
	alertsDest := transport.NewWebhookDestination("alerts-hook")
	poster := &recordingWatchPoster{}
	var stderr bytes.Buffer
	post, alert := newWatchPosters(poster, operatorDest, alertsDest, &stderr)

	alert("down")
	if poster.dest != alertsDest || poster.username != "flotilla-watch" || poster.content != "⚠️ down" {
		t.Fatalf("warning post = (%v,%q,%q), want dedicated alerts destination", poster.dest, poster.username, poster.content)
	}
	post("flotilla-watch", "operator relay mirror")
	if poster.dest != operatorDest || poster.content != "operator relay mirror" {
		t.Fatalf("operator post moved destinations: dest=%v content=%q", poster.dest, poster.content)
	}
	if stderr.Len() != 0 {
		t.Fatalf("configured destinations degraded to stderr: %q", stderr.String())
	}
}

func TestWatchPostersLegacyFallbackAndNoWebhookDegradation(t *testing.T) {
	xoDest := transport.NewWebhookDestination("xo-hook")
	poster := &recordingWatchPoster{}
	var stderr bytes.Buffer
	_, alert := newWatchPosters(poster, xoDest, xoDest, &stderr)
	alert("legacy")
	if poster.dest != xoDest || poster.content != "⚠️ legacy" {
		t.Fatalf("legacy warning changed: dest=%v content=%q", poster.dest, poster.content)
	}

	stderr.Reset()
	_, alert = newWatchPosters(nil, nil, nil, &stderr)
	alert("no webhook")
	if got, want := stderr.String(), "flotilla watch: ⚠️ no webhook\n"; got != want {
		t.Fatalf("stderr degradation = %q, want %q", got, want)
	}
}
