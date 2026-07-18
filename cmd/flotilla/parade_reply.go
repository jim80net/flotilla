package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/paradeconversation"
	"github.com/jim80net/flotilla/internal/roster"
)

// cmdParadeReply appends a fleet-authored reply to the same durable slide thread
// the operator reads. Author identity is FLOTILLA_SELF and cannot be overridden by
// request text or a flag.
func cmdParadeReply(args []string) error {
	fs := flag.NewFlagSet("parade reply", flag.ContinueOnError)
	date := fs.String("date", "", "parade date (YYYY-MM-DD)")
	slide := fs.Int("slide", 0, "one-based slide number")
	text := fs.String("text", "", "reply text")
	file := fs.String("file", "", "read reply text from file ('-' for stdin)")
	kind := fs.String("kind", "note", "note, kudos, invest, or feedback")
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	paradesDir := fs.String("parades-dir", "", "parade archive root (default <roster-dir>/parades)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 || *date == "" || *slide < 1 || (*text == "" && *file == "") || (*text != "" && *file != "") {
		return fmt.Errorf("usage: flotilla parade reply --date YYYY-MM-DD --slide N (--text <reply> | --file <path|->) [--kind note] [--roster <path>]")
	}
	parsedDate, err := time.Parse("2006-01-02", *date)
	if err != nil || parsedDate.Format("2006-01-02") != *date {
		return fmt.Errorf("parade reply: invalid date %q (want YYYY-MM-DD)", *date)
	}
	author := strings.TrimSpace(os.Getenv("FLOTILLA_SELF"))
	if author == "" {
		return fmt.Errorf("parade reply: fleet identity required (set $FLOTILLA_SELF)")
	}
	resolvedRoster, err := resolveRosterPath(*rosterPath)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(resolvedRoster)
	if err != nil {
		return err
	}
	if _, err := cfg.Agent(author); err != nil {
		return fmt.Errorf("parade reply: FLOTILLA_SELF %q is not a roster seat: %w", author, err)
	}
	if *paradesDir == "" {
		*paradesDir = filepath.Join(filepath.Dir(resolvedRoster), "parades")
	}
	archiveDir := filepath.Join(*paradesDir, *date)
	info, err := os.Lstat(archiveDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("parade reply: parade %s not found", *date)
	}
	titles, err := paradeconversation.SlideTitles(archiveDir, *date)
	if err != nil {
		return fmt.Errorf("parade reply: read slides: %w", err)
	}
	index := *slide - 1
	if index >= len(titles) {
		return fmt.Errorf("parade reply: slide %d is outside parade %s (%d slides)", *slide, *date, len(titles))
	}
	body := *text
	if *file != "" {
		if *file == "-" {
			if fi, statErr := os.Stdin.Stat(); statErr == nil && fi.Mode()&os.ModeCharDevice != 0 {
				return fmt.Errorf("parade reply: --file - requires piped stdin")
			}
			raw, readErr := io.ReadAll(os.Stdin)
			if readErr != nil {
				return fmt.Errorf("parade reply: read stdin: %w", readErr)
			}
			body = string(raw)
		} else {
			raw, readErr := os.ReadFile(*file)
			if readErr != nil {
				return fmt.Errorf("parade reply: read %s: %w", *file, readErr)
			}
			body = string(raw)
		}
	}
	message, err := paradeconversation.NewMessage(author, *kind, body, time.Now())
	if err != nil {
		return fmt.Errorf("parade reply: %w", err)
	}
	if _, err := paradeconversation.Append(filepath.Join(archiveDir, "conversations.json"), index, titles[index], message); err != nil {
		return fmt.Errorf("parade reply: save: %w", err)
	}
	fmt.Printf("parade reply saved date=%s slide=%d author=%s id=%s\n", *date, *slide, author, message.ID)
	return nil
}
