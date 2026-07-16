package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"

	"github.com/jim80net/flotilla/internal/authdomain"
	"github.com/jim80net/flotilla/internal/gmailbroker"
	"github.com/jim80net/flotilla/internal/roster"
)

type gmailAuditFile struct{ path string }

func (a gmailAuditFile) write(v any) error {
	fd, e := syscall.Open(a.path, syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), a.path)
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return e
	}
	if e = validateGmailAuditFile(i, os.Geteuid()); e != nil {
		return e
	}
	return json.NewEncoder(f).Encode(v)
}
func validateGmailAuditFile(i fs.FileInfo, euid int) error {
	st, ok := i.Sys().(*syscall.Stat_t)
	if !i.Mode().IsRegular() || i.Mode().Perm() != 0600 || !ok || int(st.Uid) != euid {
		return errors.New("gmail audit file must be regular, expected-owner, mode 0600")
	}
	return nil
}
func (a gmailAuditFile) Record(e authdomain.AuditEvent) error       { return a.write(e) }
func (a gmailAuditFile) RecordGmail(e gmailbroker.AuditEvent) error { return a.write(e) }

func cmdGmail(args []string) error {
	f := flag.NewFlagSet("gmail", flag.ContinueOnError)
	rp := f.String("roster", rosterDefault(), "roster config path")
	gp := f.String("grant", "", "authorization grant file")
	label := f.String("label", "", "logical label selector")
	id := f.String("id", "", "resource id")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 1 {
		return errors.New("usage: flotilla gmail [flags] smoke|labels-list|label-get|messages-list|message-get|threads-list|thread-get")
	}
	principal := os.Getenv("FLOTILLA_SELF")
	if principal != "pa" {
		return errors.New("gmail: effective roster principal is not pa")
	}
	resolved, e := resolveRosterPath(*rp)
	if e != nil {
		return e
	}
	rc, e := roster.Load(resolved)
	if e != nil {
		return e
	}
	if _, e = rc.Agent(principal); e != nil {
		return e
	}
	if *gp == "" {
		return errors.New("gmail: --grant is required")
	}
	raw, e := os.ReadFile(*gp)
	if e != nil {
		return fmt.Errorf("gmail: read grant: %w", e)
	}
	grants, e := authdomain.Load(rc, raw)
	if e != nil {
		return e
	}
	approved := os.Getenv("FLOTILLA_PA_GMAIL_APPROVED_ACCOUNT")
	account := os.Getenv("FLOTILLA_PA_GMAIL_ACCOUNT_RESOURCE")
	ap := os.Getenv("FLOTILLA_PA_GMAIL_AUDIT_FILE")
	if approved == "" || account == "" || ap == "" {
		return errors.New("gmail: host-private approved account, account resource, and audit path are required")
	}
	labels := map[string]string{}
	if raw := os.Getenv("FLOTILLA_PA_GMAIL_LABEL_BINDINGS"); raw != "" {
		if json.Unmarshal([]byte(raw), &labels) != nil {
			return errors.New("gmail: invalid label bindings")
		}
	}
	a := gmailAuditFile{ap}
	c, e := gmailbroker.New(gmailbroker.Config{Grants: grants, GrantAudit: a, Audit: a, Principal: principal, ApprovedAccount: approved, AccountResource: account, LabelIDs: labels, Now: time.Now})
	if e != nil {
		return e
	}
	ctx := context.Background()
	var out json.RawMessage
	switch f.Arg(0) {
	case "smoke":
		return c.Smoke(ctx)
	case "labels-list":
		out, e = c.ListLabels(ctx, *label)
	case "label-get":
		out, e = c.GetLabel(ctx, *id, *label)
	case "messages-list":
		out, e = c.ListMessages(ctx, *label)
	case "message-get":
		out, e = c.GetMessage(ctx, *id, *label)
	case "threads-list":
		out, e = c.ListThreads(ctx, *label)
	case "thread-get":
		out, e = c.GetThread(ctx, *id, *label)
	default:
		return errors.New("gmail: operation not allowed")
	}
	if e != nil {
		return e
	}
	_, e = os.Stdout.Write(append(out, '\n'))
	return e
}
