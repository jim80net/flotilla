package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/harnessquality"
	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/sessionmirror"
	"github.com/jim80net/flotilla/internal/workspace"
)

func cmdQuality(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla quality <context|record|show> [flags]")
	}
	switch args[0] {
	case "context":
		return cmdQualityContext(args[1:])
	case "record":
		return cmdQualityRecord(args[1:])
	case "show":
		return cmdQualityShow(args[1:])
	default:
		return fmt.Errorf("unknown quality command %q (want context, record, or show)", args[0])
	}
}

func cmdQualityContext(args []string) error {
	fs := flag.NewFlagSet("quality context", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	workClass := fs.String("work-class", "", "strategic, maintenance, or ktlo")
	workRef := fs.String("work-ref", "", "stable issue/PR/task reference")
	harnessVersion := fs.String("harness-version", "", "live harness version when known")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: flotilla quality context <seat> --work-class <strategic|maintenance|ktlo> [--work-ref <ref>] [--harness-version <version>]")
	}
	seat := fs.Arg(0)
	if _, err := loadQualityRoster(*rosterPath, seat); err != nil {
		return err
	}
	ctx := harnessquality.Context{
		Seat: seat, WorkClass: harnessquality.WorkClass(strings.TrimSpace(*workClass)),
		WorkRef: strings.TrimSpace(*workRef), HarnessVersion: strings.TrimSpace(*harnessVersion),
	}
	if err := harnessquality.WriteContext(filepath.Dir(*rosterPath), ctx); err != nil {
		return err
	}
	fmt.Printf("quality context %s: class=%s ref=%s\n", seat, ctx.WorkClass, valueOrUnknown(ctx.WorkRef))
	return nil
}

func cmdQualityRecord(args []string) error {
	fs := flag.NewFlagSet("quality record", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	launchPath := fs.String("launch", "", "host-local launch recipes (default next to roster)")
	kind := fs.String("event", "", "completion or gate")
	outcome := fs.String("outcome", "", "completed, merged, abandoned, passed, or bounced")
	workClass := fs.String("work-class", "", "override context work class")
	workRef := fs.String("work-ref", "", "override context issue/PR/task reference")
	harnessVersion := fs.String("harness-version", "", "override context harness version")
	bounceCount := fs.Int("bounce-count", 0, "event-local bounce count")
	reworkCount := fs.Int("rework-count", 0, "rework count carried by this completion")
	sessionPtr := fs.String("session-mirror-ptr", "", "optional transcript record pointer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: flotilla quality record <seat> --event <completion|gate> --outcome <outcome> [flags]")
	}
	seat := fs.Arg(0)
	cfg, err := loadQualityRoster(*rosterPath, seat)
	if err != nil {
		return err
	}
	rosterDir := filepath.Dir(*rosterPath)
	ctx, ok, err := harnessquality.ReadContext(rosterDir, seat)
	if err != nil {
		return err
	}
	if !ok {
		ctx = harnessquality.Context{Seat: seat, WorkClass: harnessquality.WorkClass(strings.TrimSpace(*workClass))}
	}
	if strings.TrimSpace(*workClass) != "" {
		ctx.WorkClass = harnessquality.WorkClass(strings.TrimSpace(*workClass))
	}
	if !harnessquality.ValidWorkClass(ctx.WorkClass, false) {
		return fmt.Errorf("quality record requires --work-class or a valid quality context")
	}
	if strings.TrimSpace(*workRef) != "" {
		ctx.WorkRef = strings.TrimSpace(*workRef)
	}
	if strings.TrimSpace(*harnessVersion) != "" {
		ctx.HarnessVersion = strings.TrimSpace(*harnessVersion)
	}
	if *launchPath == "" {
		*launchPath = launch.DefaultPath(*rosterPath)
	}
	flat, err := loadQualityLaunch(*launchPath, cfg)
	if err != nil {
		return err
	}
	surfaceName, model := qualityHarnessMetadata(cfg, flat, seat)
	event, err := harnessquality.Append(rosterDir, harnessquality.Event{
		Seat: seat, Kind: harnessquality.EventKind(*kind), Outcome: harnessquality.Outcome(*outcome),
		WorkClass: ctx.WorkClass, WorkRef: ctx.WorkRef, Surface: surfaceName, Model: model,
		HarnessVersion: ctx.HarnessVersion, FlotillaVersion: qualityFlotillaVersion(),
		BounceCount: *bounceCount, ReworkCount: *reworkCount, SessionMirrorPtr: strings.TrimSpace(*sessionPtr),
	})
	if err != nil {
		return err
	}
	fmt.Printf("quality event %s: %s/%s seat=%s surface=%s model=%s class=%s\n", event.ID, event.Kind, event.Outcome, seat, event.Surface, event.Model, event.WorkClass)
	return nil
}

