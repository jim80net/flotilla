package dash

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/dash/tracker"
	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/outbox"
)

const (
	defaultWorkTimelineLimit = 20
	maxWorkTimelineLimit     = 100
	maxTimelineGitHubRefs    = 8
)

// WorkTimelineEventKind is the stable vocabulary shared by Goals and Issues.
// States remain separate from kinds so queued, delivered, acknowledged, gated,
// merged, superseded, and terminal outcomes can never collapse into "done".
type WorkTimelineEventKind string

const (
	TimelineIdentity    WorkTimelineEventKind = "identity"
	TimelineAssignment  WorkTimelineEventKind = "assignment"
	TimelineDispatch    WorkTimelineEventKind = "dispatch"
	TimelineGitHubIssue WorkTimelineEventKind = "github_issue"
	TimelineGitHubPR    WorkTimelineEventKind = "github_pr"
	TimelineGate        WorkTimelineEventKind = "gate"
	TimelineOutcome     WorkTimelineEventKind = "outcome"
)

// WorkTimelineEvent is one read-only fact. SourceID and SourceURL preserve the
// native durable identity; ID only disambiguates multiple lifecycle facts from
// the same source object inside this composed response.
type WorkTimelineEvent struct {
	ID        string                `json:"id"`
	Kind      WorkTimelineEventKind `json:"kind"`
	State     string                `json:"state"`
	At        string                `json:"at,omitempty"`
	Actor     string                `json:"actor,omitempty"`
	Title     string                `json:"title"`
	Detail    string                `json:"detail,omitempty"`
	Source    string                `json:"source"`
	SourceID  string                `json:"source_id"`
	SourceURL string                `json:"source_url,omitempty"`
	Cursor    string                `json:"source_cursor,omitempty"`
}

// WorkTimelineSource reports coverage independently for every adapter. Status
// is available, stale, partial, or unavailable; callers must not infer complete
// history merely because one adapter returned events.
type WorkTimelineSource struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
}

type WorkTimelineSubject struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	State     string `json:"state,omitempty"`
	SourceID  string `json:"source_id"`
	SourceURL string `json:"source_url,omitempty"`
}

// WorkTimelineDoc is the shared API contract rendered by both Goals and Issues.
type WorkTimelineDoc struct {
	Subject     WorkTimelineSubject  `json:"subject"`
	Events      []WorkTimelineEvent  `json:"events"`
	Sources     []WorkTimelineSource `json:"sources"`
	GeneratedAt string               `json:"generated_at"`
	Partial     bool                 `json:"partial"`
	Total       int                  `json:"total"`
	NextCursor  string               `json:"next_cursor,omitempty"`
}

type workTimelineBuild struct {
	subject WorkTimelineSubject
	events  []WorkTimelineEvent
	sources []WorkTimelineSource
	refs    []string
}

var timelineRefPattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*)#([1-9][0-9]*)$`)

func (s *Server) handleWorkTimeline(w http.ResponseWriter, r *http.Request) {
	limit := defaultWorkTimelineLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "timeline limit must be a positive integer")
			return
		}
		if n > maxWorkTimelineLimit {
			n = maxWorkTimelineLimit
		}
		limit = n
	}

	goalID := strings.TrimSpace(r.URL.Query().Get("goal"))
	issueRaw := strings.TrimSpace(r.URL.Query().Get("issue"))
	if (goalID == "") == (issueRaw == "") {
		writeError(w, http.StatusBadRequest, "choose exactly one timeline subject: goal or issue")
		return
	}

	var build workTimelineBuild
	if goalID != "" {
		var ok bool
		build, ok = s.goalTimelineBuild(r.Context(), goalID)
		if !ok {
			writeError(w, http.StatusNotFound, "goal not found")
			return
		}
	} else {
		number, err := strconv.Atoi(issueRaw)
		if err != nil || number <= 0 {
			writeError(w, http.StatusBadRequest, "timeline issue must be a positive integer")
			return
		}
		repo := strings.TrimSpace(r.URL.Query().Get("repo"))
		if repo == "" {
			repo = s.cfg.Repo
		}
		build = s.issueTimelineBuild(r.Context(), repo, number)
	}

	rosterDir := filepath.Dir(s.cfg.RosterPath)
	dispatchEvents, dispatchSource := buildDispatchTimeline(rosterDir, build.refs)
	build.events = append(build.events, dispatchEvents...)
	build.sources = append(build.sources, dispatchSource)
	writeJSON(w, buildWorkTimelineDoc(build, s.now(), limit, r.URL.Query().Get("before")))
}

