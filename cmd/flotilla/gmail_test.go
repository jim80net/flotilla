package main

import (
	"strings"
	"testing"
)

func TestGmailRejectsNonPAEffectivePrincipalBeforeHostLookup(t *testing.T) {
	t.Setenv("FLOTILLA_AGENT", "other")
	t.Setenv("FLOTILLA_PA_GMAIL_OAUTH_FILE", "/must-not-open")
	err := cmdGmail([]string{"smoke"})
	if err == nil || !strings.Contains(err.Error(), "not pa") {
		t.Fatalf("err=%v", err)
	}
}
