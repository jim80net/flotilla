package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	out, _ := captureStdoutStderr(t, func() {
		if err := cmdAccountsRefresh([]string{"--probe-only", "--json", "anthropic-work"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, secret := range []string{"CREDENTIAL_SECRET", "REFRESH_SECRET"} {
		if strings.Contains(out, secret) {
			t.Fatalf("refresh probe leaked %q: %s", secret, out)
		}
	}
	var report accountRefreshReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("probe JSON: %v body=%q", err, out)
	}
	if report.CredentialProbe != "ok" || report.AuthState != "unknown" || report.Warning == "" {
		t.Fatalf("report = %+v, want read-only file health without guessed login state", report)
	}
}

func TestCmdAccountsRefreshNoCredentialsReportsSeparateProbeAndAuthFacts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	if _, err := accounts.Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}

	jsonOut, _ := captureStdoutStderr(t, func() {
		if err := cmdAccountsRefresh([]string{"--probe-only", "--json", "anthropic-work"}); err != nil {
			t.Fatal(err)
		}
	})
	var report accountRefreshReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("probe JSON: %v body=%q", err, jsonOut)
	}
	if report.Credential != accounts.StatusNoCredsFile || report.CredentialProbe != "ok" || report.AuthState != "unknown" {
		t.Fatalf("no-credentials JSON facts = %+v", report)
	}
	if strings.Contains(jsonOut, `"probe"`) || strings.Contains(jsonOut, `"logged_in"`) {
		t.Fatalf("JSON retained misleading auth-probe fields: %s", jsonOut)
	}

	textOut, _ := captureStdoutStderr(t, func() {
		if err := cmdAccountsRefresh([]string{"--probe-only", "anthropic-work"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, fact := range []string{"credential=no-creds-file", "credential_probe=ok", "auth_state=unknown"} {
		if !strings.Contains(textOut, fact) {
			t.Fatalf("text report missing %q: %s", fact, textOut)
		}
	}
	if strings.Contains(textOut, "auth_probe=") {
		t.Fatalf("text retained misleading auth probe label: %s", textOut)
	}
}

func TestCmdAccountsRefreshProbeOnlyDoesNotInvokeProviderOrMutateConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	dir, err := accounts.Init("anthropic-work")
	if err != nil {
		t.Fatal(err)
	}
	credential := `{"claudeAiOauth":{"accessToken":"CREDENTIAL_SECRET","refreshToken":"REFRESH_SECRET","expiresAt":9999999999999}}`
	if err := os.WriteFile(filepath.Join(dir, accounts.CredentialsFile), []byte(credential), 0o600); err != nil {
		t.Fatal(err)
	}
	helperDir := t.TempDir()
	helper := "#!/bin/sh\nprintf invoked > \"$CLAUDE_CONFIG_DIR/provider-mutated\"\n"
	if err := os.WriteFile(filepath.Join(helperDir, "claude"), []byte(helper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", helperDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	before := snapshotDirectory(t, dir)
	if err := cmdAccountsRefresh([]string{"--probe-only", "--json", "anthropic-work"}); err != nil {
		t.Fatal(err)
	}
	after := snapshotDirectory(t, dir)
	if before != after {
		t.Fatalf("probe-only mutated credential directory\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestCmdAccountsRefreshRequiresExplicitConsent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	if _, err := accounts.Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}
	originalLogin := runClaudeAuthLogin
	t.Cleanup(func() { runClaudeAuthLogin = originalLogin })
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
	if !strings.Contains(out, "credential_probe=ok auth_state=unknown") || strings.Contains(out, "claude.ai") {
		t.Fatalf("preview output should be minimal and sanitized: %q", out)
	}
}

func TestProbeAccountRefreshDoesNotGuessLoginState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	if _, err := accounts.Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}
	report := probeAccountRefresh("anthropic-work", time.Now())
	if report.CredentialProbe != "ok" || report.AuthState != "unknown" {
		t.Fatalf("credential-file probe must not guess provider login state: %+v", report)
	}
}

func TestCmdAccountsRefreshYesRunsIsolatedClaudeLogin(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	dir, err := accounts.Init("anthropic-work")
	if err != nil {
		t.Fatal(err)
	}
	originalLogin := runClaudeAuthLogin
	t.Cleanup(func() { runClaudeAuthLogin = originalLogin })
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
	originalLogin := runClaudeAuthLogin
	t.Cleanup(func() { runClaudeAuthLogin = originalLogin })
	runClaudeAuthLogin = func(string) error { return errors.New("TOKEN_SECRET") }

	_, _ = captureStdoutStderr(t, func() {
		err := cmdAccountsRefresh([]string{"--yes", "anthropic-work"})
		if err == nil || strings.Contains(err.Error(), "TOKEN_SECRET") {
			t.Fatalf("unsanitized login error: %v", err)
		}
	})
}

func TestCmdAccountsRefreshBrokersRealProviderSubprocessOutput(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_ACCOUNTS_ROOT", root)
	if _, err := accounts.Init("anthropic-work"); err != nil {
		t.Fatal(err)
	}
	helperDir := t.TempDir()
	helper := `#!/bin/sh
printf '%s\n' 'PROVIDER_LOGIN_SECRET_STDOUT'
printf '%s\n' 'Open https://claude.ai/oauth/authorize?state=needed in your browser'
printf '%s\n' 'PROVIDER_LOGIN_SECRET_STDERR' >&2
printf '%s\n' 'Waiting for authentication...' >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(helperDir, "claude"), []byte(helper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", helperDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out, errOut := captureStdoutStderr(t, func() {
		err := cmdAccountsRefresh([]string{"--yes", "anthropic-work"})
		if err == nil {
			t.Fatal("expected provider failure")
		}
	})
	combined := out + errOut
	for _, secret := range []string{"PROVIDER_LOGIN_SECRET_STDOUT", "PROVIDER_LOGIN_SECRET_STDERR"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("raw provider output leaked %q: %s", secret, combined)
		}
	}
	if !strings.Contains(combined, "OAuth URL: https://claude.ai/oauth/authorize?state=needed") ||
		!strings.Contains(combined, "Waiting for OAuth authentication") {
		t.Fatalf("broker hid required OAuth flow output: %q", combined)
	}
}

func TestOAuthOutputBrokerEmitsNonNewlinePromptImmediately(t *testing.T) {
	var out bytes.Buffer
	broker := newOAuthOutputBroker(&out)
	if _, err := broker.Write([]byte("\x1b[36mPress Enter to open the browser\x1b[0m")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "Press Enter to open the OAuth page in your browser.\n" {
		t.Fatalf("brokered prompt = %q", got)
	}
}

func TestOAuthOutputBrokerEmitsNonNewlineURLAtProcessEnd(t *testing.T) {
	var out bytes.Buffer
	broker := newOAuthOutputBroker(&out)
	if _, err := broker.Write([]byte("Open https://claude.ai/oauth/authorize?state=needed")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("URL emitted without a record boundary: %q", got)
	}
	broker.Flush()
	if got := out.String(); got != "OAuth URL: https://claude.ai/oauth/authorize?state=needed\n" {
		t.Fatalf("brokered URL = %q", got)
	}
}

func TestOAuthOutputBrokerBuffersSplitNonNewlineURLUntilComplete(t *testing.T) {
	var out bytes.Buffer
	broker := newOAuthOutputBroker(&out)
	if _, err := broker.Write([]byte("Open https://claude.ai/oauth")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("partial URL emitted early: %q", got)
	}
	time.Sleep(75 * time.Millisecond) // longer than the former quiet-period heuristic
	if got := out.String(); got != "" {
		t.Fatalf("valid URL prefix emitted after an arbitrary pause: %q", got)
	}
	if _, err := broker.Write([]byte("/authorize?state=needed\n")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "OAuth URL: https://claude.ai/oauth/authorize?state=needed\n" {
		t.Fatalf("split URL = %q, want complete URL exactly once", got)
	}
}

func TestOAuthOutputBrokerPromptDoesNotDiscardIncompleteURL(t *testing.T) {
	var out bytes.Buffer
	broker := newOAuthOutputBroker(&out)
	if _, err := broker.Write([]byte("Press Enter to open the browser https://claude.ai/oauth")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "Press Enter to open the OAuth page in your browser.\n" {
		t.Fatalf("prompt output = %q", got)
	}
	if _, err := broker.Write([]byte("/authorize?state=needed\n")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "Press Enter to open the OAuth page in your browser.\nOAuth URL: https://claude.ai/oauth/authorize?state=needed\n" {
		t.Fatalf("completed URL output = %q", got)
	}
}

func TestOAuthOutputBrokerEmitsCompletePromptURLBeforeProcessExit(t *testing.T) {
	var out bytes.Buffer
	broker := newOAuthOutputBroker(&out)
	if _, err := broker.Write([]byte("Press Enter to open the browser https://claude.ai/oauth/authorize?state=needed")); err != nil {
		t.Fatal(err)
	}
	want := "OAuth URL: https://claude.ai/oauth/authorize?state=needed\nPress Enter to open the OAuth page in your browser.\n"
	if got := out.String(); got != want {
		t.Fatalf("output before provider exit = %q, want %q", got, want)
	}
}

func TestOAuthOutputBrokerDoesNotTreatPartialStateAsComplete(t *testing.T) {
	var out bytes.Buffer
	broker := newOAuthOutputBroker(&out)
	if _, err := broker.Write([]byte("Open https://claude.ai/oauth/authorize?state=need")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "" {
		t.Fatalf("partial state emitted early: %q", got)
	}
	if _, err := broker.Write([]byte("ed\n")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "OAuth URL: https://claude.ai/oauth/authorize?state=needed\n" {
		t.Fatalf("split state URL = %q", got)
	}
}

func TestOAuthOutputBrokerRejectsUserinfoAndUnneededURLComponents(t *testing.T) {
	for _, input := range []string{
		"Open https://PROVIDER_SECRET@claude.ai/oauth",
		"Open https://claude.ai:443/oauth",
		"Open https://claude.ai:/oauth",
		"Open https://claude.ai/oauth#PROVIDER_SECRET",
		"Open https://claude.ai",
	} {
		t.Run(input, func(t *testing.T) {
			var out bytes.Buffer
			broker := newOAuthOutputBroker(&out)
			if _, err := broker.Write([]byte(input + "\n")); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != "" {
				t.Fatalf("unsafe OAuth URL crossed output boundary: %q", got)
			}
		})
	}
}

func TestOAuthOutputBrokerAcceptsCaseInsensitiveSchemeAndHost(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: "HTTPS://claude.ai/oauth", want: "https://claude.ai/oauth"},
		{input: "HtTpS://ClAuDe.Ai/oauth/authorize?state=needed", want: "https://ClAuDe.Ai/oauth/authorize?state=needed"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var out bytes.Buffer
			broker := newOAuthOutputBroker(&out)
			if _, err := broker.Write([]byte("Open " + tc.input + "\n")); err != nil {
				t.Fatal(err)
			}
			if got, want := out.String(), "OAuth URL: "+tc.want+"\n"; got != want {
				t.Fatalf("brokered OAuth URL = %q, want %q", got, want)
			}
		})
	}
}

func snapshotDirectory(t *testing.T, root string) string {
	t.Helper()
	var snapshot strings.Builder
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&snapshot, "%s mode=%s size=%d mtime=%d", rel, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if !entry.IsDir() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&snapshot, " sha256=%x", sha256.Sum256(body))
		}
		snapshot.WriteByte('\n')
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot.String()
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
