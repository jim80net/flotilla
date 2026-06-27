// Package claudestore reads a Claude Code desk's turn-final assistant text from its own session
// transcript, located from OUTSIDE the session (the watch daemon reads pane STATE, never a Stop-hook
// payload, so it must find the transcript itself). This is the claude-code half of the generalizable
// "read a desk's latest result from its harness session store" capability — the grok half is
// internal/grokstore; the generalizable half is the surface.ResultReader interface + the
// `flotilla result` command, which both this package and the auto-mirror read through.
//
// It ports the extraction logic of the working XO Discord-mirror Stop hook
// (~/.claude/hooks/flotilla-xo-discord-mirror.sh), preserving its four hard-won bug-fixes, rather
// than re-deriving them:
//
//   - walk BACK past trailing non-text entries (tool_result / tool_use / system / attachment / …)
//     to the last text-bearing assistant turn — the hook's "tool-result trigger blindness" fix;
//   - handle a message's `content` as EITHER a list of typed blocks OR a plain string;
//   - take ONLY `text` blocks, skipping `thinking` and `tool_use` blocks;
//   - SKIP sub-agent (`isSidechain`) entries — that is a sub-agent's output, not the desk's own turn;
//   - strip harness-injected command tags and treat an empty residue as not-substantive (no post) —
//     the hook's "command-tag poisoning" fix.
//
// Store layout (live-probed 2026-06-20): the active session for a desk lives under
//
//	~/.claude/projects/<encoded-cwd>/<session-id>.jsonl
//
// where <encoded-cwd> is the pane's working directory with every '/' AND '.' replaced by '-'
// (e.g. /home/operator/workspace/github.com/jim80net/flotilla-dash ->
// -home-operator-workspace-github-com-jim80net-flotilla-dash). A project directory holds MANY sessions;
// the active one is the most-recently-modified .jsonl by mtime.
package claudestore

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
)

// maxLine bounds a single transcript line: a long turn's assistant content (or a fat tool_result
// carried inline) can be tens of kilobytes — far above bufio.Scanner's 64 KB default — so we lift
// the cap to 16 megabytes, matching grokstore. An over-cap line surfaces bufio.ErrTooLong rather
// than being silently truncated; lastTurnText reports the read as failed (ok=false) in that case.
const maxLine = 16 << 20

// quiescenceBeat / quiescenceMaxBeats bound the transcript-flush stabilization wait. The detector
// fires the mirror on the CONFIRMED Working→Idle edge — later than the Stop hook (which fired AT
// turn-end, right in the flush window, and so needed a stabilization loop, the hook's BUG-3). By the
// time the detector observes Idle on a poll, the turn has almost always been fully flushed for a
// while. This is the cheap backstop for the rare tick that lands in the sub-second window between the
// spinner clearing and the final assistant message finishing its write: read the session file's size,
// wait a beat, re-read, and require it to hold stable before extracting — so a still-flushing turn is
// never read (and posted) truncated, the one failure class worse than silence. Bounded so a file that
// keeps growing (a desk that resumed Working between our reads — should not happen post-Idle) can
// never wedge the mirror. Runs in runTail, OUTSIDE the detector mutex, so the wait never touches the
// clock.
const (
	quiescenceBeat     = 300 * time.Millisecond
	quiescenceMaxBeats = 5
)

// sleep and statSize are injected so the stabilization is deterministically unit-testable (a test
// pins sleep to a no-op and statSize to a scripted size sequence). Production uses the real clock +
// filesystem.
var (
	sleep    = time.Sleep
	statSize = func(path string) int64 {
		if info, err := os.Stat(path); err == nil {
			return info.Size()
		}
		return -1
	}
)

// waitQuiescent blocks until path's size holds stable across one beat (or the bound elapses), so a
// turn-final still being flushed to disk is not read mid-write. Size-stability is the signal the XO
// hook used (BUG-3); for the detector's late Working→Idle edge it almost always passes on the first
// re-check, costing one beat.
func waitQuiescent(path string) {
	prev := statSize(path)
	for i := 0; i < quiescenceMaxBeats; i++ {
		sleep(quiescenceBeat)
		cur := statSize(path)
		if cur == prev {
			return
		}
		prev = cur
	}
}

// encodeProjectDir encodes a pane's working directory to the directory name Claude Code uses under
// ~/.claude/projects: every '/' AND '.' becomes '-'. Live-probed (2026-06-20):
// /home/operator/workspace/github.com/jim80net/flotilla-dash ->
// -home-operator-workspace-github-com-jim80net-flotilla-dash.
func encodeProjectDir(cwd string) string {
	enc := strings.ReplaceAll(cwd, "/", "-")
	return strings.ReplaceAll(enc, ".", "-")
}

