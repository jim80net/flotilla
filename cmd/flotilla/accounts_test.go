package main

import (
	"bytes"
	"encoding/json"
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
