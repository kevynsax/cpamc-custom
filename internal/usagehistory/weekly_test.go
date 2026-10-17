package usagehistory

import (
	"testing"
	"time"
)

func TestWeeklyQuotaAttributionAndResetBoundaries(t *testing.T) {
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	cycle := Cycle{ID: "week", AccountID: "account", Scope: "account", Start: start, Reset: start.Add(Week), Samples: []Sample{{At: start, Used: 10}, {At: start.Add(10 * time.Minute), Used: 30}}}
	rows := []Row{
		{At: start.Add(5 * time.Minute), AccountID: "account", KeyID: "a", CostUSD: ptr(3), Requests: 1},
		{At: start.Add(6 * time.Minute), AccountID: "account", KeyID: "b", CostUSD: ptr(1), Requests: 1},
		{At: cycle.Reset, AccountID: "account", KeyID: "other-week", CostUSD: ptr(100), Requests: 1},
	}
	report := BuildWeek(cycle, rows, start.Add(10*time.Minute))
	if report.Used != 30 || report.Unattributed != 10 || report.Remaining != 70 || len(report.People) != 2 || report.People[0].Percent != 15 || report.People[1].Percent != 5 || !report.AllocationAvailable {
		t.Fatalf("report=%+v", report)
	}
	if report.Closed || report.UnusedUpperBound != nil {
		t.Fatal("current remainder reported as wasted")
	}
	rows[1].UnpricedRequests = 1
	report = BuildWeek(cycle, rows, start.Add(10*time.Minute))
	if report.Unattributed != 30 || report.AllocationAvailable {
		t.Fatal("unpriced interval attributed as if complete")
	}
}

func TestWeeklyLeftoverRequiresClosingObservation(t *testing.T) {
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	cycle := Cycle{ID: "week", AccountID: "account", Scope: "account", Start: start, Reset: start.Add(Week)}
	cycle.Samples = []Sample{{At: cycle.Reset.Add(-5 * time.Minute), Used: 65}}
	report := BuildWeek(cycle, nil, cycle.Reset.Add(time.Minute))
	if report.UnusedUpperBound == nil || *report.UnusedUpperBound != 35 || !report.Closed || !report.Incomplete {
		t.Fatalf("wrong closing estimate: %+v", report)
	}
	cycle.Samples[0].At = cycle.Reset.Add(-time.Hour)
	report = BuildWeek(cycle, nil, cycle.Reset.Add(time.Minute))
	if report.UnusedUpperBound != nil {
		t.Fatal("stale observation fabricated final unused quota")
	}
}

func TestWeeklyGapsAndCounterCorrectionsRemainUnattributed(t *testing.T) {
	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	cycle := Cycle{AccountID: "account", Scope: "account", Start: start, Reset: start.Add(Week), Samples: []Sample{{At: start, Used: 10}, {At: start.Add(time.Hour), Used: 40}}}
	rows := []Row{{At: start.Add(5 * time.Minute), AccountID: "account", KeyID: "a", CostUSD: ptr(10)}}
	report := BuildWeek(cycle, rows, start.Add(time.Hour))
	if report.Unattributed != 40 || !report.Incomplete || report.AllocationAvailable {
		t.Fatalf("gap attributed: %+v", report)
	}
	cycle.Samples = []Sample{{At: start, Used: 10}, {At: start.Add(10 * time.Minute), Used: 80}, {At: start.Add(20 * time.Minute), Used: 20}}
	report = BuildWeek(cycle, rows, start.Add(20*time.Minute))
	if report.Unattributed != 20 || report.People[0].Percent != 0 || report.AllocationAvailable {
		t.Fatalf("counter correction fabricated shares: %+v", report)
	}
}

func TestQuotaHistoryPersistence(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	store, err := Open(dir, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if errObserve := store.ObserveQuota("secret-account", "claude", "account", "Claude Max", now.Add(24*time.Hour), now, 40); errObserve != nil {
		t.Fatal(errObserve)
	}
	reopened, errOpen := Open(dir, func() time.Time { return now })
	if errOpen != nil {
		t.Fatal(errOpen)
	}
	reports, errWeeks := reopened.Weeks()
	if errWeeks != nil {
		t.Fatal(errWeeks)
	}
	if len(reports) != 1 || reports[0].AccountID != Fingerprint("secret-account") || reports[0].Remaining != 60 {
		t.Fatalf("reports=%+v", reports)
	}
}

func TestProviderWeeklyQuotaContracts(t *testing.T) {
	reset := "2026-06-15T12:00:00Z"
	for _, test := range []struct {
		provider, body string
		want           int
	}{
		{"claude", `{"five_hour":{"utilization":90,"resets_at":"` + reset + `"},"seven_day":{"utilization":25,"resets_at":"` + reset + `"}}`, 1},
		{"claude", `{"five_hour":{"utilization":90,"resets_at":"` + reset + `"},"seven_day_opus":{"utilization":25,"resets_at":"` + reset + `"}}`, 0},
		{"codex", `{"rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":300,"reset_at":1781524800},"secondary_window":{"used_percent":25,"limit_window_seconds":604800,"reset_at":1781524800}}}`, 1},
		{"xai", `{"config":{"currentPeriod":{"type":"monthly","end":"` + reset + `"},"creditUsagePercent":25}}`, 0},
		{"xai", `{"config":{"currentPeriod":{"type":"weekly","end":"` + reset + `"},"creditUsagePercent":25}}`, 1},
		{"zai", `{"data":{"limits":[{"unit":6,"number":1,"type":"TOKENS_LIMIT","usage":100,"currentValue":25,"nextResetTime":1781524800000}]}}`, 1},
		{"meta", `{"subs_usage":{"weekly":{"used_percent":25,"resets_at":1781524800}}}`, 1},
		{"minimax", `{"model_remains":[{"model_name":"model","current_weekly_remaining_percent":75,"weekly_end_time":1781524800000}]}`, 1},
		{"antigravity", `{"quotaInfo":{"remainingFraction":0.75,"resetTime":"` + reset + `"}}`, 0},
	} {
		t.Run(test.provider+test.body, func(t *testing.T) {
			windows, _ := ParseWeeklyQuota(test.provider, []byte(test.body))
			if len(windows) != test.want {
				t.Fatalf("windows=%+v", windows)
			}
			for _, w := range windows {
				if w.Used != 25 {
					t.Fatalf("wrong percent: %+v", w)
				}
			}
		})
	}
}
