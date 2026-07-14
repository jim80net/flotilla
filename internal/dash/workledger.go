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

// WorkLedgerDoc is the derived-on-read work facet of the fleet mental map. This
// first increment groups the current single-repo tracker by real roster/org data;
// multi-repo fanout and dispatch provenance build on the same document.
type WorkLedgerDoc struct {
	Repo          string               `json:"repo"`
	GeneratedAt   string               `json:"generated_at"`
	RecentDays    int                  `json:"recent_days"`
	InFlightCount int                  `json:"in_flight_count"`
	ShippedCount  int                  `json:"shipped_count"`
	Flotillas     []WorkLedgerFlotilla `json:"flotillas"`
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
	doc := WorkLedgerDoc{
		Repo:        repo,
		GeneratedAt: now.UTC().Format(time.RFC3339),
		RecentDays:  workLedgerRecentDays,
		Flotillas:   []WorkLedgerFlotilla{},
	}
	contexts := workLedgerContexts(repo, goals)
	cutoff := now.Add(-workLedgerRecentDays * 24 * time.Hour)

	type deskBucket struct {
		flotilla string
		desk     WorkLedgerDesk
	}
	buckets := map[string]*deskBucket{}
	for _, issue := range issues {
		ctx := contexts[issue.Number]
		desk := strings.TrimSpace(issue.Desk)
		if desk == "" {
			desk = ctx.desk
		}
		flotilla := flotillaForDesk(cfg, desk)
		if desk == "" {
			flotilla = flotillaForRepo(cfg, repo)
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
		item := WorkLedgerItem{Issue: issue, GoalID: ctx.goalID, GoalTitle: ctx.goalTitle, GoalDetail: ctx.goalDetail}
		state := strings.ToLower(strings.TrimSpace(issue.State))
		switch {
		case state == "open" && ctx.class == "in-flight":
			bucket.desk.InFlight = append(bucket.desk.InFlight, item)
			doc.InFlightCount++
		case state == "closed" && closedWithin(issue.ClosedAt, cutoff, now):
			bucket.desk.Shipped = append(bucket.desk.Shipped, item)
			doc.ShippedCount++
		}
	}

	groups := map[string][]WorkLedgerDesk{}
	for _, b := range buckets {
		if len(b.desk.InFlight) == 0 && len(b.desk.Shipped) == 0 {
			continue
		}
		groups[b.flotilla] = append(groups[b.flotilla], b.desk)
	}
	flotillaRank, deskRank := workLedgerRanks(cfg)
	flotillaNames := make([]string, 0, len(groups))
	for name := range groups {
		flotillaNames = append(flotillaNames, name)
	}
	sort.SliceStable(flotillaNames, func(i, j int) bool {
		return rankLess(flotillaNames[i], flotillaNames[j], flotillaRank)
	})
	for _, name := range flotillaNames {
		desks := groups[name]
		sort.SliceStable(desks, func(i, j int) bool { return rankLess(desks[i].Name, desks[j].Name, deskRank) })
		doc.Flotillas = append(doc.Flotillas, WorkLedgerFlotilla{Name: name, Desks: desks})
	}
	return doc
}

func workLedgerContexts(repo string, goals GoalsDoc) map[int]workLedgerContext {
	out := map[int]workLedgerContext{}
	for _, goal := range goals.Goals {
		for _, wi := range goal.WorkItems {
			if strings.ToLower(wi.Kind) != "issue" {
				continue
			}
			ref := strings.TrimSpace(wi.Ref)
			prefix := strings.TrimSpace(repo) + "#"
			if !strings.HasPrefix(ref, prefix) {
				continue
			}
			number, err := strconv.Atoi(strings.TrimPrefix(ref, prefix))
			if err != nil || number <= 0 {
				continue
			}
			desk := strings.TrimSpace(goal.Owner)
			if desk == "" {
				desk = strings.TrimSpace(goal.ConversationAgent)
			}
			out[number] = workLedgerContext{
				goalID: goal.ID, goalTitle: goal.Title, goalDetail: wi.Detail, desk: desk, class: wi.Class,
			}
		}
	}
	return out
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
