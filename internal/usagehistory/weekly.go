package usagehistory

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const Week = 7 * 24 * time.Hour

type Sample struct {
	At   time.Time `json:"at"`
	Used float64   `json:"used"`
}

type Cycle struct {
	ID            string    `json:"id"`
	AccountID     string    `json:"account_id"`
	Provider      string    `json:"provider"`
	Scope         string    `json:"scope"`
	PeriodSeconds int64     `json:"period_seconds,omitempty"`
	Plan          string    `json:"plan"`
	Start         time.Time `json:"start"`
	Reset         time.Time `json:"reset"`
	Samples       []Sample  `json:"samples"`
}

type PersonWeek struct {
	KeyID    string  `json:"key_id"`
	CostUSD  float64 `json:"cost_usd"`
	Unpriced int64   `json:"unpriced"`
	Requests int64   `json:"requests"`
	Percent  float64 `json:"percent"`
}

type WeeklyReport struct {
	Cycle
	People              []PersonWeek `json:"people"`
	Used                float64      `json:"used"`
	Unattributed        float64      `json:"unattributed"`
	Remaining           float64      `json:"remaining"`
	UnusedUpperBound    *float64     `json:"unused_upper_bound"`
	LastObserved        time.Time    `json:"last_observed"`
	Incomplete          bool         `json:"incomplete"`
	Closed              bool         `json:"closed"`
	AllocationAvailable bool         `json:"allocation_available"`
}

func (s *Store) ObserveQuota(accountID, provider, scope, plan string, reset, at time.Time, used float64) error {
	return s.ObserveQuotaWindow(accountID, provider, scope, plan, reset, at, used, Week)
}

func (s *Store) ObserveQuotaWindow(accountID, provider, scope, plan string, reset, at time.Time, used float64, period time.Duration) error {
	if period < time.Minute || period > 31*24*time.Hour || math.IsNaN(used) || at.IsZero() || reset.IsZero() || !at.Before(reset) || at.Before(reset.Add(-period)) || used < 0 || used > 100 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if at.UTC().Format(DateLayout) < s.cutoff() {
		return nil
	}
	account := Fingerprint(accountID)
	windowKey := ""
	if period != Week {
		windowKey = period.String() + "|"
	}
	id := account + "|" + scope + "|" + windowKey + reset.UTC().Format(time.RFC3339)
	cycle := s.cycles[id]
	if cycle.ID == "" {
		cycle = Cycle{ID: id, AccountID: account, Provider: provider, Scope: scope, PeriodSeconds: int64(period / time.Second), Plan: plan, Start: reset.Add(-period).UTC(), Reset: reset.UTC(), Samples: []Sample{}}
	}
	if len(cycle.Samples) > 0 && !at.After(cycle.Samples[len(cycle.Samples)-1].At) {
		return nil
	}
	if cycle.Plan == "" {
		cycle.Plan = plan
	}
	// Quota polling is minute-resolution; duplicate recipes must not multiply observations.
	if len(cycle.Samples) > 0 && at.Sub(cycle.Samples[len(cycle.Samples)-1].At) < time.Minute {
		return nil
	}
	cycle.Samples = append(cycle.Samples, Sample{At: at.UTC(), Used: used})
	s.cycles[id] = cycle
	s.quotaDirty[id] = true
	if errPrune := s.pruneCycles(); errPrune != nil {
		s.quotaErr = errPrune
		return errPrune
	}
	for key := range s.quotaDirty {
		if errWrite := s.persistCycle(s.cycles[key]); errWrite != nil {
			s.quotaErr = errWrite
			return errWrite
		}
		delete(s.quotaDirty, key)
	}
	s.quotaErr = nil
	return nil
}

func (s *Store) Weeks() ([]WeeklyReport, error) { return s.Reports("0000-01-01", "9999-12-31") }

func (s *Store) Reports(from, to string) ([]WeeklyReport, error) {
	before, after := from, to
	if at, err := time.Parse(DateLayout, from); err == nil && from != "0000-01-01" {
		before = at.AddDate(0, 0, -31).Format(DateLayout)
	}
	if at, err := time.Parse(DateLayout, to); err == nil && to != "9999-12-31" {
		after = at.AddDate(0, 0, 31).Format(DateLayout)
	}
	rows, err := s.Snapshot(before, after)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if errPrune := s.pruneCycles(); errPrune != nil {
		return nil, errPrune
	}
	if s.quotaErr != nil {
		return nil, fmt.Errorf("weekly history persistence unavailable: %w", s.quotaErr)
	}
	byAccount := map[string][]Row{}
	for _, row := range rows {
		byAccount[row.AccountID] = append(byAccount[row.AccountID], row)
	}
	for _, accountRows := range byAccount {
		sort.Slice(accountRows, func(i, j int) bool { return accountRows[i].At.Before(accountRows[j].At) })
	}
	out := make([]WeeklyReport, 0, len(s.cycles))
	for _, cycle := range s.cycles {
		if cycle.Reset.UTC().Format(DateLayout) < from || cycle.Start.UTC().Format(DateLayout) > to {
			continue
		}
		if len(cycle.Samples) == 0 || cycle.Reset.Before(s.now().AddDate(0, 0, -RetentionDays)) {
			continue
		}
		plan := s.settings.Plans[cycle.AccountID]
		if plan != "" {
			cycle.Plan = plan
		}
		accountRows := byAccount[cycle.AccountID]
		start := sort.Search(len(accountRows), func(i int) bool { return !accountRows[i].At.Before(cycle.Start) })
		end := sort.Search(len(accountRows), func(i int) bool { return !accountRows[i].At.Before(cycle.Reset) })
		out = append(out, BuildWeek(cycle, accountRows[start:end], s.now()))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reset.Equal(out[j].Reset) {
			return out[i].ID < out[j].ID
		}
		return out[i].Reset.After(out[j].Reset)
	})
	return out, nil
}