func (s *Server) goalTimelineBuild(ctx context.Context, goalID string) (workTimelineBuild, bool) {
	doc := s.loadGoals()
	for _, goal := range doc.Goals {
		if goal.ID != goalID {
			continue
		}
		build := workTimelineBuild{
			subject: WorkTimelineSubject{
				Kind: "goal", ID: goal.ID, Title: goal.Title, State: goal.StatusDisplay,
				SourceID: goal.ID,
			},
			refs: []string{goal.ID},
			sources: []WorkTimelineSource{{
				ID: "goals", Label: "Goals", Status: "available",
				Detail:    "Authored goal identity and current work-item roll-up.",
				UpdatedAt: doc.GeneratedAt,
			}},
		}
		build.events = append(build.events, WorkTimelineEvent{
			ID: "goals/identity/" + goal.ID, Kind: TimelineIdentity, State: goal.StatusDisplay,
			Actor: goal.Owner, Title: "Goal selected", Detail: goal.Title,
			Source: "goals", SourceID: goal.ID,
		})
		seenActors := map[string]bool{}
		addAssignment := func(actor, sourceID, detail string) {
			actor = strings.TrimSpace(actor)
			if actor == "" || seenActors[strings.ToLower(actor)] {
				return
			}
			seenActors[strings.ToLower(actor)] = true
			build.events = append(build.events, WorkTimelineEvent{
				ID: "goals/assignment/" + sourceID + "/" + actor, Kind: TimelineAssignment,
				State: "assigned", Actor: actor, Title: "Work mapped to " + actor,
				Detail: detail, Source: "goals", SourceID: sourceID,
			})
		}
		addAssignment(goal.Owner, goal.ID, "Goal owner")
		addAssignment(goal.ConversationAgent, goal.ID, "Conversation desk")
		for i, item := range goal.WorkItems {
			sourceID := fmt.Sprintf("%s/work-item/%d", goal.ID, i)
			if item.Ref != "" {
				build.refs = append(build.refs, item.Ref)
			}
			if item.Agent != "" {
				addAssignment(item.Agent, sourceID, item.Label)
			}
			switch item.Class {
			case "awaiting", "blocked":
				build.events = append(build.events, WorkTimelineEvent{
					ID: "goals/gate/" + sourceID, Kind: TimelineGate, State: "gated",
					Actor: item.Agent, Title: item.Label, Detail: item.Detail,
					Source: "goals", SourceID: sourceID,
				})
			case "done":
				build.events = append(build.events, WorkTimelineEvent{
					ID: "goals/outcome/" + sourceID, Kind: TimelineOutcome, State: "terminal",
					Actor: item.Agent, Title: item.Label, Detail: item.Detail,
					Source: "goals", SourceID: sourceID,
				})
			}
		}
		if goal.StatusDisplay == "achieved" || goal.Status == "cancelled" {
			state := goal.StatusDisplay
			if goal.Status == "cancelled" {
				state = "superseded"
			}
			build.events = append(build.events, WorkTimelineEvent{
				ID: "goals/outcome/" + goal.ID, Kind: TimelineOutcome, State: state,
				At: goal.AchievedAt, Actor: goal.Owner, Title: "Goal outcome",
				Detail: goal.Title, Source: "goals", SourceID: goal.ID,
			})
		}
		githubEvents, githubSource := s.githubTimelineForRefs(ctx, build.refs)
		build.events = append(build.events, githubEvents...)
		build.sources = append(build.sources, githubSource)
		return build, true
	}
	return workTimelineBuild{}, false
}

func (s *Server) issueTimelineBuild(ctx context.Context, repo string, number int) workTimelineBuild {
	ref := tracker.IssueRef(repo, number)
	build := workTimelineBuild{
		subject: WorkTimelineSubject{Kind: "issue", ID: ref, Title: ref, SourceID: ref},
		refs:    []string{ref},
		sources: []WorkTimelineSource{{
			ID: "identity", Label: "Work item", Status: "available",
			Detail: "Repository-qualified issue identity from the selected Issues row.",
		}},
		events: []WorkTimelineEvent{{
			ID: "identity/" + ref, Kind: TimelineIdentity, State: "selected",
			Title: "Issue selected", Detail: ref, Source: "identity", SourceID: ref,
		}},
	}
	events, source := s.githubTimelineForRefs(ctx, []string{ref})
	build.events = append(build.events, events...)
	build.sources = append(build.sources, source)
	for _, event := range events {
		if event.SourceID == ref {
			if event.Title != "" {
				build.subject.Title = event.Title
			}
			build.subject.State = event.State
			build.subject.SourceURL = event.SourceURL
		}
	}
	return build
}

