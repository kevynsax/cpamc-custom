package usagehistory

import (
	"sort"
	"strings"
	"time"
)

// AccountSubscription is the live credential metadata that prices one account fingerprint.
type AccountSubscription struct {
	Provider      string
	Plan          string
	MonthlyFeeUSD *float64
}

// ValuedRow is a usage row with its subscription value.
type ValuedRow struct {
	Row
	// SubscriptionUSD is the row's part of the plan fee of the quota windows it ran
	// in; absent when the fee, the quota or the row's API value is unknown.
	SubscriptionUSD *float64 `json:"subscription_usd,omitempty"`
}

// ValuedUsage is the usage rows and quota windows of one date range, both priced.
type ValuedUsage struct {
	Rows    []ValuedRow
	Windows []WeeklyReport
}

type cycleFamily struct {
	Scope    string
	Duration time.Duration
}

// MonthlyPlanFee is the public monthly web price of a known plan. Ambiguous or
// unidentified plans stay unknown.
func MonthlyPlanFee(provider, plan string) (float64, bool) {
	key := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.ToLower(plan))
	switch strings.ToLower(provider) {
	case "claude", "anthropic":
		if strings.Contains(key, "max20") || strings.Contains(key, "max200") {
			return 200, true
		}
		if strings.Contains(key, "max5") || strings.Contains(key, "max100") {
			return 100, true
		}
		if key == "pro" || key == "claudepro" {
			return 20, true
		}
	case "codex", "openai":
		if key == "prolite" || strings.Contains(key, "pro100") {
			return 100, true
		}
		if key == "pro" || strings.Contains(key, "pro200") {
			return 200, true
		}
		if key == "promax" || strings.Contains(key, "pro500") {
			return 500, true
		}
		if key == "plus" {
			return 20, true
		}
	}
	return 0, false
}

// windowMonthlyFee is the monthly fee that prices one quota window: the credential's
// explicit fee, else the public price of the plan recorded on the window, else of
// the account's current plan.
func windowMonthlyFee(report WeeklyReport, account AccountSubscription) (float64, bool) {
	if account.MonthlyFeeUSD != nil {
		return *account.MonthlyFeeUSD, true
	}
	provider := account.Provider
	if provider == "" {
		provider = report.Provider
	}
	if fee, known := MonthlyPlanFee(provider, report.Plan); known {
		return fee, true
	}
	return MonthlyPlanFee(provider, account.Plan)
}

// proratedPlanFee spreads a monthly fee over the actual length of each UTC month in the window.
func proratedPlanFee(monthly float64, window [2]time.Time) float64 {
	result := 0.0
	cursor := window[0].UTC()
	for cursor.Before(window[1]) {
		start := time.Date(cursor.Year(), cursor.Month(), 1, 0, 0, 0, 0, time.UTC)
		next := start.AddDate(0, 1, 0)
		until := next
		if window[1].Before(until) {
			until = window[1]
		}
		result += monthly * until.Sub(cursor).Seconds() / next.Sub(start).Seconds()
		cursor = until
	}
	return result
}

// duplicateWindowGap is how close two starts of one quota window must be to be the
// same window recorded twice.
const duplicateWindowGap = 15 * time.Minute

// distinctWindows drops repeated recordings of one quota window: starts chained
// within duplicateWindowGap are one window, kept from its latest observation.
// The result is ordered by start.
func distinctWindows(cycles []WeeklyReport) []WeeklyReport {
	byStart := func(i, j int) bool { return cycles[i].Start.Before(cycles[j].Start) }
	sort.Slice(cycles, byStart)
	kept := []WeeklyReport{}
	var previous time.Time
	for _, cycle := range cycles {
		if len(kept) == 0 || cycle.Start.Sub(previous) > duplicateWindowGap {
			kept = append(kept, cycle)
			previous = cycle.Start
			continue
		}
		previous = cycle.Start
		last := &kept[len(kept)-1]
		if cycle.LastObserved.After(last.LastObserved) {
			*last = cycle
		}
	}
	return kept
}

