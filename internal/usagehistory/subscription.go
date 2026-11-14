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
	// SubscriptionUSD is the row's part of what its account's plan costs in the
	// reporting period the row falls in; absent when the fee or the row's API value
	// is unknown.
	SubscriptionUSD *float64 `json:"subscription_usd,omitempty"`
}

// ValuedUsage is the usage rows and quota windows of one date range, both priced.
type ValuedUsage struct {
	Rows    []ValuedRow
	Windows []WeeklyReport
}

// accountPeriod is one account's usage in the reporting period that starts at Start.
type accountPeriod struct {
	AccountID string
	Start     time.Time
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

// subscriptionMonthlyFee is an account's monthly fee while a recorded plan was in
// effect: the credential's explicit fee, else the public price of the recorded
// plan, else of the account's current plan.
func subscriptionMonthlyFee(account AccountSubscription, recordedProvider, recordedPlan string) (float64, bool) {
	if account.MonthlyFeeUSD != nil {
		return *account.MonthlyFeeUSD, true
	}
	provider := account.Provider
	if provider == "" {
		provider = recordedProvider
	}
	if fee, known := MonthlyPlanFee(provider, recordedPlan); known {
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

// periodPlanFee is what an account's plan costs over a whole reporting period.
// windows are the account's quota windows of known fee, ordered by start; each one
// carries the plan in effect from its start until the next window starts, so a plan
// change re-prices only the time after it. An account without such windows uses its
// current fee.
func periodPlanFee(windows []WeeklyReport, account AccountSubscription, period [2]time.Time) (float64, bool) {
	if len(windows) == 0 {
		monthly, known := subscriptionMonthlyFee(account, "", "")
		if !known {
			return 0, false
		}
		return proratedPlanFee(monthly, period), true
	}
	startsLater := func(i int) bool { return windows[i].Start.After(period[0]) }
	later := sort.Search(len(windows), startsLater)
	// The window in effect when the period starts; the earliest one when none had started.
	current := max(0, later-1)
	total := 0.0
	cursor := period[0]
	for cursor.Before(period[1]) {
		until := period[1]
		if current+1 < len(windows) && windows[current+1].Start.Before(until) {
			until = windows[current+1].Start
		}
		if until.After(cursor) {
			segment := [2]time.Time{cursor, until}
			total += proratedPlanFee(*windows[current].MonthlyFeeUSD, segment)
			cursor = until
		}
		current++
	}
	return total, true
}

// subscriptionRowValues is the subscription value of every row, by index; nil when
// the account's fee or the row's API value is unknown.
// The same usage is priced at what the plans cost: each account's fee for a whole
// reporting period (week or month) is split across the account's rows in that
// period by API value. A running period already carries its whole fee.
// reports carry MonthlyFeeUSD where the fee is known.
func subscriptionRowValues(rows []Row, reports []WeeklyReport, accounts map[string]AccountSubscription, period string) []*float64 {
	windows := map[string][]WeeklyReport{}
	for _, report := range reports {
		if report.MonthlyFeeUSD == nil {
			continue
		}
		windows[report.AccountID] = append(windows[report.AccountID], report)
	}
	for _, accountWindows := range windows {
		byStart := func(i, j int) bool { return accountWindows[i].Start.Before(accountWindows[j].Start) }
		sort.SliceStable(accountWindows, byStart)
	}
	totals := map[accountPeriod]float64{}
	spans := map[accountPeriod][2]time.Time{}
	keys := make([]accountPeriod, len(rows))
	for index, row := range rows {
		if row.CostUSD == nil {
			continue
		}
		at := rowInstant(row)
		span := reportingPeriod(period, at)
		key := accountPeriod{AccountID: row.AccountID, Start: span[0]}
		keys[index] = key
		spans[key] = span
		totals[key] += *row.CostUSD
	}
	fees := map[accountPeriod]float64{}
	for key, span := range spans {
		fee, known := periodPlanFee(windows[key.AccountID], accounts[key.AccountID], span)
		if known {
			fees[key] = fee
		}
	}
	values := make([]*float64, len(rows))
	for index, row := range rows {
		if row.CostUSD == nil {
			continue
		}
		key := keys[index]
		fee, known := fees[key]
		total := totals[key]
		if !known || total <= 0 {
			continue
		}
		value := fee * *row.CostUSD / total
		values[index] = &value
	}
	return values
}

// ValuedUsage returns the rows and quota windows of the inclusive UTC date range,
// with rows valued within the reporting periods ("week" or "month") they fall in.
// accounts carries live credential metadata keyed by account fingerprint.
func (s *Store) ValuedUsage(from, to, period string, accounts map[string]AccountSubscription) (ValuedUsage, error) {
	// The padded range holds every row of each reporting period that touches the
	// requested one, so a row's value does not depend on where the range ends.
	before, after := paddedDates(from, to)
	rows, err := s.Snapshot(before, after)
	if err != nil {
		return ValuedUsage{}, err
	}
	reports, errReports := s.reports(rows, before, after)
	if errReports != nil {
		return ValuedUsage{}, errReports
	}
	valued := ValuedUsage{Rows: []ValuedRow{}, Windows: []WeeklyReport{}}
	for index := range reports {
		account := accounts[reports[index].AccountID]
		fee, known := subscriptionMonthlyFee(account, reports[index].Provider, reports[index].Plan)
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
	values := subscriptionRowValues(rows, reports, accounts, period)
	for index, row := range rows {
		if row.Date < from || row.Date > to {
			continue
		}
		valuedRow := ValuedRow{Row: row, SubscriptionUSD: values[index]}
		valued.Rows = append(valued.Rows, valuedRow)
	}
	return valued, nil
}