// projectsRoot returns ~/.claude/projects, or ok=false when the home directory cannot be resolved
// (the same fail-clear-not-bogus-path discipline as grokstore's empty-grokHome guard — never read a
// relative ".claude/projects").
func projectsRoot() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".claude", "projects"), true
}

// sessionsByMtime returns every transcript (*.jsonl) under cwd's project directory, NEWEST mtime
// first, with ok=false when the home directory is unresolvable, the project directory does not
// exist, or it holds no .jsonl files. A project directory accumulates many sessions over a desk's
// life (and, under the lossy-encoding collision, possibly other desks' sessions too); the caller
// walks newest-first and disambiguates by the recorded cwd.
func sessionsByMtime(cwd string) ([]string, bool) {
	root, ok := projectsRoot()
	if !ok {
		return nil, false
	}
	dir := filepath.Join(root, encodeProjectDir(cwd))
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(matches) == 0 {
		return nil, false
	}
	type sessionMod struct {
		path string
		mod  int64
	}
	sms := make([]sessionMod, 0, len(matches))
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue // a file that vanished between glob and stat — skip it, never fail the whole read
		}
		sms = append(sms, sessionMod{m, info.ModTime().UnixNano()})
	}
	if len(sms) == 0 {
		return nil, false
	}
	sort.SliceStable(sms, func(i, j int) bool { return sms[i].mod > sms[j].mod })
	paths := make([]string, len(sms))
	for i, s := range sms {
		paths[i] = s.path
	}
	return paths, true
}

// LatestSession returns the path of the most-recently-modified transcript (*.jsonl) under the
// project directory for cwd. A back-compat convenience over sessionsByMtime; LatestTurnText walks
// ALL sessions newest-first (to disambiguate the encoding collision) rather than trusting this one.
func LatestSession(cwd string) (path string, ok bool) {
	sessions, ok := sessionsByMtime(cwd)
	if !ok {
		return "", false
	}
	return sessions[0], true
}

