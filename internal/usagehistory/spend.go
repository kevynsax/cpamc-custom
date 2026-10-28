package usagehistory

import (
	"sort"
	"strings"
	"time"
)

// SpendItem is one key's API value on one account and model during a period.
type SpendItem struct {
	KeyID     string  `json:"key_id"`
	AccountID string  `json:"account_id"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	Mode      string  `json:"mode,omitempty"`
	CostUSD   float64 `json:"cost_usd"`
	Unpriced  int64   `json:"unpriced"`
}

// SpendMaxOffset bounds how far back a week or month can be requested.
func SpendMaxOffset(period string) int {
	if period == "month" {
		return 11
	}
	return 50
}

// Spend sums API value per key, account and model for one week or month, using
// the same rules as the Usage page: weeks start Thursday at 20:00 UTC-3,
// months are UTC calendar months, and in-house rows use reference prices.
// providers optionally overrides the provider recorded on an account's rows.
func (s *Store) Spend(period string, offset int, providers map[string]string) ([]SpendItem, error) {
	now := s.now().UTC()
	from, to := spendDates(period, offset, now)
	rows, err := s.Snapshot(from, to)
	if err != nil {
		return nil, err
	}
	window := spendWindow(period, offset, now)
	end := window[1]
	if limit := now.Add(time.Millisecond); end.After(limit) {
		end = limit
	}
	totals := map[string]*SpendItem{}
	for _, row := range WithInHouseReferenceEstimates(rows) {
		at := row.At
		if at.IsZero() {
			at, _ = time.Parse(DateLayout, row.Date)
		}
		if at.Before(window[0]) || !at.Before(end) {
			continue
		}
		provider := row.Provider
		if override := providers[row.AccountID]; override != "" {
			provider = override
		}
		model := row.Model
		if alias := strings.TrimSpace(row.ModelAlias); alias != "" {
			model = alias
		}
		key := strings.Join([]string{row.KeyID, row.AccountID, provider, model, row.Mode}, "\x00")
		item := totals[key]
		if item == nil {
			item = &SpendItem{KeyID: row.KeyID, AccountID: row.AccountID, Provider: provider, Model: model, Mode: row.Mode}
			totals[key] = item
		}
		cost, unpriced := spendValue(row)
		item.CostUSD += cost
		item.Unpriced += unpriced
	}
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]SpendItem, 0, len(keys))
	for _, key := range keys {
		items = append(items, *totals[key])
	}
	return items, nil
}

// spendValue matches the Usage page: an estimated reference cost is priced, a row
// without any cost counts every request as unpriced.
func spendValue(row ReferenceRow) (float64, int64) {
	if row.CostUSD != nil {
		return *row.CostUSD, row.UnpricedRequests
	}
	if row.ReferenceCostUSD != nil {
		return *row.ReferenceCostUSD, 0
	}
	return 0, row.Requests
}

// spendWindow uses fixed reporting boundaries, never provider quota-reset times.
func spendWindow(period string, offset int, now time.Time) [2]time.Time {
	now = now.UTC()
	if period == "month" {
		return [2]time.Time{monthStart(now, offset), monthStart(now, offset-1)}
	}
	daysSinceThursday := (int(now.Weekday()) - int(time.Thursday) + 7) % 7
	start := time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, time.UTC).AddDate(0, 0, -daysSinceThursday)
	if start.After(now) {
		start = start.Add(-Week)
	}
	start = start.Add(-time.Duration(offset) * Week)
	return [2]time.Time{start, start.Add(Week)}
}

func monthStart(now time.Time, offset int) time.Time {
	return time.Date(now.Year(), now.Month()-time.Month(offset), 1, 0, 0, 0, 0, time.UTC)
}

// spendDates is the inclusive UTC date window covering the reporting period.
func spendDates(period string, offset int, now time.Time) (string, string) {
	window := spendWindow(period, offset, now)
	end := window[1].Add(-time.Nanosecond)
	if end.After(now) {
		end = now.UTC()
	}
	return window[0].Format(DateLayout), end.Format(DateLayout)
}
