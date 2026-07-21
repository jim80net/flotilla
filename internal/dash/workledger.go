package dash

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/dash/tracker"
	"github.com/jim80net/flotilla/internal/roster"
)

const workLedgerRecentDays = 14

const (
	maxWorkLedgerRepos       = 32
	workLedgerFanoutParallel = 4
)

// WorkLedgerDoc is the derived-on-read work facet of the fleet mental map. It
// carries repository-qualified work and explicit fanout coverage alongside the
// roster/org grouping.
type WorkLedgerDoc struct {
	Repo          string               `json:"repo"`
	Repos         []string             `json:"repos"`
	Coverage      WorkLedgerCoverage   `json:"coverage"`
	GeneratedAt   string               `json:"generated_at"`
	RecentDays    int                  `json:"recent_days"`
	InFlightCount int                  `json:"in_flight_count"`
	ShippedCount  int                  `json:"shipped_count"`
	Flotillas     []WorkLedgerFlotilla `json:"flotillas"`
}

// WorkLedgerCoverage makes the collection boundary operator-visible. A partial
// GitHub fanout is still useful, but it must never masquerade as a complete fleet.
type WorkLedgerCoverage struct {
	Complete        bool                       `json:"complete"`
	ExpectedRepos   int                        `json:"expected_repos"`
	IndexedRepos    []string                   `json:"indexed_repos"`
	FailedRepos     []WorkLedgerRepoFailure    `json:"failed_repos"`
	OmittedRepos    []string                   `json:"omitted_repos"`
	UnmappedDomains []string                   `json:"unmapped_domains"`
	Domains         []WorkLedgerDomainCoverage `json:"domains"`
}

// WorkLedgerDomainCoverage keeps roster intent distinct from repository read
// health. State is one of mapped, repository-less, missing, or failed. Omitted
// repositories remain listed on WorkLedgerCoverage so the safety bound stays
// independently visible and fail-closed.
type WorkLedgerDomainCoverage struct {
	Name  string   `json:"name"`
	State string   `json:"state"`
	Repos []string `json:"repos"`
}

type WorkLedgerRepoFailure struct {
	Repo  string `json:"repo"`
	Error string `json:"error"`
}

// WorkLedgerRepoIssues preserves repository identity through derivation. Issue
// numbers are only unique within a repository.
type WorkLedgerRepoIssues struct {
	Repo   string
	Issues []tracker.Issue
}

type WorkLedgerFlotilla struct {
	Name  string           `json:"name"`
	Desks []WorkLedgerDesk `json:"desks"`
}

type WorkLedgerDesk struct {
	Name     string           `json:"name"`
	InFlight []WorkLedgerItem `json:"in_flight"`
	Shipped  []WorkLedgerItem `json:"shipped"`
}

type WorkLedgerItem struct {
	Repo       string        `json:"repo"`
	Issue      tracker.Issue `json:"issue"`
	GoalID     string        `json:"goal_id,omitempty"`
	GoalTitle  string        `json:"goal_title,omitempty"`
	GoalDetail string        `json:"goal_detail,omitempty"`
}

type workLedgerContext struct {
	goalID, goalTitle, goalDetail, desk, class string
}

// BuildWorkLedger joins GitHub issues to goals + roster/org attribution. Nothing
// is persisted: the result is regenerated from live inputs on every request.
func BuildWorkLedger(repo string, issues []tracker.Issue, goals GoalsDoc, cfg *roster.Config, now time.Time) WorkLedgerDoc {
	coverage := WorkLedgerCoverage{Complete: true, ExpectedRepos: 1, IndexedRepos: []string{repo}}
	return BuildMultiRepoWorkLedger(repo, []WorkLedgerRepoIssues{{Repo: repo, Issues: issues}}, goals, cfg, coverage, now)
}

