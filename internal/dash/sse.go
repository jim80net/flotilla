package dash

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// maxSSEClients caps concurrent SSE connections so a flood of EventSource
// connections cannot exhaust goroutines/file descriptors. A client over the cap
// is refused with 503 and falls back to polling /api/status.
const maxSSEClients = 64

// pollInterval is the stat-poll cadence for detecting artifact changes (design
// §2: ~1s, stdlib-only — no fsnotify dependency in Phase 1).
const pollInterval = time.Second

// sseClient is one connected EventSource. events is buffered so a momentarily
// slow reader does not block the hub's fan-out; a client whose buffer is full is
// DROPPED (closed), never allowed to block the poller.
type sseClient struct {
	events chan string
}

// hub is the SSE fan-out: a single set of registered clients plus the
// register/unregister/broadcast channels that mutate it. One goroutine (run)
// owns the client set, so no mutex is needed and fan-out is race-free.
//
// done is closed when run exits (on ctx cancel). Every client-facing producer
// (add/remove/emit/count) selects on it so that, once the hub is shutting down,
// a send no longer has a receiver — instead of blocking forever it falls
// through. Without this a handler parked mid-register, or running its deferred
// remove after run exits, would block the goroutine permanently and stall
// srv.Shutdown (the graceful-stop invariant). The fix makes shutdown leak-free
// and deterministic.
type hub struct {
	register   chan registerReq
	unregister chan *sseClient
	broadcast  chan string
	clients    map[*sseClient]bool
	countReq   chan chan int // test/introspection: ask for the live client count
	done       chan struct{} // closed by run on exit
}

// registerReq asks run to admit a client. The cap decision is made by run (the
// single owner of the client set) and returned on reply, so the cap is exact —
// no check-then-register TOCTOU where concurrent connects overshoot maxSSEClients.
type registerReq struct {
	client *sseClient
	reply  chan bool // buffered (cap 1) so run never blocks replying
}

func newHub() *hub {
	return &hub{
		register:   make(chan registerReq),
		unregister: make(chan *sseClient),
		broadcast:  make(chan string),
		clients:    make(map[*sseClient]bool),
		countReq:   make(chan chan int),
		done:       make(chan struct{}),
	}
}

// run owns the client set until ctx is cancelled. It is the ONLY goroutine that
// touches s.clients, so registration, removal, and fan-out never race. On exit
// it closes done so producers stop blocking on the (now receiver-less) channels.
func (h *hub) run(ctx context.Context) {
	defer close(h.done)
	for {
		select {
		case <-ctx.Done():
			for c := range h.clients {
				close(c.events)
				delete(h.clients, c)
			}
			return
		case req := <-h.register:
			// The cap is enforced HERE, atomically, against the authoritative
			// client count — never a check-then-act race in the caller.
			if len(h.clients) >= maxSSEClients {
				req.reply <- false
			} else {
				h.clients[req.client] = true
				req.reply <- true
			}
		case c := <-h.unregister:
			if h.clients[c] {
				delete(h.clients, c)
				close(c.events)
			}
		case msg := <-h.broadcast:
			for c := range h.clients {
				// Non-blocking send: a client whose buffer is full is dropped (its
				// events channel closed and removed) rather than blocking the hub.
				select {
				case c.events <- msg:
				default:
					delete(h.clients, c)
					close(c.events)
				}
			}
		case reply := <-h.countReq:
			reply <- len(h.clients)
		}
	}
}

// add registers a client, returning false if the connection cap is reached (the
// caller then refuses the connection) OR the hub is shutting down. The cap is
// decided by run under its single-owner lock (exact, no overshoot). Safe from a
// handler goroutine: it never blocks past run's exit (the done guard).
func (h *hub) add(c *sseClient) bool {
	reply := make(chan bool, 1)
	select {
	case h.register <- registerReq{client: c, reply: reply}:
		select {
		case ok := <-reply:
			return ok
		case <-h.done:
			return false
		}
	case <-h.done:
		return false
	}
}

func (h *hub) remove(c *sseClient) {
	select {
	case h.unregister <- c:
	case <-h.done:
	}
}

func (h *hub) emit(msg string) {
	select {
	case h.broadcast <- msg:
	case <-h.done:
	}
}

// count returns the live client count (via the hub goroutine, so it is
// consistent with run's view); 0 once the hub is shutting down.
func (h *hub) count() int {
	reply := make(chan int)
	select {
	case h.countReq <- reply:
		return <-reply
	case <-h.done:
		return 0
	}
}