// subscriptionRowValues is the subscription value of every row, by index; nil when
// no priced quota window covers the row or its API value is unknown.
// Every quota window, including one ended early by a reset, is worth the plan fee
// for its full length. A key gets its attributed quota percentage of it, split
// across the key's rows in the window by API value, so a row's value belongs to the
// moment the usage happened. Independent scope/duration families measure the same
// plan, so a row covered by several is worth their average.
func subscriptionRowValues(rows []Row, reports []WeeklyReport) []*float64 {
	families := map[string]map[cycleFamily][]WeeklyReport{}
	for _, report := range reports {
		if report.MonthlyFeeUSD == nil {
			continue
		}
		if families[report.AccountID] == nil {
			families[report.AccountID] = map[cycleFamily][]WeeklyReport{}
		}
		family := cycleFamily{Scope: report.Scope, Duration: report.Reset.Sub(report.Start)}
		families[report.AccountID][family] = append(families[report.AccountID][family], report)
	}
	byAccount := map[string][]int{}
	for index, row := range rows {
		if row.At.IsZero() || row.CostUSD == nil {
			continue
		}
		byAccount[row.AccountID] = append(byAccount[row.AccountID], index)
	}
	sums := make([]float64, len(rows))
	covering := make([]int, len(rows))
	for account, accountFamilies := range families {
		indexes := byAccount[account]
		byTime := func(i, j int) bool { return rows[indexes[i]].At.Before(rows[indexes[j]].At) }
		sort.SliceStable(indexes, byTime)
		for _, cycles := range accountFamilies {
			windows := distinctWindows(cycles)
			for position, window := range windows {
				end := window.Reset
				// A reset starts the next window early; later usage belongs to that one.
				if position+1 < len(windows) && windows[position+1].Start.Before(end) {
					end = windows[position+1].Start
				}
				valueWindow(window, end, rows, indexes, sums, covering)
			}
		}
	}
	values := make([]*float64, len(rows))
	for index := range rows {
		if covering[index] == 0 {
			continue
		}
		value := sums[index] / float64(covering[index])
		values[index] = &value
	}
	return values
}

// valueWindow adds one quota window's value to the rows it covers (indexes are one
// account's priced rows ordered by time) and counts the window as covering them.
// A window whose use cannot be tied to keys covers its rows with no value.
func valueWindow(window WeeklyReport, end time.Time, rows []Row, indexes []int, sums []float64, covering []int) {
	inWindow := func(i int) bool { return !rows[indexes[i]].At.Before(window.Start) }
	first := sort.Search(len(indexes), inWindow)
	costs := map[string]float64{}
	var covered []int
	for _, index := range indexes[first:] {
		row := rows[index]
		if !row.At.Before(end) {
			break
		}
		if !quotaScopeMatches(window.Scope, row.Model) {
			continue
		}
		covered = append(covered, index)
		costs[row.KeyID] += *row.CostUSD
	}
	percents := map[string]float64{}
	attributed := 0.0
	for _, person := range window.People {
		percents[person.KeyID] = person.Percent
		attributed += person.Percent
	}
	scale := 1.0
	if attributed > window.Used && attributed > 0 {
		scale = window.Used / attributed
	}
	fullLength := [2]time.Time{window.Start, window.Reset}
	fee := proratedPlanFee(*window.MonthlyFeeUSD, fullLength)
	for _, index := range covered {
		covering[index]++
		row := rows[index]
		keyCost := costs[row.KeyID]
		if !window.AllocationAvailable || keyCost <= 0 {
			continue
		}
		sums[index] += fee * percents[row.KeyID] * scale / 100 * *row.CostUSD / keyCost
	}
}

// ValuedUsage returns the rows and quota windows of the inclusive UTC date range.
// accounts carries live credential metadata keyed by account fingerprint.
func (s *Store) ValuedUsage(from, to string, accounts map[string]AccountSubscription) (ValuedUsage, error) {
	before, after := paddedDates(from, to)
	rows, err := s.Snapshot(before, after)
	if err != nil {
		return ValuedUsage{}, err
	}
	// Windows of the padded range are valued too, so a row's value does not depend on
	// where the requested range ends.
	reports, errReports := s.reports(rows, before, after)
	if errReports != nil {
		return ValuedUsage{}, errReports
	}
	valued := ValuedUsage{Rows: []ValuedRow{}, Windows: []WeeklyReport{}}
	for index := range reports {
		account := accounts[reports[index].AccountID]
		fee, known := windowMonthlyFee(reports[index], account)
		if known {
			reports[index].MonthlyFeeUSD = &fee
		}
		resetDate := reports[index].Reset.UTC().Format(DateLayout)
		startDate := reports[index].Start.UTC().Format(DateLayout)
		if resetDate < from || startDate > to {
			continue
		}
		valued.Windows = append(valued.Windows, reports[index])
	}
	values := subscriptionRowValues(rows, reports)
	for index, row := range rows {
		if row.Date < from || row.Date > to {
			continue
		}
		valuedRow := ValuedRow{Row: row, SubscriptionUSD: values[index]}
		valued.Rows = append(valued.Rows, valuedRow)
	}
	return valued, nil
}