// BuildMultiRepoWorkLedger joins repository-qualified GitHub issues to goals and
// roster/org attribution. Nothing is persisted.
func BuildMultiRepoWorkLedger(primaryRepo string, sources []WorkLedgerRepoIssues, goals GoalsDoc, cfg *roster.Config, coverage WorkLedgerCoverage, now time.Time) WorkLedgerDoc {
	doc := WorkLedgerDoc{
		Repo:        primaryRepo,
		Repos:       append([]string(nil), coverage.IndexedRepos...),
		Coverage:    coverage,
		GeneratedAt: now.UTC().Format(time.RFC3339),
		RecentDays:  workLedgerRecentDays,
		Flotillas:   []WorkLedgerFlotilla{},
	}
	contexts := workLedgerContexts(goals)
	cutoff := now.Add(-workLedgerRecentDays * 24 * time.Hour)

	type deskBucket struct {
		flotilla string
		desk     WorkLedgerDesk
	}
	buckets := map[string]*deskBucket{}
	for _, source := range sources {
		repo := strings.TrimSpace(source.Repo)
		for _, issue := range source.Issues {
			ctx := contexts[workLedgerRef(repo, issue.Number)]
			desk := strings.TrimSpace(issue.Desk)
			if desk == "" {
				desk = ctx.desk
			}
			if desk == "" {
				desk = deskForRepo(cfg, repo)
			}
			flotilla := flotillaForDesk(cfg, desk)
			if flotilla == "" {
				flotilla = flotillaForRepo(cfg, repo)
			}
			if desk == "" {
				desk = "Unassigned"
			}
			if flotilla == "" {
				flotilla = "Unassigned"
			}
			key := strings.ToLower(flotilla + "\x00" + desk)
			bucket := buckets[key]
			if bucket == nil {
				bucket = &deskBucket{flotilla: flotilla, desk: WorkLedgerDesk{
					Name: desk, InFlight: []WorkLedgerItem{}, Shipped: []WorkLedgerItem{},
				}}
				buckets[key] = bucket
			}
			// Body was fetched only to derive trailers. Do not duplicate it in the list
			// document; issue detail remains the one full-body read surface.
			issue.Body, issue.Comments = "", nil
			item := WorkLedgerItem{Repo: repo, Issue: issue, GoalID: ctx.goalID, GoalTitle: ctx.goalTitle, GoalDetail: ctx.goalDetail}
			state := strings.ToLower(strings.TrimSpace(issue.State))
			switch {
			case state == "open":
				bucket.desk.InFlight = append(bucket.desk.InFlight, item)
				doc.InFlightCount++
			case state == "closed" && closedWithin(issue.ClosedAt, cutoff, now):
				bucket.desk.Shipped = append(bucket.desk.Shipped, item)
				doc.ShippedCount++
			}
		}
	}

	groups := map[string][]WorkLedgerDesk{}
	for _, b := range buckets {
		if len(b.desk.InFlight) == 0 && len(b.desk.Shipped) == 0 {
			continue
		}
		groups[b.flotilla] = append(groups[b.flotilla], b.desk)
	}
	doc.Flotillas = orderedWorkLedgerFlotillas(groups, cfg)
	return doc
}

// orderedWorkLedgerFlotillas makes the default ledger answer "what is moving?"
// before presenting recent history. Roster order remains the deterministic tie-break
// within the moving and shipped-only partitions.
func orderedWorkLedgerFlotillas(groups map[string][]WorkLedgerDesk, cfg *roster.Config) []WorkLedgerFlotilla {
	flotillaRank, deskRank := workLedgerRanks(cfg)
	flotillaNames := make([]string, 0, len(groups))
	flotillaMoving := make(map[string]bool, len(groups))
	for name := range groups {
		flotillaNames = append(flotillaNames, name)
		flotillaMoving[name] = desksHaveMovingWork(groups[name])
	}
	sort.SliceStable(flotillaNames, func(i, j int) bool {
		movingI := flotillaMoving[flotillaNames[i]]
		movingJ := flotillaMoving[flotillaNames[j]]
		if movingI != movingJ {
			return movingI
		}
		return rankLess(flotillaNames[i], flotillaNames[j], flotillaRank)
	})
	ordered := make([]WorkLedgerFlotilla, 0, len(flotillaNames))
	for _, name := range flotillaNames {
		desks := groups[name]
		sort.SliceStable(desks, func(i, j int) bool {
			movingI := len(desks[i].InFlight) > 0
			movingJ := len(desks[j].InFlight) > 0
			if movingI != movingJ {
				return movingI
			}
			return rankLess(desks[i].Name, desks[j].Name, deskRank)
		})
		ordered = append(ordered, WorkLedgerFlotilla{Name: name, Desks: desks})
	}
	return ordered
}

func desksHaveMovingWork(desks []WorkLedgerDesk) bool {
	for _, desk := range desks {
		if len(desk.InFlight) > 0 {
			return true
		}
	}
	return false
}