// handleEvents serves the SSE stream. It registers with the hub, streams events
// until the client disconnects (Request.Context().Done()) or the buffer overflows
// (the hub drops it), and DEREGISTERS on exit so a disconnected client never
// leaks. An initial event prompts the client to do its reconcile-on-connect read
// of /api/status.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	client := &sseClient{events: make(chan string, 8)}
	if !s.hub.add(client) {
		http.Error(w, "too many SSE clients — fall back to polling /api/status", http.StatusServiceUnavailable)
		return
	}
	defer s.hub.remove(client)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Prompt an immediate reconcile read on (re)connect.
	fmt.Fprint(w, "event: update\ndata: connected\n\n")
	flusher.Flush()

	// A keepalive comment every 25s keeps intermediaries from idling the stream
	// out before the IdleTimeout; it also detects a dead client promptly.
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-client.events:
			if !ok {
				return // hub dropped or closed us
			}
			fmt.Fprintf(w, "event: update\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// poll is the single shared file poller. It stats the snapshot, ledger, backlog,
// goals JSON/YAML, and session-mirror ledgers every pollInterval and emits an SSE
// update whenever any signature changes — so all connected clients refetch the
// JSON endpoints. One poller serves every client (not one per connection).
func (s *Server) poll(ctx context.Context) {
	paths := []string{s.cfg.SnapshotPath, s.cfg.LedgerPath, s.cfg.BacklogPath, s.cfg.GoalsPath, s.cfg.GoalsYAMLPath}
	prev := fileSigs(paths, s.cfg.SessionMirrorDir)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cur := fileSigs(paths, s.cfg.SessionMirrorDir)
			if cur != prev {
				// #418: a change to any goals-roll-up input (board snapshot, backlog,
				// goals json/yaml) is a done-history observation trigger — loadGoals
				// records achieve/regress transitions, so history accrues even when no
				// browser is open. Async: the tracker bind inside loadGoals can be slow
				// and must not delay the SSE emit; the recorder serializes appends.
				if cur.snap != prev.snap || cur.backlog != prev.backlog ||
					cur.goals != prev.goals || cur.goalsYAML != prev.goalsYAML {
					go s.loadGoals()
				}
				prev = cur
				s.hub.emit(s.now().UTC().Format(time.RFC3339))
			}
		}
	}
}

// fileSig is a file's change signature: its modification time (unix nanos) and
// size. Comparing (mtime,size) catches a same-second rewrite that changes the
// size even when the mtime second is unchanged; a same-second same-size change
// (rare) is reconciled by the client's /api/status poll, the authoritative read.
type fileSig struct {
	mtime int64
	size  int64
	exist bool
}

// sigBundle is the combined signature of all watched files (a comparable value
// so the poller can detect "any change" with a single ==).
type sigBundle struct {
	snap, ledger, backlog, goals, goalsYAML fileSig
	sessionMirror                           fileSig
}

// statSig deliberately collapses "absent" and "stat error" to the same
// zero-value signature. The poller is ONLY a change-TRIGGER: a signature change
// tells clients to refetch /api/status (the authoritative read that reports the
// honest absent/stale/fresh state via loadBoard). So a transient stat error has
// only two harmless outcomes — no signature change (no event), or a single
// spurious refresh when the file recovers (clients refetch the authoritative
// read anyway). Logging here would spam stderr once per poll during an outage
// for no operator benefit; the real freshness signal is surfaced by loadBoard,
// not by this trigger. The swallow is intentional, not hidden.
func statSig(path string) fileSig {
	if path == "" {
		return fileSig{}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fileSig{}
	}
	return fileSig{mtime: fi.ModTime().UnixNano(), size: fi.Size(), exist: true}
}

// fileSigs computes the combined signature for [snapshot, ledger, backlog, goals,
// goalsYAML] plus the aggregated session-mirror/ ledger mtimes.
func fileSigs(paths []string, sessionMirrorDir string) sigBundle {
	return sigBundle{
		snap:          statSig(paths[0]),
		ledger:        statSig(paths[1]),
		backlog:       statSig(paths[2]),
		goals:         statSig(paths[3]),
		goalsYAML:     statSig(paths[4]),
		sessionMirror: sessionMirrorDirSig(sessionMirrorDir),
	}
}

// sessionMirrorDirSig aggregates max(mtime) and sum(size) across *.jsonl ledgers
// under session-mirror/ so append events fire SSE even when the directory mtime
// itself is unchanged.
func sessionMirrorDirSig(dir string) fileSig {
	if dir == "" {
		return fileSig{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fileSig{}
	}
	var sig fileSig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		sig.exist = true
		m := fi.ModTime().UnixNano()
		if m > sig.mtime {
			sig.mtime = m
		}
		sig.size += fi.Size()
	}
	return sig
}
