package usagehistory

import (
	"sort"
	"strings"
	"time"
)

// SpendAccount is the live credential metadata Spend needs for one account fingerprint.
type SpendAccount struct {
	Provider      string
	Plan          string
	MonthlyFeeUSD *float64
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
func distinctWindows(cycles []WeeklyReport) []WeeklyReport {
	sort.Slice(cycles, func(i, j int) bool { return cycles[i].Start.Before(cycles[j].Start) })
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

// subscriptionValues is each key's subscription value per account in the window.
// Every quota window, including one ended early by a reset, is worth the plan fee
// for its full length; a key gets its attributed quota percentage of it, prorated
// by how much of the window falls inside the reporting period. Independent
// scope/duration families measure the same plan and are averaged.
func subscriptionValues(reports []WeeklyReport, monthlyFees map[string]float64, window [2]time.Time, now time.Time) map[string]map[string]float64 {
	limit := window[1]
	if now.Before(limit) {
		limit = now
	}
	families := map[string]map[cycleFamily][]WeeklyReport{}
	for _, report := range reports {
		if _, priced := monthlyFees[report.AccountID]; !priced {
			continue
		}
		if !report.Start.Before(limit) || !report.Reset.After(window[0]) {
			continue
		}
		if families[report.AccountID] == nil {
			families[report.AccountID] = map[cycleFamily][]WeeklyReport{}
		}
		family := cycleFamily{Scope: report.Scope, Duration: report.Reset.Sub(report.Start)}
		families[report.AccountID][family] = append(families[report.AccountID][family], report)
	}
	values := map[string]map[string]float64{}
	for account, accountFamilies := range families {
		values[account] = map[string]float64{}
		for _, cycles := range accountFamilies {
			for _, cycle := range distinctWindows(cycles) {
				start := cycle.Start
				if window[0].After(start) {
					start = window[0]
				}
				end := cycle.Reset
				if window[1].Before(end) {
					end = window[1]
				}
				overlap := end.Sub(start).Seconds() / cycle.Reset.Sub(cycle.Start).Seconds()
				if overlap <= 0 || !cycle.AllocationAvailable {
					continue
				}
				percents := 0.0
				for _, person := range cycle.People {
					percents += person.Percent
				}
				scale := 1.0
				if percents > cycle.Used && percents > 0 {
					scale = cycle.Used / percents
				}
				windowFee := proratedPlanFee(monthlyFees[account], [2]time.Time{cycle.Start, cycle.Reset})
				for _, person := range cycle.People {
					values[account][person.KeyID] += windowFee * overlap * person.Percent * scale / 100 / float64(len(accountFamilies))
				}
			}
		}
	}
	return values
}

// windowPlans is the plan reported by the latest cycle of each account in the window.
func windowPlans(reports []WeeklyReport) map[string]string {
	plans := map[string]string{}
	for _, report := range reports {
		if plans[report.AccountID] == "" {
			plans[report.AccountID] = report.Plan
		}
	}
	return plans
}

// monthlyFees is the known monthly fee of every account; explicit metadata wins
// over published plan prices.
func monthlyFees(reports []WeeklyReport, accounts map[string]SpendAccount) map[string]float64 {
	plans := windowPlans(reports)
	fees := map[string]float64{}
	for id, account := range accounts {
		if account.MonthlyFeeUSD != nil {
			fees[id] = *account.MonthlyFeeUSD
			continue
		}
		plan := account.Plan
		if plan == "" {
			plan = plans[id]
		}
		if monthly, known := MonthlyPlanFee(account.Provider, plan); known {
			fees[id] = monthly
		}
	}
	return fees
}

// assignSubscription gives every item its part of the key's subscription value on
// that account, split across the key's items by API value. Accounts without a
// known fee or quota observation stay unset.
func assignSubscription(items []SpendItem, reports []WeeklyReport, accounts map[string]SpendAccount, window [2]time.Time, now time.Time) {
	fees := monthlyFees(reports, accounts)
	values := subscriptionValues(reports, fees, window, now)
	costs := map[[2]string]float64{}
	for _, item := range items {
		costs[[2]string{item.KeyID, item.AccountID}] += item.CostUSD
	}
	for i := range items {
		item := &items[i]
		accountValues, measured := values[item.AccountID]
		cost := costs[[2]string{item.KeyID, item.AccountID}]
		if !measured || cost <= 0 {
			continue
		}
		value := accountValues[item.KeyID] * item.CostUSD / cost
		item.SubscriptionUSD = &value
	}
}
