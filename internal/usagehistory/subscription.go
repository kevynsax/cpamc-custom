package usagehistory

import (
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

// capacityParts are account capacity slices that never belong to a key.
var capacityParts = map[string]bool{"unidentified": true, "unknown": true, "remaining": true, "expired": true}

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

// subscriptionShares is each key's share of every account's quota capacity in the
// window, using the Usage page rules: cycles are prorated by overlap, each
// scope/duration family weighs the same, and unknown capacity is never a key's.
func subscriptionShares(reports []WeeklyReport, window [2]time.Time, now time.Time) map[string]map[string]float64 {
	limit := window[1]
	if now.Before(limit) {
		limit = now
	}
	families := map[string]map[cycleFamily][]WeeklyReport{}
	for _, report := range reports {
		if !report.Start.Before(limit) || !report.Reset.After(window[0]) {
			continue
		}
		if families[report.AccountID] == nil {
			families[report.AccountID] = map[cycleFamily][]WeeklyReport{}
		}
		family := cycleFamily{Scope: report.Scope, Duration: report.Reset.Sub(report.Start)}
		families[report.AccountID][family] = append(families[report.AccountID][family], report)
	}
	shares := map[string]map[string]float64{}
	for account, accountFamilies := range families {
		normalized := map[string]float64{}
		for _, cycles := range accountFamilies {
			budget := 0.0
			slices := map[string]float64{}
			for _, cycle := range cycles {
				start := cycle.Start
				if window[0].After(start) {
					start = window[0]
				}
				end := cycle.Reset
				if window[1].Before(end) {
					end = window[1]
				}
				overlap := end.Sub(start).Seconds() / cycle.Reset.Sub(cycle.Start).Seconds()
				if overlap <= 0 {
					continue
				}
				budget += overlap
				percents := 0.0
				if cycle.AllocationAvailable {
					for _, person := range cycle.People {
						percents += person.Percent
					}
				}
				scale := 1.0
				if percents > cycle.Used && percents > 0 {
					scale = cycle.Used / percents
				}
				if cycle.AllocationAvailable {
					for _, person := range cycle.People {
						slices[person.KeyID] += overlap * person.Percent * scale / 100
					}
				}
				slices["unidentified"] += overlap * max(0, cycle.Used-min(cycle.Used, percents)) / 100
				switch {
				case !cycle.Closed:
					slices["remaining"] += overlap * (100 - cycle.Used) / 100
				case cycle.UnusedUpperBound == nil:
					slices["unknown"] += overlap * (100 - cycle.Used) / 100
				default:
					slices["expired"] += overlap * *cycle.UnusedUpperBound / 100
				}
			}
			if budget <= 0 {
				continue
			}
			for key, value := range slices {
				normalized[key] += value / budget / float64(len(accountFamilies))
			}
		}
		total := 0.0
		for _, value := range normalized {
			total += value
		}
		if total <= 0 {
			continue
		}
		shares[account] = map[string]float64{}
		for key, value := range normalized {
			if !capacityParts[key] {
				shares[account][key] = value / total
			}
		}
	}
	return shares
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

// assignSubscription gives every item its part of the key's subscription value on
// that account: the prorated plan fee times the key's quota share, split across
// the key's items by API value. Unknown fees, shares or API values stay unset.
func assignSubscription(items []SpendItem, reports []WeeklyReport, accounts map[string]SpendAccount, window [2]time.Time, now time.Time) {
	shares := subscriptionShares(reports, window, now)
	plans := windowPlans(reports)
	costs := map[[2]string]float64{}
	for _, item := range items {
		costs[[2]string{item.KeyID, item.AccountID}] += item.CostUSD
	}
	for i := range items {
		item := &items[i]
		account := accounts[item.AccountID]
		accountShares, measured := shares[item.AccountID]
		cost := costs[[2]string{item.KeyID, item.AccountID}]
		if !measured || cost <= 0 {
			continue
		}
		plan := account.Plan
		if plan == "" {
			plan = plans[item.AccountID]
		}
		monthly, known := MonthlyPlanFee(item.Provider, plan)
		if account.MonthlyFeeUSD != nil {
			monthly, known = *account.MonthlyFeeUSD, true
		}
		if !known {
			continue
		}
		value := proratedPlanFee(monthly, window) * accountShares[item.KeyID] * item.CostUSD / cost
		item.SubscriptionUSD = &value
	}
}