// transcriptEntry decodes the fields of a transcript line the extraction needs. `Type` is the
// top-level entry type (assistant / user / system / attachment / file-history-snapshot / …);
// `IsSidechain` marks sub-agent output (skipped); `Message` carries the role + content for a message
// entry. Content is a json.RawMessage because Claude Code writes it as EITHER a list of typed blocks
// OR a plain string (see extractText) — decoding straight into one shape would silently drop the
// other.
type transcriptEntry struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	// Cwd is the working directory the session recorded for this entry (present on every
	// user/assistant/attachment entry — live-probed 2026-06-20). It disambiguates the LOSSY project-dir
	// encoding: because both '/' and '.' map to '-', two distinct working dirs (e.g. /a/my.app and
	// /a/my/app) collide to one project directory; comparing the recorded cwd to the desk's actual cwd
	// catches that collision so one desk's turn is never read from (and posted as) another's.
	Cwd     string `json:"cwd"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// contentBlock is one typed block of a list-shaped message content. Only `text` blocks contribute to
// the turn-final text; `thinking` and `tool_use` blocks are skipped (their Text is empty anyway).
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// extractText pulls the spoken text out of a message's content, whether it is a plain JSON string or
// a list of typed blocks. For the list shape it concatenates ONLY the `text` blocks (skipping
// `thinking` / `tool_use`), joined by newlines to match the hook's `"\n".join(...)`.
func extractText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// lastTurnText reverse-walks a transcript and returns the text of the last text-bearing assistant
// turn — the desk's own turn-final output. ok=false means no such turn was found (or the file could
// not be read), which the caller treats as "nothing substantive to mirror".
//
// The walk skips, in the hook's spirit: sub-agent (isSidechain) entries; every non-message entry
// type (system / attachment / file-history-snapshot / ai-title / …); and, within an assistant
// message, everything but its `text` blocks. By scanning the WHOLE file and keeping the LAST
// qualifying assistant turn, it walks back past any trailing tool_result / tool_use / system entries
// that follow the real turn-final (the hook's "tool-result trigger blindness" fix), reading forward
// once rather than indexing backward.
func lastTurnText(jsonlPath string) (string, bool) {
	text, _, ok := lastTurnTextWithCwd(jsonlPath)
	return text, ok
}

// lastTurnTextWithCwd is lastTurnText plus the session's recorded working directory (the last
// non-empty `cwd` seen), so the caller can verify the session actually belongs to the desk it
// resolved — the guard against the lossy project-dir encoding collision. cwd is "" when no entry
// recorded one (then the caller cannot verify and accepts, never a false skip).
func lastTurnTextWithCwd(jsonlPath string) (text string, cwd string, ok bool) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var last string
	found := false
	for sc.Scan() {
		var e transcriptEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue // a torn/partial line is skipped, never fatal — keep the last VALID turn
		}
		if e.Cwd != "" {
			cwd = e.Cwd // all entries share the session's cwd; last non-empty wins
		}
		if e.IsSidechain { // sub-agent output, not the desk's own turn
			continue
		}
		if e.Type != "assistant" || e.Message.Role != "assistant" {
			continue
		}
		if t := extractText(e.Message.Content); strings.TrimSpace(t) != "" {
			last = t
			found = true
		}
	}
	if sc.Err() != nil { // an over-long line (ErrTooLong) or read error — report unread, don't post a fragment
		return "", "", false
	}
	return last, cwd, found
}

// stripTags removes the harness-injected blocks Claude Code embeds in a turn — slash-command
// metadata, captured local-command output, and system reminders — so a turn whose only content is
// command noise classifies as not-substantive. Ported from the hook's _STRIP regex (BUG-2 fix).
var stripTags = regexp.MustCompile(
	`(?s)<command-name>.*?</command-name>` +
		`|<command-message>.*?</command-message>` +
		`|<command-args>.*?</command-args>` +
		`|<local-command-stdout>.*?</local-command-stdout>` +
		`|<local-command-caveat>.*?</local-command-caveat>` +
		`|<system-reminder>.*?</system-reminder>`,
)

// stripAndClassify strips the harness-injected command tags from a turn's text and reports whether
// the residue is substantive. substantive=false (empty/whitespace residue) means the turn was pure
// command noise — there is nothing for the desk to have "said", so the caller skips the post. The
// returned clean text (the residue) is what gets mirrored.
func stripAndClassify(text string) (clean string, substantive bool) {
	clean = strings.TrimSpace(stripTags.ReplaceAllString(text, ""))
	return clean, clean != ""
}

// LatestTurnText returns the desk's substantive turn-final text for the agent at the resolved pane:
// it locates the active session for the pane's working directory, extracts the last text-bearing
// assistant turn, and strips command-tag noise. ok=false means there is nothing substantive to
// mirror — no session located, no completed assistant turn yet, or a turn that was pure command
// noise. err is non-nil only when the pane's working directory could not be resolved (a tmux read
// failure); a located-but-empty session is (ok=false, err=nil), never an error.
func LatestTurnText(pane string) (text string, ok bool, err error) {
	cwd, err := deliver.PaneCWD(pane)
	if err != nil {
		return "", false, err
	}
	text, ok = latestTurnTextForCwd(cwd)
	return text, ok, nil
}

// ReplyAfter returns the XO's verbatim reply to a specific operator message (the #175 hotline
// correlation): it locates the operator's message as a recorded USER turn (the relay delivers it into
// the session, where Claude records it verbatim) and returns the text-bearing ASSISTANT turn that
// follows it. found=false means the reply has not landed yet (the user turn isn't recorded, or no
// assistant turn follows it) — the watcher keeps polling. Correlating to the user turn (NOT a bare
// turn-count delta) is what makes the reply the answer to THIS message, immune to a queued/interleaved
// turn being mis-routed. err is non-nil only on a pane→cwd resolution failure.
func ReplyAfter(pane, operatorMsg string) (text string, found bool, err error) {
	cwd, err := deliver.PaneCWD(pane)
	if err != nil {
		return "", false, err
	}
	text, found = replyAfterForCwd(cwd, operatorMsg)
	return text, found, nil
}

// replyAfterForCwd resolves the authoritative session for cwd (the same collision-guarded selection as
// latestTurnTextForCwd) and returns the text-bearing assistant turn following the LATEST user turn that
// carries operatorMsg.
func replyAfterForCwd(cwd, operatorMsg string) (string, bool) {
	sessions, ok := sessionsByMtime(cwd)
	if !ok {
		return "", false
	}
	for _, sessionPath := range sessions {
		waitQuiescent(sessionPath)
		raw, sessionCwd, found := replyAfterUserMsg(sessionPath, operatorMsg)
		if sessionCwd != "" && sessionCwd != cwd {
			continue // a colliding desk's session — keep looking for THIS desk's newest
		}
		if !found {
			return "", false // THIS desk's authoritative session has no reply yet; no stale fallback
		}
		clean, substantive := stripAndClassify(raw)
		if !substantive {
			return "", false
		}
		return clean, true
	}
	return "", false
}

// normMsg normalizes an operator message for an EXACT (not substring) anchor match: collapse all
// whitespace runs to single spaces and trim, so a paste that alters a newline/trailing space still
// matches but a DIFFERENT message (or a turn that merely contains this one as a substring) does not.
func normMsg(s string) string { return strings.Join(strings.Fields(s), " ") }

// replyAfterUserMsg scans a transcript and returns the text-bearing assistant turn following the LATEST
// user turn whose recorded text contains operatorMsg. A new occurrence of operatorMsg (a re-asked
// message) re-anchors — the reply must follow the MOST RECENT delivery. cwd is the session's recorded
// cwd (the collision guard). found=false ⇒ the matching user turn isn't recorded yet, or no assistant
// turn follows it.
func replyAfterUserMsg(jsonlPath, operatorMsg string) (text string, cwd string, found bool) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	want := normMsg(operatorMsg)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	armed := false // saw the matching user turn; an assistant turn after it is the reply
	for sc.Scan() {
		var e transcriptEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if e.Cwd != "" {
			cwd = e.Cwd
		}
		if e.IsSidechain {
			continue
		}
		switch {
		case e.Type == "user" && e.Message.Role == "user":
			ut := extractText(e.Message.Content)
			switch {
			case want != "" && normMsg(ut) == want:
				// EXACT (whitespace-normalized) match — NOT a substring. A substring match would let a
				// short/common operator message ("ok", "status?") re-anchor to a LATER turn that merely
				// CONTAINS it (a self-cont prompt, a quote), mis-routing that turn's output. The relay
				// records the operator message verbatim as a user turn (live-verified), so exact-after-
				// whitespace-normalization is the correct, mis-route-proof anchor.
				armed = true // (re-)anchor to this delivery of the operator message
				text, found = "", false
			case armed && strings.TrimSpace(ut) != "":
				// A SUBSTANTIVE non-anchor user turn (a self-continuation wake, a later prompt) CLOSES the
				// reply window — a new turn began, so a later assistant turn is NOT the answer to THIS
				// message. (tool_result user entries have empty text, so they do NOT close it — the
				// tool-result walk-back to the real turn-final is preserved.)
				armed = false
			}
		case e.Type == "assistant" && e.Message.Role == "assistant":
			if armed {
				if t := extractText(e.Message.Content); strings.TrimSpace(t) != "" {
					text, found = t, true // the latest text-bearing assistant turn after the user msg
				}
			}
		}
	}
	if sc.Err() != nil {
		return "", "", false
	}
	return text, cwd, found
}

// latestTurnTextForCwd is LatestTurnText minus the tmux read, so the session selection + the
// collision disambiguation are unit-testable. It walks the project dir's sessions NEWEST first and:
//   - SKIPS a session whose recorded cwd disagrees with cwd — a COLLIDING desk's transcript (the
//     lossy-encoding guard, cubic P2): keep looking for THIS desk's session rather than bailing, so a
//     valid mirror is not dropped just because a colliding desk wrote more recently.
//   - Once it reaches THIS desk's newest session (matching, or an unverifiable empty cwd), that
//     session is AUTHORITATIVE — it returns that session's turn-final (or ok=false when it has none
//     substantive). It does NOT fall through to an OLDER session in that case, because an older
//     session's turn is stale and re-posting it would duplicate a turn already mirrored.
func latestTurnTextForCwd(cwd string) (string, bool) {
	sessions, ok := sessionsByMtime(cwd)
	if !ok {
		return "", false
	}
	for _, sessionPath := range sessions {
		waitQuiescent(sessionPath) // let a still-flushing turn-final settle before extracting (no truncated post)
		raw, sessionCwd, ok := lastTurnTextWithCwd(sessionPath)
		if sessionCwd != "" && sessionCwd != cwd {
			continue // a colliding desk's session — keep looking for THIS desk's newest
		}
		// THIS desk's newest session (cwd matches, or is unverifiable) — authoritative; no stale fallback.
		if !ok {
			return "", false
		}
		clean, substantive := stripAndClassify(raw)
		if !substantive {
			return "", false
		}
		return clean, true
	}
	return "", false
}
