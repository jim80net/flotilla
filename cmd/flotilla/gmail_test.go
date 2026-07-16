package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGmailRejectsNonPAEffectivePrincipalBeforeHostLookup(t *testing.T) {
	t.Setenv("FLOTILLA_SELF", "other")
	t.Setenv("FLOTILLA_PA_GMAIL_OAUTH_FILE", "/must-not-open")
	err := cmdGmail([]string{"smoke"})
	if err == nil || !strings.Contains(err.Error(), "not pa") {
		t.Fatalf("err=%v", err)
	}
}

func TestGmailAuditFileSecurity(t *testing.T) {
	d := t.TempDir()
	good := filepath.Join(d, "audit.jsonl")
	a := gmailAuditFile{good}
	if err := a.write(map[string]string{"result": "ok"}); err != nil {
		t.Fatal(err)
	}
	i, err := os.Stat(good)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateGmailAuditFile(i, os.Geteuid()+1); err == nil {
		t.Fatal("wrong owner accepted")
	}
	badMode := filepath.Join(d, "bad-mode")
	if err = os.WriteFile(badMode, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err = (gmailAuditFile{badMode}).write(struct{}{}); err == nil {
		t.Fatal("bad mode accepted")
	}
	link := filepath.Join(d, "link")
	if err = os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if err = (gmailAuditFile{link}).write(struct{}{}); err == nil {
		t.Fatal("symlink accepted")
	}
	if err = (gmailAuditFile{d}).write(struct{}{}); err == nil {
		t.Fatal("directory accepted")
	}
}

func TestGmailCanonicalPAIdentityReachesCommand(t *testing.T) {
	t.Setenv("FLOTILLA_SELF", "pa")
	err := cmdGmail([]string{"smoke"})
	if err == nil || strings.Contains(err.Error(), "not pa") {
		t.Fatalf("err=%v", err)
	}
}