func workLedgerContexts(goals GoalsDoc) map[string]workLedgerContext {
	out := map[string]workLedgerContext{}
	for _, goal := range goals.Goals {
		for _, wi := range goal.WorkItems {
			if strings.ToLower(wi.Kind) != "issue" {
				continue
			}
			ref := strings.TrimSpace(wi.Ref)
			hash := strings.LastIndex(ref, "#")
			if hash <= 0 {
				continue
			}
			repo := strings.TrimSpace(ref[:hash])
			number, err := strconv.Atoi(strings.TrimSpace(ref[hash+1:]))
			if err != nil || number <= 0 {
				continue
			}
			desk := strings.TrimSpace(goal.Owner)
			if desk == "" {
				desk = strings.TrimSpace(goal.ConversationAgent)
			}
			out[workLedgerRef(repo, number)] = workLedgerContext{
				goalID: goal.ID, goalTitle: goal.Title, goalDetail: wi.Detail, desk: desk, class: wi.Class,
			}
		}
	}
	return out
}

func workLedgerRef(repo string, number int) string {
	return strings.ToLower(strings.TrimSpace(repo)) + "#" + strconv.Itoa(number)
}

func workLedgerRepos(primary string, goals GoalsDoc, cfg *roster.Config) (selected, omitted []string, domains []WorkLedgerDomainCoverage) {
	seen := map[string]string{}
	add := func(repo string) {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return
		}
		key := strings.ToLower(repo)
		if _, ok := seen[key]; !ok {
			seen[key] = repo
		}
	}
	add(primary)
	if cfg != nil {
		for _, a := range cfg.Agents {
			add(a.PrimaryRepo)
			for _, repo := range a.SecondaryRepos {
				add(repo)
			}
		}
	}
	for _, goal := range goals.Goals {
		for _, wi := range goal.WorkItems {
			if !strings.EqualFold(wi.Kind, "issue") {
				continue
			}
			ref := strings.TrimSpace(wi.Ref)
			if hash := strings.LastIndex(ref, "#"); hash > 0 {
				add(ref[:hash])
			}
		}
	}
	for _, repo := range seen {
		selected = append(selected, repo)
	}
	sort.Slice(selected, func(i, j int) bool {
		if strings.EqualFold(selected[i], primary) != strings.EqualFold(selected[j], primary) {
			return strings.EqualFold(selected[i], primary)
		}
		return strings.ToLower(selected[i]) < strings.ToLower(selected[j])
	})
	if len(selected) > maxWorkLedgerRepos {
		omitted = append(omitted, selected[maxWorkLedgerRepos:]...)
		selected = selected[:maxWorkLedgerRepos]
	}
	return selected, omitted, workLedgerDomains(cfg)
}

func workLedgerDomains(cfg *roster.Config) []WorkLedgerDomainCoverage {
	if cfg == nil {
		return nil
	}
	type domainIntent struct {
		name           string
		repos          map[string]string
		repositoryless bool
	}
	intents := map[string]*domainIntent{}
	var domains []WorkLedgerDomainCoverage
	for _, a := range cfg.Agents {
		domain := flotillaForDesk(cfg, a.Name)
		if domain == "" || domain == cfg.XOAgent || domain == cfg.CosAgent {
			continue
		}
		key := strings.ToLower(domain)
		intent := intents[key]
		if intent == nil {
			intent = &domainIntent{name: domain, repos: map[string]string{}}
			intents[key] = intent
		}
		for _, repo := range append([]string{a.PrimaryRepo}, a.SecondaryRepos...) {
			repo = strings.TrimSpace(repo)
			if repo != "" {
				intent.repos[strings.ToLower(repo)] = repo
			}
		}
		intent.repositoryless = intent.repositoryless || a.WorkLedgerRepositoryless
	}
	for _, intent := range intents {
		domain := WorkLedgerDomainCoverage{Name: intent.name, State: "missing", Repos: []string{}}
		for _, repo := range intent.repos {
			domain.Repos = append(domain.Repos, repo)
		}
		sort.Slice(domain.Repos, func(i, j int) bool { return strings.ToLower(domain.Repos[i]) < strings.ToLower(domain.Repos[j]) })
		if len(domain.Repos) > 0 {
			domain.State = "mapped"
		} else if intent.repositoryless {
			domain.State = "repository-less"
		}
		domains = append(domains, domain)
	}
	sort.Slice(domains, func(i, j int) bool { return strings.ToLower(domains[i].Name) < strings.ToLower(domains[j].Name) })
	return domains
}

func markFailedWorkLedgerDomains(domains []WorkLedgerDomainCoverage, failures []WorkLedgerRepoFailure) {
	failed := make(map[string]bool, len(failures))
	for _, failure := range failures {
		failed[strings.ToLower(failure.Repo)] = true
	}
	for i := range domains {
		for _, repo := range domains[i].Repos {
			if failed[strings.ToLower(repo)] {
				domains[i].State = "failed"
				break
			}
		}
	}
}