func (s *Server) githubTimelineForRefs(ctx context.Context, refs []string) ([]WorkTimelineEvent, WorkTimelineSource) {
	source := WorkTimelineSource{
		ID: "github", Label: "GitHub", Status: "available",
		Detail: "Linked issue and pull-request state fetched from source repositories.",
	}
	unique := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		if !timelineRefPattern.MatchString(ref) || seen[strings.ToLower(ref)] {
			continue
		}
		seen[strings.ToLower(ref)] = true
		unique = append(unique, ref)
	}
	sort.Strings(unique)
	if len(unique) > maxTimelineGitHubRefs {
		source.Truncated = true
		source.Cursor = unique[maxTimelineGitHubRefs-1]
		unique = unique[:maxTimelineGitHubRefs]
	}
	if len(unique) == 0 {
		source.Detail = "No repository-qualified GitHub issue or pull-request link is authored for this work."
		return nil, source
	}

	var events []WorkTimelineEvent
	var failures []string
	for _, ref := range unique {
		match := timelineRefPattern.FindStringSubmatch(ref)
		number, _ := strconv.Atoi(match[2])
		reader, err := s.timelineTracker(match[1])
		if err != nil {
			failures = append(failures, ref+": "+err.Error())
			continue
		}
		issue, err := reader.Get(ctx, number)
		if err == nil {
			events = append(events, timelineIssueEvents(match[1], issue)...)
			continue
		}
		if pullReader, ok := reader.(tracker.PullRequestReader); ok {
			pull, pullErr := pullReader.GetPullRequest(ctx, number)
			if pullErr == nil {
				events = append(events, timelinePullEvents(match[1], pull)...)
				continue
			}
			err = pullErr
		}
		failures = append(failures, ref+": "+err.Error())
	}
	if len(failures) > 0 {
		source.Status = "partial"
		source.Detail = fmt.Sprintf("%d of %d linked GitHub sources unavailable; available facts remain visible.", len(failures), len(unique))
		if len(events) == 0 {
			source.Status = "unavailable"
		}
	}
	if source.Truncated {
		source.Status = "partial"
		source.Detail += fmt.Sprintf(" Only the first %d stable references were read.", maxTimelineGitHubRefs)
	}
	return events, source
}

func (s *Server) timelineTracker(repo string) (tracker.Tracker, error) {
	repos, _, _ := workLedgerRepos(s.cfg.Repo, GoalsDoc{}, s.roster)
	for _, allowed := range repos {
		if strings.EqualFold(allowed, repo) {
			return s.workLedgerTracker(allowed)
		}
	}
	return nil, fmt.Errorf("repository is outside the roster-authored timeline boundary")
}

func timelineIssueEvents(repo string, issue tracker.Issue) []WorkTimelineEvent {
	ref := tracker.IssueRef(repo, issue.Number)
	url := issue.URL
	if url == "" {
		url = "https://github.com/" + repo + "/issues/" + strconv.Itoa(issue.Number)
	}
	events := []WorkTimelineEvent{{
		ID: "github/issue/opened/" + ref, Kind: TimelineGitHubIssue, State: "opened",
		At: issue.CreatedAt, Actor: issue.Author.Login, Title: issue.Title,
		Detail: "GitHub issue opened", Source: "github", SourceID: ref, SourceURL: url,
	}}
	state := strings.ToLower(strings.TrimSpace(issue.State))
	if state == "closed" {
		if hasTimelineLabel(issue.Labels, "superseded") {
			state = "superseded"
		}
		events = append(events, WorkTimelineEvent{
			ID: "github/issue/outcome/" + ref, Kind: TimelineOutcome, State: state,
			At: issue.ClosedAt, Actor: issue.Author.Login, Title: issue.Title,
			Detail: "GitHub issue " + state, Source: "github", SourceID: ref, SourceURL: url,
		})
	} else if issue.UpdatedAt != "" && issue.UpdatedAt != issue.CreatedAt {
		events = append(events, WorkTimelineEvent{
			ID: "github/issue/state/" + ref, Kind: TimelineGitHubIssue, State: "open",
			At: issue.UpdatedAt, Actor: issue.Author.Login, Title: issue.Title,
			Detail: "GitHub issue last updated", Source: "github", SourceID: ref, SourceURL: url,
		})
	}
	return events
}