func cmdQualityShow(args []string) error {
	fs := flag.NewFlagSet("quality show", flag.ContinueOnError)
	rosterPath := fs.String("roster", rosterDefault(), "roster config path")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: flotilla quality show [--json] [--roster <path>]")
	}
	summary := harnessquality.LoadSummary(filepath.Dir(*rosterPath), time.Now())
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}
	fmt.Printf("Harness quality — events:%d · classified:%s%% · gates:%d · bounce:%s%% · completions:%d · rework:%s%%\n",
		summary.TotalEvents, oneDecimal(summary.TaggingCoveragePercent), summary.GateEvents,
		oneDecimal(summary.BounceRatePercent), summary.CompletionEvents, oneDecimal(summary.ReworkRatePercent))
	if summary.State != "available" {
		fmt.Printf("  unavailable: %s\n", summary.Diagnostic)
		return nil
	}
	for _, group := range summary.Groups {
		fmt.Printf("  %s/%s · %s · events:%d gates:%d bounce:%s%% completions:%d rework:%s%% · harness:%s flotilla:%s\n",
			group.Surface, group.Model, group.WorkClass, group.Events, group.GateEvents,
			oneDecimal(group.BounceRatePercent), group.CompletionEvents, oneDecimal(group.ReworkRatePercent),
			strings.Join(group.HarnessVersions, ","), strings.Join(group.FlotillaVersions, ","))
	}
	return nil
}

func loadQualityRoster(path, seat string) (*roster.Config, error) {
	cfg, err := roster.Load(path)
	if err != nil {
		return nil, err
	}
	if _, err := cfg.Agent(seat); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadQualityLaunch(path string, cfg *roster.Config) (*launch.Config, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	agents := make(map[string]bool, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		agents[agent.Name] = true
	}
	return launch.Load(path, agents)
}

func qualityHarnessMetadata(cfg *roster.Config, flat *launch.Config, seat string) (string, string) {
	agent, _ := cfg.Agent(seat)
	surfaceName := effectiveSurface(agent.Surface)
	model := "unknown"
	slotName := workspace.SlotPrimary
	if overlay, ok, err := workspace.ReadActiveOverlay(seat); err == nil && ok {
		if overlay.Slot != "" {
			slotName = overlay.Slot
		}
		if overlay.Surface != "" {
			surfaceName = overlay.Surface
		}
	}
	if flat == nil {
		return surfaceName, model
	}
	recipe, ok := flat.Recipe(seat)
	if !ok {
		return surfaceName, model
	}
	for _, slot := range recipe.Slots() {
		if slot.Name != slotName {
			continue
		}
		if slot.Surface != "" {
			surfaceName = slot.Surface
		}
		if slot.Model != "" {
			model = slot.Model
		}
		break
	}
	return surfaceName, model
}

func finishQualityAppend(cfg *roster.Config, flat *launch.Config, rosterDir string) func(string, sessionmirror.Record) error {
	return func(seat string, rec sessionmirror.Record) error {
		ctx, ok, err := harnessquality.ReadContext(rosterDir, seat)
		if err != nil {
			return err
		}
		if !ok {
			ctx = harnessquality.Context{Seat: seat, WorkClass: harnessquality.WorkUnclassified}
		}
		surfaceName, model := qualityHarnessMetadata(cfg, flat, seat)
		_, err = harnessquality.Append(rosterDir, harnessquality.Event{
			TS: rec.TS, Seat: seat, Kind: harnessquality.KindCompletion, Outcome: harnessquality.OutcomeCompleted,
			WorkClass: ctx.WorkClass, WorkRef: ctx.WorkRef, Surface: surfaceName, Model: model,
			HarnessVersion: ctx.HarnessVersion, FlotillaVersion: qualityFlotillaVersion(),
			SessionMirrorPtr: filepath.ToSlash(filepath.Join("session-mirror", seat+".jsonl")) + "@" + rec.TS,
		})
		return err
	}
}

func oneDecimal(value float64) string { return strconv.FormatFloat(value, 'f', 1, 64) }

func qualityFlotillaVersion() string {
	revision := dashBuildRevision()
	if revision == "" || revision == "unavailable" {
		return version + "+revision-unavailable"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	return version + "+" + revision
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