func missingWorkLedgerDomains(domains []WorkLedgerDomainCoverage) []string {
	var missing []string
	for _, domain := range domains {
		if domain.State == "missing" {
			missing = append(missing, domain.Name)
		}
	}
	return missing
}

// deskForRepo attributes a trailer-less issue to a single explicit roster seat
// when possible. Shared repositories remain a repo-wide desk rather than the
// misleading global Unassigned bucket.
func deskForRepo(cfg *roster.Config, repo string) string {
	if cfg == nil || repo == "" {
		return ""
	}
	var primary, secondaryMatches []string
	for _, a := range cfg.Agents {
		if strings.EqualFold(a.PrimaryRepo, repo) {
			primary = append(primary, a.Name)
			continue
		}
		for _, secondaryRepo := range a.SecondaryRepos {
			if strings.EqualFold(secondaryRepo, repo) {
				secondaryMatches = append(secondaryMatches, a.Name)
				break
			}
		}
	}
	matches := primary
	if len(matches) == 0 {
		matches = secondaryMatches
	}
	if len(matches) == 1 {
		return matches[0]
	}
	if len(matches) > 1 {
		var explicitCoordinator string
		for _, name := range matches {
			a, err := cfg.Agent(name)
			if err == nil && a.Coordinator != nil && *a.Coordinator {
				if explicitCoordinator != "" {
					explicitCoordinator = ""
					break
				}
				explicitCoordinator = name
			}
		}
		if explicitCoordinator != "" {
			return explicitCoordinator
		}
		root := cfg.XOAgent
		if root == "" && len(cfg.Agents) > 0 {
			root = cfg.Agents[0].Name
		}
		for _, candidate := range matches {
			ownsAll := true
			for _, other := range matches {
				if other != candidate && cfg.OwningXO(other, root) != candidate {
					ownsAll = false
					break
				}
			}
			if ownsAll {
				return candidate
			}
		}
	}
	if len(matches) > 1 && flotillaForRepo(cfg, repo) != "" {
		parts := strings.Split(strings.TrimSpace(repo), "/")
		return parts[len(parts)-1] + " (repo)"
	}
	return ""
}

func closedWithin(stamp string, cutoff, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(stamp))
	return err == nil && !t.Before(cutoff) && !t.After(now.Add(time.Minute))
}

func flotillaForDesk(cfg *roster.Config, desk string) string {
	if cfg == nil || desk == "" {
		return ""
	}
	// A coordinator is already the flotilla boundary. Walking its parent would
	// incorrectly collapse every project flotilla into fleet command.
	if cfg.IsCoordinator(desk) {
		return desk
	}
	root := cfg.XOAgent
	if root == "" && len(cfg.Agents) > 0 {
		root = cfg.Agents[0].Name
	}
	owner := cfg.OwningXO(desk, root)
	if owner == "" || owner == desk {
		return ""
	}
	return owner
}

func flotillaForRepo(cfg *roster.Config, repo string) string {
	if cfg == nil || repo == "" {
		return ""
	}
	set := map[string]bool{}
	for _, a := range cfg.Agents {
		match := strings.EqualFold(a.PrimaryRepo, repo)
		for _, secondary := range a.SecondaryRepos {
			match = match || strings.EqualFold(secondary, repo)
		}
		if match {
			if f := flotillaForDesk(cfg, a.Name); f != "" {
				set[f] = true
			}
		}
	}
	if len(set) != 1 {
		return ""
	}
	for name := range set {
		return name
	}
	return ""
}

func workLedgerRanks(cfg *roster.Config) (map[string]int, map[string]int) {
	fr, dr := map[string]int{}, map[string]int{}
	if cfg == nil {
		return fr, dr
	}
	for i, ch := range cfg.Bindings() {
		if !ch.IsFleetCommand() {
			if _, ok := fr[ch.XOAgent]; !ok {
				fr[ch.XOAgent] = i
			}
		}
	}
	for i, a := range cfg.Agents {
		dr[a.Name] = i
	}
	return fr, dr
}

func rankLess(a, b string, ranks map[string]int) bool {
	ra, oka := ranks[a]
	rb, okb := ranks[b]
	if oka != okb {
		return oka
	}
	if oka && ra != rb {
		return ra < rb
	}
	return strings.ToLower(a) < strings.ToLower(b)
}