func timelinePullEvents(repo string, pull tracker.PullRequest) []WorkTimelineEvent {
	ref := tracker.IssueRef(repo, pull.Number)
	url := pull.URL
	if url == "" {
		url = "https://github.com/" + repo + "/pull/" + strconv.Itoa(pull.Number)
	}
	state := strings.ToLower(strings.TrimSpace(pull.State))
	if pull.IsDraft && state == "open" {
		state = "draft"
	}
	events := []WorkTimelineEvent{{
		ID: "github/pr/opened/" + ref, Kind: TimelineGitHubPR, State: "opened",
		At: pull.CreatedAt, Title: pull.Title,
		Detail: pull.HeadRefName + " → " + pull.BaseRefName,
		Source: "github", SourceID: ref, SourceURL: url,
	}}
	if pull.MergedAt != "" || state == "merged" {
		events = append(events, WorkTimelineEvent{
			ID: "github/pr/merged/" + ref, Kind: TimelineOutcome, State: "merged",
			At: pull.MergedAt, Title: pull.Title, Detail: "Pull request merged",
			Source: "github", SourceID: ref, SourceURL: url,
		})
	} else if state == "closed" {
		events = append(events, WorkTimelineEvent{
			ID: "github/pr/closed/" + ref, Kind: TimelineOutcome, State: "closed",
			At: pull.ClosedAt, Title: pull.Title, Detail: "Pull request closed without merge",
			Source: "github", SourceID: ref, SourceURL: url,
		})
	} else if pull.UpdatedAt != "" && pull.UpdatedAt != pull.CreatedAt {
		events = append(events, WorkTimelineEvent{
			ID: "github/pr/state/" + ref, Kind: TimelineGitHubPR, State: state,
			At: pull.UpdatedAt, Title: pull.Title, Detail: "Pull request state",
			Source: "github", SourceID: ref, SourceURL: url,
		})
	}
	return events
}

func hasTimelineLabel(labels []tracker.Label, want string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label.Name), want) {
			return true
		}
	}
	return false
}

func buildDispatchTimeline(rosterDir string, refs []string) ([]WorkTimelineEvent, WorkTimelineSource) {
	source := WorkTimelineSource{
		ID: "dispatch", Label: "Dispatch ledger", Status: "partial",
		Detail: "Exact retained work references only. Acknowledged events require a nonce join; compacted payloads and canceled generations are not reconstructed.",
	}
	var events []WorkTimelineEvent
	matchedNonces := map[string]bool{}
	for _, entry := range outbox.ListAll(rosterDir) {
		if !dispatch.RecipientQueueMember(rosterDir, entry, entry.Recipient) {
			continue
		}
		if !timelineMessageMatches(entry.Message, refs) {
			continue
		}
		nonce := inbound.ParseDispatchNonce(entry.Message)
		if nonce != "" {
			matchedNonces[nonce] = true
		}
		events = append(events, WorkTimelineEvent{
			ID: "dispatch/queued/" + entry.ID, Kind: TimelineDispatch, State: "queued",
			At: entry.EnqueuedAt.UTC().Format(time.RFC3339), Actor: entry.Sender,
			Title: entry.Sender + " → " + entry.Recipient, Detail: "Queued for confirmed delivery",
			Source: "outbox", SourceID: entry.ID, Cursor: nonce,
		})
	}
	for _, entry := range inbound.ListAll(rosterDir) {
		if !timelineMessageMatches(entry.Message, refs) {
			continue
		}
		if entry.Nonce != "" {
			matchedNonces[entry.Nonce] = true
		}
		events = append(events, WorkTimelineEvent{
			ID: "dispatch/delivered/" + entry.ID, Kind: TimelineDispatch, State: "delivered",
			At: entry.DeliveredAt.UTC().Format(time.RFC3339), Actor: entry.Sender,
			Title: entry.Sender + " → " + entry.Recipient, Detail: "Pane delivery confirmed; durable acknowledgment pending",
			Source: "inbound", SourceID: entry.ID, Cursor: entry.Nonce,
		})
	}
	for _, entry := range dispatch.NewRegistry(rosterDir).Load() {
		if entry.Nonce == "" || !matchedNonces[entry.Nonce] {
			continue
		}
		events = append(events, WorkTimelineEvent{
			ID:   "dispatch/acknowledged/" + entry.Nonce + "/" + entry.Recipient,
			Kind: TimelineDispatch, State: "acknowledged", At: entry.ConsumedAt.UTC().Format(time.RFC3339),
			Actor: entry.Recipient, Title: entry.Sender + " → " + entry.Recipient,
			Detail: "Durable acknowledgment · " + entry.Reason,
			Source: "dispatch", SourceID: entry.Nonce, Cursor: entry.PayloadHash,
		})
	}
	return events, source
}

