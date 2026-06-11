//go:build voiceopus

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/voice"
)

// cmdVoice runs the operator↔XO voice process (the voiceopus build only — it links the real
// libopus codec). It loads config, opens its OWN discordgo session with voice intents, and
// supervises a joined voice channel running the inbound + outbound pipelines. A single shared
// cost Meter caps STT + TTS spend; a SpeakerTable fail-closed gate restricts injection to the
// operator. The process is isolated from flotilla-watch (the clock) per design.
func cmdVoice(args []string) error {
	fs := flag.NewFlagSet("voice", flag.ContinueOnError)
	configPath := fs.String("config", "state/voice.env", "voice env file (XAI key + channel/operator/cap)")
	rosterPath := fs.String("roster", os.Getenv("FLOTILLA_ROSTER"), "roster config (default ./flotilla.json or $FLOTILLA_ROSTER)")
	secretsPath := fs.String("secrets", os.Getenv("FLOTILLA_SECRETS"), "secrets env file (discord bot token)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadVoiceConfig(*configPath)
	if err != nil {
		return err
	}
	secrets, err := roster.LoadSecrets(*secretsPath)
	if err != nil {
		return fmt.Errorf("voice: load secrets (bot token): %w", err)
	}
	rcfg, err := roster.Load(*rosterPath)
	if err != nil {
		return fmt.Errorf("voice: load roster: %w", err)
	}
	xo, err := rcfg.Agent(cfg.XOAgent)
	if err != nil {
		return fmt.Errorf("voice: XO agent %q not in roster: %w", cfg.XOAgent, err)
	}
	drv, ok := surface.Get(xo.Surface)
	if !ok {
		return fmt.Errorf("voice: unknown surface %q for XO agent %q", xo.Surface, cfg.XOAgent)
	}
	pane, err := deliver.ResolvePane(xo.Title())
	if err != nil {
		return fmt.Errorf("voice: resolve XO pane: %w", err)
	}

	// Two separate codecs (one decode-only for inbound, one encode-only for outbound) so each
	// pipeline owns its libopus state — honoring the codec's one-per-stream contract.
	decCodec, err := voice.NewOpusCodec()
	if err != nil {
		return fmt.Errorf("voice: %w", err)
	}
	defer decCodec.Close()
	encCodec, err := voice.NewOpusCodec()
	if err != nil {
		return fmt.Errorf("voice: %w", err)
	}
	defer encCodec.Close()

	provider := voice.NewGrokProvider(cfg.XAIKey)
	meter := voice.NewMeter(cfg.CapUSD, voice.GrokVoiceCaps)
	gate := voice.NewSpeakerTable(cfg.OperatorUserID)
	injector := paneInjector{pane: pane, drv: drv}

	// Operator notices go to the LOG (journalctl), NOT the XO pane: a voice-status line pasted
	// into the XO composer would itself be injected as an XO command. Reaching the operator in
	// the channel (spoken / text-channel) is a documented refinement.
	notice := func(msg string) { log.Printf("flotilla voice: %s", msg) }

	// Our OWN discordgo session with voice intents (the relay's gateway carries only
	// Guild-Messages; ChannelVoiceJoin's handshake needs voice-state events — P2-1).
	dg, err := discordgo.New("Bot " + secrets.BotToken())
	if err != nil {
		return fmt.Errorf("voice: discord session: %w", err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildVoiceStates
	if err := dg.Open(); err != nil {
		return fmt.Errorf("voice: open discord gateway: %w", err)
	}
	defer dg.Close()

	connect := func(_ context.Context) (voice.Session, <-chan struct{}, func(), error) {
		return voice.JoinVoice(dg, cfg.GuildID, cfg.ChannelID)
	}
	run := func(ctx context.Context, sess voice.Session) {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = voice.NewInboundPipeline(sess, decCodec, provider, meter, gate, injector, notice, voice.InboundConfig{}).Run(ctx)
		}()
		go func() {
			defer wg.Done()
			_ = voice.NewOutboundPipeline(sess, encCodec, provider, meter, notice, voice.OutboundConfig{}).Run(ctx)
		}()
		wg.Wait()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Printf("flotilla voice: ready (guild=%s channel=%s XO-pane=%s cap=$%.2f)", cfg.GuildID, cfg.ChannelID, pane, cfg.CapUSD)
	return voice.Supervise(ctx, connect, run, notice, voice.SuperviseConfig{})
}

// paneInjector is the real PaneInjector: busy via the surface driver's pane assessment, inject
// via deliver.Send (which takes the per-pane cross-process lock, P1-2).
type paneInjector struct {
	pane string
	drv  surface.Driver
}

func (p paneInjector) Busy() bool               { return p.drv.Assess(p.pane) == surface.StateWorking }
func (p paneInjector) Inject(text string) error { return deliver.Send(p.pane, text) }
