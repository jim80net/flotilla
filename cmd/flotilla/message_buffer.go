package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/messagebuffer"
)

func cmdPull(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: flotilla pull [--roster <path>] [--json]")
	}
	recipient := strings.TrimSpace(os.Getenv("FLOTILLA_SELF"))
	if recipient == "" {
		return fmt.Errorf("pull: recipient identity required (set $FLOTILLA_SELF)")
	}
	rp, err := resolveRosterPath(*rosterPath)
	if err != nil {
		return err
	}
	entries, err := messagebuffer.Pull(filepath.Dir(rp), recipient, time.Now().UTC())
	if err != nil {
		return err
	}
	if *asJSON {
		raw, err := json.Marshal(struct {
			Recipient string                `json:"recipient"`
			Pending   int                   `json:"pending"`
			Entries   []messagebuffer.Entry `json:"entries"`
		}{recipient, len(entries), entries})
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	fmt.Printf("PULL recipient=%s pending=%d\n", recipient, len(entries))
	for _, e := range entries {
		status := "CURRENT"
		if e.SupersededBy != "" {
			status = "SUPERSEDED_DO_NOT_ACT superseded-by:" + e.SupersededBy
		}
		fmt.Printf("\n--- message id=%s sender=%s sender-seq=%d status=%s enqueued=%s\n", e.ID, e.Sender, e.SenderSequence, status, e.EnqueuedAt.Format(time.RFC3339))
		if len(e.Supersedes) > 0 {
			fmt.Printf("supersedes: %s\n", strings.Join(e.Supersedes, ","))
		}
		if e.MigratedFrom != "" {
			fmt.Printf("migration: from=%s legacy-deferrals=%d\n", e.MigratedFrom, e.LegacyDeferrals)
		}
		fmt.Println(e.Message)
		if e.Nonce == "" {
			fmt.Printf("ack: flotilla buffer ack %s\n", e.ID)
		}
		fmt.Printf("--- end message id=%s\n", e.ID)
	}
	return nil
}

func cmdBuffer(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla buffer inspect|ack|migrate ...")
	}
	switch args[0] {
	case "inspect":
		return cmdBufferInspect(args[1:])
	case "ack":
		return cmdBufferAck(args[1:])
	case "migrate":
		return cmdBufferMigrate(args[1:])
	default:
		return fmt.Errorf("unknown buffer command %q (want inspect, ack, or migrate)", args[0])
	}
}

func cmdBufferInspect(args []string) error {
	fs := flag.NewFlagSet("buffer inspect", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	all := fs.Bool("all", false, "inspect every recipient buffer")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if *all && len(rest) != 0 || len(rest) > 1 {
		return fmt.Errorf("usage: flotilla buffer inspect [<seat>] [--all] [--json]")
	}
	rp, err := resolveRosterPath(*rosterPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(rp)
	now := time.Now().UTC()
	var summaries []messagebuffer.Summary
	if *all {
		summaries, err = messagebuffer.InspectAll(dir, now)
	} else {
		recipient := strings.TrimSpace(os.Getenv("FLOTILLA_SELF"))
		if len(rest) == 1 {
			recipient = rest[0]
		}
		if recipient == "" {
			return fmt.Errorf("buffer inspect: seat required (argument or $FLOTILLA_SELF)")
		}
		var summary messagebuffer.Summary
		summary, err = messagebuffer.Inspect(dir, recipient, now)
		summaries = []messagebuffer.Summary{summary}
	}
	if err != nil {
		return err
	}
	if *asJSON {
		raw, err := json.Marshal(summaries)
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	for _, s := range summaries {
		fmt.Printf("%s pending=%d unread=%d pulled=%d superseded=%d oldest=%s\n", s.Recipient, s.Pending, s.Unread, s.Pulled, s.Superseded, durationDash(s.OldestAge))
	}
	return nil
}

func cmdBufferAck(args []string) error {
	fs := flag.NewFlagSet("buffer ack", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 {
		return fmt.Errorf("usage: flotilla buffer ack <message-id> [--roster <path>]")
	}
	recipient := strings.TrimSpace(os.Getenv("FLOTILLA_SELF"))
	if recipient == "" {
		return fmt.Errorf("buffer ack: recipient identity required (set $FLOTILLA_SELF)")
	}
	rp, err := resolveRosterPath(*rosterPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(rp)
	path, err := messagebuffer.Path(dir, recipient)
	if err != nil {
		return err
	}
	for _, pending := range messagebuffer.NewStore(path).Load() {
		if pending.ID == fs.Args()[0] && pending.Nonce != "" {
			return fmt.Errorf("buffer ack: message %s is a dispatch; run `flotilla dispatch-ack %s`", pending.ID, pending.Nonce)
		}
	}
	e, already, err := messagebuffer.AckID(dir, recipient, fs.Args()[0], time.Now().UTC())
	if err != nil {
		return err
	}
	status := "durable"
	if already {
		status = "already-durable"
	}
	fmt.Printf("buffer ack %s id=%s recipient=%s sender=%s\n", status, e.ID, recipient, e.Sender)
	return nil
}

func cmdBufferMigrate(args []string) error {
	fs := flag.NewFlagSet("buffer migrate", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: flotilla buffer migrate [--roster <path>]")
	}
	rp, err := resolveRosterPath(*rosterPath)
	if err != nil {
		return err
	}
	result, err := messagebuffer.MigrateOutboxes(filepath.Dir(rp))
	if err != nil {
		return err
	}
	fmt.Printf("buffer migration complete migrated=%d recipients=%s\n", result.Migrated, strings.Join(result.Recipients, ","))
	return nil
}

func durationDash(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return d.String()
}