// BuildWeek weights observed quota deltas by priced gateway usage in the same interval.
// Pre-tracking consumption, gaps, unpriced traffic and quota with no gateway use
// stay unattributed. Outside-gateway traffic can still bias the estimated shares.
func BuildWeek(cycle Cycle, rows []Row, now time.Time) WeeklyReport {
	report := WeeklyReport{Cycle: cycle, People: []PersonWeek{}, Closed: !now.Before(cycle.Reset)}
	if len(cycle.Samples) == 0 {
		return report
	}
	last := cycle.Samples[len(cycle.Samples)-1]
	report.Used = last.Used
	report.Remaining = 100 - last.Used
	report.LastObserved = last.At
	report.Incomplete = cycle.Samples[0].At.Sub(cycle.Start) > 15*time.Minute || last.At.Before(minTime(now, cycle.Reset).Add(-15*time.Minute))
	if report.Closed && cycle.Reset.Sub(last.At) <= 15*time.Minute {
		remaining := report.Remaining
		report.UnusedUpperBound = &remaining
	}
	people := map[string]*PersonWeek{}
	var eligible []Row
	for _, row := range rows {
		if row.AccountID != cycle.AccountID || !row.At.IsZero() && (row.At.Before(cycle.Start) || !row.At.Before(cycle.Reset)) {
			continue
		}
		if row.At.IsZero() {
			continue
		} // Old daily rows cannot be assigned across reset boundaries.
		if !quotaScopeMatches(cycle.Scope, row.Model) {
			continue
		}
		person := people[row.KeyID]
		if person == nil {
			person = &PersonWeek{KeyID: row.KeyID}
			people[row.KeyID] = person
		}
		person.Requests += row.Requests
		if row.CostUSD != nil {
			person.CostUSD += *row.CostUSD
		}
		person.Unpriced += row.UnpricedRequests
		if row.CostUSD == nil && row.UnpricedRequests == 0 {
			person.Unpriced += row.Requests
		}
		eligible = append(eligible, row)
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].At.Before(eligible[j].At) })
	cursor := 0
	attributed := 0.0
	decreased := false
	for i := 1; i < len(cycle.Samples); i++ {
		before, after := cycle.Samples[i-1], cycle.Samples[i]
		costs := map[string]float64{}
		total := 0.0
		unpriced := false
		for cursor < len(eligible) && eligible[cursor].At.Before(after.At) {
			row := eligible[cursor]
			cursor++
			if row.At.Before(before.At) {
				continue
			}
			if row.UnpricedRequests > 0 || row.CostUSD == nil {
				unpriced = true
				continue
			}
			costs[row.KeyID] += *row.CostUSD
			total += *row.CostUSD
		}
		delta := after.Used - before.Used
		if delta < 0 {
			decreased = true
			report.Incomplete = true
			continue
		}
		if after.At.Sub(before.At) > 30*time.Minute {
			report.Incomplete = true
			continue
		}
		if delta <= 0 || total <= 0 || unpriced {
			continue
		}
		for key, cost := range costs {
			share := delta * cost / total
			people[key].Percent += share
			attributed += share
		}
	}
	if decreased {
		for _, person := range people {
			person.Percent = 0
		}
		attributed = 0
	}
	report.AllocationAvailable = attributed > 0 || report.Used == 0
	report.Unattributed = max(0, report.Used-attributed)
	if report.Unattributed < 1e-9 {
		report.Unattributed = 0
	}
	for _, person := range people {
		report.People = append(report.People, *person)
	}
	sort.Slice(report.People, func(i, j int) bool {
		if report.People[i].CostUSD == report.People[j].CostUSD {
			return report.People[i].KeyID < report.People[j].KeyID
		}
		return report.People[i].CostUSD > report.People[j].CostUSD
	})
	// Raw provider observations remain private to the backend; the API returns summary coverage.
	report.Samples = nil
	return report
}

func quotaScopeMatches(scope, model string) bool {
	if scope == "" || scope == "account" {
		return true
	}
	if strings.HasPrefix(scope, "family:") {
		return strings.HasPrefix(strings.ToLower(model), "claude-"+strings.TrimPrefix(scope, "family:")+"-")
	}
	return scope == model
}

func validCycle(key string, cycle Cycle) bool {
	if cycle.ID != key || cycle.AccountID == "" || cycle.Reset.Sub(cycle.Start) < time.Minute || cycle.Reset.Sub(cycle.Start) > 31*24*time.Hour || cycle.PeriodSeconds != 0 && time.Duration(cycle.PeriodSeconds)*time.Second != cycle.Reset.Sub(cycle.Start) || len(cycle.Samples) == 0 {
		return false
	}
	var before time.Time
	for _, sample := range cycle.Samples {
		if sample.At.Before(cycle.Start) || !sample.At.Before(cycle.Reset) || !sample.At.After(before) || math.IsNaN(sample.Used) || sample.Used < 0 || sample.Used > 100 {
			return false
		}
		before = sample.At
	}
	return true
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
