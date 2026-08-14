package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/accounts"
)

func TestCmdAccountsInit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	if err := cmdAccountsInit([]string{"anthropic-work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "anthropic-work", accounts.ClaudeConfigSubdir)); err != nil {
		t.Fatalf("config dir not created: %v", err)
	}
}

func TestCmdAccountsListJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	if _, err := accounts.Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}
	dir, _ := accounts.ConfigDir("anthropic-work")
	body := fmt.Sprintf(`{"claudeAiOauth":{"expiresAt":%d,"subscriptionType":"max"}}`, time.Now().Add(48*time.Hour).UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, accounts.CredentialsFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	cmdErr := cmdAccountsList([]string{"--json"})
	w.Close()
	os.Stdout = old
	if cmdErr != nil {
		t.Fatal(cmdErr)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	var list []accounts.Health
	if err := json.Unmarshal(buf.Bytes(), &list); err != nil {
		t.Fatalf("json: %v body=%q", err, buf.String())
	}
	if len(list) != 1 || list[0].SubscriptionID != "anthropic-work" {
		t.Fatalf("list = %+v", list)
	}
	if strings.Contains(buf.String(), "accessToken") {
		t.Error("list json must not contain token fields")
	}
}

func TestCmdAccountsInitRejectsInvalidID(t *testing.T) {
	if err := cmdAccountsInit([]string{"Bad-ID"}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCmdAccountsRefreshProbeOnlySanitizesProviderOutput(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	dir, err := accounts.Init("anthropic-work")
	if err != nil {
		t.Fatal(err)
	}
	credential := `{"claudeAiOauth":{"accessToken":"CREDENTIAL_SECRET","refreshToken":"REFRESH_SECRET","expiresAt":1,"subscriptionType":"max"}}`
	if err := os.WriteFile(filepath.Join(dir, accounts.CredentialsFile), []byte(credential), 0o600); err != nil {
		t.Fatal(err)
	}
	originalStatus := runClaudeAuthStatus
	t.Cleanup(func() { runClaudeAuthStatus = originalStatus })
	runClaudeAuthStatus = func(context.Context, string) ([]byte, error) {
		return []byte(`{"loggedIn":false,"authMethod":"STATUS_SECRET","apiProvider":"PROVIDER_SECRET","accessToken":"ACCESS_SECRET"}`), errors.New("provider emitted ERROR_SECRET")
	}

	out, _ := captureStdoutStderr(t, func() {
		if err := cmdAccountsRefresh([]string{"--probe-only", "--json", "anthropic-work"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, secret := range []string{"CREDENTIAL_SECRET", "REFRESH_SECRET", "STATUS_SECRET", "PROVIDER_SECRET", "ACCESS_SECRET", "ERROR_SECRET"} {
		if strings.Contains(out, secret) {
			t.Fatalf("refresh probe leaked %q: %s", secret, out)
		}
	}
	var report accountRefreshReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("probe JSON: %v body=%q", err, out)
	}
	if report.Probe != "ok" || report.LoggedIn == nil || *report.LoggedIn || report.Warning == "" {
		t.Fatalf("report = %+v, want logged-out warning from allowlisted fields", report)
	}
}

func TestCmdAccountsRefreshRequiresExplicitConsent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	if _, err := accounts.Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}
	originalStatus, originalLogin := runClaudeAuthStatus, runClaudeAuthLogin
	t.Cleanup(func() { runClaudeAuthStatus, runClaudeAuthLogin = originalStatus, originalLogin })
	runClaudeAuthStatus = func(context.Context, string) ([]byte, error) {
		return []byte(`{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty"}`), nil
	}
	loginCalls := 0
	runClaudeAuthLogin = func(string) error { loginCalls++; return nil }

	out, _ := captureStdoutStderr(t, func() {
		err := cmdAccountsRefresh([]string{"anthropic-work"})
		if err == nil || !strings.Contains(err.Error(), "--yes") {
			t.Fatalf("refresh error = %v, want explicit --yes boundary", err)
		}
	})
	if loginCalls != 0 {
		t.Fatalf("login called %d times without consent", loginCalls)
	}
	if !strings.Contains(out, "auth_probe=ok") || strings.Contains(out, "claude.ai") {
		t.Fatalf("preview output should be minimal and sanitized: %q", out)
	}
}

func TestProbeAccountRefreshDoesNotInferLoggedOutFromUnknownJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	dir, err := accounts.Init("anthropic-work")
	if err != nil {
		t.Fatal(err)
	}
	originalStatus := runClaudeAuthStatus
	t.Cleanup(func() { runClaudeAuthStatus = originalStatus })
	runClaudeAuthStatus = func(context.Context, string) ([]byte, error) {
		return []byte(`{"error":"TOKEN_SECRET"}`), errors.New("status failed")
	}
	report := probeAccountRefresh("anthropic-work", dir, time.Now())
	if report.Probe != "unavailable" || report.LoggedIn != nil {
		t.Fatalf("unknown provider JSON must remain unknown: %+v", report)
	}
}

func TestCmdAccountsRefreshYesRunsIsolatedClaudeLogin(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	dir, err := accounts.Init("anthropic-work")
	if err != nil {
		t.Fatal(err)
	}
	originalStatus, originalLogin := runClaudeAuthStatus, runClaudeAuthLogin
	t.Cleanup(func() { runClaudeAuthStatus, runClaudeAuthLogin = originalStatus, originalLogin })
	runClaudeAuthStatus = func(context.Context, string) ([]byte, error) {
		return []byte(`{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty"}`), nil
	}
	loginCalls := 0
	runClaudeAuthLogin = func(gotDir string) error {
		loginCalls++
		if gotDir != dir {
			t.Fatalf("login config dir = %q, want %q", gotDir, dir)
		}
		return nil
	}

	out, errOut := captureStdoutStderr(t, func() {
		if err := cmdAccountsRefresh([]string{"--yes", "anthropic-work"}); err != nil {
			t.Fatal(err)
		}
	})
	if loginCalls != 1 {
		t.Fatalf("login called %d times, want 1", loginCalls)
	}
	if !strings.Contains(out, "OAuth renewal completed") || !strings.Contains(errOut, dir) {
		t.Fatalf("stdout=%q stderr=%q", out, errOut)
	}
}

func TestCmdAccountsRefreshSuppressesProviderErrorDetails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	if _, err := accounts.Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}
	originalStatus, originalLogin := runClaudeAuthStatus, runClaudeAuthLogin
	t.Cleanup(func() { runClaudeAuthStatus, runClaudeAuthLogin = originalStatus, originalLogin })
	runClaudeAuthStatus = func(context.Context, string) ([]byte, error) { return nil, errors.New("STATUS_SECRET") }
	runClaudeAuthLogin = func(string) error { return errors.New("TOKEN_SECRET") }

	_, _ = captureStdoutStderr(t, func() {
		err := cmdAccountsRefresh([]string{"--yes", "anthropic-work"})
		if err == nil || strings.Contains(err.Error(), "TOKEN_SECRET") || strings.Contains(err.Error(), "STATUS_SECRET") {
			t.Fatalf("unsanitized login error: %v", err)
		}
	})
}

func TestAccountEnvReplacesInheritedClaudeConfigDir(t *testing.T) {
	t.Setenv(accounts.ClaudeConfigEnv, "/wrong")
	env := accountEnv("/isolated")
	want := accounts.ClaudeConfigEnv + "=/isolated"
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, accounts.ClaudeConfigEnv+"=") {
			count++
			if entry != want {
				t.Fatalf("config env = %q, want %q", entry, want)
			}
		}
	}
	if count != 1 {
		t.Fatalf("config env entries = %d, want 1", count)
	}
}