func timelineMessageMatches(message string, refs []string) bool {
	body := inbound.StripDispatchFooter(message)
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if containsTimelineToken(body, ref) {
			return true
		}
		if match := timelineRefPattern.FindStringSubmatch(ref); match != nil {
			urlIssue := "https://github.com/" + match[1] + "/issues/" + match[2]
			urlPull := "https://github.com/" + match[1] + "/pull/" + match[2]
			if containsTimelineToken(body, urlIssue) || containsTimelineToken(body, urlPull) {
				return true
			}
		}
	}
	return false
}

func containsTimelineToken(body, token string) bool {
	for offset := 0; offset <= len(body)-len(token); {
		found := strings.Index(body[offset:], token)
		if found < 0 {
			return false
		}
		start := offset + found
		end := start + len(token)
		beforeOK := start == 0 || !timelineTokenByte(body[start-1])
		afterOK := end == len(body) || !timelineTokenByte(body[end])
		if timelineRefPattern.MatchString(token) || strings.HasPrefix(token, "https://github.com/") {
			// Repository references end in a decimal number. A following digit is
			// the only ambiguous continuation; punctuation/words may legitimately
			// follow without whitespace.
			afterOK = end == len(body) || body[end] < '0' || body[end] > '9'
		}
		if beforeOK && afterOK {
			return true
		}
		offset = start + 1
	}
	return false
}

func timelineTokenByte(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9') || value == '_' || value == '.' || value == '-'
}

func buildWorkTimelineDoc(build workTimelineBuild, now time.Time, limit int, before string) WorkTimelineDoc {
	events := append([]WorkTimelineEvent(nil), build.events...)
	sort.SliceStable(events, func(i, j int) bool {
		return timelineSortKey(events[i]) < timelineSortKey(events[j])
	})
	if before != "" {
		if decoded, err := base64.RawURLEncoding.DecodeString(before); err == nil {
			key := string(decoded)
			filtered := events[:0]
			for _, event := range events {
				if timelineSortKey(event) < key {
					filtered = append(filtered, event)
				}
			}
			events = filtered
		}
	}
	total := len(events)
	next := ""
	if len(events) > limit {
		events = events[len(events)-limit:]
		next = base64.RawURLEncoding.EncodeToString([]byte(timelineSortKey(events[0])))
	}
	if events == nil {
		events = []WorkTimelineEvent{}
	}
	partial := false
	for _, source := range build.sources {
		if source.Status != "available" {
			partial = true
			break
		}
	}
	return WorkTimelineDoc{
		Subject: build.subject, Events: events, Sources: build.sources,
		GeneratedAt: now.UTC().Format(time.RFC3339), Partial: partial,
		Total: total, NextCursor: next,
	}
}

func timelineSortKey(event WorkTimelineEvent) string {
	at := event.At
	if _, err := time.Parse(time.RFC3339, at); err != nil {
		at = "0001-01-01T00:00:00Z"
	}
	return at + "\x1f" + timelineKindOrder(event.Kind) + "\x1f" +
		event.Source + "\x1f" + event.SourceID + "\x1f" + event.ID
}

func timelineKindOrder(kind WorkTimelineEventKind) string {
	switch kind {
	case TimelineIdentity:
		return "00"
	case TimelineAssignment:
		return "10"
	case TimelineDispatch:
		return "20"
	case TimelineGitHubIssue, TimelineGitHubPR:
		return "30"
	case TimelineGate:
		return "40"
	case TimelineOutcome:
		return "50"
	default:
		return "90"
	}
}
